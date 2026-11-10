"""Attribute clauses: reading an attribute and applying one op to it."""

from __future__ import annotations

import math
from collections.abc import Mapping
from typing import Any

from ._context import DEFAULT_KIND, Contexts
from ._dates import to_instant
from ._pattern import compile_pattern, pattern_matches
from ._reference import parse_reference
from ._semver import compare_semantic_versions, parse_semantic_version
from ._text import u16

_STRING_OPS = frozenset({"contains", "startsWith", "endsWith", "matches"})
_ORDER_OPS = frozenset({"lt", "lte", "gt", "gte"})
_SEMVER_OPS = frozenset({"semverEq", "semverLt", "semverLte", "semverGt", "semverGte"})


def _own(source: Mapping[str, Any], name: str) -> Any:
    return source.get(name)


def read_attribute(key: str, attributes: Mapping[str, Any] | None, name: str) -> Any:
    """The value a name or a `/` reference reaches, or `None` when it is missing (or null)."""
    components = parse_reference(name) if name.startswith("/") else [name]
    if not components:
        return None
    first, rest = components[0], components[1:]
    current: Any = None
    if first == "key":
        current = key
    elif attributes is not None:
        current = _own(attributes, first)
    for component in rest:
        if not isinstance(current, Mapping):
            return None
        current = _own(current, component)
    return current


def _is_number(value: object) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _as_float(value: int | float) -> float:
    """The binary64 a number reads as: an integer too big for one is infinite and so never matches."""
    try:
        return float(value)
    except OverflowError:
        return math.inf if value > 0 else -math.inf


def _sign(a: Any, b: Any) -> int:
    return -1 if a < b else (1 if a > b else 0)


def _compare(attribute: object, value: object) -> int | None:
    """-1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise `None`."""
    if _is_number(attribute) and _is_number(value):
        number = _as_float(attribute)
        return _sign(number, _as_float(value)) if math.isfinite(number) else None
    if isinstance(attribute, str) and isinstance(value, str):
        return _sign(u16(attribute), u16(value))
    return None


def _matches_pattern(attribute: str, value: str) -> bool:
    program = compile_pattern(value)
    return program is not None and pattern_matches(program, attribute)


def _matches_string(op: str, attribute: str, value: str) -> bool:
    if op == "matches":
        return _matches_pattern(attribute, value)
    left = u16(attribute)
    right = u16(value)
    if op == "contains":
        return right in left
    return left.startswith(right) if op == "startsWith" else left.endswith(right)


def _matches_order(op: str, order: int) -> bool:
    if op == "lt":
        return order < 0
    if op == "lte":
        return order <= 0
    return order > 0 if op == "gt" else order >= 0


def _matches_semver(op: str, attribute: object, value: object) -> bool:
    if not isinstance(attribute, str) or not isinstance(value, str):
        return False
    candidate = parse_semantic_version(attribute)
    target = parse_semantic_version(value)
    if candidate is None or target is None:
        return False
    order = compare_semantic_versions(candidate, target)
    if op == "semverEq":
        return order == 0
    if op == "semverLt":
        return order < 0
    if op == "semverLte":
        return order <= 0
    return order > 0 if op == "semverGt" else order >= 0


def _matches_date(op: str, attribute: object, value: object) -> bool:
    candidate = to_instant(attribute)
    target = to_instant(value)
    if candidate is None or target is None:
        return False
    return candidate < target if op == "before" else candidate > target


def _matches_value(op: str, attribute: object, value: object) -> bool:
    if op == "in":
        if isinstance(attribute, bool):
            return isinstance(value, bool) and attribute == value
        return _compare(attribute, value) == 0
    if op in _STRING_OPS:
        return isinstance(attribute, str) and isinstance(value, str) and _matches_string(op, attribute, value)
    if op in _SEMVER_OPS:
        return _matches_semver(op, attribute, value)
    if op in ("before", "after"):
        return _matches_date(op, attribute, value)
    order = _compare(attribute, value)
    return order is not None and _matches_order(op, order)


def attribute_clause_matches(clause: Mapping[str, Any], contexts: Contexts) -> bool:
    """Does one attribute clause hold? A missing attribute, or a missing kind, fails it whatever `negate` says."""
    context = contexts.get(clause.get("contextKind") or DEFAULT_KIND)
    if context is None:
        return False
    attribute = read_attribute(context[0], context[1], clause["attribute"])
    if attribute is None:
        return False
    op = clause["op"]
    values = clause["values"]
    candidates = attribute if isinstance(attribute, (list, tuple)) else (attribute,)
    matched = any(_matches_value(op, candidate, value) for candidate in candidates for value in values)
    return not matched if clause.get("negate") is True else matched
