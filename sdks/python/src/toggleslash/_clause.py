"""Attribute clauses: reading the attribute, and the ops."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from . import _text
from ._context import DEFAULT_KIND, Contexts, KindContext
from ._dates import to_instant
from ._json import is_finite, is_number, to_float
from ._pattern import compile_pattern, pattern_matches
from ._reference import parse_reference
from ._semver import compare_semver, parse_semver

_MISSING = object()


def _own(source: object, name: str) -> Any:
    if isinstance(source, Mapping) and name in source:
        return source[name]
    return _MISSING


def read_attribute(context: KindContext, name: str) -> Any:
    """The value a name or a `/` reference reaches, or `_MISSING`."""
    components = parse_reference(name) if name.startswith("/") else [name]
    if not components:
        return _MISSING
    first, rest = components[0], components[1:]
    if first == "key":
        current: Any = context.key
    elif context.attributes is not None:
        current = _own(context.attributes, first)
    else:
        current = _MISSING
    for component in rest:
        if not isinstance(current, Mapping):
            return _MISSING
        current = _own(current, component)
    return current


def _sign(a: Any, b: Any) -> int:
    return -1 if a < b else (1 if a > b else 0)


def _compare(attribute: object, value: object) -> int | None:
    """-1, 0 or 1 for two numbers (the attribute finite) or two strings (by UTF-16 code unit); otherwise None."""
    if is_number(attribute) and is_number(value):
        a = to_float(attribute)  # type: ignore[arg-type]
        if not is_finite(a):
            return None
        return _sign(a, to_float(value))  # type: ignore[arg-type]
    if isinstance(attribute, str) and isinstance(value, str):
        return _text.compare(attribute, value)
    return None


def _matches_pattern(attribute: str, value: str) -> bool:
    program = compile_pattern(value)
    return program is not None and pattern_matches(program, attribute)


def _matches_string(op: str, attribute: str, value: str) -> bool:
    if op == "matches":
        return _matches_pattern(attribute, value)
    if op == "contains":
        return _text.contains(attribute, value)
    if op == "startsWith":
        return _text.starts_with(attribute, value)
    return _text.ends_with(attribute, value)


def _matches_order(op: str, order: int) -> bool:
    if op == "lt":
        return order < 0
    if op == "lte":
        return order <= 0
    if op == "gt":
        return order > 0
    return order >= 0


def _matches_semver(op: str, attribute: object, value: object) -> bool:
    if not isinstance(attribute, str) or not isinstance(value, str):
        return False
    candidate, target = parse_semver(attribute), parse_semver(value)
    if candidate is None or target is None:
        return False
    order = compare_semver(candidate, target)
    if op == "semverEq":
        return order == 0
    if op == "semverLt":
        return order < 0
    if op == "semverLte":
        return order <= 0
    return order > 0 if op == "semverGt" else order >= 0


def _matches_date(op: str, attribute: object, value: object) -> bool:
    candidate, target = to_instant(attribute), to_instant(value)
    if candidate is None or target is None:
        return False
    return candidate < target if op == "before" else candidate > target


_STRING_OPS = frozenset(("contains", "startsWith", "endsWith", "matches"))
_TYPED_OPS = frozenset(("semverEq", "semverLt", "semverLte", "semverGt", "semverGte", "before", "after"))


def _matches_value(op: str, attribute: object, value: object) -> bool:
    if op == "in":
        if isinstance(attribute, bool):
            return isinstance(value, bool) and attribute == value
        return _compare(attribute, value) == 0
    if op in _STRING_OPS:
        return isinstance(attribute, str) and isinstance(value, str) and _matches_string(op, attribute, value)
    if op in _TYPED_OPS:
        if op in ("before", "after"):
            return _matches_date(op, attribute, value)
        return _matches_semver(op, attribute, value)
    order = _compare(attribute, value)
    return order is not None and _matches_order(op, order)


def attribute_clause_matches(clause: Mapping[str, Any], contexts: Contexts) -> bool:
    """Does one attribute clause hold? A missing attribute, or a missing kind, fails it, whatever `negate` says."""
    context = contexts.get(clause.get("contextKind") or DEFAULT_KIND)
    if context is None:
        return False
    attribute = read_attribute(context, clause["attribute"])
    if attribute is _MISSING or attribute is None:
        return False
    candidates = attribute if isinstance(attribute, (list, tuple)) else [attribute]
    op = clause["op"]
    values = clause["values"]
    matched = any(_matches_value(op, candidate, value) for candidate in candidates for value in values)
    return not matched if clause.get("negate") is True else matched
