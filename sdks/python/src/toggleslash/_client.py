"""The client: options, evaluation, the sync loop, usage and close (contract 2 to 12)."""

from __future__ import annotations

import dataclasses
import math
import random
import threading
import time
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any

from . import _json
from ._evaluate import Ruleset, evaluate, load_ruleset
from ._http import (
    STREAM_IDLE_SECONDS,
    Aborted,
    Aborts,
    Endpoint,
    NetworkFailure,
    RequestTimeout,
    Transport,
)
from ._sse import SseParser
from ._types import (
    Detail,
    ErrorCode,
    EvaluationEvent,
    Hooks,
    LastError,
    Reason,
    ReasonKind,
    Status,
    StreamState,
    ToggleSlashError,
)
from ._usage import Usage, retry_after

DEFAULT_POLL_SECONDS = 30.0
DEFAULT_FLUSH_SECONDS = 30.0
DEFAULT_TIMEOUT_SECONDS = 10.0
MAX_WAIT_SECONDS = 31_536_000.0
INT64_MIN = -(2**63)
INT64_MAX = 2**63 - 1

Evaluator = Callable[[Ruleset, Any, Any], tuple[Any, "int | None", Reason]]


class SystemClock:
    """Real time. A test passes a clock with the same three methods."""

    def monotonic(self) -> float:
        return time.monotonic()

    def now(self) -> datetime:
        return datetime.now(UTC)

    def wait(self, event: threading.Event, seconds: float) -> bool:
        """Wait for ``event`` up to ``seconds``; True when it was set."""
        return event.wait(max(0.0, min(seconds, MAX_WAIT_SECONDS)))


def _jitter() -> float:
    return 0.5 + random.random() * 0.5


@dataclasses.dataclass(frozen=True, slots=True)
class _Held:
    """The ruleset and the ETag that names it: swapped together by one assignment."""

    ruleset: Ruleset | None
    etag: str | None


@dataclasses.dataclass(frozen=True, slots=True)
class _State:
    last_sync_at: datetime | None
    stream: StreamState
    last_error: str | None


def _duration(value: Any, default: float) -> float:
    if isinstance(value, bool) or not isinstance(value, int | float):
        return default
    try:
        seconds = float(value)
    except OverflowError:
        return default
    if math.isnan(seconds) or seconds <= 0:
        return default
    return min(seconds, MAX_WAIT_SECONDS)


def _expected_kind(fallback: Any) -> str:
    if fallback is None:
        return "any"
    if isinstance(fallback, bool):
        return "boolean"
    if isinstance(fallback, int | float):
        return "number"
    if isinstance(fallback, str):
        return "string"
    if isinstance(fallback, list | tuple):
        return "array"
    if isinstance(fallback, dict):
        return "object"
    return "invalid"


def _served_matches(kind: str, value: Any) -> bool:
    if kind == "any":
        return True
    if value is None or kind == "invalid":
        return False
    if kind == "boolean":
        return isinstance(value, bool)
    if kind == "number":
        return isinstance(value, int | float) and not isinstance(value, bool)
    if kind == "string":
        return isinstance(value, str)
    if kind == "array":
        return isinstance(value, list)
    return isinstance(value, dict)


def _as_int64(value: Any) -> int | None:
    """The served number as an integer, or None when it has a fraction or leaves 64 bits."""
    if isinstance(value, bool) or not isinstance(value, int | float):
        return None
    if isinstance(value, float):
        if not math.isfinite(value) or not value.is_integer():
            return None
        value = int(value)
    return value if INT64_MIN <= value <= INT64_MAX else None


def _error_detail(fallback: Any, code: ErrorCode) -> Detail:
    return Detail(fallback, None, Reason(ReasonKind.ERROR, code=code))


class Client:
    """A ToggleSlash server SDK client. Build one with :func:`toggleslash.create`."""

    def __init__(
        self,
        url: Any,
        sdk_key: Any,
        *,
        stream: bool = True,
        poll_interval: Any = DEFAULT_POLL_SECONDS,
        flush_interval: Any = DEFAULT_FLUSH_SECONDS,
        request_timeout: Any = DEFAULT_TIMEOUT_SECONDS,
        hooks: Any = None,
        on_error: Any = None,
        _transport: Any = None,
        _clock: Any = None,
        _jitter_source: Callable[[], float] | None = None,
        _stream_watchdog: float = STREAM_IDLE_SECONDS,
        _evaluator: Evaluator | None = None,
    ) -> None:
        base = url.rstrip("/") if isinstance(url, str) else ""
        self._endpoint = Endpoint(base)
        self._headers = {"Authorization": "Bearer " + (sdk_key if isinstance(sdk_key, str) else "")}
        self._stream_on = stream is not False
        self._poll_interval = _duration(poll_interval, DEFAULT_POLL_SECONDS)
        self._flush_interval = _duration(flush_interval, DEFAULT_FLUSH_SECONDS)
        self._request_timeout = _duration(request_timeout, DEFAULT_TIMEOUT_SECONDS)
        hook = getattr(hooks, "after_evaluation", None) if hooks is not None else None
        self._after_evaluation = hook if callable(hook) else None
        self._on_error = on_error if callable(on_error) else None
        self._transport = _transport if _transport is not None else Transport()
        self._clock = _clock if _clock is not None else SystemClock()
        self._jitter = _jitter_source if _jitter_source is not None else _jitter
        self._watchdog = _stream_watchdog
        self._evaluator: Evaluator = _evaluator if _evaluator is not None else evaluate

        self._held = _Held(None, None)
        self._state_lock = threading.Lock()
        self._state = _State(None, StreamState.CONNECTING if self._stream_on else StreamState.OFF, None)
        self._closing = threading.Event()
        self._aborts = Aborts()
        self._hold_until = 0.0
        self._ready_event = threading.Event()
        self._ready_value = False
        self._close_lock = threading.Lock()
        self._close_started = False
        self._closed = threading.Event()
        self._usage = Usage(
            endpoint=self._endpoint,
            headers=self._headers,
            transport=self._transport,
            monotonic=self._clock.monotonic,
            request_timeout=self._request_timeout,
            report=self._report_usage,
        )
        self._sync_thread = threading.Thread(target=self._run_sync, name="toggleslash-sync", daemon=True)
        self._flush_thread = threading.Thread(target=self._run_flush, name="toggleslash-flush", daemon=True)

    def _start(self) -> None:
        self._sync_thread.start()
        self._flush_thread.start()

    def __enter__(self) -> Client:
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()

    # ------------------------------------------------------------------ callbacks

    def _notify(self, error: BaseException) -> None:
        """Hand an error to ``on_error``. No SDK lock is held; whatever it raises is dropped."""
        callback = self._on_error
        if callback is None:
            return
        try:
            callback(error)
        except Exception:
            pass

    def _report_usage(self, code: str, message: str) -> None:
        self._notify(ToggleSlashError(message, code))

    # ------------------------------------------------------------------ evaluation

    def variation(self, flag_key: str, context: Any, fallback: Any) -> Any:
        """The flag's value for ``context``, or ``fallback``. Never raises."""
        return self._detail(flag_key, context, fallback, False).value

    def variation_detail(self, flag_key: str, context: Any, fallback: Any) -> Detail:
        """Like :meth:`variation`, with the variation index and the reason."""
        return self._detail(flag_key, context, fallback, False)

    def int_variation(self, flag_key: str, context: Any, fallback: int) -> int:
        """The integer accessor: a fraction or a value beyond signed 64 bits is ``TYPE_MISMATCH``."""
        return self._detail(flag_key, context, fallback, True).value

    def int_variation_detail(self, flag_key: str, context: Any, fallback: int) -> Detail:
        return self._detail(flag_key, context, fallback, True)

    def _detail(self, flag_key: Any, context: Any, fallback: Any, integer: bool) -> Detail:
        held = self._held
        failure: BaseException | None = None
        try:
            detail = self._compute(held, flag_key, context, fallback, integer)
        except Exception as error:
            failure = error
            detail = _error_detail(fallback, ErrorCode.EVALUATION_FAILED)
        if failure is not None:
            self._notify(
                ToggleSlashError("evaluation failed: " + type(failure).__name__, "evaluation_failed")
            )
        self._run_hook(held, flag_key, context, detail)
        return detail

    def _compute(self, held: _Held, flag_key: Any, context: Any, fallback: Any, integer: bool) -> Detail:
        ruleset = held.ruleset
        if ruleset is None:
            return _error_detail(fallback, ErrorCode.NOT_READY)
        value, index, reason = self._evaluator(ruleset, flag_key, context)
        if index is None:
            return Detail(fallback, None, reason)
        kind = _expected_kind(fallback)
        if integer:
            if kind != "number":
                return _error_detail(fallback, ErrorCode.TYPE_MISMATCH)
            whole = _as_int64(value)
            if whole is None:
                return _error_detail(fallback, ErrorCode.TYPE_MISMATCH)
            value = whole
        elif not _served_matches(kind, value):
            return _error_detail(fallback, ErrorCode.TYPE_MISMATCH)
        self._usage.add(flag_key, index, self._clock.now().astimezone(UTC).strftime("%Y-%m-%d"))
        return Detail(value, index, reason)

    def _run_hook(self, held: _Held, flag_key: Any, context: Any, detail: Detail) -> None:
        hook = self._after_evaluation
        if hook is None:
            return
        version: int | None = None
        try:
            version = held.ruleset.flag_version(flag_key) if held.ruleset is not None else None
        except Exception:
            version = None
        try:
            hook(
                EvaluationEvent(
                    flag_key,
                    context,
                    detail.value,
                    detail.variation_index,
                    detail.reason,
                    held.etag,
                    version,
                )
            )
        except Exception as error:
            self._notify(error)

    # ------------------------------------------------------------------ ready, status

    def ready(self) -> bool:
        """Block until the first ruleset attempt has settled (bounded). True if a ruleset is held."""
        if self._ready_event.is_set():
            return self._ready_value
        if threading.current_thread() in (self._sync_thread, self._flush_thread):
            return self._held.ruleset is not None
        self._ready_event.wait(self._request_timeout + 1.0)
        self._settle(self._held.ruleset is not None)
        return self._ready_value

    def _settle(self, value: bool) -> None:
        with self._state_lock:
            if self._ready_event.is_set():
                return
            self._ready_value = value
            self._ready_event.set()

    def status(self) -> Status:
        """One snapshot of the ETag, the last sync, the stream and the last error."""
        with self._state_lock:
            held, state = self._held, self._state
        return Status(held.etag, state.last_sync_at, state.stream, state.last_error)

    def _set_stream(self, stream: StreamState) -> None:
        with self._state_lock:
            if self._state.stream is StreamState.CLOSED:
                return
            self._state = dataclasses.replace(self._state, stream=stream)

    def _synced(self, held: _Held | None) -> None:
        now = self._clock.now().astimezone(UTC)
        with self._state_lock:
            if held is not None:
                self._held = held
            self._state = dataclasses.replace(self._state, last_sync_at=now, last_error=None)

    def _clear_error(self) -> None:
        with self._state_lock:
            self._state = dataclasses.replace(self._state, last_error=None)

    def _sync_error(self, code: str, message: str) -> None:
        """A failure of a ruleset or stream request. Nothing is set for a failure close caused."""
        with self._state_lock:
            if self._closing.is_set():
                return
            self._state = dataclasses.replace(self._state, last_error=code)
        self._notify(ToggleSlashError(message, code))

    # ------------------------------------------------------------------ the sync thread

    def _run_sync(self) -> None:
        try:
            try:
                self._fetch_ruleset()
            except Exception:
                pass
            finally:
                self._settle(self._held.ruleset is not None)
            self._loop()
        except Exception:
            pass

    def _hold(self, seconds: float) -> None:
        until = self._clock.monotonic() + seconds
        with self._state_lock:
            self._hold_until = max(self._hold_until, until)

    def _wait_hold(self) -> None:
        while not self._closing.is_set():
            remaining = self._hold_until - self._clock.monotonic()
            if remaining <= 0:
                return
            if self._clock.wait(self._closing, remaining):
                return

    def _loop(self) -> None:
        failures = 0
        while not self._closing.is_set():
            if self._stream_on:
                self._wait_hold()
                if self._closing.is_set():
                    return
            connected = self._follow() if self._stream_on else False
            if self._closing.is_set():
                return
            failures = 0 if connected else failures + 1
            if self._stream_on:
                ceiling = min(self._poll_interval, 2.0 ** min(failures, 16))
                delay = ceiling * self._jitter()
            else:
                delay = self._poll_interval
            if self._clock.wait(self._closing, delay):
                return
            self._wait_hold()
            if self._closing.is_set():
                return
            if not connected:
                self._fetch_ruleset()

    def _fetch_ruleset(self) -> bool:
        """The ruleset request of 7.1. True when it was a sync."""
        headers = {"Accept": "application/json", **self._headers}
        held = self._held
        if held.ruleset is not None and held.etag is not None:
            headers["If-None-Match"] = held.etag
        try:
            response = self._transport.fetch(
                self._endpoint,
                "GET",
                "/sdk/v1/ruleset",
                headers,
                None,
                self._request_timeout,
                self._aborts,
            )
        except RequestTimeout:
            self._sync_error(LastError.TIMEOUT, "ruleset request failed: timeout")
            return False
        except Aborted:
            return False
        except NetworkFailure:
            self._sync_error(LastError.NETWORK, "ruleset request failed: network")
            return False
        status = response.status
        if status == 304:
            self._synced(None)
            return True
        if status == 200:
            return self._accept(response)
        if status == 429:
            seconds = retry_after(response.headers)
            if seconds is not None:
                self._hold(seconds)
        code = f"http_{status}"
        self._sync_error(code, f"ruleset request failed: {code}")
        return False

    def _accept(self, response: Any) -> bool:
        try:
            raw = _json.loads(response.body)
        except (ValueError, RecursionError):
            raw = None
        if not (
            isinstance(raw, dict)
            and isinstance(raw.get("flags"), list)
            and isinstance(raw.get("segments"), list)
        ):
            self._sync_error(LastError.BAD_RULESET, "ruleset request failed: bad_ruleset")
            return False
        etag = response.headers.get("ETag") if response.headers is not None else None
        self._synced(_Held(load_ruleset(raw), etag if isinstance(etag, str) else None))
        return True

    # ------------------------------------------------------------------ the stream

    def _follow(self) -> bool:
        """One stream attempt (7.2). True when it connected."""
        self._set_stream(StreamState.CONNECTING)
        stream = None
        try:
            try:
                stream = self._transport.open_stream(
                    self._endpoint,
                    "/sdk/v1/stream",
                    {"Accept": "text/event-stream", **self._headers},
                    self._request_timeout,
                    self._watchdog,
                    self._aborts,
                )
            except RequestTimeout:
                self._sync_error(LastError.TIMEOUT, "stream request failed: timeout")
                return False
            except Aborted:
                return False
            except NetworkFailure:
                self._sync_error(LastError.NETWORK, "stream request failed: network")
                return False
            if stream.status != 200 or stream.bodyless:
                if stream.status == 429:
                    seconds = retry_after(stream.headers)
                    if seconds is not None:
                        self._hold(seconds)
                code = f"http_{stream.status}"
                self._sync_error(code, f"stream request failed: {code}")
                return False
            return self._read_events(stream)
        finally:
            if stream is not None:
                stream.close()
            self._set_stream(StreamState.DISCONNECTED)

    def _read_events(self, stream: Any) -> bool:
        parser = SseParser()
        connected = False
        while True:
            try:
                chunk = stream.read()
            except RequestTimeout:
                self._sync_error(LastError.TIMEOUT, "stream failed: timeout")
                return connected
            except Aborted:
                return connected
            except NetworkFailure:
                self._sync_error(LastError.NETWORK, "stream failed: network")
                return connected
            if not chunk:
                return connected
            for name, data in parser.feed(chunk):
                if name == "connected":
                    connected = True
                    self._set_stream(StreamState.CONNECTED)
                    self._clear_error()
                elif name != "changed":
                    continue
                if self._closing.is_set():
                    return connected
                held = self._held
                if held.ruleset is not None and held.etag is not None and held.etag == data:
                    self._synced(None)
                elif not self._fetch_ruleset():
                    return False

    # ------------------------------------------------------------------ usage and close

    def _run_flush(self) -> None:
        while not self._clock.wait(self._closing, self._flush_interval):
            try:
                self._usage.flush()
            except Exception:
                pass

    def close(self) -> None:
        """Stop, send the final usage flush and return. Safe twice, and from many threads."""
        with self._close_lock:
            leader = not self._close_started
            self._close_started = True
        if not leader:
            self._closed.wait(4 * self._request_timeout)
            return
        try:
            with self._state_lock:
                self._closing.set()
                self._state = dataclasses.replace(self._state, stream=StreamState.CLOSED)
            self._aborts.abort_all()
            deadline = time.monotonic() + self._request_timeout
            me = threading.current_thread()
            for thread in (self._sync_thread, self._flush_thread):
                if thread is not me and thread.is_alive():
                    thread.join(max(0.0, deadline - time.monotonic()))
            self._usage.stop()
            self._final_flush()
            self._settle(False)
        finally:
            self._closed.set()

    def _final_flush(self) -> None:
        try:
            self._usage.flush(lock_timeout=self._request_timeout)
        except Exception:
            pass
        pending = self._usage.pending()
        if pending and self._usage.holding():
            self._notify(
                ToggleSlashError(
                    f"closed with {pending} usage counts unsent: the server asked to wait",
                    "usage_unsent",
                )
            )


def create(
    url: str,
    sdk_key: str,
    *,
    stream: bool = True,
    poll_interval: float = DEFAULT_POLL_SECONDS,
    flush_interval: float = DEFAULT_FLUSH_SECONDS,
    request_timeout: float = DEFAULT_TIMEOUT_SECONDS,
    hooks: Hooks | None = None,
    on_error: Callable[[BaseException], Any] | None = None,
    _transport: Any = None,
    _clock: Any = None,
    _jitter_source: Callable[[], float] | None = None,
    _stream_watchdog: float = STREAM_IDLE_SECONDS,
    _evaluator: Evaluator | None = None,
) -> Client:
    """Start a client: two daemon threads, and the first ruleset request at once. It returns
    immediately; call ``client.ready()`` to wait for the first answer."""
    client = Client(
        url,
        sdk_key,
        stream=stream,
        poll_interval=poll_interval,
        flush_interval=flush_interval,
        request_timeout=request_timeout,
        hooks=hooks,
        on_error=on_error,
        _transport=_transport,
        _clock=_clock,
        _jitter_source=_jitter_source,
        _stream_watchdog=_stream_watchdog,
        _evaluator=_evaluator,
    )
    client._start()
    return client
