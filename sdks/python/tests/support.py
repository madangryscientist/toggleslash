"""Helpers the tests share."""

from __future__ import annotations

import json
import math
import queue
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

REPO = Path(__file__).resolve().parents[3]
FIXTURES = REPO / "fixtures" / "evaluation"


def deep_equal(a: Any, b: Any) -> bool:
    """JSON equality with numbers compared as IEEE-754 binary64 (so `-0.0` equals `0.0`), and `True` never a number."""
    if isinstance(a, bool) or isinstance(b, bool):
        return isinstance(a, bool) and isinstance(b, bool) and a == b
    if a is None or b is None:
        return a is None and b is None
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return float(a) == float(b) or (math.isnan(a) and math.isnan(b))
    if isinstance(a, str) and isinstance(b, str):
        return a == b
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(deep_equal(x, y) for x, y in zip(a, b, strict=True))
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(deep_equal(a[k], b[k]) for k in a)
    return False


# ---------------------------------------------------------------------------------------------------------------
# A fake ToggleSlash server, a fake clock and small waiting helpers shared by the client tests.


def bool_flag(key, serves, version=1, salt="s"):
    return {
        "key": key,
        "variations": [False, True],
        "on": True,
        "offVariation": 0,
        "targets": [],
        "prerequisites": [],
        "rules": [],
        "fallthrough": {"variation": 1 if serves else 0},
        "salt": salt,
        "version": version,
    }


FLAGS = {"flags": [bool_flag("checkout", True, 7), bool_flag("legacy", False, 3)], "segments": []}
CTX = {"key": "u1"}


def wait_until(check, timeout=10.0, step=0.01, what="condition"):
    """Polls `check` on real time until it is truthy; fails the test if it never is."""
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        value = check()
        if value:
            return value
        time.sleep(step)
    raise AssertionError(f"timed out waiting for {what}")


def join_all(threads, timeout=10.0):
    for thread in threads:
        thread.join(timeout)
        if thread.is_alive():
            raise AssertionError(f"thread {thread.name} did not finish: a deadlock or a hang")


class FakeClock:
    """Time that only moves when a test moves it. `wait` blocks until the event is set or the clock has passed
    the deadline. Step it only when `armed()` says the SDK is waiting on it."""

    def __init__(self, start_ms=1_700_000_000_000):
        self._cond = threading.Condition()
        self._now = start_ms
        self._timers = []
        self.waits = []

    def now_ms(self):
        with self._cond:
            return self._now

    def wait(self, event, seconds):
        with self._cond:
            if event.is_set():
                return True
            token = object()
            self._timers.append(token)
            self.waits.append(seconds)
            deadline = self._now + seconds * 1000
            try:
                while not event.is_set() and self._now < deadline:
                    self._cond.wait(0.005)
                return event.is_set()
            finally:
                self._timers.remove(token)

    def armed(self):
        with self._cond:
            return len(self._timers)

    def started(self):
        """How many waits have been started in all, so a test can tell a wait has begun again."""
        with self._cond:
            return len(self.waits)

    def wait_started(self, count, timeout=10.0):
        wait_until(lambda: self.started() >= count, timeout, what=f"{count} waits started")

    def advance(self, seconds):
        with self._cond:
            self._now += int(seconds * 1000)
            self._cond.notify_all()

    def wait_armed(self, count=1, timeout=10.0):
        wait_until(lambda: self.armed() >= count, timeout, what=f"{count} armed timer(s)")

    def run_until(self, check, step=1.0, timeout=10.0, what="condition"):
        """Moves time by `step` each time the SDK is waiting on it, until `check` holds."""
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if check():
                return
            if self.armed() > 0:
                self.advance(step)
            time.sleep(0.005)
        raise AssertionError(f"timed out waiting for {what}")

    def step(self, seconds, count=1):
        """Waits for the SDK to be waiting, then moves time."""
        self.wait_armed(count)
        self.advance(seconds)


class _Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def _record(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        entry = {
            "method": self.command,
            "path": self.path,
            "headers": {k.lower(): v for k, v in self.headers.items()},
            "body": body,
        }
        server = self.server.owner
        with server.lock:
            server.requests.append(entry)
        return entry

    def _route(self):
        return self.path.split("?")[0]

    def _scripted(self, route):
        server = self.server.owner
        with server.lock:
            queue_ = server.scripts.get(route)
            if queue_:
                return queue_.pop(0)
        return None

    def _answer(self, status, headers=None, body=b""):
        self.send_response(status)
        for name, value in (headers or {}).items():
            self.send_header(name, value)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _hang(self, route):
        server = self.server.owner
        if route in server.hang:
            server.stopping.wait(60)
            return True
        return False

    def do_GET(self):
        self._record()
        server = self.server.owner
        route = self._route()
        if self._hang(route):
            return
        scripted = self._scripted(route)
        if scripted is not None:
            status, headers = scripted
            self._answer(status, headers)
            return
        if route == "/sdk/v1/ruleset":
            with server.lock:
                etag, ruleset = server.etag, server.ruleset
                body = server.raw_body if server.raw_body is not None else json.dumps(ruleset).encode()
            if self.headers.get("If-None-Match") == etag:
                self._answer(304, {"ETag": etag})
            else:
                self._answer(200, {"ETag": etag, "Content-Type": "application/json"}, body)
            return
        if route == "/sdk/v1/stream":
            self._stream()
            return
        self._answer(404)

    def _stream(self):
        server = self.server.owner
        feed = queue.Queue()
        with server.lock:
            server.streams.append(feed)
            etag = server.etag
        # No Content-Length and no chunking: the body ends with the connection, which is the shape that makes
        # `http.client` drop its own reference to the socket.
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Connection", "close")
        self.end_headers()
        self.close_connection = True
        try:
            if server.send_connected:
                self.wfile.write(f"event: connected\ndata: {etag}\n\n".encode())
                self.wfile.flush()
            while not server.stopping.is_set():
                try:
                    chunk = feed.get(timeout=0.05)
                except queue.Empty:
                    continue
                if chunk is None:
                    return
                self.wfile.write(chunk)
                self.wfile.flush()
        except OSError:
            pass
        finally:
            with server.lock:
                if feed in server.streams:
                    server.streams.remove(feed)

    def do_POST(self):
        self._record()
        route = self._route()
        if self._hang(route):
            return
        scripted = self._scripted(route)
        if scripted is not None:
            status, headers = scripted
            self._answer(status, headers)
            return
        self._answer(200, {"Content-Type": "application/json"}, b"{}")


class _QuietServer(ThreadingHTTPServer):
    daemon_threads = True

    def handle_error(self, request, client_address):
        pass  # a client that hangs up mid-request is part of what the tests do


class FakeServer:
    def __init__(self, ruleset=None, etag='"v1"', tls=None):
        self.ruleset = ruleset if ruleset is not None else json.loads(json.dumps(FLAGS))
        self.etag = etag
        self.raw_body = None
        self.requests = []
        self.scripts = {}
        self.hang = set()
        self.streams = []
        self.send_connected = True
        self.lock = threading.Lock()
        self.stopping = threading.Event()
        self._httpd = _QuietServer(("127.0.0.1", 0), _Handler)
        self._httpd.owner = self
        if tls is not None:
            self._httpd.socket = tls.wrap_socket(self._httpd.socket, server_side=True)
        self.port = self._httpd.server_address[1]
        self.url = f"{'https' if tls is not None else 'http'}://127.0.0.1:{self.port}"
        self._thread = threading.Thread(target=self._httpd.serve_forever, daemon=True)
        self._thread.start()

    def set(self, ruleset, etag):
        with self.lock:
            self.ruleset = ruleset
            self.etag = etag

    def script(self, route, *answers):
        with self.lock:
            self.scripts.setdefault(route, []).extend(answers)

    def count(self, method, path):
        with self.lock:
            return sum(1 for r in self.requests if r["method"] == method and r["path"].split("?")[0] == path)

    def requests_to(self, method, path):
        with self.lock:
            return [r for r in self.requests if r["method"] == method and r["path"].split("?")[0] == path]

    def publish(self, tag=None):
        with self.lock:
            tag = tag if tag is not None else self.etag
            feeds = list(self.streams)
        for feed in feeds:
            feed.put(f"event: changed\ndata: {tag}\n\n".encode())

    def write(self, text):
        with self.lock:
            feeds = list(self.streams)
        for feed in feeds:
            feed.put(text.encode() if isinstance(text, str) else text)

    def drop_streams(self):
        with self.lock:
            feeds = list(self.streams)
        for feed in feeds:
            feed.put(None)

    def stop(self):
        self.stopping.set()
        self._httpd.shutdown()
        self._httpd.server_close()
