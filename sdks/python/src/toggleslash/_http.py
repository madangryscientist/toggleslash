"""One HTTP request at a time over ``http.client``, with the whole request bounded (contract 7.1, 7.2).

``http.client``'s own timeout is per socket operation, so a server that trickles a body would never
trip it. Each request here starts a daemon ``threading.Timer`` that shuts down the socket the SDK
itself holds, which makes a blocked read return, and a request whose timer fired is a timeout. The
SDK keeps its own reference to the socket because ``http.client`` sets ``conn.sock`` to ``None``
after ``getresponse()`` for an answer that ends with the connection.
"""

from __future__ import annotations

import http.client
import socket
import ssl
import threading
import time
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlsplit

STREAM_IDLE_SECONDS = 60.0


class RequestTimeout(Exception):
    """The request ran past its bound, or the stream went quiet."""


class NetworkFailure(Exception):
    """The connection failed, or anything else went wrong with the request."""


class Aborted(Exception):
    """``close`` abandoned the request."""


@dataclass(frozen=True, slots=True)
class Response:
    status: int
    headers: Any  # an ``email.message.Message``: ``headers.get("ETag")``
    body: bytes


class Aborts:
    """The sockets of the requests that ``close`` may abandon (the ruleset and the stream)."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._sockets: set[socket.socket] = set()
        self.closed = False

    def add(self, sock: socket.socket) -> bool:
        """Register ``sock``; False when the registry is already closed (the caller gives up)."""
        with self._lock:
            if self.closed:
                return False
            self._sockets.add(sock)
            return True

    def remove(self, sock: socket.socket) -> None:
        with self._lock:
            self._sockets.discard(sock)

    def abort_all(self) -> None:
        with self._lock:
            self.closed = True
            sockets = list(self._sockets)
        for sock in sockets:
            _shutdown(sock)


def _shutdown(sock: socket.socket) -> None:
    try:
        sock.shutdown(socket.SHUT_RDWR)
    except (OSError, ValueError):
        pass


class Endpoint:
    """A parsed base URL: scheme, host, port and a path prefix."""

    def __init__(self, url: str) -> None:
        self.valid = False
        self.https = False
        self.host = ""
        self.port = 80
        self.prefix = ""
        try:
            parts = urlsplit(url)
            host = parts.hostname
            port = parts.port
        except ValueError:
            return
        if parts.scheme not in ("http", "https") or not host:
            return
        self.https = parts.scheme == "https"
        self.host = host
        self.port = port if port is not None else (443 if self.https else 80)
        self.prefix = parts.path
        self.valid = True


class Transport:
    """The default HTTP client. A test replaces it with an object of the same two methods."""

    def __init__(self, ssl_context: ssl.SSLContext | None = None) -> None:
        self._ssl_context = ssl_context

    def _connect(
        self, endpoint: Endpoint, timeout: float
    ) -> tuple[http.client.HTTPConnection, socket.socket]:
        if endpoint.https:
            context = self._ssl_context or ssl.create_default_context()
            conn: http.client.HTTPConnection = http.client.HTTPSConnection(
                endpoint.host, endpoint.port, timeout=timeout, context=context
            )
        else:
            conn = http.client.HTTPConnection(endpoint.host, endpoint.port, timeout=timeout)
        try:
            conn.connect()
        except BaseException:
            conn.close()
            raise
        sock = conn.sock
        if sock is None:
            raise OSError("no socket")
        return conn, sock

    def fetch(
        self,
        endpoint: Endpoint,
        method: str,
        path: str,
        headers: dict[str, str],
        body: bytes | None,
        timeout: float,
        aborts: Aborts | None,
    ) -> Response:
        """One whole request, body read, in ``timeout`` seconds. Raises a ``RequestTimeout``,
        ``Aborted`` or ``NetworkFailure`` and nothing else."""
        if not endpoint.valid:
            raise NetworkFailure("unusable url")
        deadline = time.monotonic() + timeout
        timed_out = threading.Event()
        conn: http.client.HTTPConnection | None = None
        sock: socket.socket | None = None
        timer: threading.Timer | None = None
        response: http.client.HTTPResponse | None = None
        try:
            conn, sock = self._connect(endpoint, max(timeout, 0.001))
            if aborts is not None and not aborts.add(sock):
                raise Aborted
            held = sock

            def expire() -> None:
                timed_out.set()
                _shutdown(held)

            timer = threading.Timer(max(deadline - time.monotonic(), 0.0), expire)
            timer.daemon = True
            timer.start()
            sock.settimeout(max(deadline - time.monotonic(), 0.001))
            conn.request(method, endpoint.prefix + path, body=body, headers=headers)
            response = conn.getresponse()
            data = response.read()
            if timed_out.is_set():
                raise RequestTimeout
            return Response(response.status, response.headers, data)
        except (Aborted, RequestTimeout):
            raise
        except BaseException as error:
            if not isinstance(error, Exception):
                raise
            if timed_out.is_set() or isinstance(error, TimeoutError):
                raise RequestTimeout from None
            if aborts is not None and aborts.closed:
                raise Aborted from None
            raise NetworkFailure(type(error).__name__) from None
        finally:
            if timer is not None:
                timer.cancel()
            if aborts is not None and sock is not None:
                aborts.remove(sock)
            _close(response, conn, sock)

    def open_stream(
        self,
        endpoint: Endpoint,
        path: str,
        headers: dict[str, str],
        open_timeout: float,
        idle_timeout: float,
        aborts: Aborts,
    ) -> Stream:
        """Open ``GET path``: bounded by ``open_timeout`` until the headers arrive; after that every
        read waits at most ``idle_timeout`` for a byte. The caller closes the stream."""
        if not endpoint.valid:
            raise NetworkFailure("unusable url")
        timed_out = threading.Event()
        conn: http.client.HTTPConnection | None = None
        sock: socket.socket | None = None
        timer: threading.Timer | None = None
        response: http.client.HTTPResponse | None = None
        try:
            deadline = time.monotonic() + open_timeout
            conn, sock = self._connect(endpoint, max(open_timeout, 0.001))
            if not aborts.add(sock):
                raise Aborted
            held = sock

            def expire() -> None:
                timed_out.set()
                _shutdown(held)

            timer = threading.Timer(max(deadline - time.monotonic(), 0.0), expire)
            timer.daemon = True
            timer.start()
            sock.settimeout(max(deadline - time.monotonic(), 0.001))
            conn.request("GET", endpoint.prefix + path, headers=headers)
            response = conn.getresponse()
            timer.cancel()
            if timed_out.is_set():
                raise RequestTimeout
            sock.settimeout(idle_timeout)
            return Stream(response, conn, sock, aborts)
        except (Aborted, RequestTimeout):
            self._abandon(timer, aborts, sock, response, conn)
            raise
        except BaseException as error:
            self._abandon(timer, aborts, sock, response, conn)
            if not isinstance(error, Exception):
                raise
            if timed_out.is_set() or isinstance(error, TimeoutError):
                raise RequestTimeout from None
            if aborts.closed:
                raise Aborted from None
            raise NetworkFailure(type(error).__name__) from None

    @staticmethod
    def _abandon(
        timer: threading.Timer | None,
        aborts: Aborts,
        sock: socket.socket | None,
        response: http.client.HTTPResponse | None,
        conn: http.client.HTTPConnection | None,
    ) -> None:
        if timer is not None:
            timer.cancel()
        if sock is not None:
            aborts.remove(sock)
        _close(response, conn, sock)


class Stream:
    """An open response being read in pieces."""

    def __init__(
        self,
        response: http.client.HTTPResponse,
        conn: http.client.HTTPConnection,
        sock: socket.socket,
        aborts: Aborts,
    ) -> None:
        self._response = response
        self._conn = conn
        self._sock = sock
        self._aborts = aborts
        self.status = response.status
        self.headers = response.headers
        # ``length`` is 0 for ``Content-Length: 0`` and None for a chunked or read-until-close body.
        self.bodyless = response.length == 0

    def read(self) -> bytes:
        """The next bytes (at least one), or ``b""`` when the server ended the stream."""
        try:
            return self._response.read1(65536)
        except BaseException as error:
            if not isinstance(error, Exception):
                raise
            if self._aborts.closed:
                raise Aborted from None
            if isinstance(error, TimeoutError):
                raise RequestTimeout from None
            raise NetworkFailure(type(error).__name__) from None

    def close(self) -> None:
        self._aborts.remove(self._sock)
        _close(self._response, self._conn, self._sock)


def _close(
    response: http.client.HTTPResponse | None,
    conn: http.client.HTTPConnection | None,
    sock: socket.socket | None,
) -> None:
    for closer in (response, sock, conn):
        if closer is None:
            continue
        try:
            closer.close()
        except Exception:
            pass
