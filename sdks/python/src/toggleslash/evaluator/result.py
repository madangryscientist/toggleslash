"""The result of an evaluation and the closed sets it names."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum
from typing import Any


class ReasonKind(StrEnum):
    OFF = "OFF"
    PREREQUISITE_FAILED = "PREREQUISITE_FAILED"
    TARGET_MATCH = "TARGET_MATCH"
    RULE_MATCH = "RULE_MATCH"
    FALLTHROUGH = "FALLTHROUGH"
    ERROR = "ERROR"


class ErrorCode(StrEnum):
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


@dataclass(frozen=True, slots=True)
class Reason:
    """Why a flag evaluated as it did. Only the field its kind carries is set."""

    kind: ReasonKind
    rule_index: int | None = None
    flag_key: str | None = None
    code: ErrorCode | None = None

    def to_json(self) -> dict[str, Any]:
        """The reason in the spec's JSON shape."""
        out: dict[str, Any] = {"kind": str(self.kind)}
        if self.rule_index is not None:
            out["ruleIndex"] = self.rule_index
        if self.flag_key is not None:
            out["flagKey"] = self.flag_key
        if self.code is not None:
            out["code"] = str(self.code)
        return out


@dataclass(frozen=True, slots=True)
class Result:
    """What one evaluation produced: the value, its index in ``variations`` and the reason."""

    value: Any
    variation_index: int | None
    reason: Reason

    def to_json(self) -> dict[str, Any]:
        return {
            "value": self.value,
            "variationIndex": self.variation_index,
            "reason": self.reason.to_json(),
        }
