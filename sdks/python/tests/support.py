"""A fake ToggleSlash server on loopback, built on raw sockets so a test controls every byte and every pause."""

from __future__ import annotations

import contextlib
import json
import select
import socket
import threading
import time
from collections.abc import Callable
from typing import Any

from toggleslash._client import Client, Config


class Request:
    def __init__(
        self, method: str, path: str, raw_headers: list[tuple[str, str]], body: bytes
    ) -> None:
        self.method = method
        self.path = path
        self.raw_headers = raw_headers
        self.headers = {name.lower(): value for name, value in raw_headers}
        self.body = body

    def json(self) -> Any:
        return json.loads(self.body)


class Connection:
    def __init__(self, sock: socket.socket) -> None:
        self.sock = sock

    def send(self, data: bytes) -> None:
        self.sock.sendall(data)

    def close(self) -> None:
        with contextlib.suppress(OSError):
            self.sock.shutdown(socket.SHUT_RDWR)
        with contextlib.suppress(OSError):
            self.sock.close()

    def wait_closed(self, timeout: float) -> bool:
        """True once the client has closed its end."""
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            try:
                ready, _, _ = select.select([self.sock], [], [], 0.02)
                if ready and self.sock.recv(1) == b"":
                    return True
            except (OSError, ValueError):
                return True
        return False


Handler = Callable[[Request, Connection], None]


def response(status: int = 200, body: bytes | str = b"", **headers: str) -> bytes:
    data = body.encode() if isinstance(body, str) else body
    lines = [f"HTTP/1.1 {status} X".encode(), f"Content-Length: {len(data)}".encode()]
    lines += [f"{name.replace('_', '-')}: {value}".encode() for name, value in headers.items()]
    return b"\r\n".join(lines) + b"\r\n\r\n" + data


def reply(status: int = 200, body: bytes | str = b"", **headers: str) -> Handler:
    def handle(_request: Request, connection: Connection) -> None:
        connection.send(response(status, body, **headers))
        connection.close()

    return handle


def chunk(data: bytes | str) -> bytes:
    raw = data.encode() if isinstance(data, str) else data
    return f"{len(raw):x}\r\n".encode() + raw + b"\r\n"


STREAM_HEAD = (
    b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n"
)


def event(name: str, data: str) -> bytes:
    return chunk(f"event: {name}\ndata: {data}\n\n")


def flag_json(
    key: str = "f",
    variations: list[Any] | None = None,
    *,
    on: bool = True,
    fallthrough: int = 1,
    version: int = 3,
    **extra: Any,
) -> dict[str, Any]:
    base: dict[str, Any] = {
        "key": key,
        "on": on,
        "variations": [False, True] if variations is None else variations,
        "offVariation": 0,
        "salt": "salt",
        "version": version,
        "targets": [],
        "prerequisites": [],
        "rules": [],
        "fallthrough": {"variation": fallthrough},
    }
    base.update(extra)
    return base


def ruleset_json(*flags: dict[str, Any]) -> str:
    return json.dumps({"flags": list(flags), "segments": []})


class FakeServer:
    def __init__(self) -> None:
        self.requests: list[Request] = []
        self._routes: dict[tuple[str, str], list[Handler]] = {}
        self._lock = threading.Lock()
        self._connections: list[Connection] = []
        self._listener = socket.socket()
        self._listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self._listener.bind(("127.0.0.1", 0))
        self._listener.listen(64)
        self.port = self._listener.getsockname()[1]
        self._stopped = False
        self._thread = threading.Thread(target=self._accept, daemon=True)
        self._thread.start()

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.port}"

    def on(self, method: str, path: str, *handlers: Handler) -> None:
        """The handlers answer successive requests; the last one answers every request after."""
        with self._lock:
            self._routes[(method, path)] = list(handlers)

    def seen(self, method: str, path: str) -> list[Request]:
        with self._lock:
            return [r for r in self.requests if r.method == method and r.path == path]

    def stop(self) -> None:
        self._stopped = True
        with contextlib.suppress(OSError):
            self._listener.shutdown(
                socket.SHUT_RDWR
            )  # wakes the accept that close alone leaves blocked
        with contextlib.suppress(OSError):
            self._listener.close()
        with self._lock:
            connections = list(self._connections)
        for connection in connections:
            connection.close()
        self._thread.join(2)

    def _accept(self) -> None:
        while not self._stopped:
            try:
                sock, _ = self._listener.accept()
            except OSError:
                return
            connection = Connection(sock)
            with self._lock:
                self._connections.append(connection)
            threading.Thread(target=self._serve, args=(connection,), daemon=True).start()

    def _read_request(self, sock: socket.socket) -> Request | None:
        data = b""
        while b"\r\n\r\n" not in data:
            piece = sock.recv(65536)
            if not piece:
                return None
            data += piece
        head, _, rest = data.partition(b"\r\n\r\n")
        lines = head.decode("latin-1").split("\r\n")
        method, path, _version = lines[0].split(" ", 2)
        raw = [tuple(line.split(": ", 1)) for line in lines[1:]]
        length = int(next((v for n, v in raw if n.lower() == "content-length"), "0"))
        while len(rest) < length:
            rest += sock.recv(65536)
        return Request(method, path, raw, rest[:length])

    def _serve(self, connection: Connection) -> None:
        try:
            request = self._read_request(connection.sock)
            if request is None:
                connection.close()
                return
            with self._lock:
                self.requests.append(request)
                handlers = self._routes.get((request.method, request.path), [])
                handler = (
                    handlers.pop(0) if len(handlers) > 1 else (handlers[0] if handlers else None)
                )
            (handler or reply(404))(request, connection)
        except (OSError, ValueError):
            connection.close()


def wait_for(predicate: Callable[[], Any], timeout: float = 5.0) -> bool:
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if predicate():
            return True
        time.sleep(0.005)
    return bool(predicate())


def config(server: FakeServer, **changes: Any) -> Config:
    base: dict[str, Any] = {
        "url": server.url,
        "sdk_key": "test-key",
        "stream": False,
        "poll_interval": 0.05,
        "flush_interval": 3600.0,
        "request_timeout": 2.0,
    }
    base.update(changes)
    return Config(**base)


def make_client(server: FakeServer, **changes: Any) -> Client:
    return Client(config(server, **changes))


def stream(*pieces: bytes, then: str = "hold", gap: float = 0.0) -> Handler:
    """Answers a stream request with the head and the pieces, then holds the connection open (`hold`), closes it
    cleanly (`end`, with the chunked terminator) or abruptly (`cut`)."""

    def handle(_request: Request, connection: Connection) -> None:
        connection.send(STREAM_HEAD)
        for piece in pieces:
            connection.send(piece)
            if gap:
                time.sleep(gap)
        if then == "end":
            connection.send(b"0\r\n\r\n")
            connection.close()
        elif then == "cut":
            connection.close()
        else:
            connection.wait_closed(20)
            connection.close()

    return handle


def stalled(seconds: float = 20.0) -> Handler:
    """Reads the request and says nothing until the client gives up."""

    def handle(_request: Request, connection: Connection) -> None:
        connection.wait_closed(seconds)
        connection.close()

    return handle
