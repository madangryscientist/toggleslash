"""Keeps one ruleset current: the first load, the stream, the loop with its backoff, and ``429`` holds."""

from __future__ import annotations

import random as random_module
import threading
from collections.abc import Callable
from dataclasses import dataclass

from . import _json, http
from .clock import Clock
from .evaluator import create_ruleset
from .sse import SseParser
from .state import StateBox
from .types import StreamState, ToggleSlashError

BACKOFF_BASE = 1.0
STREAM_IDLE = 60.0


@dataclass(frozen=True, slots=True)
class SyncOptions:
    base: http.Base
    sdk_key: str
    stream: bool
    poll_interval: float
    request_timeout: float
    stream_idle: float
    clock: Clock
    random: Callable[[], float]
    box: StateBox
    guards: http.GuardSet
    stop: threading.Event
    report: Callable[[Exception], None]
    first_load_done: Callable[[], None]


class _Failed(Exception):
    """A ruleset or stream attempt that failed; ``code`` is a ``lastError`` code."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


def _has_ruleset_shape(body: object) -> bool:
    return (
        isinstance(body, dict)
        and isinstance(body.get("flags"), list)
        and isinstance(body.get("segments"), list)
    )


class Sync:
    """Runs on the sync thread. Nothing in here raises out of ``run``: every failure becomes
    ``last_error`` and a call to ``report``, and the last good ruleset stays in place."""

    def __init__(self, options: SyncOptions) -> None:
        self._o = options
        self._hold_until = 0.0
        self._connected = False

    # -- the loop (contract 7.3) -------------------------------------------------------------

    def run(self) -> None:
        o = self._o
        try:
            self.refresh()
        finally:
            o.first_load_done()
        failures = 0
        while not o.stop.is_set():
            try:
                if o.stream:
                    self._sleep(0.0)
                if o.stop.is_set():
                    return
                connected = self._follow() if o.stream else False
                failures = 0 if connected else failures + 1
                self._sleep(self._delay(failures))
                if not connected and not o.stop.is_set():
                    self.refresh()
            except Exception as error:
                if o.stop.is_set():
                    return
                failures += 1
                self._record(_Failed("network", "sync failed unexpectedly"), error)
                self._sleep(self._delay(failures))

    def _sleep(self, seconds: float) -> None:
        o = self._o
        wait = max(seconds, self._hold_until - o.clock.monotonic())
        if wait > 0 and not o.stop.is_set():
            o.clock.wait(o.stop, wait)

    def _delay(self, failures: int) -> float:
        o = self._o
        if not o.stream:
            return o.poll_interval
        ceiling = min(o.poll_interval, BACKOFF_BASE * 2.0 ** min(failures, 16))
        return ceiling * (0.5 + o.random() / 2.0)

    def _hold(self, status: int, headers) -> None:
        wait = http.retry_after(status, headers)
        if wait is not None:
            self._hold_until = max(self._hold_until, self._o.clock.monotonic() + wait)

    # -- bookkeeping -------------------------------------------------------------------------

    def _synced(self) -> None:
        self._o.box.update(last_sync_at=self._o.clock.now(), last_error=None)

    def _record(self, failure: _Failed, cause: BaseException | None = None) -> None:
        """Set ``last_error`` and tell ``on_error``, unless ``close`` caused the failure."""
        o = self._o
        if o.stop.is_set():
            return
        o.box.update(last_error=failure.code)
        error = ToggleSlashError(str(failure), failure.code)
        error.__cause__ = cause
        o.report(error)

    # -- the ruleset (contract 7.1) ----------------------------------------------------------

    def refresh(self) -> bool:
        """One conditional fetch of the ruleset. True if it is a sync."""
        o = self._o
        state = o.box.get()
        headers = {"Authorization": f"Bearer {o.sdk_key}"}
        if state.etag is not None and state.ruleset is not None:
            headers["If-None-Match"] = state.etag
        try:
            answer = http.request(
                o.base,
                o.guards,
                o.clock,
                "GET",
                "/sdk/v1/ruleset",
                headers,
                timeout=o.request_timeout,
                read_body=lambda status: status == 200,
            )
            if answer.status == 304:
                self._synced()
                return True
            if answer.status != 200:
                self._hold(answer.status, answer.headers)
                raise _Failed(f"http_{answer.status}", f"GET /sdk/v1/ruleset answered {answer.status}")
            ruleset, etag = self._read_ruleset(answer)
            o.box.update(
                ruleset=ruleset,
                etag=etag,
                last_sync_at=o.clock.now(),
                last_error=None,
            )
        except http.Aborted:
            return False
        except http.RequestFailure as failure:
            self._record(_Failed(failure.code, f"GET /sdk/v1/ruleset failed: {failure.code}"), failure)
            return False
        except _Failed as failure:
            self._record(failure)
            return False
        else:
            return True

    def _read_ruleset(self, answer: http.Answer):
        try:
            body = _json.parse(answer.body)
        except _json.JSONError as error:
            raise _Failed("bad_ruleset", "GET /sdk/v1/ruleset sent a body that is not JSON") from error
        if not _has_ruleset_shape(body):
            raise _Failed("bad_ruleset", "GET /sdk/v1/ruleset sent no flags and segments")
        return create_ruleset(body), answer.headers.get("ETag")

    # -- the stream (contract 7.2) -----------------------------------------------------------

    def _follow(self) -> bool:
        """Follow ``GET /sdk/v1/stream`` until it ends or fails. True if it connected."""
        o = self._o
        o.box.update(stream=StreamState.CONNECTING)
        self._connected = False
        stream: http.Stream | None = None
        try:
            stream = http.open_stream(
                o.base,
                o.guards,
                o.clock,
                "/sdk/v1/stream",
                {"Authorization": f"Bearer {o.sdk_key}", "Accept": "text/event-stream"},
                open_timeout=o.request_timeout,
                idle_timeout=o.stream_idle,
            )
            if stream.status != 200:
                self._hold(stream.status, stream.headers)
                raise _Failed(f"http_{stream.status}", f"GET /sdk/v1/stream answered {stream.status}")
            self._pump(stream)
        except http.Aborted:
            pass
        except http.RequestFailure as failure:
            self._record(_Failed(failure.code, f"GET /sdk/v1/stream failed: {failure.code}"), failure)
        except _Failed as failure:
            self._record(failure)
        finally:
            if stream is not None:
                stream.close()
            o.box.update(stream=StreamState.DISCONNECTED)
        return self._connected

    def _pump(self, stream: http.Stream) -> None:
        """Read events until the stream ends. The socket's idle timeout is the watchdog."""
        o = self._o
        parser = SseParser()
        while not o.stop.is_set():
            chunk = stream.read()
            if not chunk:
                return
            for event in parser.feed(chunk):
                if event.event not in ("connected", "changed"):
                    continue
                if event.event == "connected":
                    self._connected = True
                    o.box.update(stream=StreamState.CONNECTED, last_error=None)
                if not self._on_tag(event.data):
                    # The fetch the stream asked for failed, and a stream that stays up would never
                    # retry it: end it and count it as not connected, so the backoff and the poll take over.
                    self._connected = False
                    return
                if o.stop.is_set():
                    return

    def _on_tag(self, tag: str) -> bool:
        """The stream names the ruleset's ETag; fetch only when it is not the one held."""
        state = self._o.box.get()
        if state.ruleset is not None and tag == state.etag:
            self._synced()
            return True
        return self.refresh()


def default_random() -> float:
    return random_module.random()
