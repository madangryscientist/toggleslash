"""One HTTP request with a bound on the whole of it, and the reader of a stream.

`http.client` bounds each socket call, not a request, so a server that trickles a body would hold a thread
for as long as it likes. Each request here has an `Aborter`: a timer that shuts the socket down at the
deadline, which unblocks whatever the thread is reading, and which `close` uses the same way.
"""

from __future__ import annotations

import http.client
import socket
import threading
from collections.abc import Callable
from typing import Protocol
from urllib.parse import urlsplit

from ._types import NETWORK, TIMEOUT, ToggleSlashError

RULESET_ROUTE = "/sdk/v1/ruleset"
STREAM_ROUTE = "/sdk/v1/stream"
EVENTS_ROUTE = "/sdk/v1/events"

_CHUNK = 65536


class TransportError(ToggleSlashError):
    """A request failed. `code` is the contract's `timeout` or `network`."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class Aborted(Exception):
    """The request was abandoned by `close`."""


class Aborter:
    """Ends one request from another thread: at a deadline (`timeout`) or because the client closed."""

    def __init__(self, timeout: float) -> None:
        self.timeout = timeout
        self._lock = threading.Lock()
        self._sock: socket.socket | None = None
        self.timed_out = False
        self.aborted = False
        self._timer: threading.Timer | None = threading.Timer(timeout, self.abort, kwargs={"timed_out": True})
        self._timer.daemon = True
        self._timer.start()

    def attach(self, sock: socket.socket | None) -> None:
        with self._lock:
            self._sock = sock
            if self.timed_out or self.aborted:
                _shut(sock)

    def abort(self, *, timed_out: bool = False) -> None:
        with self._lock:
            if timed_out:
                self.timed_out = True
            else:
                self.aborted = True
            _shut(self._sock)

    def stop_timer(self) -> None:
        """The request has got what the deadline was for (the stream's headers); a quiet stream is the
        watchdog's business from here."""
        timer, self._timer = self._timer, None
        if timer is not None:
            timer.cancel()

    def check(self) -> None:
        if self.aborted:
            raise Aborted
        if self.timed_out:
            raise TransportError(TIMEOUT, "the request exceeded its time limit")


def _shut(sock: socket.socket | None) -> None:
    if sock is None:
        return
    try:
        sock.shutdown(socket.SHUT_RDWR)
    except OSError:
        pass


class Reply(Protocol):
    """What the sync and usage code need of an answer. Tests supply their own."""

    status: int
    no_body: bool

    def header(self, name: str) -> str | None: ...
    def read_body(self) -> bytes: ...
    def read_chunk(self, watchdog: float) -> bytes: ...
    def close(self) -> None: ...


Transport = Callable[[str, str, dict[str, str], bytes | None, Aborter], Reply]
"""`(method, route, headers, body, aborter) -> Reply`. It raises `TransportError` or `Aborted`."""


class _HttpReply:
    def __init__(
        self,
        connection: http.client.HTTPConnection,
        sock: socket.socket,
        response: http.client.HTTPResponse,
        aborter: Aborter,
    ) -> None:
        self._connection = connection
        # `getresponse` lets go of `connection.sock` for an answer that ends with the connection, so the
        # socket the watchdog sets its timeout on is kept here.
        self._sock = sock
        self._response = response
        self._aborter = aborter
        self.status = response.status
        self.no_body = response.length == 0

    def header(self, name: str) -> str | None:
        return self._response.getheader(name)

    def read_body(self) -> bytes:
        try:
            data = self._response.read()
        except TimeoutError as error:
            self._aborter.check()
            raise TransportError(TIMEOUT, "the body did not arrive in time") from error
        except (OSError, http.client.HTTPException) as error:
            self._aborter.check()
            raise TransportError(NETWORK, "the connection failed while reading the body") from error
        self._aborter.check()
        return data

    def read_chunk(self, watchdog: float) -> bytes:
        """The next bytes of a stream, or `b""` at its end. No byte for `watchdog` seconds is a timeout."""
        try:
            self._sock.settimeout(watchdog)
            data = self._response.read1(_CHUNK)
        except TimeoutError as error:
            self._aborter.check()
            raise TransportError(TIMEOUT, "the stream went quiet") from error
        except (OSError, http.client.HTTPException) as error:
            self._aborter.check()
            raise TransportError(NETWORK, "the connection failed while reading the stream") from error
        self._aborter.check()
        return data

    def close(self) -> None:
        self._aborter.stop_timer()
        for closing in (self._response, self._connection, self._sock):
            try:
                closing.close()
            except OSError:
                pass


class HttpTransport:
    """Requests against one base URL, a connection each, closed after the answer."""

    def __init__(self, base_url: str) -> None:
        self._base = base_url
        try:
            parts = urlsplit(base_url)
            self._https = parts.scheme == "https"
            if parts.scheme not in ("http", "https") or not parts.hostname:
                raise ValueError("not an http(s) URL")
            self._host = parts.hostname
            self._port = parts.port
            self._prefix = parts.path.rstrip("/")
            self._error: str | None = None
        except ValueError:
            self._https = False
            self._host = ""
            self._port = None
            self._prefix = ""
            self._error = "the url cannot be used"

    def __call__(self, method: str, route: str, headers: dict[str, str], body: bytes | None, aborter: Aborter) -> Reply:
        if self._error is not None:
            raise TransportError(NETWORK, self._error)
        factory = http.client.HTTPSConnection if self._https else http.client.HTTPConnection
        connection = factory(self._host, self._port, timeout=aborter.timeout)
        try:
            connection.connect()
            sock = connection.sock
            assert sock is not None
            aborter.attach(sock)
            aborter.check()
            connection.request(method, self._prefix + route, body=body, headers=headers)
            aborter.check()
            response = connection.getresponse()
            aborter.check()
        except BaseException as error:
            try:
                connection.close()
            except OSError:
                pass
            if isinstance(error, (Aborted, TransportError)):
                raise
            if isinstance(error, TimeoutError):
                aborter.check()
                raise TransportError(TIMEOUT, "the request timed out") from error
            if isinstance(error, (OSError, http.client.HTTPException, ValueError)):
                aborter.check()
                raise TransportError(NETWORK, "the request failed") from error
            raise
        return _HttpReply(connection, sock, response, aborter)
