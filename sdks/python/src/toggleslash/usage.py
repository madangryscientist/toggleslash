"""Usage counts: how often each flag variation was served, per UTC day, and the batches that report them."""

from __future__ import annotations

import json
import threading
from collections.abc import Callable

from . import http
from .clock import Clock, utc_day
from .types import ToggleSlashError

# The server takes 1000 events and 128 KiB a request; an entry is at most about 140 bytes, so 500 stay
# under both.
MAX_BATCH = 500

Entry = tuple[str, int, str]  # flag key, variation index, UTC day


def _retryable(status: int) -> bool:
    return status >= 500 or status in (408, 429)


class UsageReporter:
    """Counts in memory (``record`` is a dict add under a lock held for nothing else) and sends them.

    A batch that fails for a reason that can pass is kept; one the server refused is dropped, since it
    would be refused again. After a ``429`` with a valid ``Retry-After`` nothing is sent until that has
    passed. The flush lock is held across a request, and only the flush thread and ``close`` take it.
    Failures are reported after it is released, so ``on_error`` may call back into the client.
    """

    def __init__(
        self,
        base: http.Base,
        sdk_key: str,
        clock: Clock,
        request_timeout: float,
        report: Callable[[Exception], None],
    ) -> None:
        self._base = base
        self._key = sdk_key
        self._clock = clock
        self._timeout = request_timeout
        self._report = report
        self._guards = http.GuardSet()  # never aborted: a usage request is not abandoned by close
        self._counts: dict[Entry, int] = {}
        self._counts_lock = threading.Lock()
        self._counting = True
        self._flush_lock = threading.Lock()
        self._hold_until = 0.0

    def record(self, flag_key: str, variation_index: int) -> None:
        key = (flag_key, variation_index, utc_day(self._clock.now()))
        with self._counts_lock:
            if self._counting:
                self._counts[key] = self._counts.get(key, 0) + 1

    def stop_counting(self) -> None:
        with self._counts_lock:
            self._counting = False

    def pending(self) -> int:
        with self._counts_lock:
            return len(self._counts)

    def flush(self) -> None:
        """Send what is pending. Never raises; a call during a flush waits for that flush first."""
        errors: list[Exception] = []
        with self._flush_lock:
            try:
                self._send(errors)
            except Exception as error:
                errors.append(error)
        for error in errors:
            self._report(error)

    def finish(self) -> None:
        """The last flush, on closing. It will not wait out a hold, and counts held back are lost."""
        self.flush()
        pending = self.pending()
        if pending > 0 and self._holding():
            self._report(
                ToggleSlashError(f"closed with {pending} usage counts unsent: the server asked to wait")
            )

    def _holding(self) -> bool:
        return self._clock.monotonic() < self._hold_until

    def _merge(self, entries: list[tuple[Entry, int]]) -> None:
        with self._counts_lock:
            for key, count in entries:
                self._counts[key] = self._counts.get(key, 0) + count

    def _send(self, errors: list[Exception]) -> None:
        if self._holding():
            return
        with self._counts_lock:
            entries = list(self._counts.items())
            self._counts = {}
        for start in range(0, len(entries), MAX_BATCH):
            if not self._post(entries[start : start + MAX_BATCH], errors):
                self._merge(entries[start:])
                return

    def _post(self, batch: list[tuple[Entry, int]], errors: list[Exception]) -> bool:
        """False when this batch, and those after it, are to be kept for another try."""
        body = json.dumps(
            {
                "events": [
                    {"flag": flag, "variationIndex": index, "count": count, "day": day}
                    for (flag, index, day), count in batch
                ]
            },
            separators=(",", ":"),
        ).encode("ascii")
        headers = {
            "Authorization": f"Bearer {self._key}",
            "Content-Type": "application/json",
        }
        try:
            answer = http.request(
                self._base,
                self._guards,
                self._clock,
                "POST",
                "/sdk/v1/events",
                headers,
                body,
                timeout=self._timeout,
            )
        except (http.RequestFailure, http.Aborted) as failure:
            errors.append(ToggleSlashError("POST /sdk/v1/events failed: " + _describe(failure)))
            return False
        if 200 <= answer.status < 300:
            return True
        wait = http.retry_after(answer.status, answer.headers)
        if wait is not None:
            self._hold_until = max(self._hold_until, self._clock.monotonic() + wait)
        errors.append(ToggleSlashError(f"POST /sdk/v1/events answered {answer.status}"))
        return not _retryable(answer.status)


def _describe(failure: Exception) -> str:
    return failure.code if isinstance(failure, http.RequestFailure) else "abandoned"
