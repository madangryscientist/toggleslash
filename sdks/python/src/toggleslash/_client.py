"""The client: options, evaluation, the sync loop of contract 7.3, usage and close."""

from __future__ import annotations

import datetime
import inspect
import math
import random
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass, replace
from typing import Any

from . import _json
from ._evaluate import Outcome, evaluate
from ._http import (
    Aborted,
    Hold,
    NetworkError,
    RequestTimeout,
    Transport,
    Wake,
    retry_after,
)
from ._ruleset import Ruleset, create_ruleset
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
from ._usage import UsageReporter

DEFAULT_POLL = 30.0
DEFAULT_FLUSH = 30.0
DEFAULT_TIMEOUT = 10.0
MAX_DURATION = 31_536_000.0
STREAM_IDLE = 60.0
INT_MIN = -(2**63)
INT_LIMIT = 2**63
_EPOCH = datetime.datetime(1970, 1, 1, tzinfo=datetime.UTC)


@dataclass(frozen=True, slots=True)
class Config:
    """Everything a client is built from. The last four are seams for the tests: the public `create` leaves
    them at their defaults."""

    url: str
    sdk_key: str
    stream: bool = True
    poll_interval: float = DEFAULT_POLL
    flush_interval: float = DEFAULT_FLUSH
    request_timeout: float = DEFAULT_TIMEOUT
    after_evaluation: Callable[[EvaluationEvent], Any] | None = None
    on_error: Callable[[Exception], Any] | None = None
    now: Callable[[], float] = time.time
    monotonic: Callable[[], float] = time.monotonic
    random: Callable[[], float] = random.random
    stream_idle: float = STREAM_IDLE


class _Held:
    """A ruleset and the ETag that names it. Never changed once built, so one assignment swaps both."""

    __slots__ = ("etag", "ruleset")

    def __init__(self, ruleset: Ruleset, etag: str | None) -> None:
        self.ruleset = ruleset
        self.etag = etag


@dataclass(frozen=True, slots=True)
class _State:
    last_sync_at: float | None
    stream: StreamState
    last_error: str | None


class _BadRuleset(Exception):
    pass


def _kind_of(value: Any) -> str | None:
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
    if isinstance(value, dict):
        return "object"
    return None


def _same_kind(served: Any, fallback: Any) -> bool:
    if fallback is None:
        return True
    wanted = _kind_of(fallback)
    return wanted is not None and wanted == _kind_of(served)


def _whole_in_range(value: Any) -> bool:
    if isinstance(value, bool) or not isinstance(value, int | float):
        return False
    if isinstance(value, float):
        if not math.isfinite(value) or value != math.floor(value):
            return False
        return INT_MIN <= value < INT_LIMIT
    return INT_MIN <= value < INT_LIMIT


def _reason(outcome: Outcome) -> Reason:
    code = ErrorCode(outcome.code) if outcome.code is not None else None
    return Reason(ReasonKind(outcome.kind), code, outcome.rule_index, outcome.flag_key)


def _error_reason(code: ErrorCode) -> Reason:
    return Reason(ReasonKind.ERROR, code)


class Client:
    """A ToggleSlash server SDK client. Every method is safe to call from any thread and none raises."""

    def __init__(self, config: Config) -> None:
        self._config = config
        self._held: _Held | None = None
        self._state = _State(
            None, StreamState.CONNECTING if config.stream else StreamState.OFF, None
        )
        self._state_lock = threading.Lock()
        self._closing = False
        self._ready_event = threading.Event()
        self._ready_value = False
        self._stop = threading.Event()
        self._closed = threading.Event()
        self._close_lock = threading.Lock()
        self._close_started = False
        self._hold = Hold(config.monotonic)
        self._sync_wake = Wake()
        self._flush_wake = Wake()
        self._sync_transport = Transport(config.url, self._sync_wake)
        self._usage = UsageReporter(
            transport=Transport(config.url, self._flush_wake),
            sdk_key=config.sdk_key,
            request_timeout=config.request_timeout,
            on_error=self._report,
            now=config.now,
            hold=Hold(config.monotonic),
        )
        self._sync_thread = threading.Thread(
            target=self._run_sync, name="toggleslash-sync", daemon=True
        )
        self._flush_thread = threading.Thread(
            target=self._run_flush, name="toggleslash-flush", daemon=True
        )
        try:
            self._sync_thread.start()
            self._flush_thread.start()
        except RuntimeError as exc:
            self._stop.set()
            self._settle_ready(False)
            self._report(exc)

    # ---- the public methods -------------------------------------------------------------------------

    def ready(self) -> bool:
        """Waits for the first ruleset attempt to settle (bounded by `request_timeout`). True if a ruleset is held."""
        try:
            if self._ready_event.is_set():
                return self._ready_value
            if self._on_sdk_thread():
                return self._held is not None
            self._ready_event.wait(self._config.request_timeout + 1.0)
            if not self._ready_event.is_set():
                self._settle_ready(False)
            return self._ready_value
        except Exception:
            return False

    def variation(self, flag_key: str, context: Any, fallback: Any) -> Any:
        return self.variation_detail(flag_key, context, fallback).value

    def variation_detail(self, flag_key: str, context: Any, fallback: Any) -> Detail:
        return self._answer(flag_key, context, fallback, integer=False)

    def int_variation(self, flag_key: str, context: Any, fallback: int | None) -> int | None:
        return self.int_variation_detail(flag_key, context, fallback).value

    def int_variation_detail(self, flag_key: str, context: Any, fallback: int | None) -> Detail:
        return self._answer(flag_key, context, fallback, integer=True)

    def status(self) -> Status:
        try:
            with self._state_lock:
                held, state, closing = self._held, self._state, self._closing
            synced = (
                None
                if state.last_sync_at is None
                else datetime.datetime.fromtimestamp(state.last_sync_at, datetime.UTC)
            )
            return Status(
                held.etag if held is not None else None,
                synced,
                StreamState.CLOSED if closing else state.stream,
                state.last_error,
            )
        except Exception:
            return Status(
                None, None, StreamState.CLOSED if self._closing else StreamState.OFF, None
            )

    def close(self) -> None:
        """Stops following the server, stops the flush loop, waits for both, sends the final usage flush."""
        try:
            self._close()
        except Exception as exc:
            self._report(exc)

    def __enter__(self) -> Client:
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()

    # ---- evaluation ---------------------------------------------------------------------------------

    def _answer(self, flag_key: Any, context: Any, fallback: Any, *, integer: bool) -> Detail:
        held = self._held
        try:
            detail = self._detail(held, flag_key, context, fallback, integer)
        except Exception as exc:
            self._report(exc)
            detail = Detail(fallback, None, _error_reason(ErrorCode.EVALUATION_FAILED))
        self._run_hook(held, flag_key, context, detail)
        return detail

    def _detail(
        self, held: _Held | None, flag_key: Any, context: Any, fallback: Any, integer: bool
    ) -> Detail:
        if held is None:
            return Detail(fallback, None, _error_reason(ErrorCode.NOT_READY))
        outcome = evaluate(held.ruleset, flag_key, context)
        if outcome.variation_index is None:
            return Detail(fallback, None, _reason(outcome))
        value = outcome.value
        if integer:
            matched = _whole_in_range(value) and (
                fallback is None or _kind_of(fallback) == "number"
            )
            if matched:
                value = int(value)
        else:
            matched = _same_kind(value, fallback)
        if not matched:
            return Detail(fallback, None, _error_reason(ErrorCode.TYPE_MISMATCH))
        self._usage.record(flag_key, outcome.variation_index)
        if isinstance(value, list | dict):
            value = _json.copy(value)
        return Detail(value, outcome.variation_index, _reason(outcome))

    def _run_hook(self, held: _Held | None, flag_key: Any, context: Any, detail: Detail) -> None:
        hook = self._config.after_evaluation
        if hook is None:
            return
        try:
            entry = (
                held.ruleset.flags.get(flag_key)
                if held is not None and isinstance(flag_key, str)
                else None
            )
            event = EvaluationEvent(
                flag_key,
                context,
                detail.value,
                detail.variation_index,
                detail.reason,
                held.etag if held is not None else None,
                entry.version if entry is not None else None,
            )
            result = hook(event)
            self._watch_result(result)
        except Exception as exc:
            self._report(exc)

    def _watch_result(self, result: Any) -> None:
        """A hook's return value is ignored. A coroutine is closed unawaited; a future that later fails reports."""
        if result is None:
            return
        if inspect.iscoroutine(result):
            result.close()
            return
        add = getattr(result, "add_done_callback", None)
        if callable(add):
            add(self._future_done)

    def _future_done(self, future: Any) -> None:
        try:
            error = future.exception()
        except BaseException:
            return
        if error is not None:
            self._report(error)

    # ---- callbacks and state ------------------------------------------------------------------------

    def _report(self, error: BaseException) -> None:
        """Tells `on_error`, with no lock held. An error `on_error` raises is ignored."""
        callback = self._config.on_error
        if callback is None:
            return
        try:
            callback(error)
        except Exception:
            return

    def _on_sdk_thread(self) -> bool:
        return threading.current_thread() in {self._sync_thread, self._flush_thread}

    def _settle_ready(self, value: bool) -> None:
        with self._state_lock:
            if self._ready_event.is_set():
                return
            self._ready_value = value
            self._ready_event.set()

    def _set_state(self, **changes: Any) -> None:
        with self._state_lock:
            self._state = replace(self._state, **changes)

    def _swap(self, held: _Held) -> None:
        """A 200: the new pair, the sync time and a cleared error, together."""
        with self._state_lock:
            self._held = held
            self._state = replace(self._state, last_sync_at=self._config.now(), last_error=None)

    def _touch(self) -> None:
        """A sync that changes nothing held: a 304, or a stream tag equal to the one held."""
        with self._state_lock:
            self._state = replace(self._state, last_sync_at=self._config.now(), last_error=None)

    def _fail(self, code: str, message: str, *, first: bool, disconnect: bool) -> None:
        """Records a failed attempt (nothing is recorded once close has begun) and tells `on_error`."""
        with self._state_lock:
            if self._closing:
                return
            change = {"last_error": code}
            if disconnect:
                change["stream"] = StreamState.DISCONNECTED
            self._state = replace(self._state, **change)
        if first:
            self._settle_ready(self._held is not None)
        self._report(ToggleSlashError(message, code))

    # ---- the sync thread ----------------------------------------------------------------------------

    def _run_sync(self) -> None:
        try:
            try:
                self._fetch_ruleset(first=True, disconnect=False)
            except Exception as exc:
                self._report(exc)
            self._settle_ready(self._held is not None)
            self._loop()
        except Exception as exc:
            self._report(exc)
        finally:
            self._settle_ready(False)

    def _loop(self) -> None:
        stream_on = self._config.stream
        failures = 0
        while not self._stop.is_set():
            try:
                if stream_on:
                    self._wait_for_hold()
                    if self._stop.is_set():
                        break
                connected = self._follow() if stream_on else False
            except Exception as exc:
                self._report(exc)
                connected = False
            if self._stop.is_set():
                break
            failures = 0 if connected else failures + 1
            self._stop.wait(max(self._delay(failures), self._hold.remaining()))
            if self._stop.is_set():
                break
            if not connected:
                try:
                    self._fetch_ruleset(first=False, disconnect=False)
                except Exception as exc:
                    self._report(exc)

    def _wait_for_hold(self) -> None:
        while not self._stop.is_set():
            remaining = self._hold.remaining()
            if remaining <= 0:
                return
            self._stop.wait(remaining)

    def _delay(self, failures: int) -> float:
        config = self._config
        if not config.stream:
            return config.poll_interval
        ceiling = min(config.poll_interval, 1.0 * 2 ** min(failures, 16))
        return ceiling * (0.5 + config.random() / 2)

    def _headers(self, *extra: tuple[str, str]) -> dict[str, str]:
        headers = {"Authorization": f"Bearer {self._config.sdk_key}"}
        headers.update(extra)
        return headers

    def _fetch_ruleset(self, *, first: bool, disconnect: bool) -> bool:
        """The ruleset request of 7.1. True if it was a sync."""
        held = self._held
        extra = ()
        if held is not None and held.etag:
            extra = (("If-None-Match", held.etag),)
        try:
            response = self._sync_transport.request(
                "GET", "/sdk/v1/ruleset", self._headers(*extra), None, self._config.request_timeout
            )
            status = response.status
            if status == 200:
                self._swap(self._read_ruleset(response.body, response.headers.get("etag")))
                return True
            if status == 304 and held is not None:
                self._touch()
                return True
            wait = retry_after(status, response.headers.get("retry-after"))
            if wait is not None:
                self._hold.raise_to(wait)
            self._fail(
                f"http_{status}",
                f"GET /sdk/v1/ruleset answered {status}",
                first=first,
                disconnect=disconnect,
            )
        except Aborted:
            pass
        except RequestTimeout:
            self._fail(
                LastError.TIMEOUT,
                "the ruleset request timed out",
                first=first,
                disconnect=disconnect,
            )
        except _BadRuleset:
            self._fail(
                LastError.BAD_RULESET,
                "the ruleset is not valid",
                first=first,
                disconnect=disconnect,
            )
        except NetworkError:
            self._fail(
                LastError.NETWORK, "the ruleset request failed", first=first, disconnect=disconnect
            )
        except Exception:
            self._fail(
                LastError.NETWORK, "the ruleset request failed", first=first, disconnect=disconnect
            )
        return False

    @staticmethod
    def _read_ruleset(body: bytes, etag: str | None) -> _Held:
        try:
            data = _json.loads(body)
        except (ValueError, RecursionError):
            raise _BadRuleset from None
        if not isinstance(data, dict) or not isinstance(data.get("flags"), list):
            raise _BadRuleset
        if not isinstance(data.get("segments"), list):
            raise _BadRuleset
        return _Held(create_ruleset(data), etag or None)

    def _follow(self) -> bool:
        """One stream attempt (7.2). True if it connected and was not ended by a failed fetch."""
        config = self._config
        self._set_state(stream=StreamState.CONNECTING)
        try:
            opened = self._sync_transport.open_stream(
                "/sdk/v1/stream",
                self._headers(("Accept", "text/event-stream")),
                config.request_timeout,
            )
        except Aborted:
            return False
        except RequestTimeout:
            self._fail(
                LastError.TIMEOUT, "the stream did not open in time", first=False, disconnect=True
            )
            return False
        except Exception:
            self._fail(LastError.NETWORK, "the stream request failed", first=False, disconnect=True)
            return False
        connected = False
        try:
            if opened.status != 200:
                wait = retry_after(opened.status, opened.headers.get("retry-after"))
                if wait is not None:
                    self._hold.raise_to(wait)
                self._fail(
                    f"http_{opened.status}",
                    f"GET /sdk/v1/stream answered {opened.status}",
                    first=False,
                    disconnect=True,
                )
                return False
            parser = SseParser()
            try:
                for chunk in opened.chunks(lambda: time.monotonic() + config.stream_idle):
                    for event in parser.feed(chunk):
                        if event.name not in {"connected", "changed"}:
                            continue
                        if event.name == "connected":
                            connected = True
                            with self._state_lock:
                                self._state = replace(
                                    self._state, stream=StreamState.CONNECTED, last_error=None
                                )
                        if not self._on_tag(event.data):
                            return False
            except Aborted:
                return connected
            except RequestTimeout:
                self._fail(
                    LastError.TIMEOUT,
                    "the stream was quiet for too long",
                    first=False,
                    disconnect=True,
                )
                return connected
            except Exception:
                self._fail(
                    LastError.NETWORK, "the stream ended abruptly", first=False, disconnect=True
                )
                return connected
            self._set_state(stream=StreamState.DISCONNECTED)
            return connected
        finally:
            opened.close()

    def _on_tag(self, tag: str) -> bool:
        """A `connected` or `changed` event. False if the fetch it asked for failed (the stream ends)."""
        held = self._held
        if held is not None and held.etag and tag == held.etag:
            self._touch()
            return True
        return self._fetch_ruleset(first=False, disconnect=True)

    # ---- the flush thread ---------------------------------------------------------------------------

    def _run_flush(self) -> None:
        while not self._stop.wait(self._config.flush_interval):
            try:
                self._usage.flush()
            except Exception as exc:
                self._report(exc)

    # ---- close --------------------------------------------------------------------------------------

    def _close(self) -> None:
        timeout = self._config.request_timeout
        with self._close_lock:
            performer = not self._close_started
            self._close_started = True
        with self._state_lock:
            self._closing = True
        self._usage.stop_counting()
        self._stop.set()
        self._sync_wake.set()
        if not performer:
            if not self._on_sdk_thread():
                self._closed.wait(4 * timeout)
            return
        try:
            deadline = time.monotonic() + timeout
            me = threading.current_thread()
            for thread in (self._sync_thread, self._flush_thread):
                if thread is not me and thread.is_alive():
                    thread.join(max(deadline - time.monotonic(), 0.0))
            self._usage.finish(timeout)
            self._settle_ready(False)
        finally:
            self._sync_wake.close()
            self._flush_wake.close()
            self._closed.set()


def _duration(value: Any, default: float) -> float:
    if isinstance(value, bool) or not isinstance(value, int | float):
        return default
    try:
        seconds = float(value)
    except OverflowError:
        return default
    if not math.isfinite(seconds) or seconds <= 0:
        return default
    return min(seconds, MAX_DURATION)


def _hook_of(hooks: Any) -> Callable[[EvaluationEvent], Any] | None:
    if hooks is None:
        return None
    hook = (
        hooks.get("after_evaluation")
        if isinstance(hooks, dict)
        else getattr(hooks, "after_evaluation", None)
    )
    return hook if callable(hook) else None


def create(
    *,
    url: str,
    sdk_key: str,
    stream: bool = True,
    poll_interval: float = DEFAULT_POLL,
    flush_interval: float = DEFAULT_FLUSH,
    request_timeout: float = DEFAULT_TIMEOUT,
    hooks: Hooks | None = None,
    on_error: Callable[[Exception], Any] | None = None,
) -> Client:
    """Starts a client and returns at once; the first load happens on its own thread. It never raises."""
    try:
        config = Config(
            url=url if isinstance(url, str) else "",
            sdk_key=sdk_key if isinstance(sdk_key, str) else "",
            stream=stream is not False,
            poll_interval=_duration(poll_interval, DEFAULT_POLL),
            flush_interval=_duration(flush_interval, DEFAULT_FLUSH),
            request_timeout=_duration(request_timeout, DEFAULT_TIMEOUT),
            after_evaluation=_hook_of(hooks),
            on_error=on_error if callable(on_error) else None,
        )
    except Exception:
        config = Config(url="", sdk_key="")
    return Client(config)
