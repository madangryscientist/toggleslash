"""Attribute clauses, to the "Clauses" section of docs/evaluation.md."""

from __future__ import annotations

import math
from collections.abc import Mapping
from typing import Any

from . import _text
from ._context import DEFAULT_KIND, KindContext, number_of
from ._dates import to_instant
from ._pattern import Pattern, compile_pattern, pattern_matches
from ._reference import parse_reference
from ._semver import compare_semantic_versions, parse_semantic_version

_STRING_OPS = frozenset(("contains", "startsWith", "endsWith", "matches"))
_DATE_OPS = frozenset(("before", "after"))
_SEMVER_OPS = frozenset(("semverEq", "semverLt", "semverLte", "semverGt", "semverGte"))

_MISSING = object()
_PATTERN_CACHE_SIZE = 256
_patterns: dict[str, Pattern | None] = {}


def _pattern_for(source: str) -> Pattern | None:
    """The compiled pattern for `source`, or None when it is not valid (a small cache; patterns are immutable)."""
    try:
        return _patterns[source]
    except KeyError:
        pass
    compiled = compile_pattern(source)
    if len(_patterns) >= _PATTERN_CACHE_SIZE:
        _patterns.clear()
    _patterns[source] = compiled
    return compiled


def _own(source: Any, name: str) -> Any:
    if isinstance(source, Mapping) and name in source:
        return source[name]
    return _MISSING


def read_attribute(context: KindContext, name: str) -> Any:
    """The value a name or a `/` reference reaches, or None when it is missing (missing and null agree)."""
    components = parse_reference(name) if name.startswith("/") else [name]
    if not components:
        return None
    first, rest = components[0], components[1:]
    current: Any = _MISSING
    if first == "key":
        current = context.key
    elif context.attributes is not None:
        current = _own(context.attributes, first)
    for component in rest:
        if not isinstance(current, Mapping):
            return None
        current = _own(current, component)
    return None if current is _MISSING else current


def _is_number(value: Any) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _order(attribute: Any, value: Any) -> int | None:
    """-1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise None."""
    if _is_number(attribute) and _is_number(value):
        if not math.isfinite(attribute):
            return None
        if attribute < value:
            return -1
        return 1 if attribute > value else 0
    if isinstance(attribute, str) and isinstance(value, str):
        return _text.compare(attribute, value)
    return None


def _matches_string(op: str, attribute: str, value: str) -> bool:
    if op == "matches":
        pattern = _pattern_for(value)
        return pattern is not None and pattern_matches(pattern, attribute)
    if op == "contains":
        return _text.contains(attribute, value)
    if op == "startsWith":
        return _text.starts_with(attribute, value)
    return _text.ends_with(attribute, value)


def _matches_semver(op: str, attribute: Any, value: Any) -> bool:
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


def _matches_date(op: str, attribute: Any, value: Any) -> bool:
    candidate, target = to_instant(attribute), to_instant(value)
    if candidate is None or target is None:
        return False
    return candidate < target if op == "before" else candidate > target


def _matches_value(op: str, attribute: Any, value: Any) -> bool:
    if op == "in":
        if isinstance(attribute, bool):
            return isinstance(value, bool) and attribute == value
        return _order(attribute, value) == 0
    if op in _STRING_OPS:
        return (
            isinstance(attribute, str)
            and isinstance(value, str)
            and _matches_string(op, attribute, value)
        )
    if op in _SEMVER_OPS:
        return _matches_semver(op, attribute, value)
    if op in _DATE_OPS:
        return _matches_date(op, attribute, value)
    order = _order(attribute, value)
    if order is None:
        return False
    if op == "lt":
        return order < 0
    if op == "lte":
        return order <= 0
    return order > 0 if op == "gt" else order >= 0


def _candidates(attribute: Any) -> list[Any]:
    items = list(attribute) if isinstance(attribute, (list, tuple)) else [attribute]
    # Numbers a caller hands over (a huge int, a Decimal) are read as binary64; anything else is kept.
    return [_numeric(item) for item in items]


def _numeric(item: Any) -> Any:
    if isinstance(item, (str, bool, dict)) or item is None:
        return item
    number = number_of(item)
    return item if number is None else number


def attribute_clause_matches(
    clause: Mapping[str, Any], contexts: Mapping[str, KindContext]
) -> bool:
    """Does one attribute clause hold? A missing attribute, or a missing kind, fails it, whatever `negate` says."""
    context = contexts.get(clause.get("contextKind") or DEFAULT_KIND)
    if context is None:
        return False
    attribute = read_attribute(context, clause["attribute"])
    if attribute is None:
        return False
    op = clause["op"]
    values = clause["values"]
    matched = any(
        _matches_value(op, candidate, value)
        for candidate in _candidates(attribute)
        for value in values
    )
    return not matched if clause.get("negate") is True else matched
