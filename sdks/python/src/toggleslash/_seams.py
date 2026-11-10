"""Test seams: options on `create`, never a global and never an environment variable."""

from __future__ import annotations

import random
import ssl
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any

from ._clock import Clock, SystemClock
from ._evaluate import evaluate


@dataclass(frozen=True, slots=True)
class Seams:
    """`clock` (time and waiting), `jitter` (a float in [0, 1)), `watchdog` (seconds of silence the stream may
    have, 60 by default), `evaluator` (replaces the evaluator, to make it raise) and `ssl_context` (the trust
    for an `https` URL, defaulting to the system's)."""

    clock: Clock = field(default_factory=SystemClock)
    jitter: Callable[[], float] = random.random
    watchdog: float = 60.0
    evaluator: Callable[[Any, str, Any], Any] = evaluate
    ssl_context: ssl.SSLContext | None = None
