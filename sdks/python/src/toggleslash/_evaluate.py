"""Flag evaluation, to docs/evaluation.md: the order, the comparison rules and the reasons."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from ._clause import attribute_clause_matches
from ._context import Contexts, kind_bucket, normalise_context
from ._ruleset import FlagEntry, ReadySegment, Ruleset, lists_context, targeted_variation
from ._types import ErrorCode, Reason, ReasonKind, VariationDetail

# Prerequisite links from the requested flag; docs/evaluation.md pins the limit.
MAX_PREREQUISITE_DEPTH = 32

_OFF = Reason(ReasonKind.OFF)
_TARGET_MATCH = Reason(ReasonKind.TARGET_MATCH)
_FALLTHROUGH = Reason(ReasonKind.FALLTHROUGH)
_FAILURES = {code: VariationDetail(None, None, Reason(ReasonKind.ERROR, code=code)) for code in ErrorCode}
_REACHED_SERVE = frozenset({ReasonKind.TARGET_MATCH, ReasonKind.RULE_MATCH, ReasonKind.FALLTHROUGH})


class _Run:
    __slots__ = ("active", "completed", "contexts", "ruleset")

    def __init__(self, ruleset: Ruleset, contexts: Contexts) -> None:
        self.ruleset = ruleset
        self.contexts = contexts
        self.active: set[str] = set()
        self.completed: dict[str, VariationDetail] = {}


def failure(code: ErrorCode) -> VariationDetail:
    return _FAILURES[code]


def _served(variations: list[Any], index: Any, reason: Reason) -> VariationDetail:
    position = int(index)
    return VariationDetail(variations[position], position, reason)


def _off_result(flag: Mapping[str, Any], reason: Reason) -> VariationDetail:
    index = flag.get("offVariation")
    if index is None:
        return VariationDetail(None, None, reason)
    return _served(flag["variations"], index, reason)


def _rollout_variation(rollout: Mapping[str, Any], position: int) -> int:
    chosen = 0
    cumulative = 0
    for item in rollout["variations"]:
        chosen = int(item["variation"])
        cumulative += item["weight"]
        if position < cumulative:
            break
    return chosen


def _serve_result(flag: Mapping[str, Any], serve: Mapping[str, Any], contexts: Contexts, reason: Reason):
    rollout = serve.get("rollout")
    if rollout is None:
        return _served(flag["variations"], serve["variation"], reason)
    position = kind_bucket(flag["key"], flag["salt"], rollout.get("contextKind"), contexts)
    return _served(flag["variations"], _rollout_variation(rollout, position), reason)


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
        if ready is not None and _segment_matches(ready, run.contexts):
            matched = True
            break
    return not matched if clause.get("negate") is True else matched


def _prerequisite_met(prerequisite: Mapping[str, Any], result: VariationDetail) -> bool:
    return result.reason.kind in _REACHED_SERVE and result.variation_index == prerequisite["variation"]


def _check_prerequisites(flag: Mapping[str, Any], run: _Run, depth: int) -> VariationDetail | None:
    for prerequisite in flag["prerequisites"]:
        result = _evaluate_flag(run, prerequisite["flagKey"], depth + 1)
        if result.reason.kind == ReasonKind.ERROR:
            return result
        if not _prerequisite_met(prerequisite, result):
            return _off_result(flag, Reason(ReasonKind.PREREQUISITE_FAILED, flag_key=prerequisite["flagKey"]))
    return None


def _evaluate_entry(entry: FlagEntry, run: _Run, depth: int) -> VariationDetail:
    if entry.status == "malformed":
        return failure(ErrorCode.MALFORMED_FLAG)
    flag = entry.flag
    assert flag is not None
    if not flag["on"]:
        return _off_result(flag, _OFF)
    if entry.status == "broken":
        assert entry.code is not None
        return failure(entry.code)
    blocked = _check_prerequisites(flag, run, depth)
    if blocked is not None:
        return blocked
    assert entry.targets is not None
    targeted = targeted_variation(entry.targets, run.contexts)
    if targeted is not None:
        return _served(flag["variations"], targeted, _TARGET_MATCH)
    for rule_index, rule in enumerate(flag["rules"]):
        if all(_clause_matches(clause, run) for clause in rule["clauses"]):
            return _serve_result(flag, rule, run.contexts, Reason(ReasonKind.RULE_MATCH, rule_index=rule_index))
    return _serve_result(flag, flag["fallthrough"], run.contexts, _FALLTHROUGH)


def _evaluate_flag(run: _Run, key: str, depth: int) -> VariationDetail:
    completed = run.completed.get(key)
    if completed is not None:
        return completed
    if key in run.active:
        return failure(ErrorCode.PREREQUISITE_CYCLE)
    if depth > MAX_PREREQUISITE_DEPTH:
        return failure(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED)
    entry = run.ruleset.flags.get(key)
    if entry is None:
        return failure(ErrorCode.FLAG_NOT_FOUND)
    run.active.add(key)
    try:
        result = _evaluate_entry(entry, run, depth)
    finally:
        run.active.discard(key)
    run.completed[key] = result
    return result


def evaluate(ruleset: Ruleset, flag_key: str, context: object) -> VariationDetail:
    """Evaluates one flag for one context. Every failure, a prerequisite cycle included, is an `ERROR` result."""
    contexts = normalise_context(context)
    if contexts is None:
        return failure(ErrorCode.INVALID_CONTEXT)
    return _evaluate_flag(_Run(ruleset, contexts), flag_key, 0)
