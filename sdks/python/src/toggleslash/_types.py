"""The public value types. Nothing here does I/O."""

from __future__ import annotations

import enum
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, NamedTuple


class ReasonKind(enum.StrEnum):
    OFF = "OFF"
    PREREQUISITE_FAILED = "PREREQUISITE_FAILED"
    TARGET_MATCH = "TARGET_MATCH"
    RULE_MATCH = "RULE_MATCH"
    FALLTHROUGH = "FALLTHROUGH"
    ERROR = "ERROR"


class ErrorCode(enum.StrEnum):
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


class StreamState(enum.StrEnum):
    OFF = "off"
    CONNECTING = "connecting"
    CONNECTED = "connected"
    DISCONNECTED = "disconnected"
    CLOSED = "closed"


class LastError:
    """The spellings of `Status.last_error`."""

    TIMEOUT = "timeout"
    BAD_RULESET = "bad_ruleset"
    NETWORK = "network"

    @staticmethod
    def http(status: int) -> str:
        return f"http_{status}"


@dataclass(frozen=True, slots=True)
class Reason:
    """Why a value was served. `rule_index`, `flag_key` and `code` are set only for the kinds that carry them."""

    kind: ReasonKind
    rule_index: int | None = None
    flag_key: str | None = None
    code: ErrorCode | None = None

    def to_json(self) -> dict[str, Any]:
        out: dict[str, Any] = {"kind": str(self.kind)}
        if self.rule_index is not None:
            out["ruleIndex"] = self.rule_index
        if self.flag_key is not None:
            out["flagKey"] = self.flag_key
        if self.code is not None:
            out["code"] = str(self.code)
        return out


class EvalResult(NamedTuple):
    """What the evaluator answers: the served value, its index (or None) and the reason."""

    value: Any
    variation_index: int | None
    reason: Reason


@dataclass(frozen=True, slots=True)
class Detail:
    value: Any
    variation_index: int | None
    reason: Reason


@dataclass(frozen=True, slots=True)
class Status:
    etag: str | None
    last_sync_at: int | None
    stream: StreamState
    last_error: str | None


@dataclass(frozen=True, slots=True)
class EvaluationEvent:
    flag_key: str
    context: Any
    value: Any
    variation_index: int | None
    reason: Reason
    etag: str | None
    flag_version: int | None


@dataclass(frozen=True, slots=True)
class Hooks:
    after_evaluation: Callable[[EvaluationEvent], Any] | None = None
