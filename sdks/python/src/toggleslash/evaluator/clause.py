"""Attribute clauses, to "Clauses" in docs/evaluation.md."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from .context import DEFAULT_KIND, Contexts, KindContext
from .dates import to_instant
from .numbers import as_float, is_number
from .pattern import compile_pattern, pattern_matches
from .reference import parse_reference
from .semver import compare_versions, parse_version
from .text import same, units
from .validate import is_array


def _read_attribute(context: KindContext, name: str) -> Any:
    """The value a name or a ``/`` reference reaches, or None when it is missing."""
    components = parse_reference(name) if name.startswith("/") else [name]
    if not components:
        return None
    first, rest = components[0], components[1:]
    if first == "key":
        current: Any = context.key
    elif context.attributes is not None and first in context.attributes:
        current = context.attributes[first]
    else:
        return None
    for component in rest:
        if not isinstance(current, Mapping) or component not in current:
            return None
        current = current[component]
    return current


def _sign(left: Any, right: Any) -> int:
    if left < right:
        return -1
    return 1 if left > right else 0


def _compare(attribute: Any, value: Any) -> int | None:
    """-1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); else None."""
    if is_number(attribute) and is_number(value):
        left = as_float(attribute)
        return _sign(left, as_float(value)) if left - left == 0 else None
    if isinstance(attribute, str) and isinstance(value, str):
        return _sign(units(attribute), units(value))
    return None


def _matches_pattern(attribute: str, value: str) -> bool:
    program = compile_pattern(value)
    return program is not None and pattern_matches(program, attribute)


def _matches_string(op: str, attribute: str, value: str) -> bool:
    if op == "matches":
        return _matches_pattern(attribute, value)
    left, right = units(attribute), units(value)
    if op == "contains":
        return right in left
    if op == "startsWith":
        return left.startswith(right)
    return left.endswith(right)


def _matches_order(op: str, order: int) -> bool:
    if op == "lt":
        return order < 0
    if op == "lte":
        return order <= 0
    if op == "gt":
        return order > 0
    return order >= 0


def _matches_semver(op: str, attribute: Any, value: Any) -> bool:
    if not isinstance(attribute, str) or not isinstance(value, str):
        return False
    candidate = parse_version(attribute)
    target = parse_version(value)
    if candidate is None or target is None:
        return False
    order = compare_versions(candidate, target)
    if op == "semverEq":
        return order == 0
    if op == "semverLt":
        return order < 0
    if op == "semverLte":
        return order <= 0
    return order > 0 if op == "semverGt" else order >= 0


def _matches_date(op: str, attribute: Any, value: Any) -> bool:
    candidate = to_instant(attribute)
    target = to_instant(value)
    if candidate is None or target is None:
        return False
    return candidate < target if op == "before" else candidate > target


def _equal(attribute: Any, value: Any) -> bool:
    """``in``: the same type (string, number or boolean) and equal."""
    if isinstance(attribute, bool) or isinstance(value, bool):
        return isinstance(attribute, bool) and isinstance(value, bool) and attribute == value
    if isinstance(attribute, str) and isinstance(value, str):
        return same(attribute, value)
    if is_number(attribute) and is_number(value):
        return as_float(attribute) == as_float(value)
    return False


_STRING_OPS = frozenset(("contains", "startsWith", "endsWith", "matches"))
_SEMVER_OPS = frozenset(("semverEq", "semverLt", "semverLte", "semverGt", "semverGte"))


def _matches_value(op: str, attribute: Any, value: Any) -> bool:
    if op == "in":
        return _equal(attribute, value)
    if op in _STRING_OPS:
        return isinstance(attribute, str) and isinstance(value, str) and _matches_string(op, attribute, value)
    if op in _SEMVER_OPS:
        return _matches_semver(op, attribute, value)
    if op in ("before", "after"):
        return _matches_date(op, attribute, value)
    order = _compare(attribute, value)
    return order is not None and _matches_order(op, order)


def attribute_clause_matches(clause: Mapping[str, Any], contexts: Contexts) -> bool:
    """Does one attribute clause hold? A missing attribute or kind fails it, whatever ``negate``."""
    context = contexts.get(clause.get("contextKind") or DEFAULT_KIND)
    if context is None:
        return False
    attribute = _read_attribute(context, clause["attribute"])
    if attribute is None:
        return False
    candidates = attribute if is_array(attribute) else [attribute]
    op = clause["op"]
    matched = any(
        _matches_value(op, candidate, value) for candidate in candidates for value in clause["values"]
    )
    return not matched if clause.get("negate") is True else matched
