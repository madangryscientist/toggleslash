"""Usage counts: how often each flag variation was served, per UTC day, sent in batches. Counts only: no
context, no context key, nothing about an end user."""

from __future__ import annotations

import threading
import time
from collections.abc import Callable

from . import _json
from ._http import Aborted, Hold, RequestTimeout, Transport, retry_after
from ._types import LastError, ToggleSlashError

MAX_BATCH = 500
_Key = tuple[str, int, str]


def retryable(status: int) -> bool:
    """Whether a batch the server refused is kept for another try. The status alone decides."""
    return status >= 500 or status in {408, 429}


class UsageReporter:
    def __init__(
        self,
        *,
        transport: Transport,
        sdk_key: str,
        request_timeout: float,
        on_error: Callable[[Exception], None],
        now: Callable[[], float],
        hold: Hold,
    ) -> None:
        self._transport = transport
        self._sdk_key = sdk_key
        self._timeout = request_timeout
        self._on_error = on_error
        self._now = now
        self._hold = hold
        self._pending: dict[_Key, int] = {}
        self._usage_lock = threading.Lock()
        self._flush_lock = threading.Lock()
        self._counting = True
        self._stopping = threading.Event()

    def record(self, flag: str, variation_index: int) -> None:
        day = time.strftime("%Y-%m-%d", time.gmtime(self._now()))
        key = (flag, variation_index, day)
        with self._usage_lock:
            if self._counting:
                self._pending[key] = self._pending.get(key, 0) + 1

    def stop_counting(self) -> None:
        """Nothing is counted from here on, and a flush in progress stops after its current batch."""
        with self._usage_lock:
            self._counting = False
        self._stopping.set()

    def pending_count(self) -> int:
        with self._usage_lock:
            return len(self._pending)

    def flush(self) -> None:
        """Sends what is pending. Flushes run one at a time. It never raises, and `on_error` runs after the
        flush lock is released."""
        with self._flush_lock:
            errors = self._send(final=False)
        self._tell(errors)

    def finish(self, wait: float) -> None:
        """The last flush, on closing. It does not wait out a `Retry-After`, and it says so when that costs counts."""
        if not self._flush_lock.acquire(timeout=max(wait, 0.0)):
            left = self.pending_count()
            self._tell(
                [
                    ToggleSlashError(
                        f"closed with {left} usage counts unsent: a flush was still running"
                    )
                ]
            )
            return
        try:
            errors = self._send(final=True)
        finally:
            self._flush_lock.release()
        left = self.pending_count()
        if left and self._hold.remaining() > 0:
            errors.append(
                ToggleSlashError(
                    f"closed with {left} usage counts unsent: the server asked to wait"
                )
            )
        self._tell(errors)

    def _tell(self, errors: list[Exception]) -> None:
        for error in errors:
            self._on_error(error)

    def _take(self) -> list[tuple[_Key, int]]:
        with self._usage_lock:
            entries = list(self._pending.items())
            self._pending = {}
        return entries

    def _put_back(self, entries: list[tuple[_Key, int]]) -> None:
        with self._usage_lock:
            for key, count in entries:
                self._pending[key] = self._pending.get(key, 0) + count

    def _send(self, *, final: bool) -> list[Exception]:
        errors: list[Exception] = []
        if self._hold.remaining() > 0:
            return errors
        entries = self._take()
        for start in range(0, len(entries), MAX_BATCH):
            if not final and self._stopping.is_set():
                self._put_back(entries[start:])
                return errors
            if not self._post(entries[start : start + MAX_BATCH], errors):
                self._put_back(entries[start:])
                return errors
        return errors

    def _post(self, batch: list[tuple[_Key, int]], errors: list[Exception]) -> bool:
        """False when the batch (and the rest) should be kept and the flush should stop."""
        events = [
            {"flag": flag, "variationIndex": index, "count": count, "day": day}
            for (flag, index, day), count in batch
        ]
        headers = {"Authorization": f"Bearer {self._sdk_key}", "Content-Type": "application/json"}
        try:
            body = _json.dumps({"events": events}).encode("ascii")
            response = self._transport.request(
                "POST", "/sdk/v1/events", headers, body, self._timeout
            )
        except Aborted:
            return False
        except RequestTimeout:
            errors.append(ToggleSlashError("POST /sdk/v1/events timed out", LastError.TIMEOUT))
            return False
        except Exception:
            errors.append(ToggleSlashError("POST /sdk/v1/events failed", LastError.NETWORK))
            return False
        status = response.status
        if 200 <= status < 300:
            return True
        wait = retry_after(status, response.headers.get("retry-after"))
        if wait is not None:
            self._hold.raise_to(wait)
        errors.append(ToggleSlashError(f"POST /sdk/v1/events answered {status}", f"http_{status}"))
        return not retryable(status)
