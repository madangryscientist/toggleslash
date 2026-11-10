"""The public value types: the closed sets of the contract and the results the client hands back."""

from __future__ import annotations

import enum
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from datetime import datetime
from typing import Any


class ReasonKind(enum.StrEnum):
    """Why a flag served what it served. The text is the canonical spelling."""

    OFF = "OFF"
    PREREQUISITE_FAILED = "PREREQUISITE_FAILED"
    TARGET_MATCH = "TARGET_MATCH"
    RULE_MATCH = "RULE_MATCH"
    FALLTHROUGH = "FALLTHROUGH"
    ERROR = "ERROR"


class ErrorCode(enum.StrEnum):
    """The code of an `ERROR` reason: the evaluator's seven and the SDK's three."""

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
    """Where the live-update stream is."""

    OFF = "off"
    CONNECTING = "connecting"
    CONNECTED = "connected"
    DISCONNECTED = "disconnected"
    CLOSED = "closed"


# The fixed `last_error` codes. A failed request with a status is `http_<status>`.
TIMEOUT = "timeout"
BAD_RULESET = "bad_ruleset"
NETWORK = "network"


class ToggleSlashError(Exception):
    """A failure the client absorbed and handed to `on_error`. Its text names a route or a status, never a
    URL, a key or anything about a context."""


@dataclass(frozen=True, slots=True)
class Reason:
    """The reason of one evaluation. A field that does not apply to `kind` is `None`."""

    kind: ReasonKind
    rule_index: int | None = None
    flag_key: str | None = None
    code: ErrorCode | None = None


@dataclass(frozen=True, slots=True)
class VariationDetail:
    """A served value, the index of the variation it came from (`None` for the fallback) and why."""

    value: Any
    variation_index: int | None
    reason: Reason


@dataclass(frozen=True, slots=True)
class ClientStatus:
    """A consistent snapshot of the client's sync state."""

    etag: str | None
    last_sync_at: datetime | None
    stream: StreamState
    last_error: str | None


@dataclass(frozen=True, slots=True)
class EvaluationEvent:
    """What `Hooks.after_evaluation` receives after every evaluation."""

    flag_key: str
    context: Any
    value: Any
    variation_index: int | None
    reason: Reason
    etag: str | None
    flag_version: int | float | None


@dataclass(frozen=True, slots=True)
class Hooks:
    """Callbacks the client calls. `after_evaluation` runs on the thread that evaluated."""

    after_evaluation: Callable[[EvaluationEvent], None] | None = None


Context = Mapping[str, Any]
