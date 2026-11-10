"""The Python SDK's conformance test service: an HTTP server that wraps ``create_client`` so the harness in
``packages/sdk-conformance`` can drive it from outside. The protocol is ``docs/sdk-test-service.md``; the
accessor mapping is in this directory's README. It catches nothing the SDK raises: a call that raises
answers 500 with the traceback, and the harness fails the scenario.

Run it from ``sdks/python`` (``bin/test-service`` does). Never run it with ``src/toggleslash`` as the working
directory: that package has a ``types.py`` and an ``http.py`` that would shadow the standard library's.
"""

from __future__ import annotations

import contextlib
import itertools
import json
import sys
import threading
import traceback
from datetime import UTC, datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from toggleslash import Client, EvaluationEvent, Hooks, create_client
from toggleslash._json import parse

ACCESSORS = {
    "bool": "bool_variation_detail",
    "string": "string_variation_detail",
    "number": "float_variation_detail",
    "int": "int_variation_detail",
    "json": "json_variation_detail",
    "any": "variation_detail",
}

EPOCH = datetime(1970, 1, 1, tzinfo=UTC)


class Entry:
    def __init__(self) -> None:
        self.client: Client | None = None
        self.hook_events: list[dict[str, Any]] = []
        self.errors: list[str] = []
        self.lock = threading.Lock()


CLIENTS: dict[str, Entry] = {}
CLIENTS_LOCK = threading.Lock()
IDS = itertools.count(1)


class Deliberate(Exception):
    pass


def event_json(event: EvaluationEvent) -> dict[str, Any]:
    return {
        "flagKey": event.flag_key,
        "context": event.context,
        "value": event.value,
        "variationIndex": event.variation_index,
        "reason": event.reason.to_json(),
        "etag": event.etag,
        "flagVersion": event.flag_version,
    }


def seconds(body: dict[str, Any], name: str) -> float | None:
    value = body.get(name)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    return value / 1000.0


def make_client(body: dict[str, Any], entry: Entry) -> Client:
    def on_error(error: Exception) -> None:
        with entry.lock:
            entry.errors.append(str(error))
        if body.get("onErrorThrows") is True:
            raise Deliberate("on_error raised, as the test asked")

    def after_evaluation(event: EvaluationEvent) -> None:
        with entry.lock:
            entry.hook_events.append(event_json(event))
        if body.get("hookThrows") is True:
            raise Deliberate("the hook raised, as the test asked")

    options: dict[str, Any] = {
        "url": str(body.get("url")),
        "sdk_key": str(body.get("sdkKey")),
        "on_error": on_error,
    }
    if isinstance(body.get("stream"), bool):
        options["stream"] = body["stream"]
    for name, option in (
        ("pollIntervalMs", "poll_interval"),
        ("flushIntervalMs", "flush_interval"),
        ("requestTimeoutMs", "request_timeout"),
    ):
        value = seconds(body, name)
        if value is not None:
            options[option] = value
    if body.get("hook") is True or body.get("hookThrows") is True:
        options["hooks"] = Hooks(after_evaluation=after_evaluation)
    return create_client(**options)


def status_json(client: Client) -> dict[str, Any]:
    status = client.status()
    synced = None
    if status.last_sync_at is not None:
        synced = int((status.last_sync_at - EPOCH).total_seconds() * 1000)
    return {
        "etag": status.etag,
        "lastSyncAt": synced,
        "stream": str(status.stream),
        "lastError": status.last_error,
    }


def evaluate(client: Client, body: dict[str, Any]) -> dict[str, Any]:
    method = getattr(client, ACCESSORS[str(body.get("accessor") or "any")])
    detail = method(str(body.get("flagKey")), body.get("context"), body.get("fallback"))
    return {
        "value": detail.value,
        "variationIndex": detail.variation_index,
        "reason": detail.reason.to_json(),
    }


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args: object) -> None:
        pass

    def reply(self, status: int, payload: Any) -> None:
        data = json.dumps(payload).encode() if not isinstance(payload, bytes) else payload
        self.send_response(status)
        self.send_header(
            "Content-Type", "application/json" if not isinstance(payload, bytes) else "text/plain"
        )
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def body(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        parsed = parse(raw) if raw.strip() else {}
        if not isinstance(parsed, dict):
            raise TypeError("the body is not a JSON object")
        return parsed

    def handle_request(self, method: str) -> None:
        try:
            self.route(method)
        except Exception:
            self.reply(500, traceback.format_exc().encode())

    def route(self, method: str) -> None:
        path = self.path.split("?", 1)[0]
        if method == "GET" and path == "/":
            self.reply(200, {"name": "toggleslash-sdk (Python)", "capabilities": ["integer-accessor"]})
            return
        if method == "POST" and path == "/clients":
            entry = Entry()
            entry.client = make_client(self.body(), entry)
            with CLIENTS_LOCK:
                identifier = str(next(IDS))
                CLIENTS[identifier] = entry
            self.reply(201, {"id": identifier})
            return
        parts = path.strip("/").split("/")
        entry = CLIENTS.get(parts[1]) if len(parts) == 3 and parts[0] == "clients" else None
        if entry is None or entry.client is None:
            self.reply(404, b"no such client")
            return
        client, action = entry.client, parts[2]
        if (method, action) == ("POST", "ready"):
            self.reply(200, {"ready": client.ready()})
        elif (method, action) == ("POST", "evaluate"):
            self.reply(200, evaluate(client, self.body()))
        elif (method, action) == ("GET", "status"):
            self.reply(200, status_json(client))
        elif (method, action) == ("GET", "hook-events"):
            with entry.lock:
                self.reply(200, {"events": list(entry.hook_events)})
        elif (method, action) == ("GET", "errors"):
            with entry.lock:
                self.reply(200, {"count": len(entry.errors), "messages": list(entry.errors)})
        elif (method, action) == ("POST", "close"):
            client.close()
            self.reply(200, {})
        else:
            self.reply(404, b"no such route")

    def do_GET(self) -> None:
        self.handle_request("GET")

    def do_POST(self) -> None:
        self.handle_request("POST")


class Server(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 128


def main() -> None:
    server = Server(("127.0.0.1", 0), Handler)
    sys.stdout.write(f"listening http://127.0.0.1:{server.server_address[1]}\n")
    sys.stdout.flush()
    with contextlib.suppress(KeyboardInterrupt):
        server.serve_forever()


if __name__ == "__main__":
    main()
