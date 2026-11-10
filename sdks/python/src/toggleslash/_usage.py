"""Usage counts: (flag, variation index, UTC day) to a count, flushed in batches (contract 9)."""

from __future__ import annotations

import json
import threading
from collections.abc import Callable
from typing import Any

from ._http import Aborted, Endpoint, NetworkFailure, RequestTimeout, Transport

MAX_BATCH = 500
MAX_HOLD_SECONDS = 300.0


def retry_after(headers: Any) -> float | None:
    """The seconds a ``Retry-After`` asks for, capped at 300, or None when the header is not whole
    decimal digits (spaces around it are ignored)."""
    if headers is None:
        return None
    raw = headers.get("Retry-After")
    if not isinstance(raw, str):
        return None
    text = raw.strip(" ")
    if not text or not text.isascii() or not text.isdigit():
        return None
    if len(text) > 6:
        return MAX_HOLD_SECONDS
    return min(float(int(text)), MAX_HOLD_SECONDS)


def retryable(status: int) -> bool:
    return status >= 500 or status in (408, 429)


class Usage:
    """The counts and the flush. ``flush`` is serial; a hold keeps the counts and sends nothing."""

    def __init__(
        self,
        *,
        endpoint: Endpoint,
        headers: dict[str, str],
        transport: Transport,
        monotonic: Callable[[], float],
        request_timeout: float,
        report: Callable[[str, str], None],
    ) -> None:
        self._endpoint = endpoint
        self._headers = headers
        self._transport = transport
        self._monotonic = monotonic
        self._request_timeout = request_timeout
        self._report = report
        self._lock = threading.Lock()
        self._flush_lock = threading.Lock()
        self._counts: dict[tuple[str, int, str], int] = {}
        self._stopped = False
        self._hold_until = 0.0

    def add(self, flag: str, index: int, day: str) -> None:
        with self._lock:
            if self._stopped:
                return
            key = (flag, index, day)
            self._counts[key] = self._counts.get(key, 0) + 1

    def stop(self) -> None:
        """Nothing more is counted (called by ``close`` before the final flush)."""
        with self._lock:
            self._stopped = True

    def pending(self) -> int:
        with self._lock:
            return len(self._counts)

    def holding(self) -> bool:
        return self._monotonic() < self._hold_until

    def _readd(self, entries: list[tuple[tuple[str, int, str], int]]) -> None:
        with self._lock:
            for key, count in entries:
                self._counts[key] = self._counts.get(key, 0) + count

    def flush(self, lock_timeout: float = -1) -> None:
        """Send what is pending, in batches of at most 500, one request at a time. ``lock_timeout``
        bounds the wait for a flush already running (``close`` passes one)."""
        if not self._flush_lock.acquire(timeout=lock_timeout):
            return
        try:
            if self.holding():
                return
            with self._lock:
                if not self._counts:
                    return
                taken = list(self._counts.items())
                self._counts = {}
            start = 0
            while start < len(taken):
                batch = taken[start : start + MAX_BATCH]
                outcome = self._send(batch)
                if outcome == "kept":
                    self._readd(taken[start:])
                    return
                start += MAX_BATCH
        finally:
            self._flush_lock.release()

    def _send(self, batch: list[tuple[tuple[str, int, str], int]]) -> str:
        payload = {
            "events": [
                {"flag": flag, "variationIndex": index, "count": count, "day": day}
                for (flag, index, day), count in batch
            ]
        }
        body = json.dumps(payload, ensure_ascii=True, separators=(",", ":")).encode("ascii")
        headers = dict(self._headers)
        headers["Content-Type"] = "application/json"
        try:
            response = self._transport.fetch(
                self._endpoint,
                "POST",
                "/sdk/v1/events",
                headers,
                body,
                self._request_timeout,
                None,
            )
        except RequestTimeout:
            self._report("timeout", "usage request failed: timeout")
            return "kept"
        except (NetworkFailure, Aborted):
            self._report("network", "usage request failed: network")
            return "kept"
        status = response.status
        if 200 <= status < 300:
            return "sent"
        self._report(f"http_{status}", f"usage request failed: http_{status}")
        if status == 429:
            seconds = retry_after(response.headers)
            if seconds is not None:
                self._hold_until = max(self._hold_until, self._monotonic() + seconds)
        return "kept" if retryable(status) else "dropped"
