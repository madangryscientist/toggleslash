"""Evaluation of one flag for one context, in the order docs/evaluation.md fixes. It never raises."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from ._clause import attribute_clause_matches
from ._context import KindContext, kind_bucket, normalise_context
from ._ruleset import FlagEntry, ReadySegment, Ruleset, lists_context, targeted_variation

MAX_PREREQUISITE_DEPTH = 32


@dataclass(frozen=True, slots=True)
class Outcome:
    """What an evaluation answers: the served JSON value, its index and the reason.

    `kind` is one of OFF, TARGET_MATCH, RULE_MATCH, FALLTHROUGH, PREREQUISITE_FAILED and ERROR. `rule_index`
    is set for RULE_MATCH, `flag_key` for PREREQUISITE_FAILED and `code` for ERROR.
    """

    value: Any
    variation_index: int | None
    kind: str
    rule_index: int | None = None
    flag_key: str | None = None
    code: str | None = None


class _Run:
    __slots__ = ("active", "completed", "contexts", "ruleset")

    def __init__(self, ruleset: Ruleset, contexts: dict[str, KindContext]) -> None:
        self.ruleset = ruleset
        self.contexts = contexts
        self.active: set[str] = set()
        self.completed: dict[str, Outcome] = {}


def _failure(code: str) -> Outcome:
    return Outcome(None, None, "ERROR", code=code)


def _served(variations: list[Any], index: int, **reason: Any) -> Outcome:
    return Outcome(variations[index], index, **reason)


def _off_result(flag: dict[str, Any], **reason: Any) -> Outcome:
    index = flag.get("offVariation")
    if index is None:
        return Outcome(None, None, **reason)
    return _served(flag["variations"], int(index), **reason)


def _rollout_variation(rollout: dict[str, Any], position: int) -> int:
    chosen = 0
    cumulative = 0
    for entry in rollout["variations"]:
        chosen = int(entry["variation"])
        cumulative += entry["weight"]
        if position < cumulative:
            break
    return chosen


def _serve_result(flag: dict[str, Any], serve: dict[str, Any], run: _Run, **reason: Any) -> Outcome:
    rollout = serve.get("rollout")
    if rollout is None:
        index = int(serve["variation"])
    else:
        position = kind_bucket(flag["key"], flag["salt"], rollout.get("contextKind"), run.contexts)
        index = _rollout_variation(rollout, position)
    return _served(flag["variations"], index, **reason)


def _segment_rule_matches(rule: dict[str, Any], ready: ReadySegment, run: _Run) -> bool:
    if not all(attribute_clause_matches(clause, run.contexts) for clause in rule["clauses"]):
        return False
    weight = rule.get("weight")
    if weight is None:
        return True
    segment = ready.segment
    position = kind_bucket(
        segment["key"], segment["salt"], rule.get("rolloutContextKind"), run.contexts
    )
    return position < weight


def _segment_matches(ready: ReadySegment, run: _Run) -> bool:
    if lists_context(ready.excluded, run.contexts):
        return False
    if lists_context(ready.included, run.contexts):
        return True
    return any(_segment_rule_matches(rule, ready, run) for rule in ready.segment["rules"])


def _clause_matches(clause: dict[str, Any], run: _Run) -> bool:
    if clause["op"] != "segmentMatch":
        return attribute_clause_matches(clause, run.contexts)
    matched = False
    for key in clause["values"]:
        ready = run.ruleset.segments.get(key)
        if ready is not None and _segment_matches(ready, run):
            matched = True
            break
    return not matched if clause.get("negate") is True else matched


def _prerequisite_met(variation: Any, result: Outcome) -> bool:
    reached = result.kind in ("TARGET_MATCH", "RULE_MATCH", "FALLTHROUGH")
    return reached and result.variation_index == variation


def _check_prerequisites(flag: dict[str, Any], run: _Run, depth: int) -> Outcome | None:
    for prerequisite in flag["prerequisites"]:
        result = _evaluate_flag(run, prerequisite["flagKey"], depth + 1)
        if result.kind == "ERROR":
            return result
        if not _prerequisite_met(prerequisite["variation"], result):
            return _off_result(flag, kind="PREREQUISITE_FAILED", flag_key=prerequisite["flagKey"])
    return None


def _evaluate_entry(entry: FlagEntry, run: _Run, depth: int) -> Outcome:
    flag = entry.flag
    if entry.status == "malformed" or flag is None:
        return _failure("MALFORMED_FLAG")
    if not flag["on"]:
        return _off_result(flag, kind="OFF")
    if entry.status == "broken":
        return _failure(entry.code or "MALFORMED_FLAG")
    blocked = _check_prerequisites(flag, run, depth)
    if blocked is not None:
        return blocked
    targeted = targeted_variation(entry.targets or {}, run.contexts)
    if targeted is not None:
        return _served(flag["variations"], targeted, kind="TARGET_MATCH")
    for index, rule in enumerate(flag["rules"]):
        if all(_clause_matches(clause, run) for clause in rule["clauses"]):
            return _serve_result(flag, rule, run, kind="RULE_MATCH", rule_index=index)
    return _serve_result(flag, flag["fallthrough"], run, kind="FALLTHROUGH")


def _evaluate_flag(run: _Run, key: str, depth: int) -> Outcome:
    completed = run.completed.get(key)
    if completed is not None:
        return completed
    if key in run.active:
        return _failure("PREREQUISITE_CYCLE")
    if depth > MAX_PREREQUISITE_DEPTH:
        return _failure("PREREQUISITE_DEPTH_EXCEEDED")
    entry = run.ruleset.flags.get(key)
    if entry is None:
        return _failure("FLAG_NOT_FOUND")
    run.active.add(key)
    try:
        result = _evaluate_entry(entry, run, depth)
    finally:
        run.active.discard(key)
    run.completed[key] = result
    return result


def evaluate(ruleset: Ruleset, flag_key: str, context: Any) -> Outcome:
    """Evaluates one flag for one context. Every failure, including a prerequisite cycle, is an ERROR outcome."""
    contexts = normalise_context(context)
    if contexts is None:
        return _failure("INVALID_CONTEXT")
    return _evaluate_flag(_Run(ruleset, contexts), flag_key, 0)
