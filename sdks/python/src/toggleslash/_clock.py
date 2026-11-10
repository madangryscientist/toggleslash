"""Time as the SDK sees it, behind a seam so a test moves it by hand instead of waiting."""

from __future__ import annotations

import threading
import time
from typing import Protocol


class Clock(Protocol):
    def now_ms(self) -> int:
        """Milliseconds since the Unix epoch."""
        ...

    def wait(self, event: threading.Event, seconds: float) -> bool:
        """Blocks until `event` is set or `seconds` have passed on this clock. True if the event is set."""
        ...


class SystemClock:
    def now_ms(self) -> int:
        return int(time.time() * 1000)

    def wait(self, event: threading.Event, seconds: float) -> bool:
        return event.wait(seconds)


def utc_day(epoch_ms: int) -> str:
    """The UTC date of a time, `yyyy-MM-dd`."""
    return time.strftime("%Y-%m-%d", time.gmtime(epoch_ms // 1000))
