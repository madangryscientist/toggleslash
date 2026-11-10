"""The Python SDK's conformance test service.

It wraps `toggleslash.create` in the HTTP protocol of `docs/sdk-test-service.md`, so the harness in
`packages/sdk-conformance` can drive the SDK from outside. The accessor mapping is in this directory's README.
It catches nothing the SDK raises: a call that raises answers 500 with the error text, and the harness fails
the scenario. Standard library only; it serves on 127.0.0.1 and a port the OS picks, one thread per request.
"""

from __future__ import annotations

import json
import re
import sys
import threading
import traceback
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

import toggleslash
from toggleslash import EvaluationEvent, Hooks

CLIENT_ROUTE = re.compile(r"^/clients/([^/]+)/([a-z-]+)$")


@dataclass
class Entry:
    client: Any
    hook_events: list[dict[str, Any]] = field(default_factory=list)
    errors: list[str] = field(default_factory=list)


CLIENTS: dict[str, Entry] = {}
LOCK = threading.Lock()
COUNTER = [0]


def seconds(body: dict[str, Any], name: str) -> float | None:
    value = body.get(name)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    return value / 1000


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


def create_client(body: dict[str, Any]) -> str:
    entry = Entry(client=None)

    def on_error(error: BaseException) -> None:
        entry.errors.append(str(error))
        if body.get("onErrorThrows") is True:
            raise RuntimeError("onError raised, as the test asked")

    def after_evaluation(event: EvaluationEvent) -> None:
        entry.hook_events.append(event_json(event))
        if body.get("hookThrows") is True:
            raise RuntimeError("the hook raised, as the test asked")

    options: dict[str, Any] = {"url": str(body["url"]), "sdk_key": str(body["sdkKey"]), "on_error": on_error}
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
    entry.client = toggleslash.create(**options)
    with LOCK:
        COUNTER[0] += 1
        client_id = str(COUNTER[0])
        CLIENTS[client_id] = entry
    return client_id


def evaluate(entry: Entry, body: dict[str, Any]) -> dict[str, Any]:
    client = entry.client
    key, context, fallback = str(body["flagKey"]), body.get("context"), body.get("fallback")
    accessor = body.get("accessor", "any")
    if accessor == "int" and isinstance(fallback, float) and fallback.is_integer():
        fallback = int(fallback)
    method = {
        "bool": client.bool_variation_detail,
        "string": client.string_variation_detail,
        "number": client.number_variation_detail,
        "int": client.int_variation_detail,
        "json": client.json_variation_detail,
        "any": client.variation_detail,
    }.get(accessor)
    if method is None:
        raise ValueError(f"unknown accessor {accessor!r}")
    detail = method(key, context, fallback)
    return {
        "value": detail.value,
        "variationIndex": detail.variation_index,
        "reason": detail.reason.to_json(),
    }


def status_json(entry: Entry) -> dict[str, Any]:
    status = entry.client.status()
    return {
        "etag": status.etag,
        "lastSyncAt": status.last_sync_at,
        "stream": str(status.stream),
        "lastError": status.last_error,
    }


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args: Any) -> None:
        pass

    def send_json(self, status: int, value: Any) -> None:
        data = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def send_text(self, status: int, text: str) -> None:
        data = text.encode()
        self.send_response(status)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def read_body(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length") or 0)
        text = self.rfile.read(length).decode() if length else ""
        body = json.loads(text) if text else {}
        if not isinstance(body, dict):
            raise TypeError("the body is not a JSON object")
        return body

    def handle_any(self) -> None:
        try:
            self.route()
        except Exception:  # the harness fails the scenario on a 500, which is the point
            self.send_text(500, traceback.format_exc())

    do_GET = do_POST = handle_any

    def route(self) -> None:
        path = self.path.split("?")[0]
        if self.command == "GET" and path == "/":
            self.send_json(200, {"name": "toggleslash-sdk (Python)", "capabilities": ["integer-accessor"]})
            return
        if self.command == "POST" and path == "/clients":
            self.send_json(201, {"id": create_client(self.read_body())})
            return
        match = CLIENT_ROUTE.match(path)
        entry = CLIENTS.get(match.group(1)) if match else None
        if match is None or entry is None:
            self.send_text(404, "no such client")
            return
        action = f"{self.command} {match.group(2)}"
        if action == "POST ready":
            self.send_json(200, {"ready": entry.client.ready()})
        elif action == "POST evaluate":
            self.send_json(200, evaluate(entry, self.read_body()))
        elif action == "GET status":
            self.send_json(200, status_json(entry))
        elif action == "GET hook-events":
            self.send_json(200, {"events": list(entry.hook_events)})
        elif action == "GET errors":
            self.send_json(200, {"count": len(entry.errors), "messages": list(entry.errors)})
        elif action == "POST close":
            entry.client.close()
            self.send_json(200, {})
        else:
            self.send_text(404, "no such route")


class Server(ThreadingHTTPServer):
    daemon_threads = True


def main() -> None:
    server = Server(("127.0.0.1", 0), Handler)
    sys.stdout.write(f"listening http://127.0.0.1:{server.server_address[1]}\n")
    sys.stdout.flush()
    server.serve_forever()


if __name__ == "__main__":
    main()
