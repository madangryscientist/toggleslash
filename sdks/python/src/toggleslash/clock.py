"""Time as the SDK sees it, injectable so a test moves it by hand instead of waiting."""

from __future__ import annotations

import threading
import time
from typing import Protocol


class Clock(Protocol):
    """The three things the SDK asks of time."""

    def now(self) -> float:
        """Seconds since the Unix epoch (the wall clock): ``last_sync_at`` and the UTC day."""

    def monotonic(self) -> float:
        """Seconds on a clock that never goes back: a ``Retry-After`` hold is a point on this clock."""

    def wait(self, event: threading.Event, seconds: float) -> bool:
        """Block until ``event`` is set or ``seconds`` pass on this clock. True if the event was set."""


class SystemClock:
    """The real clock."""

    def now(self) -> float:
        return time.time()

    def monotonic(self) -> float:
        return time.monotonic()

    def wait(self, event: threading.Event, seconds: float) -> bool:
        return event.wait(max(seconds, 0.0))


SYSTEM_CLOCK = SystemClock()


def utc_day(epoch_seconds: float) -> str:
    """The UTC date of a time, ``yyyy-MM-dd``, as ``POST /sdk/v1/events`` wants it."""
    parts = time.gmtime(epoch_seconds)
    return f"{parts.tm_year:04d}-{parts.tm_mon:02d}-{parts.tm_mday:02d}"
