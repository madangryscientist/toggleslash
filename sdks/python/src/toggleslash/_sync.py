"""Keeps one ruleset current: it fetches with `If-None-Match`, follows the stream when it can and polls when it
cannot. Nothing in here raises out of the thread; every failure goes to `on_error` and leaves the last good
ruleset (and its ETag) in place."""

from __future__ import annotations

import codecs
import threading
from collections.abc import Callable

from . import _json
from ._clock import Clock
from ._errors import BadRulesetError, HttpStatusError, error_code
from ._hold import Hold, parse_retry_after
from ._http import AbortGroup, Response, Transport
from ._ruleset import Ruleset, create_ruleset, is_ruleset_shape
from ._sse import SseParser
from ._types import Status, StreamState

BACKOFF_BASE_SECONDS = 1.0


class RulesetSync:
    def __init__(
        self,
        transport: Transport,
        sdk_key: str,
        clock: Clock,
        jitter: Callable[[], float],
        stream: bool,
        poll_interval: float,
        request_timeout: float,
        watchdog: float,
        on_error: Callable[[BaseException], None],
    ) -> None:
        self._transport = transport
        self._sdk_key = sdk_key
        self._clock = clock
        self._jitter = jitter
        self._stream = stream
        self._poll_interval = poll_interval
        self._timeout = request_timeout
        self._watchdog = watchdog
        self._on_error = on_error
        self._lock = threading.Lock()
        self._ruleset: Ruleset | None = None
        self._etag: str | None = None
        self._last_sync_at: int | None = None
        self._last_error: str | None = None
        self._stream_state = StreamState.CONNECTING if stream else StreamState.OFF
        self._hold = Hold(clock)
        self._group = AbortGroup()
        self.stopped = threading.Event()

    # State, each read one snapshot.

    def held(self) -> tuple[Ruleset | None, str | None]:
        """The ruleset and its ETag, always from the same swap."""
        with self._lock:
            return self._ruleset, self._etag

    def status(self) -> Status:
        with self._lock:
            state = StreamState.CLOSED if self.stopped.is_set() else self._stream_state
            return Status(self._etag, self._last_sync_at, state, self._last_error)

    def stop(self) -> None:
        self.stopped.set()
        self._group.abort()

    def _synced(self) -> None:
        now = self._clock.now_ms()
        with self._lock:
            self._last_sync_at = now
            self._last_error = None

    def _failed(self, error: BaseException) -> None:
        if self.stopped.is_set():
            return
        with self._lock:
            self._last_error = error_code(error)
        self._on_error(error)

    def _set_stream(self, state: StreamState) -> None:
        with self._lock:
            self._stream_state = state

    # The loop.

    def run(self, on_first_load: Callable[[], None]) -> None:
        try:
            self.refresh()
        finally:
            on_first_load()
        failures = 0
        while not self.stopped.is_set():
            # A 429 on the ruleset says wait, and without this the stream would be asked straight after it.
            if self._stream:
                self._sleep(0.0)
            if self.stopped.is_set():
                return
            connected = self._follow() if self._stream else False
            failures = 0 if connected else failures + 1
            self._sleep(self._delay(failures))
            # Sleep first: the ruleset was only just loaded, or the stream only just failed. A stream that was up
            # needs no poll, since reconnecting announces the tag and catches up on its own.
            if not connected and not self.stopped.is_set():
                self.refresh()

    def _sleep(self, seconds: float) -> None:
        wait = max(seconds, self._hold.remaining())
        if wait > 0:
            self._clock.wait(self.stopped, wait)

    def _delay(self, failures: int) -> float:
        if not self._stream:
            return self._poll_interval
        ceiling = min(self._poll_interval, BACKOFF_BASE_SECONDS * 2 ** min(failures, 16))
        return ceiling * (0.5 + self._jitter() / 2)

    def _honour(self, response: Response) -> None:
        if response.status == 429:
            wait = parse_retry_after(response.header("Retry-After"))
            if wait is not None:
                self._hold.extend(wait)

    # One conditional fetch.

    def refresh(self) -> bool:
        try:
            self._fetch()
        except Exception as error:
            self._failed(error)
            return False
        self._synced()
        return True

    def _fetch(self) -> None:
        headers = {"Authorization": f"Bearer {self._sdk_key}"}
        ruleset, etag = self.held()
        if ruleset is not None and etag is not None:
            headers["If-None-Match"] = etag
        with self._transport.open("GET", "/sdk/v1/ruleset", headers, None, self._timeout, self._group) as response:
            if response.status == 304:
                return
            if response.status != 200:
                self._honour(response)
                raise HttpStatusError(response.status, f"GET /sdk/v1/ruleset answered {response.status}")
            body = response.read()
            new_etag = response.header("ETag")
        try:
            parsed = _json.loads(body)
        except (ValueError, RecursionError):
            raise BadRulesetError("GET /sdk/v1/ruleset sent no valid JSON") from None
        if not is_ruleset_shape(parsed):
            raise BadRulesetError("GET /sdk/v1/ruleset sent no flags and segments")
        try:
            built = create_ruleset(parsed)
        except Exception:
            raise BadRulesetError("GET /sdk/v1/ruleset sent a ruleset that could not be read") from None
        with self._lock:
            self._ruleset = built
            self._etag = new_etag

    # The stream.

    def _follow(self) -> bool:
        """Follows `GET /sdk/v1/stream` until it ends or fails. True if it connected."""
        connected = False
        self._set_stream(StreamState.CONNECTING)
        try:
            headers = {"Authorization": f"Bearer {self._sdk_key}", "Accept": "text/event-stream"}
            with self._transport.open("GET", "/sdk/v1/stream", headers, None, self._timeout, self._group) as response:
                if response.status != 200:
                    self._honour(response)
                    raise HttpStatusError(response.status, f"GET /sdk/v1/stream answered {response.status}")
                response.end_request_timer()
                response.set_idle_timeout(self._watchdog)
                connected = self._pump(response)
        except Exception as error:
            self._failed(error)
        finally:
            self._set_stream(StreamState.DISCONNECTED)
        return connected

    def _pump(self, response: Response) -> bool:
        """Reads events until the stream ends. The idle timeout on the socket drops one that goes quiet."""
        decoder = codecs.getincrementaldecoder("utf-8")(errors="replace")
        parser = SseParser()
        connected = False
        while not self.stopped.is_set():
            chunk = response.read_chunk()
            if not chunk:
                return connected
            for event in parser.feed(decoder.decode(chunk)):
                if event.event not in {"changed", "connected"}:
                    continue
                if event.event == "connected":
                    connected = True
                    self._set_stream(StreamState.CONNECTED)
                    with self._lock:
                        self._last_error = None
                if not self._on_tag(event.data):
                    # The fetch the stream asked for failed, and a stream that stays up would never retry it.
                    # End it and count it as not connected, so the backoff and the poll take over.
                    return False
        return connected

    def _on_tag(self, tag: str) -> bool:
        ruleset, etag = self.held()
        if ruleset is not None and tag == etag:
            self._synced()
            return True
        return self.refresh()
