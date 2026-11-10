"""A fake ToggleSlash server on loopback and small helpers for the client tests."""

from __future__ import annotations

import json
import threading
import time
from collections.abc import Callable
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

KEY = "test-key-9f3a"


def flag(key: str, variations: list[Any], serve: int, version: int = 1, on: bool = True) -> dict[str, Any]:
    return {
        "key": key,
        "on": on,
        "variations": variations,
        "offVariation": 0,
        "salt": "salt-" + key,
        "version": version,
        "targets": [],
        "prerequisites": [],
        "rules": [],
        "fallthrough": {"variation": serve},
    }


def bool_flag(key: str, value: bool, version: int = 1) -> dict[str, Any]:
    return flag(key, [False, True], 1 if value else 0, version)


def ruleset(*flags: dict[str, Any]) -> dict[str, Any]:
    return {"flags": list(flags), "segments": []}


def wait_until(predicate: Callable[[], Any], timeout: float = 5.0, step: float = 0.005) -> bool:
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if predicate():
            return True
        time.sleep(step)
    return bool(predicate())


class Recorded:
    def __init__(self, route: str, headers: dict[str, str], body: bytes, method: str, path: str) -> None:
        self.route = route
        self.headers = headers
        self.body = body
        self.method = method
        self.path = path

    def json(self) -> Any:
        return json.loads(self.body)


class Request:
    """One request inside a handler: send an answer, or open a stream and write to it."""

    def __init__(self, handler: BaseHTTPRequestHandler, server: FakeServer, recorded: Recorded) -> None:
        self.handler = handler
        self.server = server
        self.recorded = recorded

    def send(self, status: int = 200, body: bytes = b"", headers: dict[str, str] | None = None) -> None:
        self.handler.send_response(status)
        for name, value in (headers or {}).items():
            self.handler.send_header(name, value)
        self.handler.send_header("Content-Length", str(len(body)))
        self.handler.end_headers()
        self.handler.wfile.write(body)

    def open_stream(self, status: int = 200) -> None:
        """Headers only: no Content-Length and not chunked, so the body ends when the server closes."""
        self.handler.send_response(status)
        self.handler.send_header("Content-Type", "text/event-stream")
        self.handler.end_headers()
        self.handler.wfile.flush()

    def write(self, data: bytes) -> None:
        self.handler.wfile.write(data)
        self.handler.wfile.flush()

    def hang(self, seconds: float = 30.0) -> None:
        self.server.stopping.wait(seconds)


Handler = Callable[[Request], None]


class FakeServer:
    def __init__(self, tls: tuple[str, str] | None = None) -> None:
        self.stopping = threading.Event()
        self.lock = threading.Lock()
        self.requests: list[Recorded] = []
        self.ruleset: dict[str, Any] = ruleset(bool_flag("checkout", True), bool_flag("legacy", False))
        self.version = 1
        self.overrides: dict[str, Handler] = {}
        outer = self

        class _Handler(BaseHTTPRequestHandler):
            def log_message(self, format: str, *args: Any) -> None:
                return

            def do_GET(self) -> None:
                outer.handle(self)

            def do_POST(self) -> None:
                outer.handle(self)

        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
        self.httpd.daemon_threads = True
        self.httpd.request_queue_size = 128
        scheme = "http"
        if tls is not None:
            import ssl

            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            context.load_cert_chain(tls[0], tls[1])
            self.httpd.socket = context.wrap_socket(self.httpd.socket, server_side=True)
            scheme = "https"
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self.thread.start()
        self.url = f"{scheme}://127.0.0.1:{self.httpd.server_address[1]}"

    @property
    def etag(self) -> str:
        return f'"v{self.version}"'

    def publish(self, new: dict[str, Any]) -> None:
        with self.lock:
            self.ruleset = new
            self.version += 1

    def bump(self, *flags: dict[str, Any]) -> None:
        """Publish a ruleset whose flags carry the new version number, built under the lock."""
        with self.lock:
            self.version += 1
            self.ruleset = ruleset(*(dict(f, version=self.version) for f in flags))

    def on(self, route: str, handler: Handler | None) -> None:
        if handler is None:
            self.overrides.pop(route, None)
        else:
            self.overrides[route] = handler

    def count(self, route: str) -> int:
        with self.lock:
            return sum(1 for r in self.requests if r.route == route)

    def recorded(self, route: str) -> list[Recorded]:
        with self.lock:
            return [r for r in self.requests if r.route == route]

    def handle(self, handler: BaseHTTPRequestHandler) -> None:
        length = int(handler.headers.get("Content-Length") or 0)
        body = handler.rfile.read(length) if length else b""
        path = handler.path
        route = {"/sdk/v1/ruleset": "ruleset", "/sdk/v1/stream": "stream", "/sdk/v1/events": "events"}.get(
            path, "other"
        )
        recorded = Recorded(route, dict(handler.headers.items()), body, handler.command, path)
        with self.lock:
            self.requests.append(recorded)
        request = Request(handler, self, recorded)
        try:
            (self.overrides.get(route) or getattr(self, "_" + route, self._other))(request)
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
            pass

    def _other(self, request: Request) -> None:
        request.send(404)

    def _ruleset(self, request: Request) -> None:
        with self.lock:
            etag, body = self.etag, json.dumps(self.ruleset).encode()
        if request.recorded.headers.get("If-None-Match") == etag:
            request.send(304, headers={"ETag": etag})
            return
        request.send(200, body, {"ETag": etag, "Content-Type": "application/json"})

    def _events(self, request: Request) -> None:
        request.send(200, b"{}")

    def _stream(self, request: Request) -> None:
        request.open_stream()
        with self.lock:
            sent = self.etag
        request.write(f"event: connected\ndata: {sent}\n\n".encode())
        while not self.stopping.wait(0.02):
            with self.lock:
                now = self.etag
            if now != sent:
                sent = now
                request.write(f"event: changed\ndata: {sent}\n\n".encode())

    def close(self) -> None:
        self.stopping.set()
        self.httpd.shutdown()
        self.httpd.server_close()
        self.thread.join(5)
