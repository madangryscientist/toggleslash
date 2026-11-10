"""The client: `create` starts two daemon threads (the ruleset loop and the usage flush loop) and everything a
caller does afterwards is a local read."""

from __future__ import annotations

import contextlib
import copy
import math
import threading
from collections.abc import Callable
from datetime import timedelta
from typing import Any

from ._errors import SdkError
from ._evaluate import flag_version
from ._http import Transport
from ._seams import Seams
from ._sync import RulesetSync
from ._types import (
    Detail,
    ErrorCode,
    EvaluationEvent,
    Hooks,
    Reason,
    ReasonKind,
    Status,
)
from ._usage import UsageReporter

DEFAULT_INTERVAL = 30.0
DEFAULT_TIMEOUT = 10.0
INT64_MIN = -(2**63)
INT64_MAX = 2**63 - 1

_NULL, _BOOL, _NUMBER, _STRING, _ARRAY, _OBJECT, _OTHER = (
    "null",
    "boolean",
    "number",
    "string",
    "array",
    "object",
    "other",
)

Accept = Callable[[Any], tuple[bool, Any]]


def _kind(value: Any) -> str:
    if value is None:
        return _NULL
    if isinstance(value, bool):  # before number: a bool is an int
        return _BOOL
    if isinstance(value, (int, float)):
        return _NUMBER
    if isinstance(value, str):
        return _STRING
    if isinstance(value, (list, tuple)):
        return _ARRAY
    if isinstance(value, dict):
        return _OBJECT
    return _OTHER


def _accept_kind(expected: str) -> Accept:
    def accept(value: Any) -> tuple[bool, Any]:
        return _kind(value) == expected, value

    return accept


def _accept_like(fallback: Any) -> Accept:
    """The generic accessor: the served value must be of the fallback's JSON kind; a null fallback takes any."""
    if fallback is None:
        return lambda value: (True, value)

    def accept(value: Any) -> tuple[bool, Any]:
        return value is not None and _kind(value) == _kind(fallback), value

    return accept


def _accept_int(value: Any) -> tuple[bool, Any]:
    """Signed 64-bit: `2.0` counts as `2`; a fraction or a value outside the range does not."""
    if _kind(value) != _NUMBER:
        return False, None
    if isinstance(value, float):
        if not math.isfinite(value) or value != math.floor(value):
            return False, None
        value = int(value)
    if value < INT64_MIN or value > INT64_MAX:
        return False, None
    return True, value


def _duration(value: Any, default: float) -> float:
    """Seconds as a number or a `timedelta`; anything else, or zero or less, is the default."""
    if isinstance(value, timedelta):
        value = value.total_seconds()
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return default
    if not math.isfinite(value) or value <= 0:
        return default
    return float(value)


def _error_reason(code: ErrorCode) -> Reason:
    return Reason(ReasonKind.ERROR, code=code)


class Client:
    """A ToggleSlash server SDK client. Build one with `toggleslash.create`."""

    def __init__(
        self,
        *,
        url: str,
        sdk_key: str,
        stream: bool | None = None,
        poll_interval: Any = None,
        flush_interval: Any = None,
        request_timeout: Any = None,
        hooks: Hooks | None = None,
        on_error: Callable[[BaseException], None] | None = None,
        _seams: Seams | None = None,
    ) -> None:
        seams = _seams or Seams()
        self._on_error_callback = on_error if callable(on_error) else None
        hook = getattr(hooks, "after_evaluation", None)
        self._hook: Callable[[EvaluationEvent], Any] | None = hook if callable(hook) else None
        self._evaluator = seams.evaluator
        self._clock = seams.clock
        timeout = _duration(request_timeout, DEFAULT_TIMEOUT)
        self._timeout = timeout
        flush = _duration(flush_interval, DEFAULT_INTERVAL)
        base = url.rstrip("/") if isinstance(url, str) else ""
        key = sdk_key if isinstance(sdk_key, str) else str(sdk_key)
        transport = Transport(base, seams.ssl_context)

        self._closing = threading.Event()
        self._close_lock = threading.Lock()
        self._leader: int | None = None
        self._closed = threading.Event()
        self._ready_lock = threading.Lock()
        self._ready_event = threading.Event()
        self._ready_value = False
        self._sdk_threads: set[int] = set()

        self._sync = RulesetSync(
            transport,
            key,
            self._clock,
            seams.jitter,
            stream if isinstance(stream, bool) else True,
            _duration(poll_interval, DEFAULT_INTERVAL),
            timeout,
            seams.watchdog,
            self._report,
        )
        self._usage = UsageReporter(transport, key, self._clock, timeout, self._report)
        self._flush_interval = flush
        self._sync_thread = threading.Thread(target=self._run_sync, name="toggleslash-sync", daemon=True)
        self._flush_thread = threading.Thread(target=self._run_flush, name="toggleslash-flush", daemon=True)
        self._sync_thread.start()
        self._flush_thread.start()

    # Threads.

    def _run_sync(self) -> None:
        self._sdk_threads.add(threading.get_ident())
        try:
            self._sync.run(self._first_load_done)
        except Exception as error:
            self._report(error)
        finally:
            self._settle(False)

    def _run_flush(self) -> None:
        self._sdk_threads.add(threading.get_ident())
        try:
            while not self._closing.is_set():
                self._clock.wait(self._closing, self._flush_interval)
                if not self._closing.is_set():
                    self._usage.flush()
        except Exception as error:
            self._report(error)

    def _report(self, error: BaseException) -> None:
        """Tells `on_error`; an error it raises is ignored. Never called with a lock held."""
        callback = self._on_error_callback
        if callback is None:
            return
        with contextlib.suppress(Exception):
            callback(error)

    # ready.

    def _first_load_done(self) -> None:
        ruleset, _etag = self._sync.held()
        self._settle(ruleset is not None)

    def _settle(self, value: bool) -> None:
        """The first answer stands: `ready` never changes once it has one."""
        with self._ready_lock:
            if self._ready_event.is_set():
                return
            self._ready_value = value
            self._ready_event.set()

    def ready(self) -> bool:
        """Waits for the first ruleset load and says whether it worked. Bounded by the request timeout; never
        raises. False means flags serve fallbacks until the SDK reaches the server."""
        if self._ready_event.is_set():
            return self._ready_value
        if threading.get_ident() in self._sdk_threads:
            return False  # called from the SDK's own thread (a callback): waiting would wait for itself
        if not self._ready_event.wait(self._timeout + 1.0):
            self._settle(False)
        return self._ready_value

    # Evaluation.

    def variation(self, flag_key: str, context: Any, fallback: Any) -> Any:
        return self.variation_detail(flag_key, context, fallback).value

    def variation_detail(self, flag_key: str, context: Any, fallback: Any) -> Detail:
        return self._evaluate(flag_key, context, fallback, _accept_like(fallback))

    def bool_variation(self, flag_key: str, context: Any, fallback: bool) -> bool:
        return self.bool_variation_detail(flag_key, context, fallback).value

    def bool_variation_detail(self, flag_key: str, context: Any, fallback: bool) -> Detail:
        return self._evaluate(flag_key, context, fallback, _accept_kind(_BOOL))

    def string_variation(self, flag_key: str, context: Any, fallback: str) -> str:
        return self.string_variation_detail(flag_key, context, fallback).value

    def string_variation_detail(self, flag_key: str, context: Any, fallback: str) -> Detail:
        return self._evaluate(flag_key, context, fallback, _accept_kind(_STRING))

    def number_variation(self, flag_key: str, context: Any, fallback: float) -> float:
        return self.number_variation_detail(flag_key, context, fallback).value

    def number_variation_detail(self, flag_key: str, context: Any, fallback: float) -> Detail:
        return self._evaluate(flag_key, context, fallback, _accept_kind(_NUMBER))

    def int_variation(self, flag_key: str, context: Any, fallback: int) -> int:
        return self.int_variation_detail(flag_key, context, fallback).value

    def int_variation_detail(self, flag_key: str, context: Any, fallback: int) -> Detail:
        return self._evaluate(flag_key, context, fallback, _accept_int)

    def json_variation(self, flag_key: str, context: Any, fallback: Any) -> Any:
        return self.json_variation_detail(flag_key, context, fallback).value

    def json_variation_detail(self, flag_key: str, context: Any, fallback: Any) -> Detail:
        """An array or an object, compared by kind with the fallback (a null fallback takes either)."""
        return self._evaluate(flag_key, context, fallback, _accept_json(fallback))

    def _evaluate(self, flag_key: str, context: Any, fallback: Any, accept: Accept) -> Detail:
        ruleset, etag = self._sync.held()
        try:
            detail = self._detail(ruleset, flag_key, context, fallback, accept)
        except Exception as error:
            self._report(error)
            detail = Detail(fallback, None, _error_reason(ErrorCode.EVALUATION_FAILED))
        if self._hook is not None:
            self._run_hook(flag_key, context, detail, etag, ruleset)
        return detail

    def _detail(self, ruleset: Any, flag_key: str, context: Any, fallback: Any, accept: Accept) -> Detail:
        if ruleset is None:
            return Detail(fallback, None, _error_reason(ErrorCode.NOT_READY))
        result = self._evaluator(ruleset, flag_key, context)
        if result.variation_index is None:
            return Detail(fallback, None, result.reason)
        ok, value = accept(result.value)
        if not ok:
            return Detail(fallback, None, _error_reason(ErrorCode.TYPE_MISMATCH))
        if isinstance(value, (dict, list)):
            value = copy.deepcopy(value)  # the held ruleset is shared; a caller must not be able to change it
        if not self._closing.is_set():
            self._usage.record(flag_key, result.variation_index)
        return Detail(value, result.variation_index, result.reason)

    def _run_hook(self, flag_key: str, context: Any, detail: Detail, etag: str | None, ruleset: Any) -> None:
        try:
            version = flag_version(ruleset, flag_key) if ruleset is not None else None
            event = EvaluationEvent(
                flag_key, context, detail.value, detail.variation_index, detail.reason, etag, version
            )
            hook = self._hook
            if hook is not None:
                hook(event)
        except Exception as error:
            self._report(error)

    # Status and close.

    def status(self) -> Status:
        """What this SDK has loaded and how its stream is doing. Local, and it never raises."""
        return self._sync.status()

    def close(self) -> None:
        """Stops following the server and sends the pending usage counts. Idempotent, safe from many threads."""
        me = threading.get_ident()
        with self._close_lock:
            leading = self._leader is None
            if leading:
                self._leader = me
        if not leading:
            if me != self._leader:
                self._closed.wait(self._timeout * 2 + 2.0)
            return
        try:
            self._shut_down()
        finally:
            self._closed.set()

    def _shut_down(self) -> None:
        self._closing.set()
        self._sync.stop()
        bound = self._timeout + 1.0
        for thread in (self._sync_thread, self._flush_thread):
            if thread.ident != threading.get_ident():
                thread.join(bound)
        self._settle(False)
        self._usage.finish()

    def __enter__(self) -> Client:
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()


def _accept_json(fallback: Any) -> Accept:
    if fallback is None:
        return lambda value: (_kind(value) in (_ARRAY, _OBJECT), value)

    def accept(value: Any) -> tuple[bool, Any]:
        return _kind(value) in (_ARRAY, _OBJECT) and _kind(value) == _kind(fallback), value

    return accept


def create(
    *,
    url: str,
    sdk_key: str,
    stream: bool | None = None,
    poll_interval: Any = None,
    flush_interval: Any = None,
    request_timeout: Any = None,
    hooks: Hooks | None = None,
    on_error: Callable[[BaseException], None] | None = None,
    _seams: Seams | None = None,
) -> Client:
    """Starts a client. It returns at once; `Client.ready()` says when the first load has finished.

    Durations are seconds (a number or a `timedelta`). The defaults are the stream on, 30 s for the poll and the
    usage flush, and 10 s for a request.
    """
    return Client(
        url=url,
        sdk_key=sdk_key,
        stream=stream,
        poll_interval=poll_interval,
        flush_interval=flush_interval,
        request_timeout=request_timeout,
        hooks=hooks,
        on_error=on_error,
        _seams=_seams,
    )


__all__ = ["Client", "SdkError", "create"]
