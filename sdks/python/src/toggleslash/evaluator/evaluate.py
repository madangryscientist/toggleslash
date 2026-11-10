"""Evaluating one flag for one context, to "Evaluation" in docs/evaluation.md.

It never raises: every failure, a prerequisite cycle included, is an ERROR result.
"""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, field
from typing import Any

from .clause import attribute_clause_matches
from .context import Contexts, kind_bucket, normalise_context
from .result import ErrorCode, Reason, ReasonKind, Result
from .ruleset import FlagEntry, ReadySegment, Ruleset, lists_context, targeted_variation

MAX_PREREQUISITE_DEPTH = 32


@dataclass(slots=True)
class _Run:
    ruleset: Ruleset
    contexts: Contexts
    active: set[str] = field(default_factory=set)
    completed: dict[str, Result] = field(default_factory=dict)


def _failure(code: ErrorCode) -> Result:
    return Result(None, None, Reason(ReasonKind.ERROR, code=code))


def _served(variations: list[Any], index: int, reason: Reason) -> Result:
    return Result(variations[index], index, reason)


def _off_result(header: Mapping[str, Any], reason: Reason) -> Result:
    index = header.get("offVariation")
    if index is None:
        return Result(None, None, reason)
    return _served(header["variations"], int(index), reason)


def _rollout_variation(rollout: Mapping[str, Any], position: int) -> int:
    chosen = 0
    cumulative = 0
    for item in rollout["variations"]:
        chosen = int(item["variation"])
        cumulative += item["weight"]
        if position < cumulative:
            break
    return chosen


def _serve_result(flag: Mapping[str, Any], serve: Mapping[str, Any], run: _Run, reason: Reason):
    rollout = serve.get("rollout")
    if rollout is not None:
        position = kind_bucket(flag["key"], flag["salt"], rollout.get("contextKind"), run.contexts)
        index = _rollout_variation(rollout, position)
    else:
        index = int(serve["variation"])
    return _served(flag["variations"], index, reason)


def _segment_rule_matches(rule: Mapping[str, Any], ready: ReadySegment, contexts: Contexts) -> bool:
    if not all(attribute_clause_matches(clause, contexts) for clause in rule["clauses"]):
        return False
    weight = rule.get("weight")
    if weight is None:
        return True
    segment = ready.segment
    bucket = kind_bucket(segment["key"], segment["salt"], rule.get("rolloutContextKind"), contexts)
    return bucket < weight


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
        if isinstance(ready, ReadySegment) and _segment_matches(ready, run.contexts):
            matched = True
            break
    return not matched if clause.get("negate") is True else matched


def _prerequisite_met(prerequisite: Mapping[str, Any], result: Result) -> bool:
    kind = result.reason.kind
    reached = kind in (ReasonKind.TARGET_MATCH, ReasonKind.RULE_MATCH, ReasonKind.FALLTHROUGH)
    return reached and result.variation_index == prerequisite["variation"]


def _check_prerequisites(flag: Mapping[str, Any], run: _Run, depth: int) -> Result | None:
    for prerequisite in flag["prerequisites"]:
        result = _evaluate_flag(run, prerequisite["flagKey"], depth + 1)
        if result.reason.kind == ReasonKind.ERROR:
            return result
        if not _prerequisite_met(prerequisite, result):
            reason = Reason(ReasonKind.PREREQUISITE_FAILED, flag_key=prerequisite["flagKey"])
            return _off_result(flag, reason)
    return None


def _evaluate_entry(entry: FlagEntry, run: _Run, depth: int) -> Result:
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
    for rule_index, rule in enumerate(flag["rules"]):
        if all(_clause_matches(clause, run) for clause in rule["clauses"]):
            reason = Reason(ReasonKind.RULE_MATCH, rule_index=rule_index)
            return _serve_result(flag, rule, run, reason)
    return _serve_result(flag, flag["fallthrough"], run, Reason(ReasonKind.FALLTHROUGH))


def _evaluate_flag(run: _Run, key: str, depth: int) -> Result:
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


def evaluate(ruleset: Ruleset, flag_key: str, context: object) -> Result:
    """Evaluates one flag for one context. It does not raise."""
    contexts = normalise_context(context)
    if contexts is None:
        return _failure(ErrorCode.INVALID_CONTEXT)
    return _evaluate_flag(_Run(ruleset, contexts), flag_key, 0)
