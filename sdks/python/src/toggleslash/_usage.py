"""Usage counts: how often each flag variation was served, per UTC day, sent in batches.

`record` is a dict increment under a short lock, so it is safe on a request path. A batch that fails for a reason
that can pass (the server is down or busy) is put back; one the server refused is dropped, since it would be
refused again. After a 429 with a valid `Retry-After`, nothing is sent until that has passed. The counts carry a
flag key, a variation index and a day, and nothing about an end user.
"""

from __future__ import annotations

import threading
from collections.abc import Callable

from . import _json
from ._clock import Clock, utc_day
from ._errors import SdkError
from ._hold import Hold, parse_retry_after
from ._http import AbortGroup, HttpError, Transport

# The server takes 1000 events and 128 KiB a request; an entry is at most about 140 bytes, so 500 stay under both.
MAX_BATCH = 500

_Key = tuple[str, int, str]


def _retryable(status: int) -> bool:
    return status >= 500 or status in {408, 429}


class UsageReporter:
    def __init__(
        self,
        transport: Transport,
        sdk_key: str,
        clock: Clock,
        request_timeout: float,
        on_error: Callable[[BaseException], None],
    ) -> None:
        self._transport = transport
        self._sdk_key = sdk_key
        self._clock = clock
        self._timeout = request_timeout
        self._on_error = on_error
        self._lock = threading.Lock()
        self._flush_lock = threading.Lock()
        self._pending: dict[_Key, int] = {}
        self._hold = Hold(clock)
        self._group = AbortGroup()

    def record(self, flag: str, variation_index: int) -> None:
        key = (flag, variation_index, utc_day(self._clock.now_ms()))
        with self._lock:
            self._pending[key] = self._pending.get(key, 0) + 1

    def pending_count(self) -> int:
        with self._lock:
            return len(self._pending)

    def flush(self) -> None:
        """Sends what is pending. It never raises; a call during a flush waits for that flush first."""
        with self._flush_lock:
            try:
                self._send()
            except Exception as error:
                self._on_error(error)

    def finish(self) -> None:
        """The last flush, on closing. It does not wait out a hold and does not ignore one: counts still held
        back are lost, and that is reported."""
        self.flush()
        left = self.pending_count()
        if left > 0 and self._hold.active():
            self._on_error(SdkError(f"closed with {left} usage counts unsent: the server asked to wait"))

    def _add(self, key: _Key, count: int) -> None:
        with self._lock:
            self._pending[key] = self._pending.get(key, 0) + count

    def _send(self) -> None:
        if self._hold.active():
            return
        with self._lock:
            entries = list(self._pending.items())
            self._pending = {}
        for start in range(0, len(entries), MAX_BATCH):
            batch = entries[start : start + MAX_BATCH]
            if not self._post(batch):
                for key, count in entries[start:]:
                    self._add(key, count)
                return

    def _post(self, batch: list[tuple[_Key, int]]) -> bool:
        """False when the batch should be kept for another try."""
        body = _json.dumps(
            {
                "events": [
                    {"flag": flag, "variationIndex": index, "count": count, "day": day}
                    for (flag, index, day), count in batch
                ]
            }
        ).encode("utf-8")
        headers = {
            "Authorization": f"Bearer {self._sdk_key}",
            "Content-Type": "application/json",
        }
        try:
            with self._transport.open("POST", "/sdk/v1/events", headers, body, self._timeout, self._group) as response:
                status = response.status
                if 200 <= status < 300:
                    return True
                if status == 429:
                    wait = parse_retry_after(response.header("Retry-After"))
                    if wait is not None:
                        self._hold.extend(wait)
        except HttpError as error:
            self._on_error(error)
            return False
        self._on_error(SdkError(f"POST /sdk/v1/events answered {status}"))
        return not _retryable(status)
