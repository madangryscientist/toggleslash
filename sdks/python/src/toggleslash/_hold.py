"""What a 429 asked for: nothing reaches the server before a time, shared by every request that honours it."""

from __future__ import annotations

import threading

from ._clock import Clock

MAX_RETRY_AFTER_SECONDS = 300


def parse_retry_after(value: str | None) -> int | None:
    """Whole decimal seconds, capped at 300. A date, a fraction, a sign or anything else is no hold."""
    if value is None:
        return None
    text = value.strip()
    if not text or not text.isascii() or not text.isdecimal():
        return None
    return min(int(text), MAX_RETRY_AFTER_SECONDS)


class Hold:
    def __init__(self, clock: Clock) -> None:
        self._clock = clock
        self._lock = threading.Lock()
        self._until_ms = 0

    def extend(self, seconds: int) -> None:
        """A later hold only lengthens this one."""
        until = self._clock.now_ms() + seconds * 1000
        with self._lock:
            self._until_ms = max(self._until_ms, until)

    def remaining(self) -> float:
        """Seconds still to wait, 0 when none."""
        with self._lock:
            until = self._until_ms
        return max(0.0, (until - self._clock.now_ms()) / 1000)

    def active(self) -> bool:
        return self.remaining() > 0
