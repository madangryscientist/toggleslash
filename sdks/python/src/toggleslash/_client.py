"""The client: evaluation, hooks, status and close (contract 2, 4, 5, 6, 10 and 11)."""

from __future__ import annotations

import math
import threading
from collections.abc import Callable, Mapping
from datetime import timedelta
from typing import Any

from ._evaluate import evaluate
from ._http import HttpTransport
from ._seams import Seams
from ._sync import Snapshot, Sync
from ._types import (
    ClientStatus,
    Context,
    ErrorCode,
    EvaluationEvent,
    Hooks,
    Reason,
    ReasonKind,
    VariationDetail,
)
from ._usage import Usage

DEFAULT_POLL_INTERVAL = 30.0
DEFAULT_FLUSH_INTERVAL = 30.0
DEFAULT_REQUEST_TIMEOUT = 10.0
# Longer than anyone means, and short enough for `threading` to accept as a timeout.
MAX_SECONDS = 10.0 * 365 * 86400

_INT64_MIN = -(2**63)
_INT64_MAX = 2**63 - 1


def _seconds(value: object, default: float) -> float:
    """A duration option: seconds as a number or a `timedelta`. Anything unusable is the default."""
    try:
        if isinstance(value, timedelta):
            value = value.total_seconds()
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            return default
        number = float(value)
        if not math.isfinite(number) or number <= 0:
            return default
        return min(number, MAX_SECONDS)
    except Exception:
        return default


def _kind(value: object) -> str:
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, (int, float)):
        return "number"
    if isinstance(value, str):
        return "string"
    if isinstance(value, (list, tuple)):
        return "array"
    if isinstance(value, Mapping):
        return "object"
    return "other"


def _copy(value: Any) -> Any:
    """A copy of a served list or dict, so a caller who changes it cannot change the ruleset held."""
    if not isinstance(value, (list, dict)):
        return value
    root: Any = [] if isinstance(value, list) else {}
    stack = [(value, root)]
    while stack:
        source, target = stack.pop()
        items = enumerate(source) if isinstance(source, list) else source.items()
        for key, item in items:
            if isinstance(item, (list, dict)):
                child: Any = [] if isinstance(item, list) else {}
                stack.append((item, child))
            else:
                child = item
            if isinstance(target, list):
                target.append(child)
            else:
                target[key] = child
    return root


class _Accessor:
    """What a typed method accepts of a served value, and what it turns it into."""

    def __init__(self, kinds: frozenset[str] | None, convert: Callable[[Any], tuple[bool, Any]] | None = None) -> None:
        self.kinds = kinds  # None: the fallback's kind decides (the generic method)
        self.convert = convert

    def accept(self, value: Any, fallback: Any) -> tuple[bool, Any]:
        kind = _kind(value)
        if self.kinds is None:
            if fallback is not None and kind != _kind(fallback):
                return False, None
        elif kind not in self.kinds:
            return False, None
        elif fallback is not None and _kind(fallback) in self.kinds and kind != _kind(fallback):
            return False, None  # a JSON accessor keeps array and object apart, as the generic method does
        if self.convert is not None:
            return self.convert(value)
        return True, value


def _to_int(value: Any) -> tuple[bool, Any]:
    if isinstance(value, float):
        if not math.isfinite(value) or value != math.floor(value):
            return False, None
        value = int(value)
    if not _INT64_MIN <= value <= _INT64_MAX:
        return False, None
    return True, value


def _to_float(value: Any) -> tuple[bool, Any]:
    return True, float(value)


_GENERIC = _Accessor(None)
_BOOL = _Accessor(frozenset({"boolean"}))
_STRING = _Accessor(frozenset({"string"}))
_INT = _Accessor(frozenset({"number"}), _to_int)
_FLOAT = _Accessor(frozenset({"number"}), _to_float)
_JSON = _Accessor(frozenset({"array", "object"}))


def _fallback(fallback: Any, code: ErrorCode) -> VariationDetail:
    return VariationDetail(fallback, None, Reason(ReasonKind.ERROR, code=code))


def _flag_version(snapshot: Snapshot | None, flag_key: str) -> Any:
    if snapshot is None:
        return None
    entry = snapshot.ruleset.flags.get(flag_key)
    if entry is None or entry.status != "ready" or entry.flag is None:
        return None
    return entry.flag.get("version")


class Client:
    """A ToggleSlash server SDK client. Build one with `toggleslash.create_client`.

    Every method is safe to call from any thread and none raises. `variation` and the typed methods read
    memory and never wait for the network."""

    def __init__(
        self,
        url: str,
        sdk_key: str,
        *,
        stream: bool = True,
        poll_interval: float | timedelta | None = None,
        flush_interval: float | timedelta | None = None,
        request_timeout: float | timedelta | None = None,
        hooks: Hooks | None = None,
        on_error: Callable[[Exception], None] | None = None,
        _seams: Seams | None = None,
    ) -> None:
        seams = _seams or Seams()
        self._on_error = on_error if callable(on_error) else None
        hook = getattr(hooks, "after_evaluation", None)
        self._hook: Callable[[EvaluationEvent], Any] | None = hook if callable(hook) else None
        self._evaluator = seams.evaluator or evaluate
        base_url = url.rstrip("/") if isinstance(url, str) else ""
        key = sdk_key if isinstance(sdk_key, str) else ""
        timeout = _seconds(request_timeout, DEFAULT_REQUEST_TIMEOUT)
        self._flush_interval = _seconds(flush_interval, DEFAULT_FLUSH_INTERVAL)
        transport = seams.transport or HttpTransport(base_url)
        self._stop = threading.Event()
        self._sync = Sync(
            transport,
            key,
            stream=stream is not False,
            poll_interval=_seconds(poll_interval, DEFAULT_POLL_INTERVAL),
            request_timeout=timeout,
            report=self._report,
            stop=self._stop,
            seams=seams,
        )
        self._usage = Usage(transport, key, timeout, self._report, seams.monotonic, seams.wall)
        self._close_lock = threading.Lock()
        self._closer: int | None = None
        self._closed = False
        self._flush_thread = threading.Thread(target=self._flush_loop, name="toggleslash-flush", daemon=True)
        self._sync.start()
        try:
            self._flush_thread.start()
        except RuntimeError as error:
            self._report(error)

    # -- the public methods -------------------------------------------------------------------------------

    def ready(self) -> bool:
        """Blocks until the first load has finished, then says whether a ruleset is held."""
        return self._sync.ready()

    def variation(self, flag_key: str, context: Context, fallback: Any) -> Any:
        """The value the flag serves `context`, or `fallback`. The fallback's JSON kind (`None` for any)
        must match the served kind."""
        return self._evaluate(flag_key, context, fallback, _GENERIC).value

    def variation_detail(self, flag_key: str, context: Context, fallback: Any) -> VariationDetail:
        return self._evaluate(flag_key, context, fallback, _GENERIC)

    def bool_variation(self, flag_key: str, context: Context, fallback: bool) -> bool:
        return self._evaluate(flag_key, context, fallback, _BOOL).value

    def bool_variation_detail(self, flag_key: str, context: Context, fallback: bool) -> VariationDetail:
        return self._evaluate(flag_key, context, fallback, _BOOL)

    def string_variation(self, flag_key: str, context: Context, fallback: str) -> str:
        return self._evaluate(flag_key, context, fallback, _STRING).value

    def string_variation_detail(self, flag_key: str, context: Context, fallback: str) -> VariationDetail:
        return self._evaluate(flag_key, context, fallback, _STRING)

    def int_variation(self, flag_key: str, context: Context, fallback: int) -> int:
        """A served number with a fraction, or outside the signed 64-bit range, is `TYPE_MISMATCH`; `2.0` is `2`."""
        return self._evaluate(flag_key, context, fallback, _INT).value

    def int_variation_detail(self, flag_key: str, context: Context, fallback: int) -> VariationDetail:
        return self._evaluate(flag_key, context, fallback, _INT)

    def float_variation(self, flag_key: str, context: Context, fallback: float) -> float:
        return self._evaluate(flag_key, context, fallback, _FLOAT).value

    def float_variation_detail(self, flag_key: str, context: Context, fallback: float) -> VariationDetail:
        return self._evaluate(flag_key, context, fallback, _FLOAT)

    def json_variation(self, flag_key: str, context: Context, fallback: Any) -> Any:
        """A served list or dict (a copy: changing it does not change the ruleset held)."""
        return self._evaluate(flag_key, context, fallback, _JSON).value

    def json_variation_detail(self, flag_key: str, context: Context, fallback: Any) -> VariationDetail:
        return self._evaluate(flag_key, context, fallback, _JSON)

    def status(self) -> ClientStatus:
        """What this client has loaded and how its stream is doing. Local: it makes no request."""
        return self._sync.status()

    def flush(self) -> None:
        """Sends the pending usage counts now, under the rules of contract 9. An extra the contract allows."""
        try:
            self._usage.flush()
        except Exception as error:
            self._report(error)

    def close(self) -> None:
        """Stops following the server, sends the final usage flush and returns. Safe to call twice, from
        any thread and from a callback."""
        me = threading.get_ident()
        if self._closer == me:  # called from a callback of this very close
            return
        with self._close_lock:
            if self._closed:
                return
            self._closer = me
            try:
                self._usage.stop_recording()
                self._sync.begin_close()
                self._stop.set()
                self._sync.join()
                if self._flush_thread.is_alive() and self._flush_thread is not threading.current_thread():
                    self._flush_thread.join()
                self._usage.flush(final=True)
            except Exception as error:
                self._report(error)
            finally:
                self._closer = None
                self._closed = True

    def __enter__(self) -> Client:
        return self

    def __exit__(self, *exc: object) -> None:
        self.close()

    # -- inside -------------------------------------------------------------------------------------------

    def _report(self, error: BaseException) -> None:
        callback = self._on_error
        if callback is None:
            return
        try:
            callback(error)  # type: ignore[arg-type]
        except Exception:
            pass  # a faulty on_error must not break the client

    def _flush_loop(self) -> None:
        try:
            while not self._stop.wait(self._flush_interval):
                self._usage.flush()
        except BaseException as error:
            self._report(error)

    def _evaluate(self, flag_key: str, context: Context, fallback: Any, accessor: _Accessor) -> VariationDetail:
        snapshot = self._sync.held  # read once: the ruleset and its ETag always belong together
        failure: Exception | None = None
        try:
            if snapshot is None:
                detail = _fallback(fallback, ErrorCode.NOT_READY)
            else:
                result = self._evaluator(snapshot.ruleset, flag_key, context)
                if result.variation_index is None:
                    detail = VariationDetail(fallback, None, result.reason)
                else:
                    accepted, value = accessor.accept(result.value, fallback)
                    if not accepted:
                        detail = _fallback(fallback, ErrorCode.TYPE_MISMATCH)
                    else:
                        detail = VariationDetail(_copy(value), result.variation_index, result.reason)
                        self._usage.record(flag_key, result.variation_index)
        except Exception as error:
            failure = error
            detail = _fallback(fallback, ErrorCode.EVALUATION_FAILED)
        if failure is not None:
            self._report(failure)
        hook = self._hook
        if hook is not None:
            try:
                hook(
                    EvaluationEvent(
                        flag_key=flag_key,
                        context=context,
                        value=detail.value,
                        variation_index=detail.variation_index,
                        reason=detail.reason,
                        etag=snapshot.etag if snapshot is not None else None,
                        flag_version=_flag_version(snapshot, flag_key),
                    )
                )
            except Exception as error:
                self._report(error)
        return detail


def create_client(
    url: str,
    sdk_key: str,
    *,
    stream: bool = True,
    poll_interval: float | timedelta | None = None,
    flush_interval: float | timedelta | None = None,
    request_timeout: float | timedelta | None = None,
    hooks: Hooks | None = None,
    on_error: Callable[[Exception], None] | None = None,
) -> Client:
    """Starts a client: two daemon threads, and a first load that begins at once. It returns without waiting
    for the network and never raises. Durations are seconds (a number or a `timedelta`); `None`, zero or a
    negative one is the default (30 s, 30 s and 10 s)."""
    return Client(
        url,
        sdk_key,
        stream=stream,
        poll_interval=poll_interval,
        flush_interval=flush_interval,
        request_timeout=request_timeout,
        hooks=hooks,
        on_error=on_error,
    )
