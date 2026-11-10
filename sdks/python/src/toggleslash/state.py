"""What the client has loaded, as one immutable value swapped by one assignment."""

from __future__ import annotations

import threading
from dataclasses import dataclass, replace
from typing import Any

from .evaluator import Ruleset
from .types import StreamState


@dataclass(frozen=True, slots=True)
class State:
    """The ruleset and the ETag that names it travel together, so a reader never sees a mix."""

    ruleset: Ruleset | None
    etag: str | None
    last_sync_at: float | None
    stream: StreamState
    last_error: str | None


class StateBox:
    """The current :class:`State`. A read is one attribute load; a write builds a new state under a lock
    that covers nothing but that build, never I/O and never a callback."""

    def __init__(self, stream_on: bool) -> None:
        self._lock = threading.Lock()
        self._state = State(None, None, None, StreamState.CONNECTING if stream_on else StreamState.OFF, None)

    def get(self) -> State:
        return self._state

    def update(self, **changes: Any) -> None:
        with self._lock:
            if self._state.stream is StreamState.CLOSED:
                changes.pop("stream", None)
            self._state = replace(self._state, **changes)

    def close(self) -> None:
        with self._lock:
            self._state = replace(self._state, stream=StreamState.CLOSED)
