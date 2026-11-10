"""HTTP over sockets the SDK opens itself, so one deadline covers the whole request, body included.

Every request gets a guard: a timer that, when the request has run out of time, shuts the socket down from another
thread, which wakes any blocked read in the thread that owns the request. Closing a client aborts the guards of its
sync group the same way. The owner checks the guard's flags before it trusts what a read returned, because a shut
down socket reads as the end of the stream.

The one thing a guard cannot bound is a name lookup, which Python cannot interrupt; the callers join their threads
with a timeout, and the threads are daemons.
"""

from __future__ import annotations

import contextlib
import http.client
import socket
import ssl
import threading
import time
from urllib.parse import urlsplit


class HttpError(Exception):
    """A request failed. `code` is `timeout`, `network` or `aborted` (the client was closed)."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class AbortGroup:
    """The requests that `abort` ends together (a client's ruleset and stream requests)."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._guards: set[_Guard] = set()
        self._aborted = False

    def _add(self, guard: _Guard) -> None:
        with self._lock:
            self._guards.add(guard)
            aborted = self._aborted
        if aborted:
            guard.kill("aborted")

    def _remove(self, guard: _Guard) -> None:
        with self._lock:
            self._guards.discard(guard)

    def abort(self) -> None:
        with self._lock:
            self._aborted = True
            guards = list(self._guards)
        for guard in guards:
            guard.kill("aborted")


class _Guard:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._sock: socket.socket | None = None
        self._done = False
        self.timed_out = False
        self.aborted = False
        self.timer: threading.Timer | None = None

    def attach(self, sock: socket.socket) -> None:
        with self._lock:
            self._sock = sock
            killed = self.timed_out or self.aborted
        if killed:
            self._shutdown()

    def kill(self, reason: str) -> None:
        with self._lock:
            if self._done:
                return
            if reason == "aborted":
                self.aborted = True
            else:
                self.timed_out = True
        self._shutdown()

    def _shutdown(self) -> None:
        with self._lock:
            sock = self._sock
            if sock is None or self._done:
                return
            with contextlib.suppress(OSError):
                sock.shutdown(socket.SHUT_RDWR)

    def finish(self) -> None:
        with self._lock:
            self._done = True
        timer = self.timer
        if timer is not None:
            timer.cancel()


def _connect(host: str, port: int, deadline: float, guard: _Guard) -> socket.socket:
    last: Exception | None = None
    for family, kind, proto, _name, address in socket.getaddrinfo(host, port, type=socket.SOCK_STREAM):
        remaining = deadline - time.monotonic()
        if remaining <= 0 or guard.timed_out or guard.aborted:
            break
        sock = socket.socket(family, kind, proto)
        try:
            sock.settimeout(remaining)
            sock.connect(address)
            return sock
        except OSError as error:
            last = error
            sock.close()
    raise last if last is not None else TimeoutError("connect")


class Response:
    """An open response. `close` ends the request; the owner always calls it."""

    def __init__(
        self,
        guard: _Guard,
        group: AbortGroup,
        conn: http.client.HTTPConnection,
        resp: http.client.HTTPResponse,
        sock: socket.socket,
    ) -> None:
        self._guard = guard
        self._group = group
        self._conn = conn
        self._resp = resp
        # Kept on purpose: for an answer that ends with the connection `http.client` sets `conn.sock` to None
        # after `getresponse()`, and a `settimeout` through the connection would then go nowhere.
        self._sock = sock
        self.status: int = resp.status

    def header(self, name: str) -> str | None:
        return self._resp.getheader(name)

    def _translate(self, error: BaseException | None) -> HttpError:
        if self._guard.aborted:
            return HttpError("aborted", "the client was closed")
        if self._guard.timed_out or isinstance(error, TimeoutError):
            return HttpError("timeout", "the request timed out")
        return HttpError("network", f"the connection failed ({type(error).__name__})")

    def read(self) -> bytes:
        """The whole body, within the request's deadline."""
        try:
            data = self._resp.read()
        except Exception as error:
            raise self._translate(error) from None
        if self._guard.aborted or self._guard.timed_out:
            raise self._translate(None)
        return data

    def read_chunk(self) -> bytes:
        """Whatever bytes arrive next (at most 64 KiB); empty at the end of the body."""
        try:
            data = self._resp.read1(65536)
        except Exception as error:
            raise self._translate(error) from None
        if self._guard.aborted or self._guard.timed_out:
            raise self._translate(None)
        return data

    def end_request_timer(self) -> None:
        """The request is open: stop its deadline (the stream then has the watchdog instead)."""
        timer = self._guard.timer
        if timer is not None:
            timer.cancel()

    def set_idle_timeout(self, seconds: float) -> None:
        """Every read now fails if it waits this long for a byte, and every byte restarts the wait."""
        self._sock.settimeout(seconds)

    def close(self) -> None:
        self._guard.finish()
        self._group._remove(self._guard)
        for closer in (self._resp.close, self._conn.close, self._sock.close):
            with contextlib.suppress(Exception):
                closer()

    def __enter__(self) -> Response:
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()


class Transport:
    """Opens requests against one base URL."""

    def __init__(self, base_url: str, ssl_context: ssl.SSLContext | None = None) -> None:
        self.base_url = base_url
        self._ssl_context = ssl_context
        self._parts = urlsplit(base_url)

    def open(
        self,
        method: str,
        path: str,
        headers: dict[str, str],
        body: bytes | None,
        timeout: float,
        group: AbortGroup,
    ) -> Response:
        guard = _Guard()
        guard.timer = threading.Timer(timeout, guard.kill, args=("timeout",))
        guard.timer.daemon = True
        group._add(guard)
        guard.timer.start()
        sock: socket.socket | None = None
        conn: http.client.HTTPConnection | None = None
        try:
            parts = self._parts
            if parts.scheme not in ("http", "https") or not parts.hostname:
                raise ValueError("the URL is not an http or https URL")
            secure = parts.scheme == "https"
            port = parts.port or (443 if secure else 80)
            deadline = time.monotonic() + timeout
            sock = _connect(parts.hostname, port, deadline, guard)
            guard.attach(sock)
            if secure:
                context = self._ssl_context or ssl.create_default_context()
                sock = context.wrap_socket(sock, server_hostname=parts.hostname)
                guard.attach(sock)
                conn = http.client.HTTPSConnection(parts.hostname, port, context=context)
            else:
                conn = http.client.HTTPConnection(parts.hostname, port)
            conn.sock = sock
            sock.settimeout(max(0.001, deadline - time.monotonic()))
            conn.request(method, parts.path.rstrip("/") + path, body=body, headers=headers)
            resp = conn.getresponse()
            return Response(guard, group, conn, resp, sock)
        except BaseException as error:
            guard.finish()
            group._remove(guard)
            for closer in (conn.close if conn else None, sock.close if sock else None):
                if closer is not None:
                    with contextlib.suppress(Exception):
                        closer()
            if guard.aborted:
                raise HttpError("aborted", "the client was closed") from None
            if guard.timed_out or isinstance(error, TimeoutError):
                raise HttpError("timeout", "the request timed out") from None
            if isinstance(error, Exception):
                raise HttpError("network", f"the connection failed ({type(error).__name__})") from None
            raise
