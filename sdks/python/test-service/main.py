"""The Python SDK's conformance test service: an HTTP server that wraps `create_client` so the harness in
`packages/sdk-conformance` can drive it from outside. The protocol is `docs/sdk-test-service.md`; the
accessor mapping is in this directory's README. It catches nothing the SDK raises: a call that raises
answers 500 with the error text, and the harness fails the scenario.
"""

from __future__ import annotations

import json
import sys
import threading
import traceback
from dataclasses import dataclass, field
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

import toggleslash
from toggleslash import EvaluationEvent, Hooks

ACCESSORS = {
    "bool": "bool_variation_detail",
    "string": "string_variation_detail",
    "number": "float_variation_detail",
    "int": "int_variation_detail",
    "json": "json_variation_detail",
    "any": "variation_detail",
}


@dataclass
class Entry:
    client: toggleslash.Client | None = None
    hook_events: list[dict[str, Any]] = field(default_factory=list)
    errors: list[str] = field(default_factory=list)
    lock: threading.Lock = field(default_factory=threading.Lock)


CLIENTS: dict[str, Entry] = {}
CLIENTS_LOCK = threading.Lock()
CREATED = 0


def epoch_ms(moment: datetime | None) -> int | None:
    """Whole epoch milliseconds, by integer arithmetic so a float cannot round a microsecond up."""
    if moment is None:
        return None
    delta = moment - datetime.fromtimestamp(0, moment.tzinfo)
    return (delta.days * 86400 + delta.seconds) * 1000 + delta.microseconds // 1000


def duration(body: dict[str, Any], name: str) -> float | None:
    value = body.get(name)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    return value / 1000


def event_json(event: EvaluationEvent) -> dict[str, Any]:
    reason = event.reason
    return {
        "flagKey": event.flag_key,
        "context": event.context,
        "value": event.value,
        "variationIndex": event.variation_index,
        "reason": reason_json(reason),
        "etag": event.etag,
        "flagVersion": event.flag_version,
    }


def reason_json(reason: toggleslash.Reason) -> dict[str, Any]:
    out: dict[str, Any] = {"kind": str(reason.kind)}
    if reason.rule_index is not None:
        out["ruleIndex"] = reason.rule_index
    if reason.flag_key is not None:
        out["flagKey"] = reason.flag_key
    if reason.code is not None:
        out["code"] = str(reason.code)
    return out


def create(body: dict[str, Any]) -> dict[str, Any]:
    global CREATED
    entry = Entry()

    def on_error(error: Exception) -> None:
        with entry.lock:
            entry.errors.append(str(error))
        if body.get("onErrorThrows") is True:
            raise RuntimeError("on_error raised, as the test asked")

    def after_evaluation(event: EvaluationEvent) -> None:
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
        value = duration(body, name)
        if value is not None:
            options[option] = value
    if body.get("hook") is True or body.get("hookThrows") is True:
        options["hooks"] = Hooks(after_evaluation=after_evaluation)
    entry.client = toggleslash.create_client(str(body["url"]), str(body["sdkKey"]), **options)
    with CLIENTS_LOCK:
        CREATED += 1
        identifier = str(CREATED)
        CLIENTS[identifier] = entry
    return {"id": identifier}


def evaluate(entry: Entry, body: dict[str, Any]) -> dict[str, Any]:
    assert entry.client is not None
    accessor = ACCESSORS.get(str(body.get("accessor", "any")))
    if accessor is None:
        raise ValueError(f"unknown accessor {body.get('accessor')!r}")
    detail = getattr(entry.client, accessor)(str(body["flagKey"]), body.get("context"), body.get("fallback"))
    return {"value": detail.value, "variationIndex": detail.variation_index, "reason": reason_json(detail.reason)}


def client_route(entry: Entry, action: str, method: str, body: dict[str, Any]) -> dict[str, Any] | None:
    client = entry.client
    assert client is not None
    key = f"{method} {action}"
    if key == "POST ready":
        return {"ready": client.ready()}
    if key == "POST evaluate":
        return evaluate(entry, body)
    if key == "GET status":
        status = client.status()
        return {
            "etag": status.etag,
            "lastSyncAt": epoch_ms(status.last_sync_at),
            "stream": str(status.stream),
            "lastError": status.last_error,
        }
    if key == "GET hook-events":
        with entry.lock:
            return {"events": list(entry.hook_events)}
    if key == "GET errors":
        with entry.lock:
            return {"count": len(entry.errors), "messages": list(entry.errors)}
    if key == "POST close":
        client.close()
        return {}
    return None


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
        pass

    def reply(self, status: int, payload: Any, *, text: bool = False) -> None:
        data = payload.encode("utf-8") if text else json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "text/plain" if text else "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def read_body(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        body = json.loads(raw) if raw else {}
        if not isinstance(body, dict):
            raise TypeError("the body is not a JSON object")
        return body

    def handle_any(self) -> None:
        try:
            self.route()
        except Exception:
            self.reply(500, traceback.format_exc(), text=True)

    do_GET = handle_any
    do_POST = handle_any

    def route(self) -> None:
        path = self.path.split("?", 1)[0]
        method = self.command
        if method == "GET" and path == "/":
            self.reply(200, {"name": "toggleslash-sdk (Python)", "capabilities": ["integer-accessor"]})
            return
        if method == "POST" and path == "/clients":
            self.reply(201, create(self.read_body()))
            return
        parts = path.split("/")
        entry = None
        if len(parts) == 4 and parts[0] == "" and parts[1] == "clients":
            with CLIENTS_LOCK:
                entry = CLIENTS.get(parts[2])
        if entry is None:
            self.read_body_quietly()
            self.reply(404, "no such client", text=True)
            return
        body = self.read_body() if method == "POST" else {}
        answer = client_route(entry, parts[3], method, body)
        if answer is None:
            self.reply(404, "no such route", text=True)
        else:
            self.reply(200, answer)

    def read_body_quietly(self) -> None:
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)


def main() -> None:
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    sys.stdout.write(f"listening http://127.0.0.1:{server.server_address[1]}\n")
    sys.stdout.flush()
    server.serve_forever()


if __name__ == "__main__":
    main()
