"""Test seams (contract 13): what a test replaces to control time, the network and the dice. They are the
private `_seams` argument of `Client`, never a global and never read from the environment."""

from __future__ import annotations

import random
import time
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any

from ._http import Transport

WATCHDOG_SECONDS = 60.0


def _wall() -> datetime:
    return datetime.now(UTC)


@dataclass
class Seams:
    transport: Transport | None = None
    monotonic: Callable[[], float] = time.monotonic
    wall: Callable[[], datetime] = _wall
    # `wait(seconds)` returns True when the client was stopped. `None` is `stop_event.wait`.
    wait: Callable[[float], bool] | None = None
    jitter: Callable[[], float] = random.random
    watchdog: float = WATCHDOG_SECONDS
    evaluator: Callable[..., Any] | None = field(default=None)
