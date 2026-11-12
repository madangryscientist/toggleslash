"""A ruleset, checked and indexed once (docs/evaluation.md, "Loading a ruleset").

Rulesets arrive as JSON, so the shape is checked at run time. Loading never fails: a flag that does not
pass is an ERROR result when it is evaluated, and a flag whose header passes still serves its off variation.
Null and absent are the same for an optional field.
"""

from __future__ import annotations

import math
from typing import Any

from ._bucket import BUCKET_COUNT
from ._context import DEFAULT_KIND, KindContext, is_kind
from ._reference import parse_reference

ATTRIBUTE_OPS = frozenset(
    (
        "in",
        "contains",
        "startsWith",
        "endsWith",
        "lt",
        "lte",
        "gt",
        "gte",
        "semverEq",
        "semverLt",
        "semverLte",
        "semverGt",
        "semverGte",
        "before",
        "after",
        "matches",
    )
)

Json = dict[str, Any]


def is_record(value: Any) -> bool:
    return isinstance(value, dict)


def _is_key(value: Any) -> bool:
    return isinstance(value, str) and value != ""


def is_count(value: Any) -> bool:
    """A non-negative integer; `2.0` is one, as it is in JSON."""
    if isinstance(value, bool):
        return False
    if isinstance(value, int):
        return value >= 0
    return isinstance(value, float) and value.is_integer() and value >= 0


def _is_variation_index(value: Any, count: int) -> bool:
    return is_count(value) and value < count


def _is_string_array(value: Any) -> bool:
    return isinstance(value, list) and all(isinstance(item, str) for item in value)


def _is_clause_value(value: Any) -> bool:
    if isinstance(value, bool):
        return True
    if isinstance(value, (int, float)):
        return math.isfinite(value)
    return isinstance(value, str)


def _is_negate(value: Any) -> bool:
    return value is None or isinstance(value, bool)


def _is_optional_kind(value: Any) -> bool:
    return value is None or is_kind(value)


def _is_attribute_name(value: Any) -> bool:
    if not _is_key(value):
        return False
    return not value.startswith("/") or parse_reference(value) is not None


def _is_attribute_clause(value: Any) -> bool:
    if not is_record(value):
        return False
    op = value.get("op")
    if not isinstance(op, str) or op not in ATTRIBUTE_OPS:
        return False
    values = value.get("values")
    return (
        _is_attribute_name(value.get("attribute"))
        and _is_negate(value.get("negate"))
        and _is_optional_kind(value.get("contextKind"))
        and isinstance(values, list)
        and all(_is_clause_value(item) for item in values)
    )


def _is_segment_clause(value: Any) -> bool:
    if not is_record(value) or value.get("op") != "segmentMatch":
        return False
    return _is_negate(value.get("negate")) and _is_string_array(value.get("values"))


def _is_weighted_variation(value: Any, count: int) -> bool:
    if not is_record(value):
        return False
    weight = value.get("weight")
    return (
        _is_variation_index(value.get("variation"), count)
        and is_count(weight)
        and weight <= BUCKET_COUNT
    )


def _is_rollout(value: Any, count: int) -> bool:
    if not is_record(value) or not _is_optional_kind(value.get("contextKind")):
        return False
    variations = value.get("variations")
    if not isinstance(variations, list) or not variations:
        return False
    total = 0
    for item in variations:
        if not _is_weighted_variation(item, count):
            return False
        total += item["weight"]
    return total == BUCKET_COUNT


def _is_serve(value: Any, count: int) -> bool:
    if not is_record(value):
        return False
    variation = value.get("variation")
    rollout = value.get("rollout")
    if rollout is None:
        return _is_variation_index(variation, count)
    return variation is None and _is_rollout(rollout, count)


def _is_rule(value: Any, count: int) -> bool:
    if not is_record(value):
        return False
    clauses = value.get("clauses")
    return (
        isinstance(clauses, list)
        and all(_is_attribute_clause(c) or _is_segment_clause(c) for c in clauses)
        and _is_serve(value, count)
    )


def _is_target(value: Any, count: int) -> bool:
    if not is_record(value):
        return False
    return (
        _is_variation_index(value.get("variation"), count)
        and _is_string_array(value.get("contextKeys"))
        and _is_optional_kind(value.get("contextKind"))
    )


def _is_prerequisite(value: Any) -> bool:
    return is_record(value) and _is_key(value.get("flagKey")) and is_count(value.get("variation"))


def is_flag_header(value: Any) -> bool:
    """What serving the off variation needs. It is checked apart so `on: false` survives a bad rule."""
    if not is_record(value) or not isinstance(value.get("on"), bool):
        return False
    variations = value.get("variations")
    if not _is_key(value.get("key")) or not isinstance(variations, list) or not variations:
        return False
    off = value.get("offVariation")
    return off is None or _is_variation_index(off, len(variations))


def _every(value: Any, check: Any) -> bool:
    return isinstance(value, list) and all(check(item) for item in value)


def is_flag_body(value: Json, count: int) -> bool:
    return (
        isinstance(value.get("salt"), str)
        and is_count(value.get("version"))
        and _every(value.get("targets"), lambda item: _is_target(item, count))
        and _every(value.get("prerequisites"), _is_prerequisite)
        and _every(value.get("rules"), lambda item: _is_rule(item, count))
        and _is_serve(value.get("fallthrough"), count)
    )


def _is_segment_rule(value: Any) -> bool:
    if not is_record(value):
        return False
    weight = value.get("weight")
    weight_ok = weight is None or (is_count(weight) and weight <= BUCKET_COUNT)
    return (
        weight_ok
        and _is_optional_kind(value.get("rolloutContextKind"))
        and _every(value.get("clauses"), _is_attribute_clause)
    )


def _is_context_list(value: Any) -> bool:
    return (
        is_record(value)
        and is_kind(value.get("contextKind"))
        and _is_string_array(value.get("values"))
    )


def _is_optional_context_lists(value: Any) -> bool:
    return value is None or _every(value, _is_context_list)


def is_segment(value: Any) -> bool:
    if not is_record(value):
        return False
    return (
        _is_key(value.get("key"))
        and isinstance(value.get("salt"), str)
        and _is_string_array(value.get("included"))
        and _is_string_array(value.get("excluded"))
        and _is_optional_context_lists(value.get("includedContexts"))
        and _is_optional_context_lists(value.get("excludedContexts"))
        and _every(value.get("rules"), _is_segment_rule)
    )


KeysByKind = dict[str, set[str]]


class ReadySegment:
    """A valid segment with its included and excluded keys indexed by kind."""

    __slots__ = ("excluded", "included", "segment")

    def __init__(self, segment: Json) -> None:
        self.segment = segment
        self.included = _keys_by_kind(segment["included"], segment.get("includedContexts"))
        self.excluded = _keys_by_kind(segment["excluded"], segment.get("excludedContexts"))


def _keys_by_kind(user_keys: list[str], lists: list[Json] | None) -> KeysByKind:
    index: KeysByKind = {DEFAULT_KIND: set(user_keys)}
    for entry in lists or []:
        index.setdefault(entry["contextKind"], set()).update(entry["values"])
    return index


def lists_context(keys: KeysByKind, contexts: dict[str, KindContext]) -> bool:
    """Does the context have, in any of its kinds, a key that `keys` lists for that kind?"""
    for kind, context in contexts.items():
        listed = keys.get(kind)
        if listed is not None and context.key in listed:
            return True
    return False


class TargetHit:
    """A target that lists a key: its place in `targets` and the variation it serves."""

    __slots__ = ("order", "variation")

    def __init__(self, order: int, variation: int) -> None:
        self.order = order
        self.variation = variation


TargetIndex = dict[str, dict[str, TargetHit]]


def targeted_variation(targets: TargetIndex, contexts: dict[str, KindContext]) -> int | None:
    """The variation of the first target in array order that matches the context, or None."""
    first: TargetHit | None = None
    for kind, context in contexts.items():
        hit = targets.get(kind, {}).get(context.key)
        if hit is not None and (first is None or hit.order < first.order):
            first = hit
    return None if first is None else first.variation


def _target_index(flag: Json) -> TargetIndex:
    index: TargetIndex = {}
    for order, target in enumerate(flag["targets"]):
        kind = target.get("contextKind") or DEFAULT_KIND
        keys = index.setdefault(kind, {})
        for context_key in target["contextKeys"]:
            if context_key not in keys:
                keys[context_key] = TargetHit(order, int(target["variation"]))
    return index


class FlagEntry:
    """A flag in a ruleset: `malformed`, `broken` (a header that serves OFF, and the error otherwise) or `ready`."""

    __slots__ = ("code", "flag", "status", "targets")

    def __init__(
        self,
        status: str,
        flag: Json | None = None,
        code: str | None = None,
        targets: TargetIndex | None = None,
    ) -> None:
        self.status = status
        self.flag = flag
        self.code = code
        self.targets = targets

    @property
    def version(self) -> int | None:
        """The flag's version when it is ready; None for a flag the evaluator cannot trust."""
        if self.status != "ready" or self.flag is None:
            return None
        return int(self.flag["version"])


class Ruleset:
    """`{flags, segments}` validated and indexed; build one with `create_ruleset`."""

    __slots__ = ("flags", "segments")

    def __init__(
        self, flags: dict[str, FlagEntry], segments: dict[str, ReadySegment | None]
    ) -> None:
        self.flags = flags
        # A key whose segment is malformed maps to None.
        self.segments = segments


def _segment_problem(flag: Json, segments: dict[str, ReadySegment | None]) -> str | None:
    for rule in flag["rules"]:
        for clause in rule["clauses"]:
            if clause["op"] != "segmentMatch":
                continue
            for key in clause["values"]:
                if key not in segments:
                    return "SEGMENT_NOT_FOUND"
                if segments[key] is None:
                    return "MALFORMED_SEGMENT"
    return None


def _compile_flag(value: Any, segments: dict[str, ReadySegment | None]) -> FlagEntry:
    if not is_flag_header(value):
        return FlagEntry("malformed")
    if not is_flag_body(value, len(value["variations"])):
        return FlagEntry("broken", flag=value, code="MALFORMED_FLAG")
    code = _segment_problem(value, segments)
    if code is not None:
        return FlagEntry("broken", flag=value, code=code)
    return FlagEntry("ready", flag=value, targets=_target_index(value))


def create_ruleset(data: Any) -> Ruleset:
    """Validates and indexes `{flags, segments}` as JSON hands it over. It never raises."""
    raw_flags = data.get("flags") if is_record(data) else None
    raw_segments = data.get("segments") if is_record(data) else None
    segments: dict[str, ReadySegment | None] = {}
    for item in raw_segments if isinstance(raw_segments, list) else []:
        key = item.get("key") if is_record(item) else None
        if isinstance(key, str) and key not in segments:
            segments[key] = ReadySegment(item) if is_segment(item) else None
    flags: dict[str, FlagEntry] = {}
    for item in raw_flags if isinstance(raw_flags, list) else []:
        key = item.get("key") if is_record(item) else None
        if isinstance(key, str) and key not in flags:
            flags[key] = _compile_flag(item, segments)
    return Ruleset(flags, segments)
