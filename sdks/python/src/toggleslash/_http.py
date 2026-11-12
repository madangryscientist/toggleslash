"""A small HTTP/1.1 client: one connection per request, a deadline for the whole request, a wake socket.

Why not `http.client`: it adds `Accept-Encoding`, its timeout is per socket operation and not per request, and
for an answer that ends with the connection it sets its socket to `None`, which takes a read timeout away from
the stream. Here every step is non-blocking and waits with a selector against one monotonic deadline, and every
wait also watches the wake socket, so `close` abandons a request in flight at once.
"""

from __future__ import annotations

import contextlib
import errno
import selectors
import socket
import ssl
import threading
import time
from collections.abc import Callable, Iterator
from dataclasses import dataclass
from urllib.parse import urlsplit

_READ = selectors.EVENT_READ
_WRITE = selectors.EVENT_WRITE
_HEX = frozenset(b"0123456789abcdefABCDEF")
_PENDING = frozenset({errno.EINPROGRESS, errno.EWOULDBLOCK, errno.EALREADY, errno.EINTR})
MAX_RETRY_AFTER = 300


class Aborted(Exception):
    """The client was closed while the request was in flight."""


class RequestTimeout(Exception):
    """The deadline ran out (or the stream was quiet for too long)."""


class NetworkError(Exception):
    """The request failed below HTTP. The message never carries the URL, the key or any data."""


class HttpStatus(Exception):
    """The server answered with a status the caller treats as a failure."""

    def __init__(self, status: int, retry_after: str | None = None) -> None:
        super().__init__(f"answered {status}")
        self.status = status
        self.retry_after = retry_after


def retry_after(status: int, value: str | None) -> int | None:
    """Seconds to wait, from a `Retry-After` on a 429: digits only after trimming, capped at 300."""
    if status != 429 or value is None:
        return None
    text = value.strip(" \t")
    if not text or not text.isascii() or not text.isdigit():
        return None
    text = text.lstrip("0")
    if len(text) > 3:
        return MAX_RETRY_AFTER
    return min(int(text or "0"), MAX_RETRY_AFTER)


class Hold:
    """A time before which nothing may be sent to one group of routes. It only ever lengthens."""

    def __init__(self, monotonic: Callable[[], float]) -> None:
        self._monotonic = monotonic
        self._until = 0.0
        self._lock = threading.Lock()

    def raise_to(self, seconds: float) -> None:
        until = self._monotonic() + seconds
        with self._lock:
            self._until = max(self._until, until)

    def remaining(self) -> float:
        with self._lock:
            return max(0.0, self._until - self._monotonic())


class Wake:
    """A socket pair. Writing to it wakes every wait of the requests that watch it, and it stays set."""

    def __init__(self) -> None:
        self._reader, self._writer = socket.socketpair()
        self._reader.setblocking(False)
        self._writer.setblocking(False)
        self._set = False
        self._closed = False
        self._lock = threading.Lock()

    @property
    def is_set(self) -> bool:
        return self._set

    def set(self) -> None:
        with self._lock:
            self._set = True
            if not self._closed:
                with contextlib.suppress(OSError):
                    self._writer.send(b"x")

    def close(self) -> None:
        with self._lock:
            if self._closed:
                return
            self._closed = True
            self._set = True
            self._reader.close()
            self._writer.close()

    def register(self, selector: selectors.BaseSelector) -> None:
        with self._lock:
            if self._closed:
                raise Aborted
            selector.register(self._reader, _READ)


@dataclass(frozen=True, slots=True)
class Response:
    status: int
    headers: dict[str, str]
    body: bytes


@dataclass(frozen=True, slots=True)
class _Base:
    secure: bool
    host: str
    port: int
    prefix: str

    @property
    def host_header(self) -> str:
        host = f"[{self.host}]" if ":" in self.host else self.host
        default = 443 if self.secure else 80
        return host if self.port == default else f"{host}:{self.port}"


def _parse_base(url: str) -> _Base:
    try:
        parts = urlsplit(url.strip())
        host = parts.hostname
        port = parts.port
    except ValueError:
        raise NetworkError("the URL is not valid") from None
    if parts.scheme not in {"http", "https"} or not host:
        raise NetworkError("the URL is not an http or https URL")
    secure = parts.scheme == "https"
    if port is None:
        port = 443 if secure else 80
    if not 1 <= port <= 65535:
        raise NetworkError("the URL has a port out of range")
    return _Base(secure, host, port, parts.path.rstrip("/"))


def _literal(host: str, port: int) -> tuple[int, int, int, str, tuple] | None:
    for family in (socket.AF_INET, socket.AF_INET6):
        try:
            socket.inet_pton(family, host)
        except (OSError, ValueError):
            continue
        sockaddr = (host, port) if family == socket.AF_INET else (host, port, 0, 0)
        return (family, socket.SOCK_STREAM, 0, "", sockaddr)
    return None


def _checked(text: str) -> bytes:
    if any(c in text for c in "\r\n\0"):
        raise NetworkError("a header holds a line break")
    try:
        return text.encode("ascii")
    except UnicodeEncodeError:
        raise NetworkError("a header holds a character outside ASCII") from None


class _Connection:
    """One socket plus the selector every wait goes through."""

    def __init__(self, wake: Wake) -> None:
        self.wake = wake
        self.sock: socket.socket | None = None
        self.selector = selectors.DefaultSelector()
        try:
            wake.register(self.selector)
        except BaseException:
            self.selector.close()
            raise

    def close(self) -> None:
        if self.sock is not None:
            with contextlib.suppress(OSError):
                self.sock.close()
            self.sock = None
        self.selector.close()

    def wait(self, mask: int, deadline: float) -> None:
        """Returns when the socket is ready for `mask`; raises when the wake fires or the time is up."""
        if self.wake.is_set:
            raise Aborted
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise RequestTimeout
        sock = self.sock
        assert sock is not None  # noqa: S101
        self.selector.register(sock, mask)
        try:
            events = self.selector.select(remaining)
        finally:
            self.selector.unregister(sock)
        if not events:
            raise RequestTimeout
        if any(key.fileobj is not sock for key, _ in events) or self.wake.is_set:
            raise Aborted

    def recv(self, deadline: float) -> bytes:
        sock = self.sock
        assert sock is not None  # noqa: S101
        while True:
            try:
                return sock.recv(65536)
            except (BlockingIOError, ssl.SSLWantReadError):
                self.wait(_READ, deadline)
            except ssl.SSLWantWriteError:
                self.wait(_WRITE, deadline)
            except ssl.SSLZeroReturnError:
                return b""

    def send_all(self, data: bytes, deadline: float) -> None:
        sock = self.sock
        assert sock is not None  # noqa: S101
        view = memoryview(data)
        while view:
            try:
                sent = sock.send(view)
                view = view[sent:]
            except (BlockingIOError, ssl.SSLWantWriteError):
                self.wait(_WRITE, deadline)
            except ssl.SSLWantReadError:
                self.wait(_READ, deadline)

    def connect(self, infos: list[tuple], deadline: float) -> None:
        failure: Exception | None = None
        for family, kind, proto, _canon, sockaddr in infos:
            try:
                sock = socket.socket(family, kind, proto)
            except OSError as exc:
                failure = exc
                continue
            sock.setblocking(False)
            self.sock = sock
            try:
                code = sock.connect_ex(sockaddr)
                if code != 0:
                    if code not in _PENDING:
                        raise OSError(code, "connect failed")
                    self.wait(_WRITE, deadline)
                    code = sock.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR)
                    if code != 0:
                        raise OSError(code, "connect failed")
                return
            except OSError as exc:
                failure = exc
                sock.close()
                self.sock = None
        raise NetworkError("could not connect") from failure

    def handshake(self, context: ssl.SSLContext, host: str, deadline: float) -> None:
        raw = self.sock
        assert raw is not None  # noqa: S101
        wrapped = context.wrap_socket(raw, server_hostname=host, do_handshake_on_connect=False)
        self.sock = wrapped
        while True:
            try:
                wrapped.do_handshake()
                return
            except ssl.SSLWantReadError:
                self.wait(_READ, deadline)
            except ssl.SSLWantWriteError:
                self.wait(_WRITE, deadline)


class _Reader:
    def __init__(self, connection: _Connection, initial: bytes) -> None:
        self.connection = connection
        self.buffer = bytearray(initial)

    def fill(self, deadline: float) -> bool:
        data = self.connection.recv(deadline)
        if not data:
            return False
        self.buffer += data
        return True

    def take(self, limit: int, next_deadline: Callable[[], float]) -> bytes:
        if not self.buffer and not self.fill(next_deadline()):
            return b""
        out = bytes(self.buffer[:limit])
        del self.buffer[:limit]
        return out

    def line(self, next_deadline: Callable[[], float]) -> bytes:
        while True:
            end = self.buffer.find(b"\n")
            if end >= 0:
                out = bytes(self.buffer[:end])
                del self.buffer[: end + 1]
                return out.removesuffix(b"\r")
            if not self.fill(next_deadline()):
                raise NetworkError("the answer ended early")


def _parse_head(raw: bytes) -> tuple[int, dict[str, str]]:
    lines = raw.decode("latin-1").replace("\r\n", "\n").split("\n")
    parts = lines[0].split(" ", 2)
    if len(parts) < 2 or not parts[0].startswith("HTTP/1.") or not parts[1].isascii():
        raise NetworkError("the status line is not HTTP/1")
    if not (parts[1].isdigit() and len(parts[1]) == 3):
        raise NetworkError("the status is not a number")
    headers: dict[str, str] = {}
    for line in lines[1:]:
        if not line or line[0] in " \t":
            continue
        name, colon, value = line.partition(":")
        if not colon:
            continue
        key = name.strip().lower()
        value = value.strip(" \t")
        headers[key] = f"{headers[key]}, {value}" if key in headers else value
    return int(parts[1]), headers


class Opened:
    """An answer whose head has been read. `chunks` yields the body as it arrives."""

    def __init__(
        self, connection: _Connection, reader: _Reader, status: int, headers: dict[str, str]
    ) -> None:
        self._connection = connection
        self._reader = reader
        self.status = status
        self.headers = headers

    def close(self) -> None:
        self._connection.close()

    def chunks(self, next_deadline: Callable[[], float], *, method: str = "GET") -> Iterator[bytes]:
        """The body, as read. Each raw read takes its deadline from `next_deadline()`."""
        status, headers, reader = self.status, self.headers, self._reader
        if method == "HEAD" or status in {204, 304} or 100 <= status < 200:
            return
        encoding = headers.get("transfer-encoding", "").lower()
        if encoding:
            if encoding.split(",")[-1].strip() != "chunked":
                raise NetworkError("the answer has an unsupported transfer encoding")
            yield from self._chunked(reader, next_deadline)
            return
        length = headers.get("content-length")
        if length is not None:
            if not (length.isascii() and length.isdigit()):
                raise NetworkError("the content length is not a number")
            left = int(length) if len(length) < 18 else 1 << 62
            while left:
                data = reader.take(min(left, 65536), next_deadline)
                if not data:
                    raise NetworkError("the answer ended early")
                left -= len(data)
                yield data
            return
        while True:
            data = reader.take(65536, next_deadline)
            if not data:
                return
            yield data

    @staticmethod
    def _chunked(reader: _Reader, next_deadline: Callable[[], float]) -> Iterator[bytes]:
        while True:
            size_text = reader.line(next_deadline).split(b";", 1)[0].strip()
            if not size_text or any(c not in _HEX for c in size_text):
                raise NetworkError("a chunk size is not valid")
            size = int(size_text, 16)
            if size == 0:
                while reader.line(next_deadline):
                    pass
                return
            left = size
            while left:
                data = reader.take(min(left, 65536), next_deadline)
                if not data:
                    raise NetworkError("the answer ended early")
                left -= len(data)
                yield data
            if reader.line(next_deadline):
                raise NetworkError("a chunk is not followed by a line break")


class Transport:
    """Requests to one server, all watching one wake socket."""

    def __init__(self, url: str, wake: Wake) -> None:
        self._url = url
        self._wake = wake
        self._context: ssl.SSLContext | None = None
        self._context_lock = threading.Lock()

    def request(
        self,
        method: str,
        path: str,
        headers: dict[str, str],
        body: bytes | None,
        timeout: float,
    ) -> Response:
        """A whole request, answer body included, within `timeout` seconds."""
        deadline = time.monotonic() + timeout
        opened = self._open(method, path, headers, body, deadline)
        try:
            data = b"".join(self._guard(opened.chunks(lambda: deadline, method=method)))
            return Response(opened.status, opened.headers, data)
        finally:
            opened.close()

    def open_stream(self, path: str, headers: dict[str, str], timeout: float) -> Opened:
        """GET with the head read within `timeout`. The caller reads `chunks` and calls `close`."""
        return self._open("GET", path, headers, None, time.monotonic() + timeout)

    @staticmethod
    def _guard(chunks: Iterator[bytes]) -> Iterator[bytes]:
        try:
            yield from chunks
        except (OSError, ValueError, UnicodeError) as exc:
            raise NetworkError(type(exc).__name__) from None

    def _tls(self) -> ssl.SSLContext:
        with self._context_lock:
            if self._context is None:
                self._context = ssl.create_default_context()
            return self._context

    def _resolve(self, base: _Base, deadline: float) -> list[tuple]:
        literal = _literal(base.host, base.port)
        if literal is not None:
            return [literal]
        box: dict[str, object] = {}
        done = threading.Event()

        def look_up() -> None:
            try:
                box["infos"] = socket.getaddrinfo(base.host, base.port, type=socket.SOCK_STREAM)
            except (OSError, UnicodeError) as exc:
                box["error"] = exc
            done.set()

        threading.Thread(target=look_up, name="toggleslash-resolve", daemon=True).start()
        while not done.wait(0.02):
            if self._wake.is_set:
                raise Aborted
            if time.monotonic() >= deadline:
                raise RequestTimeout
        infos = box.get("infos")
        if not isinstance(infos, list) or not infos:
            raise NetworkError("the host name did not resolve")
        return infos

    def _open(
        self, method: str, path: str, headers: dict[str, str], body: bytes | None, deadline: float
    ) -> Opened:
        try:
            return self._open_unguarded(method, path, headers, body, deadline)
        except (Aborted, RequestTimeout, NetworkError):
            raise
        except (OSError, ValueError, UnicodeError) as exc:
            raise NetworkError(type(exc).__name__) from None

    def _open_unguarded(
        self, method: str, path: str, headers: dict[str, str], body: bytes | None, deadline: float
    ) -> Opened:
        base = _parse_base(self._url)
        lines = [
            f"{method} {base.prefix}{path} HTTP/1.1".encode("ascii"),
            _checked(f"Host: {base.host_header}"),
            b"Connection: close",
        ]
        for name, value in headers.items():
            lines.append(_checked(f"{name}: {value}"))
        if body is not None:
            lines.append(f"Content-Length: {len(body)}".encode("ascii"))
        head = b"\r\n".join(lines) + b"\r\n\r\n"
        infos = self._resolve(base, deadline)
        connection = _Connection(self._wake)
        try:
            connection.connect(infos, deadline)
            if base.secure:
                connection.handshake(self._tls(), base.host, deadline)
            connection.send_all(head + (body or b""), deadline)
            reader = _Reader(connection, b"")
            while True:
                while True:
                    marker = bytes(reader.buffer).find(b"\r\n\r\n")
                    if marker >= 0:
                        break
                    if not reader.fill(deadline):
                        raise NetworkError("the answer ended before its head did")
                raw = bytes(reader.buffer[:marker])
                del reader.buffer[: marker + 4]
                status, answer = _parse_head(raw)
                if 100 <= status < 200 and status != 101:
                    continue
                return Opened(connection, reader, status, answer)
        except BaseException:
            connection.close()
            raise
