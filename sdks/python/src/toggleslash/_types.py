"""The values the SDK hands to a caller: results, status, events, and the closed sets of the contract."""

from __future__ import annotations

import datetime
from collections.abc import Callable
from dataclasses import dataclass
from enum import StrEnum
from typing import Any


class ReasonKind(StrEnum):
    """Why a flag evaluated the way it did (docs/evaluation.md)."""

    OFF = "OFF"
    PREREQUISITE_FAILED = "PREREQUISITE_FAILED"
    TARGET_MATCH = "TARGET_MATCH"
    RULE_MATCH = "RULE_MATCH"
    FALLTHROUGH = "FALLTHROUGH"
    ERROR = "ERROR"


class ErrorCode(StrEnum):
    """The code of an `ERROR` reason: the evaluator's, then the SDK's own (contract 4)."""

    INVALID_CONTEXT = "INVALID_CONTEXT"
    FLAG_NOT_FOUND = "FLAG_NOT_FOUND"
    SEGMENT_NOT_FOUND = "SEGMENT_NOT_FOUND"
    MALFORMED_FLAG = "MALFORMED_FLAG"
    MALFORMED_SEGMENT = "MALFORMED_SEGMENT"
    PREREQUISITE_CYCLE = "PREREQUISITE_CYCLE"
    PREREQUISITE_DEPTH_EXCEEDED = "PREREQUISITE_DEPTH_EXCEEDED"
    NOT_READY = "NOT_READY"
    TYPE_MISMATCH = "TYPE_MISMATCH"
    EVALUATION_FAILED = "EVALUATION_FAILED"


class StreamState(StrEnum):
    """How the stream is doing (contract 8)."""

    OFF = "off"
    CONNECTING = "connecting"
    CONNECTED = "connected"
    DISCONNECTED = "disconnected"
    CLOSED = "closed"


class LastError(StrEnum):
    """Why the most recent ruleset or stream attempt failed, besides `http_<status>` (contract 8)."""

    TIMEOUT = "timeout"
    BAD_RULESET = "bad_ruleset"
    NETWORK = "network"


@dataclass(frozen=True, slots=True)
class Reason:
    """The evaluator's reason. `code` is set for an `ERROR`, `rule_index` for `RULE_MATCH`, `flag_key` for
    `PREREQUISITE_FAILED` (the direct prerequisite)."""

    kind: ReasonKind
    code: ErrorCode | None = None
    rule_index: int | None = None
    flag_key: str | None = None


@dataclass(frozen=True, slots=True)
class Detail:
    """What `variation_detail` answers: the value (the served one, or the fallback), the variation index
    (None when the value is the fallback) and the reason."""

    value: Any
    variation_index: int | None
    reason: Reason


@dataclass(frozen=True, slots=True)
class Status:
    """A snapshot of what the SDK has loaded. `last_error` is a `LastError` or `"http_<status>"`; it never
    holds a URL, a key, a message or a context."""

    etag: str | None
    last_sync_at: datetime.datetime | None
    stream: StreamState
    last_error: str | None


@dataclass(frozen=True, slots=True)
class EvaluationEvent:
    """What one evaluation did, handed to `Hooks.after_evaluation`. It carries the context the caller passed;
    the SDK itself sends none of it anywhere."""

    flag_key: str
    context: Any
    value: Any
    variation_index: int | None
    reason: Reason
    etag: str | None
    flag_version: int | None


@dataclass(frozen=True, slots=True)
class Hooks:
    """Observers of each evaluation. `after_evaluation` is called on the caller's thread with an
    `EvaluationEvent`; its return value is ignored, and an error it raises goes to `on_error`."""

    after_evaluation: Callable[[EvaluationEvent], Any] | None = None


class ToggleSlashError(Exception):
    """An error the SDK absorbed and tells `on_error` about. `code` is the `LastError` or `http_<status>` of a
    failed ruleset or stream attempt, and `None` for the other failures."""

    def __init__(self, message: str, code: str | None = None) -> None:
        super().__init__(message)
        self.code = code
