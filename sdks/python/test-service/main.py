"""The Python SDK's conformance test service: an HTTP server that wraps ``toggleslash.create`` so the
harness in ``packages/sdk-conformance`` can drive it. The protocol is ``docs/sdk-test-service.md``; the
accessor mapping is in this directory's README. It catches nothing the SDK raises: a call that raises
answers 500 with the error text, and the harness fails the scenario.
"""

from __future__ import annotations

import json
import math
import re
import sys
import threading
import traceback
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

import toggleslash
from toggleslash import _json


class Entry:
    def __init__(self) -> None:
        self.client: toggleslash.Client | None = None
        self.hook_events: list[dict[str, Any]] = []
        self.errors: list[str] = []
        self.lock = threading.Lock()


CLIENTS: dict[str, Entry] = {}
CLIENTS_LOCK = threading.Lock()
COUNTER = [0]


def clean(value: Any) -> Any:
    """JSON cannot hold a non-finite number: a JavaScript service would answer ``null`` for one."""
    if isinstance(value, float):
        return value if math.isfinite(value) else None
    if isinstance(value, list | tuple):
        return [clean(item) for item in value]
    if isinstance(value, dict):
        return {str(key): clean(item) for key, item in value.items()}
    return value


def reason_json(reason: toggleslash.Reason) -> dict[str, Any]:
    out: dict[str, Any] = {"kind": str(reason.kind)}
    if reason.code is not None:
        out["code"] = str(reason.code)
    if reason.rule_index is not None:
        out["ruleIndex"] = reason.rule_index
    if reason.flag_key is not None:
        out["flagKey"] = reason.flag_key
    return out


def event_json(event: toggleslash.EvaluationEvent) -> dict[str, Any]:
    return {
        "flagKey": event.flag_key,
        "context": clean(event.context),
        "value": clean(event.value),
        "variationIndex": event.variation_index,
        "reason": reason_json(event.reason),
        "etag": event.etag,
        "flagVersion": event.flag_version,
    }


def seconds(body: dict[str, Any], name: str) -> float | None:
    value = body.get(name)
    if isinstance(value, int | float) and not isinstance(value, bool):
        return value / 1000.0
    return None


def create(body: dict[str, Any]) -> dict[str, Any]:
    entry = Entry()

    def on_error(error: BaseException) -> None:
        with entry.lock:
            entry.errors.append(str(error))
        if body.get("onErrorThrows") is True:
            raise RuntimeError("on_error raised, as the test asked")

    def after_evaluation(event: toggleslash.EvaluationEvent) -> None:
        with entry.lock:
            entry.hook_events.append(event_json(event))
        if body.get("hookThrows") is True:
            raise RuntimeError("the hook raised, as the test asked")

    options: dict[str, Any] = {"on_error": on_error}
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
        options["hooks"] = toggleslash.Hooks(after_evaluation=after_evaluation)
    entry.client = toggleslash.create(str(body["url"]), str(body["sdkKey"]), **options)
    with CLIENTS_LOCK:
        COUNTER[0] += 1
        identifier = str(COUNTER[0])
        CLIENTS[identifier] = entry
    return {"id": identifier}


def evaluate(client: toggleslash.Client, body: dict[str, Any]) -> dict[str, Any]:
    accessor = body.get("accessor", "any")
    flag = str(body["flagKey"])
    context = body.get("context")
    fallback = body.get("fallback")
    if accessor == "int":
        if isinstance(fallback, float) and fallback.is_integer():
            fallback = int(fallback)
        detail = client.int_variation_detail(flag, context, fallback)
    else:
        detail = client.variation_detail(flag, context, fallback)
    return {
        "value": clean(detail.value),
        "variationIndex": detail.variation_index,
        "reason": reason_json(detail.reason),
    }


def status_json(status: toggleslash.Status) -> dict[str, Any]:
    at: datetime | None = status.last_sync_at
    stamp = None if at is None else int(at.timestamp() * 1000)
    return {
        "etag": status.etag,
        "lastSyncAt": stamp,
        "stream": str(status.stream),
        "lastError": status.last_error,
    }


class Handler(BaseHTTPRequestHandler):
    server_version = "toggleslash-test-service"

    def log_message(self, format: str, *args: Any) -> None:
        return

    def reply(self, status: int, payload: Any) -> None:
        data = json.dumps(payload, ensure_ascii=True).encode("ascii")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def text(self, status: int, message: str) -> None:
        data = message.encode("utf-8", "replace")
        self.send_response(status)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def body(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        parsed = _json.loads(raw) if raw else {}
        if not isinstance(parsed, dict):
            raise TypeError("the body is not a JSON object")
        return parsed

    def route(self) -> None:
        path = self.path.split("?", 1)[0]
        method = self.command
        if method == "GET" and path == "/":
            self.reply(200, {"name": "toggleslash-sdk (Python)", "capabilities": ["integer-accessor"]})
            return
        if method == "POST" and path == "/clients":
            self.reply(201, create(self.body()))
            return
        match = re.fullmatch(r"/clients/([^/]+)/([a-z-]+)", path)
        entry = CLIENTS.get(match.group(1)) if match else None
        if match is None or entry is None or entry.client is None:
            self.text(404, "no such client")
            return
        client, action = entry.client, f"{method} {match.group(2)}"
        if action == "POST ready":
            self.reply(200, {"ready": client.ready()})
        elif action == "POST evaluate":
            self.reply(200, evaluate(client, self.body()))
        elif action == "GET status":
            self.reply(200, status_json(client.status()))
        elif action == "GET hook-events":
            with entry.lock:
                events = list(entry.hook_events)
            self.reply(200, {"events": events})
        elif action == "GET errors":
            with entry.lock:
                messages = list(entry.errors)
            self.reply(200, {"count": len(messages), "messages": messages})
        elif action == "POST close":
            client.close()
            self.reply(200, {})
        else:
            self.text(404, "no such route")

    def handle_any(self) -> None:
        try:
            self.route()
        except Exception:
            self.text(500, traceback.format_exc())

    do_GET = handle_any
    do_POST = handle_any


class Server(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 128


def main() -> None:
    server = Server(("127.0.0.1", 0), Handler)
    sys.stdout.write(f"listening http://127.0.0.1:{server.server_address[1]}\n")
    sys.stdout.flush()
    server.serve_forever()


if __name__ == "__main__":
    main()
