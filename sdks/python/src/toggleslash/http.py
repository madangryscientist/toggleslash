"""One HTTP request at a time, bounded as a whole, and abortable from another thread.

``http.client`` bounds one socket operation, not a request: a server that drips a byte a second never
times out. So every request here has a :class:`Guard` that owns its socket, a timer that expires it at
``request_timeout`` (reading the body included) and a way for ``close`` to abort it. A read blocked in
``recv`` is not woken by ``close`` from another thread, so the guard calls ``shutdown`` instead.
"""

from __future__ import annotations

import contextlib
import http.client
import ipaddress
import re
import socket
import threading
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from email.message import Message
from urllib.parse import urlsplit

from .clock import Clock


class RequestFailure(Exception):
    """A request that did not give an answer. ``code`` is ``timeout`` or ``network``."""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


class Aborted(Exception):
    """``close`` abandoned the request."""


@dataclass(frozen=True, slots=True)
class Answer:
    status: int
    headers: Message
    body: bytes


class Guard:
    """The state of one request: its socket, its deadline and why it was cut short, if it was."""

    def __init__(self, clock: Clock, timeout: float) -> None:
        self._clock = clock
        self._lock = threading.Lock()
        self._sock: socket.socket | None = None
        self._reason: str | None = None
        self.timeout = timeout
        self.deadline = clock.monotonic() + timeout

    def expire(self) -> None:
        self._cut("timeout")

    def abort(self) -> None:
        self._cut("abort")

    def _cut(self, reason: str) -> None:
        with self._lock:
            if self._reason is None:
                self._reason = reason
            sock = self._sock
        _shutdown(sock)

    def attach(self, sock: socket.socket) -> None:
        """Take over the socket, so ``expire`` and ``abort`` can reach it. Cut already: close it."""
        with self._lock:
            self._sock = sock
            cut = self._reason
        if cut is not None:
            _shutdown(sock)
            self.check()

    def socket(self) -> socket.socket | None:
        with self._lock:
            return self._sock

    def check(self) -> None:
        """Raise what ended the request, if something did."""
        with self._lock:
            reason = self._reason
        if reason == "abort":
            raise Aborted
        if reason == "timeout":
            raise RequestFailure("timeout")

    def remaining(self) -> float:
        self.check()
        left = self.deadline - self._clock.monotonic()
        if left <= 0:
            raise RequestFailure("timeout")
        return left


def _shutdown(sock: socket.socket | None) -> None:
    if sock is None:
        return
    with contextlib.suppress(OSError):
        sock.shutdown(socket.SHUT_RDWR)


class GuardSet:
    """The requests in flight that ``close`` may abandon. Once shut, a new one is abandoned at once."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._guards: set[Guard] = set()
        self._shut = False

    def add(self, guard: Guard) -> None:
        with self._lock:
            self._guards.add(guard)
            shut = self._shut
        if shut:
            guard.abort()

    def discard(self, guard: Guard) -> None:
        with self._lock:
            self._guards.discard(guard)

    def abort_all(self) -> None:
        with self._lock:
            self._shut = True
            guards = list(self._guards)
        for guard in guards:
            guard.abort()


_MAX_RETRY_AFTER = 300.0
_WHOLE_SECONDS = re.compile(r"[0-9]+")


def retry_after(status: int, headers: Message) -> float | None:
    """How long a ``429`` asks to wait, in seconds, capped at 5 minutes.

    ``None`` for any other answer, and for a header that is missing or is not whole decimal seconds
    (a date, a fraction, a negative number): the caller's usual backoff is then the only wait.
    """
    if status != 429:
        return None
    value = (headers.get("Retry-After") or "").strip(" \t")
    if _WHOLE_SECONDS.fullmatch(value) is None:
        return None
    if len(value) > 9:
        return _MAX_RETRY_AFTER
    return min(float(int(value)), _MAX_RETRY_AFTER)


_BASE = re.compile(r"^(https?)://([^/?#]*)(.*)$", re.IGNORECASE | re.DOTALL)


class Base:
    """The server's base URL, taken apart just enough to open a connection to it."""

    def __init__(self, url: str) -> None:
        match = _BASE.match(url)
        self.valid = match is not None
        self.secure = False
        self.host = ""
        self.port: int | None = None
        self.prefix = ""
        if match is None:
            return
        scheme, authority, rest = match.groups()
        self.secure = scheme.lower() == "https"
        try:
            split = urlsplit(f"{scheme}://{authority}")
            self.host = split.hostname or ""
            self.port = split.port
        except ValueError:
            self.valid = False
        self.prefix = rest
        if not self.host:
            self.valid = False


def _resolve(host: str, port: int, guard: Guard) -> list[tuple]:
    """``getaddrinfo``, bounded by the request's deadline (the platform call itself has no bound)."""
    try:
        ipaddress.ip_address(host)
    except ValueError:
        pass
    else:
        return socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)
    done = threading.Event()
    box: list[object] = []

    def work() -> None:
        try:
            box.append(socket.getaddrinfo(host, port, type=socket.SOCK_STREAM))
        except OSError as error:
            box.append(error)
        finally:
            done.set()

    threading.Thread(target=work, daemon=True, name="toggleslash-resolve").start()
    while not done.wait(0.05):
        guard.remaining()
    result = box[0]
    if isinstance(result, OSError):
        raise result
    return result  # type: ignore[return-value]


def _connector(guard: Guard) -> Callable[..., socket.socket]:
    """A ``create_connection`` that hands its socket to the guard before it connects."""

    def connect(address: tuple[str, int], *_args: object, **_kwargs: object) -> socket.socket:
        host, port = address
        last: OSError | None = None
        for family, kind, proto, _name, target in _resolve(host, port, guard):
            sock = socket.socket(family, kind, proto)
            try:
                guard.attach(sock)
                sock.settimeout(guard.remaining())
                sock.connect(target)
            except OSError as error:
                sock.close()
                last = error
                continue
            except BaseException:
                sock.close()
                raise
            sock.settimeout(guard.timeout)
            return sock
        raise last or OSError("no address for the host")

    return connect


def _connection(base: Base, guard: Guard) -> http.client.HTTPConnection:
    if base.secure:
        conn: http.client.HTTPConnection = http.client.HTTPSConnection(
            base.host, base.port, timeout=guard.timeout
        )
    else:
        conn = http.client.HTTPConnection(base.host, base.port, timeout=guard.timeout)
    conn._create_connection = _connector(guard)  # type: ignore[attr-defined]
    return conn


def _start(base: Base, guards: GuardSet, guard: Guard, method: str, target: str, headers, body):
    """Connect and send; the guard learns the socket that will carry the answer (TLS replaces it)."""
    conn = _connection(base, guard)
    try:
        conn.connect()
        if conn.sock is not None:
            guard.attach(conn.sock)
        conn.request(method, base.prefix + target, body=body, headers=headers)
        return conn, conn.getresponse()
    except BaseException:
        conn.close()
        raise


def _failure(guard: Guard, error: BaseException) -> Exception:
    """What a failed request is: abandoned by ``close``, out of time, or a network failure."""
    try:
        guard.check()
    except (Aborted, RequestFailure) as cut:
        return cut
    if isinstance(error, TimeoutError):
        return RequestFailure("timeout")
    return RequestFailure("network")


def request(
    base: Base,
    guards: GuardSet,
    clock: Clock,
    method: str,
    target: str,
    headers: Mapping[str, str],
    body: bytes | None = None,
    *,
    timeout: float,
    read_body: Callable[[int], bool] = lambda _status: False,
) -> Answer:
    """One request, bounded by ``timeout`` as a whole. ``read_body`` says, from the status, whether the
    body is read; the whole read counts toward ``timeout``."""
    if not base.valid:
        raise RequestFailure("network")
    guard = Guard(clock, timeout)
    guards.add(guard)
    timer = threading.Timer(timeout, guard.expire)
    timer.daemon = True
    timer.start()
    conn: http.client.HTTPConnection | None = None
    response: http.client.HTTPResponse | None = None
    try:
        conn, response = _start(base, guards, guard, method, target, dict(headers), body)
        data = response.read() if read_body(response.status) else b""
        guard.check()
        return Answer(response.status, response.msg, data)
    except (Aborted, RequestFailure):
        raise
    except Exception as error:
        raise _failure(guard, error) from error
    finally:
        timer.cancel()
        guards.discard(guard)
        if response is not None:
            response.close()  # a connection-close answer keeps the socket alive until this
        if conn is not None:
            conn.close()


class Stream:
    """An open event stream. ``read`` returns the next bytes, or ``b""`` when the server ended it."""

    def __init__(
        self,
        guards: GuardSet,
        guard: Guard,
        conn: http.client.HTTPConnection,
        response: http.client.HTTPResponse,
    ) -> None:
        self._guards = guards
        self._guard = guard
        self._conn = conn
        self._response = response
        self.status = response.status
        self.headers = response.msg

    def read(self) -> bytes:
        try:
            data = self._response.read1(65536)
        except Exception as error:
            raise _failure(self._guard, error) from error
        self._guard.check()
        return data

    def close(self) -> None:
        self._guards.discard(self._guard)
        try:
            self._guard.abort()
        finally:
            self._response.close()
            self._conn.close()


def open_stream(
    base: Base,
    guards: GuardSet,
    clock: Clock,
    target: str,
    headers: Mapping[str, str],
    *,
    open_timeout: float,
    idle_timeout: float,
) -> Stream:
    """Open a stream. ``open_timeout`` bounds the wait for the headers; then every read has
    ``idle_timeout`` to produce a byte, which restarts at each one."""
    if not base.valid:
        raise RequestFailure("network")
    guard = Guard(clock, open_timeout)
    guards.add(guard)
    timer = threading.Timer(open_timeout, guard.expire)
    timer.daemon = True
    timer.start()
    try:
        try:
            conn, response = _start(base, guards, guard, "GET", target, dict(headers), None)
        except (Aborted, RequestFailure):
            raise
        except Exception as error:
            raise _failure(guard, error) from error
    except BaseException:
        guards.discard(guard)
        raise
    finally:
        timer.cancel()
    sock = guard.socket()
    if sock is not None:
        sock.settimeout(idle_timeout)
    return Stream(guards, guard, conn, response)
