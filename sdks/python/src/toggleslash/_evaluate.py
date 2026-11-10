"""The evaluator, to docs/evaluation.md. It never raises for a ruleset or a context of any shape.

It has no network code.
"""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from ._clause import attribute_clause_matches
from ._context import Contexts, kind_bucket, normalise_context
from ._ruleset import (
    MALFORMED_SEGMENT,
    FlagEntry,
    ReadySegment,
    Ruleset,
    lists_context,
    targeted_variation,
)
from ._types import ErrorCode, EvalResult, Reason, ReasonKind

MAX_PREREQUISITE_DEPTH = 32


class _Run:
    __slots__ = ("active", "completed", "contexts", "ruleset")

    def __init__(self, ruleset: Ruleset, contexts: Contexts) -> None:
        self.ruleset = ruleset
        self.contexts = contexts
        self.active: set[str] = set()
        self.completed: dict[str, EvalResult] = {}


def _failure(code: ErrorCode) -> EvalResult:
    return EvalResult(None, None, Reason(ReasonKind.ERROR, code=code))


def _served(variations: list[Any], index: int, reason: Reason) -> EvalResult:
    index = int(index)
    value = variations[index] if 0 <= index < len(variations) else None
    return EvalResult(value, index, reason)


def _off_result(header: Mapping[str, Any], reason: Reason) -> EvalResult:
    index = header.get("offVariation")
    if index is None:
        return EvalResult(None, None, reason)
    return _served(header["variations"], index, reason)


def _rollout_variation(rollout: Mapping[str, Any], position: int) -> int:
    chosen = 0
    cumulative = 0
    for entry in rollout["variations"]:
        chosen = int(entry["variation"])
        cumulative += entry["weight"]
        if position < cumulative:
            break
    return chosen


def _serve_result(flag: Mapping[str, Any], serve: Mapping[str, Any], contexts: Contexts, reason: Reason) -> EvalResult:
    rollout = serve.get("rollout")
    if rollout:
        index = _rollout_variation(
            rollout, kind_bucket(flag["key"], flag["salt"], rollout.get("contextKind"), contexts)
        )
    else:
        index = serve["variation"]
    return _served(flag["variations"], index, reason)


def _segment_rule_matches(rule: Mapping[str, Any], ready: ReadySegment, contexts: Contexts) -> bool:
    if not all(attribute_clause_matches(clause, contexts) for clause in rule["clauses"]):
        return False
    weight = rule.get("weight")
    if weight is None:
        return True
    segment = ready.segment
    return kind_bucket(segment["key"], segment["salt"], rule.get("rolloutContextKind"), contexts) < weight


def _segment_matches(ready: ReadySegment, contexts: Contexts) -> bool:
    if lists_context(ready.excluded, contexts):
        return False
    if lists_context(ready.included, contexts):
        return True
    return any(_segment_rule_matches(rule, ready, contexts) for rule in ready.segment["rules"])


def _clause_matches(clause: Mapping[str, Any], run: _Run) -> bool:
    if clause["op"] != "segmentMatch":
        return attribute_clause_matches(clause, run.contexts)
    matched = False
    for key in clause["values"]:
        ready = run.ruleset.segments.get(key)
        if ready is not None and ready != MALFORMED_SEGMENT and _segment_matches(ready, run.contexts):  # type: ignore[arg-type]
            matched = True
            break
    return not matched if clause.get("negate") is True else matched


_SERVED_KINDS = (ReasonKind.TARGET_MATCH, ReasonKind.RULE_MATCH, ReasonKind.FALLTHROUGH)


def _prerequisite_met(prerequisite: Mapping[str, Any], result: EvalResult) -> bool:
    return result.reason.kind in _SERVED_KINDS and result.variation_index == prerequisite["variation"]


def _check_prerequisites(flag: Mapping[str, Any], run: _Run, depth: int) -> EvalResult | None:
    for prerequisite in flag["prerequisites"]:
        result = _evaluate_flag(run, prerequisite["flagKey"], depth + 1)
        if result.reason.kind == ReasonKind.ERROR:
            return result
        if not _prerequisite_met(prerequisite, result):
            reason = Reason(ReasonKind.PREREQUISITE_FAILED, flag_key=prerequisite["flagKey"])
            return _off_result(flag, reason)
    return None


def _evaluate_entry(entry: FlagEntry, run: _Run, depth: int) -> EvalResult:
    if entry.status == "malformed":
        return _failure(ErrorCode.MALFORMED_FLAG)
    flag = entry.header
    assert flag is not None
    if not flag["on"]:
        return _off_result(flag, Reason(ReasonKind.OFF))
    if entry.status == "broken":
        assert entry.code is not None
        return _failure(entry.code)
    blocked = _check_prerequisites(flag, run, depth)
    if blocked is not None:
        return blocked
    assert entry.targets is not None
    targeted = targeted_variation(entry.targets, run.contexts)
    if targeted is not None:
        return _served(flag["variations"], targeted, Reason(ReasonKind.TARGET_MATCH))
    for index, rule in enumerate(flag["rules"]):
        if all(_clause_matches(clause, run) for clause in rule["clauses"]):
            reason = Reason(ReasonKind.RULE_MATCH, rule_index=index)
            return _serve_result(flag, rule, run.contexts, reason)
    return _serve_result(flag, flag["fallthrough"], run.contexts, Reason(ReasonKind.FALLTHROUGH))


def _evaluate_flag(run: _Run, key: str, depth: int) -> EvalResult:
    completed = run.completed.get(key)
    if completed is not None:
        return completed
    if key in run.active:
        return _failure(ErrorCode.PREREQUISITE_CYCLE)
    if depth > MAX_PREREQUISITE_DEPTH:
        return _failure(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED)
    entry = run.ruleset.flags.get(key)
    if entry is None:
        return _failure(ErrorCode.FLAG_NOT_FOUND)
    run.active.add(key)
    try:
        result = _evaluate_entry(entry, run, depth)
    finally:
        run.active.discard(key)
    run.completed[key] = result
    return result


def flag_version(ruleset: Ruleset, flag_key: str) -> int | None:
    """The `version` of the flag the evaluation read, when it is a ready flag."""
    entry = ruleset.flags.get(flag_key)
    if entry is None or entry.status == "malformed" or entry.header is None:
        return None
    version = entry.header.get("version")
    return int(version) if version is not None and not isinstance(version, bool) else None


def evaluate(ruleset: Ruleset, flag_key: str, context: object) -> EvalResult:
    """Evaluates one flag for one context. Every failure is an `ERROR` result."""
    contexts = normalise_context(context)
    if contexts is None:
        return _failure(ErrorCode.INVALID_CONTEXT)
    return _evaluate_flag(_Run(ruleset, contexts), flag_key, 0)
