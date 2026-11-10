"""The values the SDK hands to a caller: results, status, hook events and the closed sets."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum
from typing import Any

from .evaluator import ErrorCode, Reason, ReasonKind


class StreamState(StrEnum):
    """Where the stream is (contract section 8)."""

    OFF = "off"
    CONNECTING = "connecting"
    CONNECTED = "connected"
    DISCONNECTED = "disconnected"
    CLOSED = "closed"


class ToggleSlashError(Exception):
    """A failure the SDK absorbed and tells ``on_error`` about.

    ``code`` is a ``lastError`` code of contract section 8 for a ruleset or stream attempt, and ``None``
    for any other failure. The message never holds a URL, a key, a context or an attribute.
    """

    def __init__(self, message: str, code: str | None = None) -> None:
        super().__init__(message)
        self.code = code


@dataclass(frozen=True, slots=True)
class Detail:
    """What ``variation_detail`` returns: the value the caller gets, the variation index and the reason."""

    value: Any
    variation_index: int | None
    reason: Reason


@dataclass(frozen=True, slots=True)
class Status:
    """What the SDK has loaded and how its stream is doing: one consistent snapshot, kept local."""

    etag: str | None
    last_sync_at: datetime | None
    stream: StreamState
    last_error: str | None


@dataclass(frozen=True, slots=True)
class EvaluationEvent:
    """What one evaluation did, handed to ``Hooks.after_evaluation``.

    It carries the context the caller passed; the SDK itself sends none of it anywhere.
    """

    flag_key: str
    context: Any
    value: Any
    variation_index: int | None
    reason: Reason
    etag: str | None
    flag_version: int | None


@dataclass(frozen=True, slots=True)
class Hooks:
    """Observers of each evaluation. ``after_evaluation`` runs on the caller's thread, synchronously."""

    after_evaluation: Callable[[EvaluationEvent], object] | None = None


__all__ = [
    "Detail",
    "ErrorCode",
    "EvaluationEvent",
    "Hooks",
    "Reason",
    "ReasonKind",
    "Status",
    "StreamState",
    "ToggleSlashError",
]
