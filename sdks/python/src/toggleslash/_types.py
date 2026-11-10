"""The public value types and the closed sets, with the contract's canonical spelling."""

from __future__ import annotations

import enum
from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime
from typing import Any


class ReasonKind(enum.StrEnum):
    """Why a flag gave the value it gave (contract 4)."""

    OFF = "OFF"
    PREREQUISITE_FAILED = "PREREQUISITE_FAILED"
    TARGET_MATCH = "TARGET_MATCH"
    RULE_MATCH = "RULE_MATCH"
    FALLTHROUGH = "FALLTHROUGH"
    ERROR = "ERROR"


class ErrorCode(enum.StrEnum):
    """The code of an ``ERROR`` reason: the evaluator's and the SDK's own."""

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
    """The state of the stream in ``Status.stream`` (contract 8)."""

    OFF = "off"
    CONNECTING = "connecting"
    CONNECTED = "connected"
    DISCONNECTED = "disconnected"
    CLOSED = "closed"


class LastError:
    """The codes of ``Status.last_error`` that are fixed; ``http_<status>`` is built from a status."""

    TIMEOUT = "timeout"
    BAD_RULESET = "bad_ruleset"
    NETWORK = "network"


@dataclass(frozen=True, slots=True)
class Reason:
    """The reason of an evaluation. ``code`` is set for ``ERROR``, ``rule_index`` for ``RULE_MATCH``
    and ``flag_key`` (the direct prerequisite that failed) for ``PREREQUISITE_FAILED``."""

    kind: ReasonKind
    code: ErrorCode | None = None
    rule_index: int | None = None
    flag_key: str | None = None


@dataclass(frozen=True, slots=True)
class Detail:
    """What ``variation_detail`` returns."""

    value: Any
    variation_index: int | None
    reason: Reason


@dataclass(frozen=True, slots=True)
class Status:
    """A snapshot of what the SDK holds (contract 8). It never holds a URL, key, context or message."""

    etag: str | None
    last_sync_at: datetime | None
    stream: StreamState
    last_error: str | None


@dataclass(frozen=True, slots=True)
class EvaluationEvent:
    """What ``after_evaluation`` receives (contract 10)."""

    flag_key: str
    context: Any
    value: Any
    variation_index: int | None
    reason: Reason
    etag: str | None
    flag_version: int | None


@dataclass(frozen=True, slots=True)
class Hooks:
    """The hooks an application can install. A hook that is not callable is ignored."""

    after_evaluation: Callable[[EvaluationEvent], Any] | None = None


class ToggleSlashError(Exception):
    """An error the SDK builds for ``on_error``. ``code`` is a short stable string. It never carries a
    context, a context key or an attribute."""

    def __init__(self, message: str, code: str) -> None:
        super().__init__(message)
        self.code = code
