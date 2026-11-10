"""A loopback ToggleSlash server, a hand-moved clock and a ruleset builder, for the native tests."""

from __future__ import annotations

import json
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

SDK_KEY = "sdk-test-key"
FLUSH = 3600.0  # a flush wait no loop wait will equal, so a test can tell the two apart


class FakeClock:
    """Time that moves only when a test moves it. ``wait`` returns when the event is set or when the
    test has advanced time past its deadline, and records each wait it was asked for."""

    def __init__(self, wall: float = 1_700_000_000.0) -> None:
        self._lock = threading.Lock()
        self._mono = 1000.0
        self._wall = wall
        self.waits: list[float] = []

    def now(self) -> float:
        with self._lock:
            return self._wall

    def monotonic(self) -> float:
        with self._lock:
            return self._mono

    def advance(self, seconds: float) -> None:
        with self._lock:
            self._mono += seconds
            self._wall += seconds

    def set_wall(self, wall: float) -> None:
        with self._lock:
            self._wall = wall

    def wait(self, event: threading.Event, seconds: float) -> bool:
        with self._lock:
            self.waits.append(seconds)
            deadline = self._mono + seconds
        while True:
            if event.wait(0.002):
                return True
            with self._lock:
                if self._mono >= deadline:
                    return False

    def waited(self) -> list[float]:
        """The sync loop's waits: every wait but the flush loop's."""
        with self._lock:
            return [w for w in self.waits if w != FLUSH]


@dataclass
class Recorded:
    method: str
    path: str
    headers: dict[str, str]
    body: bytes


class Responder:
    """What a route handler uses to answer."""

    def __init__(self, handler: BaseHTTPRequestHandler, release: threading.Event) -> None:
        self._h = handler
        self.release = release

    def send(self, status: int, body: bytes = b"", headers: dict[str, str] | None = None) -> None:
        self._h.send_response(status)
        for name, value in (headers or {}).items():
            self._h.send_header(name, value)
        self._h.send_header("Content-Length", str(len(body)))
        self._h.end_headers()
        self._h.wfile.write(body)

    def stream_open(self, status: int = 200) -> None:
        self._h.send_response(status)
        self._h.send_header("Content-Type", "text/event-stream")
        self._h.end_headers()
        self._h.wfile.flush()

    def write(self, data: bytes) -> bool:
        try:
            self._h.wfile.write(data)
            self._h.wfile.flush()
        except OSError:
            return False
        return True

    def hold_until_released(self, seconds: float = 30.0) -> bool:
        return self.release.wait(seconds)


Route = Callable[[Recorded, Responder], None]


@dataclass
class FakeServer:
    """Loopback HTTP server. Routes are set per ``(method, path)``; every request is recorded."""

    routes: dict[tuple[str, str], Route] = field(default_factory=dict)
    requests: list[Recorded] = field(default_factory=list)

    def __post_init__(self) -> None:
        self._lock = threading.Lock()
        self.release = threading.Event()
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args: Any) -> None:
                return

            def _handle(self) -> None:
                length = int(self.headers.get("Content-Length") or 0)
                body = self.rfile.read(length) if length else b""
                record = Recorded(
                    self.command,
                    self.path,
                    {k.lower(): v for k, v in self.headers.items()},
                    body,
                )
                with outer._lock:
                    outer.requests.append(record)
                    route = outer.routes.get((self.command, self.path))
                if route is None:
                    self.send_response(404)
                    self.send_header("Content-Length", "0")
                    self.end_headers()
                    return
                try:
                    route(record, Responder(self, outer.release))
                except (BrokenPipeError, ConnectionResetError):
                    pass

            do_GET = _handle
            do_POST = _handle

        self._httpd = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self._httpd.daemon_threads = True
        self.url = f"http://127.0.0.1:{self._httpd.server_address[1]}"
        self._thread = threading.Thread(target=self._httpd.serve_forever, daemon=True)
        self._thread.start()

    def route(self, method: str, path: str, handler: Route) -> None:
        with self._lock:
            self.routes[(method, path)] = handler

    def seen(self, method: str, path: str) -> list[Recorded]:
        with self._lock:
            return [r for r in self.requests if r.method == method and r.path == path]

    def stop(self) -> None:
        self.release.set()
        self._httpd.shutdown()
        self._httpd.server_close()
        self._thread.join(5)


def flag(key: str, variations: list[Any], *, version: int = 1, serve: int = 0, on: bool = True) -> dict:
    """A flag that serves ``variations[serve]`` to everyone when on, and ``variations[0]`` when off."""
    return {
        "key": key,
        "on": on,
        "version": version,
        "salt": "salt-" + key,
        "variations": variations,
        "offVariation": 0,
        "targets": [],
        "prerequisites": [],
        "rules": [],
        "fallthrough": {"variation": serve},
    }


def ruleset(*flags: dict, segments: list | None = None) -> bytes:
    return json.dumps({"flags": list(flags), "segments": segments or []}).encode()


def serve_ruleset(server: FakeServer, body: bytes, etag: str = '"v1"') -> None:
    """The ruleset route, answering 304 to a matching ``If-None-Match``."""

    def handle(request: Recorded, out: Responder) -> None:
        if request.headers.get("if-none-match") == etag:
            out.send(304, b"", {"ETag": etag})
        else:
            out.send(200, body, {"ETag": etag, "Content-Type": "application/json"})

    server.route("GET", "/sdk/v1/ruleset", handle)


def sse(event: str, data: str = "") -> bytes:
    return f"event: {event}\ndata: {data}\n\n".encode()


def wait_until(check: Callable[[], bool], seconds: float = 10.0, what: str = "a condition") -> None:
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(0.005)
    raise AssertionError(f"timed out waiting for {what}")


CONTEXT = {"key": "user-1"}


def hang(request: Recorded, out: Responder) -> None:
    """A route that never answers until the server stops."""
    out.hold_until_released()


def stream_route(chunks: list[bytes], *, hold: bool = True) -> Route:
    """A stream route that sends ``chunks`` and then keeps the connection open (or ends it)."""

    def handle(request: Recorded, out: Responder) -> None:
        out.stream_open()
        for chunk in chunks:
            if not out.write(chunk):
                return
        if hold:
            out.hold_until_released()

    return handle


import unittest  # noqa: E402 - kept beside the one class that needs it

from toggleslash import Hooks, create_client  # noqa: E402
from toggleslash.client import Client, Seams  # noqa: E402


class ServerCase(unittest.TestCase):
    """A test with a fake server and a way to build clients that are closed afterwards."""

    def setUp(self) -> None:
        self.server = FakeServer()
        self.addCleanup(self.server.stop)
        self.errors: list[Exception] = []
        self.clock = FakeClock()

    def make(self, *, fake_clock: bool = False, seams: Seams | None = None, **options: Any) -> Client:
        options.setdefault("url", self.server.url)
        options.setdefault("sdk_key", SDK_KEY)
        options.setdefault("stream", False)
        options.setdefault("on_error", self.errors.append)
        if fake_clock:
            options.setdefault("flush_interval", FLUSH)
        if fake_clock and seams is None:
            seams = Seams(clock=self.clock)
        if seams is None:
            client = create_client(**options)
        else:
            client = Client(**options, _seams=seams)
        self.addCleanup(client.close)
        return client


__all__ = ["Hooks"]
