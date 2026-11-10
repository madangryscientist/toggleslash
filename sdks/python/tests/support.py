"""Helpers the client tests share: a scripted transport, a hand-moved clock and a small local server."""

from __future__ import annotations

import json
import threading
import time
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from toggleslash._http import Aborter, TransportError
from toggleslash._types import NETWORK


def flag(key: str, value: Any, version: int = 1) -> dict[str, Any]:
    """A flag that serves `value` to everyone."""
    return {
        "key": key,
        "variations": [value, value],
        "on": True,
        "offVariation": 0,
        "targets": [],
        "prerequisites": [],
        "rules": [],
        "fallthrough": {"variation": 1},
        "salt": "s",
        "version": version,
    }


def ruleset_body(*flags: dict[str, Any]) -> bytes:
    return json.dumps({"flags": list(flags), "segments": []}).encode()


class FakeReply:
    def __init__(
        self,
        status: int = 200,
        *,
        headers: dict[str, str] | None = None,
        body: bytes = b"",
        chunks: list[bytes] | None = None,
        then: Callable[[Aborter, float], bytes] | None = None,
    ) -> None:
        self.status = status
        self.no_body = False
        self._headers = {name.lower(): value for name, value in (headers or {}).items()}
        self._body = body
        self._chunks = list(chunks or [])
        self._then = then
        self.aborter: Aborter | None = None
        self.watchdogs: list[float] = []
        self.closed = False

    def header(self, name: str) -> str | None:
        return self._headers.get(name.lower())

    def read_body(self) -> bytes:
        return self._body

    def read_chunk(self, watchdog: float) -> bytes:
        self.watchdogs.append(watchdog)
        if self._chunks:
            return self._chunks.pop(0)
        if self._then is not None and self.aborter is not None:
            return self._then(self.aborter, watchdog)
        return b""

    def close(self) -> None:
        self.closed = True


def hang(aborter: Aborter, _seconds: float = 0) -> bytes:
    """Blocks as a stalled server would, until the request is aborted or times out."""
    while not (aborter.aborted or aborter.timed_out):
        time.sleep(0.005)
    aborter.check()
    return b""


class ScriptedTransport:
    """A transport whose answers a test supplies per route: a callable, or a list used in order (the last
    one repeats). Every request is recorded."""

    def __init__(self, **routes: Any) -> None:
        self.routes = routes
        self.requests: list[dict[str, Any]] = []
        self.lock = threading.Lock()

    def count(self, route: str) -> int:
        with self.lock:
            return sum(1 for request in self.requests if request["route"].endswith(route))

    def __call__(
        self, method: str, route: str, headers: dict[str, str], body: bytes | None, aborter: Aborter
    ) -> FakeReply:
        name = route.rsplit("/", 1)[1]
        with self.lock:
            self.requests.append({"method": method, "route": route, "headers": dict(headers), "body": body})
            script = self.routes.get(name)
            step = (script[0] if len(script) == 1 else script.pop(0)) if isinstance(script, list) else script
        if step is None:
            raise TransportError(NETWORK, "no such route in this test")
        reply = step(method, headers, body, aborter) if callable(step) else step
        reply.aborter = aborter
        return reply


class FakeTime:
    """A monotonic clock, a wall clock and a `wait` that moves both instead of sleeping."""

    def __init__(self, start: datetime | None = None) -> None:
        self.now = 1000.0
        self.wall_now = start or datetime(2026, 11, 10, 12, 0, tzinfo=UTC)
        self.waits: list[float] = []
        self.stop_after: int | None = None
        self.lock = threading.Lock()

    def monotonic(self) -> float:
        return self.now

    def wall(self) -> datetime:
        return self.wall_now

    def advance(self, seconds: float) -> None:
        self.now += seconds
        self.wall_now += timedelta(seconds=seconds)

    def wait(self, seconds: float) -> bool:
        """Records the wait, moves time and answers `stopped` once `stop_after` waits have been made."""
        with self.lock:
            self.waits.append(seconds)
            self.advance(seconds)
            stopped = self.stop_after is not None and len(self.waits) >= self.stop_after
        if not stopped:
            time.sleep(0.001)
        return stopped


def until(condition: Callable[[], bool], seconds: float = 5.0, message: str = "timed out") -> None:
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if condition():
            return
        time.sleep(0.005)
    raise AssertionError(message)


class LocalServer:
    """A real HTTP server on loopback with `handler(request_handler)` for every request."""

    def __init__(self, handler: Callable[[BaseHTTPRequestHandler], None]) -> None:
        outer = handler

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
                pass

            def do_GET(self) -> None:
                try:
                    outer(self)
                except (OSError, ValueError):
                    pass

            do_POST = do_GET

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"

    def __enter__(self) -> LocalServer:
        return self

    def __exit__(self, *exc: object) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
