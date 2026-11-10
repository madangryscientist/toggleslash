"""Usage counts: kept in memory per (flag, variation index, UTC day) and sent in batches (contract 9)."""

from __future__ import annotations

import json
import threading
from collections.abc import Callable
from datetime import UTC, datetime

from ._http import EVENTS_ROUTE, Aborted, Aborter, Transport, TransportError
from ._retry import retry_after
from ._types import ToggleSlashError

# The server takes 1000 events and 128 KiB a request; 500 entries stay under both.
MAX_BATCH = 500

Key = tuple[str, int, str]


def _retryable(status: int) -> bool:
    return status >= 500 or status in {408, 429}


class Usage:
    """The counts and the sender. `record` is a dictionary add under a lock no request ever holds."""

    def __init__(
        self,
        transport: Transport,
        sdk_key: str,
        request_timeout: float,
        report: Callable[[BaseException], None],
        monotonic: Callable[[], float],
        wall: Callable[[], datetime],
    ) -> None:
        self._transport = transport
        self._headers = {"Authorization": f"Bearer {sdk_key}", "Content-Type": "application/json"}
        self._timeout = request_timeout
        self._report = report
        self._monotonic = monotonic
        self._wall = wall
        self._lock = threading.Lock()  # guards _counts and _recording
        self._counts: dict[Key, int] = {}
        self._recording = True
        self._flush_lock = threading.Lock()  # flushes are serial
        self._hold_until = 0.0  # a 429's Retry-After, on the monotonic clock; only read and written under _flush_lock

    def record(self, flag: str, variation_index: int) -> None:
        day = self._wall().astimezone(UTC).strftime("%Y-%m-%d")
        key = (flag, variation_index, day)
        with self._lock:
            if self._recording:
                self._counts[key] = self._counts.get(key, 0) + 1

    def stop_recording(self) -> None:
        with self._lock:
            self._recording = False

    def flush(self, *, final: bool = False) -> None:
        """Sends what is pending. Nothing it meets raises; every failure goes to `report`, after the locks
        are released. A final flush does not wait out a hold, and says so when it loses counts to one."""
        errors: list[BaseException] = []
        with self._flush_lock:
            self._send(errors, final)
        for error in errors:
            self._report(error)

    def _send(self, errors: list[BaseException], final: bool) -> None:
        if self._monotonic() < self._hold_until:
            if final:
                with self._lock:
                    pending = len(self._counts)
                if pending:
                    errors.append(
                        ToggleSlashError(f"closed with {pending} usage counts unsent: the server asked to wait")
                    )
            return
        with self._lock:
            counts, self._counts = self._counts, {}
        entries = [(key, count) for key, count in counts.items()]
        for start in range(0, len(entries), MAX_BATCH):
            batch = entries[start : start + MAX_BATCH]
            if not self._post(batch, errors):
                self._put_back(entries[start:])
                return

    def _put_back(self, entries: list[tuple[Key, int]]) -> None:
        with self._lock:
            for key, count in entries:
                self._counts[key] = self._counts.get(key, 0) + count

    def _post(self, batch: list[tuple[Key, int]], errors: list[BaseException]) -> bool:
        """False when the batch is to be kept for another try."""
        payload = {
            "events": [
                {"flag": flag, "variationIndex": index, "count": count, "day": day}
                for (flag, index, day), count in batch
            ]
        }
        body = json.dumps(payload).encode("ascii")
        aborter = Aborter(self._timeout)
        try:
            reply = self._transport("POST", EVENTS_ROUTE, self._headers, body, aborter)
            try:
                status = reply.status
                header = reply.header("Retry-After")
            finally:
                reply.close()
        except (TransportError, Aborted) as error:
            errors.append(error)
            return False
        finally:
            aborter.stop_timer()
        if 200 <= status < 300:
            return True
        wait = retry_after(status, header)
        if wait is not None:
            self._hold_until = max(self._hold_until, self._monotonic() + wait)
        errors.append(ToggleSlashError(f"POST {EVENTS_ROUTE} answered {status}"))
        return not _retryable(status)
