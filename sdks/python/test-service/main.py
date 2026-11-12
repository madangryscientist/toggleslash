"""The Python SDK's conformance test service: an HTTP server that wraps `toggleslash.create` so the harness in
`packages/sdk-conformance` can drive it. The protocol is docs/sdk-test-service.md; the accessor mapping is in
this directory's README. It absorbs nothing the SDK raises: a call that raises answers 500 with the error text,
and the harness fails the scenario. It uses the standard library only, as the SDK does."""

from __future__ import annotations

import datetime
import json
import re
import sys
import threading
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

import toggleslash
from toggleslash import _json

ROUTE = re.compile(r"^/clients/([^/]+)/([a-z-]+)$")


class Entry:
    """One client the harness created, with the hook events and errors it asked to record."""

    def __init__(self) -> None:
        self.client: toggleslash.Client | None = None
        self.hook_events: list[dict[str, Any]] = []
        self.errors: list[str] = []
        self.lock = threading.Lock()


def clean(value: Any) -> Any:
    """JSON cannot hold a non-finite number: a JavaScript service would answer null for one."""
    return _json.finite(value)


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


def status_json(status: toggleslash.Status) -> dict[str, Any]:
    at: datetime.datetime | None = status.last_sync_at
    millis = None
    if at is not None:
        delta = at - datetime.datetime(1970, 1, 1, tzinfo=datetime.UTC)
        millis = delta // datetime.timedelta(milliseconds=1)
    return {
        "etag": status.etag,
        "lastSyncAt": millis,
        "stream": str(status.stream),
        "lastError": None if status.last_error is None else str(status.last_error),
    }


class Service:
    def __init__(self) -> None:
        self.clients: dict[str, Entry] = {}
        self.lock = threading.Lock()
        self.counter = 0

    def handle(self, method: str, path: str, body: bytes) -> tuple[int, Any]:
        path = path.split("?", 1)[0]
        if method == "GET" and path == "/":
            return 200, {"name": "toggleslash-sdk (Python)", "capabilities": ["integer-accessor"]}
        if method == "POST" and path == "/clients":
            return 201, self.create(self.parse(body))
        match = ROUTE.match(path)
        entry = None
        if match:
            with self.lock:
                entry = self.clients.get(match.group(1))
        if entry is None or entry.client is None or match is None:
            return 404, "no such client"
        return self.act(entry, entry.client, f"{method} {match.group(2)}", body)

    @staticmethod
    def parse(body: bytes) -> dict[str, Any]:
        if not body:
            return {}
        parsed = _json.loads(body)
        if not isinstance(parsed, dict):
            raise TypeError("the body is not a JSON object")
        return parsed

    def act(
        self, entry: Entry, client: toggleslash.Client, action: str, body: bytes
    ) -> tuple[int, Any]:
        if action == "POST ready":
            return 200, {"ready": client.ready()}
        if action == "POST evaluate":
            return 200, self.evaluate(client, self.parse(body))
        if action == "GET status":
            return 200, status_json(client.status())
        if action == "GET hook-events":
            with entry.lock:
                return 200, {"events": list(entry.hook_events)}
        if action == "GET errors":
            with entry.lock:
                messages = list(entry.errors)
            return 200, {"count": len(messages), "messages": messages}
        if action == "POST close":
            client.close()
            return 200, {}
        return 404, "no such route"

    def create(self, body: dict[str, Any]) -> dict[str, Any]:
        entry = Entry()
        options: dict[str, Any] = {
            "url": str(body.get("url", "")),
            "sdk_key": str(body.get("sdkKey", "")),
            "on_error": self.error_recorder(entry, body),
        }
        if isinstance(body.get("stream"), bool):
            options["stream"] = body["stream"]
        for name, option in (
            ("pollIntervalMs", "poll_interval"),
            ("flushIntervalMs", "flush_interval"),
            ("requestTimeoutMs", "request_timeout"),
        ):
            value = body.get(name)
            if isinstance(value, int | float) and not isinstance(value, bool):
                options[option] = value / 1000
        if body.get("hook") is True or body.get("hookThrows") is True:
            options["hooks"] = toggleslash.Hooks(after_evaluation=self.hook_recorder(entry, body))
        entry.client = toggleslash.create(**options)
        with self.lock:
            self.counter += 1
            identifier = str(self.counter)
            self.clients[identifier] = entry
        return {"id": identifier}

    @staticmethod
    def error_recorder(entry: Entry, body: dict[str, Any]) -> Any:
        def record(error: Exception) -> None:
            with entry.lock:
                entry.errors.append(str(error))
            if body.get("onErrorThrows") is True:
                raise RuntimeError("on_error raised, as the test asked")

        return record

    @staticmethod
    def hook_recorder(entry: Entry, body: dict[str, Any]) -> Any:
        def record(event: toggleslash.EvaluationEvent) -> None:
            with entry.lock:
                entry.hook_events.append(event_json(event))
            if body.get("hookThrows") is True:
                raise RuntimeError("the hook raised, as the test asked")

        return record

    @staticmethod
    def evaluate(client: toggleslash.Client, body: dict[str, Any]) -> dict[str, Any]:
        flag = str(body.get("flagKey", ""))
        fallback = body.get("fallback")
        if body.get("accessor") == "int":
            if isinstance(fallback, float) and fallback.is_integer():
                fallback = int(fallback)
            detail = client.int_variation_detail(flag, body.get("context"), fallback)
        else:
            detail = client.variation_detail(flag, body.get("context"), fallback)
        return {
            "value": clean(detail.value),
            "variationIndex": detail.variation_index,
            "reason": reason_json(detail.reason),
        }


SERVICE = Service()


class Handler(BaseHTTPRequestHandler):
    server_version = "toggleslash-test-service"

    def do_GET(self) -> None:
        self.answer()

    def do_POST(self) -> None:
        self.answer()

    def answer(self) -> None:
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        try:
            status, payload = SERVICE.handle(self.command, self.path, body)
        except Exception as error:  # reported, not absorbed: the harness fails the scenario
            detail = "".join(traceback.format_exception(error)[-6:])
            status, payload = 500, f"{type(error).__name__}: {error}\n{detail}"
        if isinstance(payload, str):
            kind, data = "text/plain; charset=utf-8", payload.encode()
        else:
            kind, data = "application/json", json.dumps(clean(payload)).encode()
        self.send_response(status)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, template: str, *args: Any) -> None:
        pass


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
