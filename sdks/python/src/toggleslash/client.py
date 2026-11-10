"""The client: evaluation on the caller's thread, and two daemon threads that keep it supplied."""

from __future__ import annotations

import contextlib
import inspect
import math
import threading
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from typing import Any

from . import http
from .clock import SYSTEM_CLOCK, Clock
from .evaluator import ErrorCode, Reason, ReasonKind, Result, evaluate
from .state import StateBox
from .sync import Sync, SyncOptions, default_random
from .types import Detail, EvaluationEvent, Hooks, Status, StreamState, ToggleSlashError
from .usage import UsageReporter

DEFAULT_POLL_INTERVAL = 30.0
DEFAULT_FLUSH_INTERVAL = 30.0
DEFAULT_REQUEST_TIMEOUT = 10.0
STREAM_IDLE_TIMEOUT = 60.0
INT64_MIN = -(2**63)
INT64_MAX = 2**63 - 1
# How long ``close`` waits for a thread that is stuck in the application's own callback.
JOIN_SLACK = 2.0

_NOT_READY = Reason(ReasonKind.ERROR, code=ErrorCode.NOT_READY)
_MISMATCH = Reason(ReasonKind.ERROR, code=ErrorCode.TYPE_MISMATCH)
_FAILED = Reason(ReasonKind.ERROR, code=ErrorCode.EVALUATION_FAILED)


@dataclass(slots=True)
class Seams:
    """What a test replaces. Nothing in the public API reads this."""

    clock: Clock = SYSTEM_CLOCK
    random: Callable[[], float] = default_random
    stream_idle_timeout: float = STREAM_IDLE_TIMEOUT
    evaluate: Callable[[Any, str, object], Result] = field(default=evaluate)


def _duration(value: object, default: float) -> float:
    """A positive, finite number of seconds (a number or a ``timedelta``), else the default."""
    if isinstance(value, timedelta):
        value = value.total_seconds()
    if isinstance(value, bool) or not isinstance(value, int | float):
        return default
    if math.isnan(value) or math.isinf(value) or value <= 0:
        return default
    return float(value)


def _kind(value: object) -> str:
    """The JSON kind of a value."""
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, int | float):
        return "number"
    if isinstance(value, str):
        return "string"
    if isinstance(value, list | tuple):
        return "array"
    return "object"


def _as_int64(value: object) -> int | None:
    """The value as an integer if it is one: no fractional part, inside the signed 64-bit range."""
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        number = value
    elif isinstance(value, float) and math.isfinite(value) and value == math.floor(value):
        number = int(value)
    else:
        return None
    return number if INT64_MIN <= number <= INT64_MAX else None


class Client:
    """A ToggleSlash server client. Build it with :func:`toggleslash.create_client`.

    Nothing here raises at a caller. ``variation`` and ``status`` do no I/O; the network work
    runs on two daemon threads, so a client you never close does not keep the process alive.
    """

    def __init__(
        self,
        *,
        url: str = "",
        sdk_key: str = "",
        stream: bool = True,
        poll_interval: float | timedelta = DEFAULT_POLL_INTERVAL,
        flush_interval: float | timedelta = DEFAULT_FLUSH_INTERVAL,
        request_timeout: float | timedelta = DEFAULT_REQUEST_TIMEOUT,
        hooks: Hooks | None = None,
        on_error: Callable[[Exception], object] | None = None,
        _seams: Seams | None = None,
    ) -> None:
        seams = _seams or Seams()
        self._clock = seams.clock
        self._evaluate = seams.evaluate
        self._on_error = on_error if callable(on_error) else None
        self._after = self._read_hook(hooks)
        self._request_timeout = _duration(request_timeout, DEFAULT_REQUEST_TIMEOUT)
        self._flush_interval = _duration(flush_interval, DEFAULT_FLUSH_INTERVAL)
        stream_on = stream if isinstance(stream, bool) else True
        base = http.Base(url.rstrip("/") if isinstance(url, str) else "")
        key = sdk_key if isinstance(sdk_key, str) else str(sdk_key)

        self._box = StateBox(stream_on)
        self._guards = http.GuardSet()
        self._stop = threading.Event()
        self._usage = UsageReporter(base, key, self._clock, self._request_timeout, self._report)
        self._sync = Sync(
            SyncOptions(
                base=base,
                sdk_key=key,
                stream=stream_on,
                poll_interval=_duration(poll_interval, DEFAULT_POLL_INTERVAL),
                request_timeout=self._request_timeout,
                stream_idle=_duration(seams.stream_idle_timeout, STREAM_IDLE_TIMEOUT),
                clock=self._clock,
                random=seams.random,
                box=self._box,
                guards=self._guards,
                stop=self._stop,
                report=self._report,
                first_load_done=self._first_load_done,
            )
        )
        self._ready_lock = threading.Lock()
        self._ready_settled = threading.Event()
        self._ready_value = False
        self._close_lock = threading.RLock()
        self._closing = False
        self._closed = False
        self._sync_thread = threading.Thread(target=self._sync.run, name="toggleslash-sync", daemon=True)
        self._flush_thread = threading.Thread(target=self._flush_loop, name="toggleslash-flush", daemon=True)
        self._started: list[threading.Thread] = []
        self._start()

    # -- start -------------------------------------------------------------------------------

    def _start(self) -> None:
        for thread in (self._sync_thread, self._flush_thread):
            try:
                thread.start()
                self._started.append(thread)
            except Exception as error:
                self._report(error)
                if thread is self._sync_thread:
                    self._settle_ready(False)

    @staticmethod
    def _read_hook(hooks: object) -> Callable[[EvaluationEvent], object] | None:
        try:
            after = getattr(hooks, "after_evaluation", None)
        except Exception:
            return None
        return after if callable(after) else None

    def _flush_loop(self) -> None:
        while not self._stop.is_set():
            self._clock.wait(self._stop, self._flush_interval)
            if self._stop.is_set():
                return
            self._usage.flush()

    # -- errors ------------------------------------------------------------------------------

    def _report(self, error: Exception) -> None:
        """Tell ``on_error``. It runs on the calling thread with no lock held; what it raises is dropped."""
        handler = self._on_error
        if handler is None:
            return
        with contextlib.suppress(Exception):
            handler(error)

    # -- ready -------------------------------------------------------------------------------

    def _settle_ready(self, value: bool) -> None:
        with self._ready_lock:
            if self._ready_settled.is_set():
                return
            self._ready_value = value
            self._ready_settled.set()

    def _first_load_done(self) -> None:
        self._settle_ready(self._box.get().ruleset is not None)

    def ready(self) -> bool:
        """Wait for the first load attempt: True if a ruleset loaded, False if it failed or the client closed.

        The answer is fixed after that first attempt. Called on the SDK's own sync thread (from
        ``on_error`` or a hook), it answers at once from what is held, since waiting there would never end.
        """
        try:
            if threading.current_thread() is self._sync_thread:
                return self._box.get().ruleset is not None
            self._ready_settled.wait(self._request_timeout * 2 + 5.0)
            if not self._ready_settled.is_set():
                return self._box.get().ruleset is not None
            return self._ready_value
        except Exception as error:
            self._report(error)
            return False

    # -- evaluation --------------------------------------------------------------------------

    def _flag_version(self, state: Any, flag_key: str) -> int | None:
        try:
            ruleset = state.ruleset
            if ruleset is None:
                return None
            entry = ruleset.flags.get(flag_key)
            if entry is None or entry.status != "ready" or entry.header is None:
                return None
            return int(entry.header["version"])
        except Exception:
            return None

    def _decide(self, state: Any, flag_key: str, context: object, fallback: object, accessor: str) -> Detail:
        """Contract section 4, steps 1 to 4. May raise; the caller turns that into EVALUATION_FAILED."""
        if state.ruleset is None:
            return Detail(fallback, None, _NOT_READY)
        result = self._evaluate(state.ruleset, flag_key, context)
        if result.variation_index is None:
            return Detail(fallback, None, result.reason)
        value = result.value
        if accessor == "int":
            integer = _as_int64(value)
            if integer is None:
                return Detail(fallback, None, _MISMATCH)
            value = integer
        elif not self._kind_matches(value, fallback, accessor):
            return Detail(fallback, None, _MISMATCH)
        elif accessor == "float":
            value = float(value)
        self._usage.record(flag_key, result.variation_index)
        return Detail(value, result.variation_index, result.reason)

    @staticmethod
    def _kind_matches(value: object, fallback: object, accessor: str) -> bool:
        kind = _kind(value)
        if accessor == "bool":
            return kind == "boolean"
        if accessor == "string":
            return kind == "string"
        if accessor == "float":
            return kind == "number"
        if accessor == "json" and _kind(fallback) not in ("array", "object"):
            return kind in ("array", "object")
        if fallback is None:
            return True
        return kind == _kind(fallback)

    def _detail(self, flag_key: str, context: object, fallback: object, accessor: str) -> Detail:
        state = self._box.get()  # one read: the hook's ETag and the ruleset it evaluated go together
        try:
            detail = self._decide(state, flag_key, context, fallback, accessor)
        except Exception as error:
            self._report(error)
            detail = Detail(fallback, None, _FAILED)
        self._run_hook(state, flag_key, context, detail)
        return detail

    def _run_hook(self, state: Any, flag_key: str, context: object, detail: Detail) -> None:
        hook = self._after
        if hook is None:
            return
        try:
            event = EvaluationEvent(
                flag_key=flag_key,
                context=context,
                value=detail.value,
                variation_index=detail.variation_index,
                reason=detail.reason,
                etag=state.etag,
                flag_version=self._flag_version(state, flag_key),
            )
            outcome = hook(event)
            if inspect.iscoroutine(outcome):
                outcome.close()  # an async hook has no loop to run on; never leave it un-awaited
                raise ToggleSlashError("after_evaluation returned a coroutine; hooks must be plain functions")
        except Exception as error:
            self._report(error)

    def _safe(self, flag_key: str, context: object, fallback: object, accessor: str) -> Detail:
        try:
            return self._detail(flag_key, context, fallback, accessor)
        except Exception as error:
            self._report(error)
            return Detail(fallback, None, _FAILED)

    # -- the accessors -----------------------------------------------------------------------

    def variation_detail(self, flag_key: str, context: object, fallback: object = None) -> Detail:
        """The value, variation index and reason. A null fallback accepts any served kind."""
        return self._safe(flag_key, context, fallback, "any")

    def variation(self, flag_key: str, context: object, fallback: object = None) -> Any:
        return self.variation_detail(flag_key, context, fallback).value

    def bool_variation_detail(self, flag_key: str, context: object, fallback: bool) -> Detail:
        return self._safe(flag_key, context, fallback, "bool")

    def bool_variation(self, flag_key: str, context: object, fallback: bool) -> bool:
        return self.bool_variation_detail(flag_key, context, fallback).value

    def string_variation_detail(self, flag_key: str, context: object, fallback: str) -> Detail:
        return self._safe(flag_key, context, fallback, "string")

    def string_variation(self, flag_key: str, context: object, fallback: str) -> str:
        return self.string_variation_detail(flag_key, context, fallback).value

    def float_variation_detail(self, flag_key: str, context: object, fallback: float) -> Detail:
        return self._safe(flag_key, context, fallback, "float")

    def float_variation(self, flag_key: str, context: object, fallback: float) -> float:
        return self.float_variation_detail(flag_key, context, fallback).value

    def int_variation_detail(self, flag_key: str, context: object, fallback: int) -> Detail:
        """A number with no fractional part inside the signed 64-bit range; ``2.0`` is ``2``."""
        return self._safe(flag_key, context, fallback, "int")

    def int_variation(self, flag_key: str, context: object, fallback: int) -> int:
        return self.int_variation_detail(flag_key, context, fallback).value

    def json_variation_detail(self, flag_key: str, context: object, fallback: object) -> Detail:
        """An array or an object. With an array or object fallback, the kind must be the same one."""
        return self._safe(flag_key, context, fallback, "json")

    def json_variation(self, flag_key: str, context: object, fallback: object) -> Any:
        return self.json_variation_detail(flag_key, context, fallback).value

    # -- status, flush, close ----------------------------------------------------------------

    def status(self) -> Status:
        """One consistent snapshot of what is loaded and how the stream is doing. No I/O."""
        try:
            state = self._box.get()
            synced = None if state.last_sync_at is None else datetime.fromtimestamp(state.last_sync_at, UTC)
            return Status(state.etag, synced, state.stream, state.last_error)
        except Exception as error:
            self._report(error)
            return Status(None, None, StreamState.CLOSED if self._closed else StreamState.OFF, None)

    def flush(self) -> None:
        """Send the pending usage counts now, under the same rules as the timed flush."""
        try:
            self._usage.flush()
        except Exception as error:
            self._report(error)

    def close(self) -> None:
        """Stop following the server, send the last usage counts and return. Safe to repeat and to race."""
        try:
            with self._close_lock:
                if self._closing:
                    return  # done, or a nested call from a callback that close itself triggered
                self._closing = True
                try:
                    self._shutdown()
                finally:
                    self._closed = True
        except Exception as error:
            self._report(error)

    def _shutdown(self) -> None:
        self._stop.set()
        self._usage.stop_counting()
        self._box.close()
        self._guards.abort_all()
        limit = self._request_timeout + JOIN_SLACK
        current = threading.current_thread()
        for thread in self._started:
            if thread is not current:
                thread.join(limit)
        self._settle_ready(False)
        self._usage.finish()

    def __enter__(self) -> Client:
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()


def create_client(
    *,
    url: str = "",
    sdk_key: str = "",
    stream: bool = True,
    poll_interval: float | timedelta = DEFAULT_POLL_INTERVAL,
    flush_interval: float | timedelta = DEFAULT_FLUSH_INTERVAL,
    request_timeout: float | timedelta = DEFAULT_REQUEST_TIMEOUT,
    hooks: Hooks | None = None,
    on_error: Callable[[Exception], object] | None = None,
) -> Client:
    """Start a client. It returns at once; loading happens on a background thread (see ``Client.ready``)."""
    return Client(
        url=url,
        sdk_key=sdk_key,
        stream=stream,
        poll_interval=poll_interval,
        flush_interval=flush_interval,
        request_timeout=request_timeout,
        hooks=hooks,
        on_error=on_error,
    )
