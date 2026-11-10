"""The shape of a ruleset, checked at runtime because rulesets arrive as JSON (docs/evaluation.md is normative)."""

from __future__ import annotations

import math
from collections.abc import Callable, Mapping
from typing import Any

from ._bucket import BUCKET_COUNT
from ._json import is_finite, is_number
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

_KIND_CHARS = frozenset("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-")


def is_record(value: object) -> bool:
    return isinstance(value, Mapping)


def is_absent(value: object) -> bool:
    return value is None


def is_kind(value: object) -> bool:
    """`^[A-Za-z0-9._-]+$`, and neither `kind` nor `multi`."""
    if not isinstance(value, str) or value in ("", "kind", "multi"):
        return False
    return all(c in _KIND_CHARS for c in value)


def _is_optional_kind(value: object) -> bool:
    return value is None or is_kind(value)


def _is_key(value: object) -> bool:
    return isinstance(value, str) and value != ""


def is_count(value: object) -> bool:
    if type(value) is int:
        return value >= 0
    return type(value) is float and math.isfinite(value) and value.is_integer() and value >= 0


def _is_variation_index(value: object, count: int) -> bool:
    return is_count(value) and value < count  # type: ignore[operator]


def _is_string_array(value: object) -> bool:
    return isinstance(value, list) and all(isinstance(v, str) for v in value)


def _is_clause_value(value: object) -> bool:
    if is_number(value):
        return is_finite(value)
    return isinstance(value, (str, bool))


def _is_negate(value: object) -> bool:
    return value is None or isinstance(value, bool)


def _is_attribute_name(value: object) -> bool:
    if not _is_key(value):
        return False
    assert isinstance(value, str)
    return not value.startswith("/") or parse_reference(value) is not None


def is_attribute_clause(value: object) -> bool:
    if not isinstance(value, Mapping) or value.get("op") not in ATTRIBUTE_OPS:
        return False
    values = value.get("values")
    return (
        _is_attribute_name(value.get("attribute"))
        and _is_negate(value.get("negate"))
        and _is_optional_kind(value.get("contextKind"))
        and isinstance(values, list)
        and all(_is_clause_value(v) for v in values)
    )


def _is_segment_clause(value: object) -> bool:
    if not isinstance(value, Mapping) or value.get("op") != "segmentMatch":
        return False
    return _is_negate(value.get("negate")) and _is_string_array(value.get("values"))


def _is_weighted_variation(value: object, count: int) -> bool:
    if not isinstance(value, Mapping):
        return False
    weight = value.get("weight")
    return (
        _is_variation_index(value.get("variation"), count) and is_count(weight) and weight <= BUCKET_COUNT  # type: ignore[operator]
    )


def _is_rollout(value: object, count: int) -> bool:
    if not isinstance(value, Mapping) or not _is_optional_kind(value.get("contextKind")):
        return False
    variations = value.get("variations")
    if not isinstance(variations, list) or len(variations) == 0:
        return False
    total = 0
    for item in variations:
        if not _is_weighted_variation(item, count):
            return False
        total += item["weight"]
    return total == BUCKET_COUNT


def _is_serve(value: object, count: int) -> bool:
    if not isinstance(value, Mapping):
        return False
    variation = value.get("variation")
    rollout = value.get("rollout")
    if rollout is None:
        return _is_variation_index(variation, count)
    return variation is None and _is_rollout(rollout, count)


def _is_rule(value: object, count: int) -> bool:
    if not isinstance(value, Mapping):
        return False
    clauses = value.get("clauses")
    return (
        isinstance(clauses, list)
        and all(is_attribute_clause(c) or _is_segment_clause(c) for c in clauses)
        and _is_serve(value, count)
    )


def _is_target(value: object, count: int) -> bool:
    if not isinstance(value, Mapping):
        return False
    return (
        _is_variation_index(value.get("variation"), count)
        and _is_string_array(value.get("contextKeys"))
        and _is_optional_kind(value.get("contextKind"))
    )


def _is_prerequisite(value: object) -> bool:
    return isinstance(value, Mapping) and _is_key(value.get("flagKey")) and is_count(value.get("variation"))


def is_flag_header(value: object) -> bool:
    """What serving the off variation needs. Checked apart so `on: false` survives a bad rule."""
    if not isinstance(value, Mapping) or not isinstance(value.get("on"), bool):
        return False
    variations = value.get("variations")
    if not _is_key(value.get("key")) or not isinstance(variations, list) or len(variations) == 0:
        return False
    off = value.get("offVariation")
    return off is None or _is_variation_index(off, len(variations))


def _every(value: object, check: Callable[[Any], bool]) -> bool:
    return isinstance(value, list) and all(check(item) for item in value)


def is_flag_body(value: object, count: int) -> bool:
    if not isinstance(value, Mapping):
        return False
    return (
        isinstance(value.get("salt"), str)
        and is_count(value.get("version"))
        and _every(value.get("targets"), lambda t: _is_target(t, count))
        and _every(value.get("prerequisites"), _is_prerequisite)
        and _every(value.get("rules"), lambda r: _is_rule(r, count))
        and _is_serve(value.get("fallthrough"), count)
    )


def _is_segment_rule(value: object) -> bool:
    if not isinstance(value, Mapping):
        return False
    weight = value.get("weight")
    weight_ok = weight is None or (is_count(weight) and weight <= BUCKET_COUNT)  # type: ignore[operator]
    return (
        weight_ok
        and _is_optional_kind(value.get("rolloutContextKind"))
        and _every(value.get("clauses"), is_attribute_clause)
    )


def _is_context_list(value: object) -> bool:
    return isinstance(value, Mapping) and is_kind(value.get("contextKind")) and _is_string_array(value.get("values"))


def _is_optional_context_lists(value: object) -> bool:
    return value is None or _every(value, _is_context_list)


def is_segment(value: object) -> bool:
    if not isinstance(value, Mapping):
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
