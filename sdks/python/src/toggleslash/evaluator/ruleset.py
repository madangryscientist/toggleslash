"""Loading a ruleset: validate and index once, so each evaluation skips that work."""

from __future__ import annotations

from collections.abc import Callable, Mapping
from dataclasses import dataclass
from typing import Any, TypeVar

from .context import DEFAULT_KIND, Contexts
from .result import ErrorCode
from .validate import is_array, is_flag_body, is_flag_header, is_segment

T = TypeVar("T")

KeysByKind = dict[str, set[str]]


@dataclass(frozen=True, slots=True)
class ReadySegment:
    segment: Mapping[str, Any]
    included: KeysByKind
    excluded: KeysByKind


@dataclass(frozen=True, slots=True)
class FlagEntry:
    """A flag after loading. ``status`` is ``malformed``, ``broken`` or ``ready``."""

    status: str
    header: Mapping[str, Any] | None = None
    code: ErrorCode | None = None
    targets: dict[str, dict[str, tuple[int, int]]] | None = None


MALFORMED = "malformed"


@dataclass(frozen=True, slots=True)
class Ruleset:
    flags: dict[str, FlagEntry]
    segments: dict[str, ReadySegment | str]


def _index_by_key(items: object, compile_item: Callable[[Mapping[str, Any]], T]) -> dict[str, T]:
    index: dict[str, T] = {}
    if not is_array(items):
        return index
    for item in items:  # type: ignore[attr-defined]
        key = item.get("key") if isinstance(item, Mapping) else None
        if isinstance(key, str) and key not in index:
            index[key] = compile_item(item)
    return index


def _keys_by_kind(user_keys: list[str], lists: object) -> KeysByKind:
    index: KeysByKind = {DEFAULT_KIND: set(user_keys)}
    for entry in lists or ():  # type: ignore[attr-defined]
        index.setdefault(entry["contextKind"], set()).update(entry["values"])
    return index


def _compile_segment(value: Mapping[str, Any]) -> ReadySegment | str:
    if not is_segment(value):
        return MALFORMED
    return ReadySegment(
        value,
        _keys_by_kind(list(value["included"]), value.get("includedContexts")),
        _keys_by_kind(list(value["excluded"]), value.get("excludedContexts")),
    )


def lists_context(keys: KeysByKind, contexts: Contexts) -> bool:
    """Does the context have, in any of its kinds, a key that ``keys`` lists for that kind?"""
    return any(context.key in keys.get(kind, ()) for kind, context in contexts.items())


def _segment_problem(flag: Mapping[str, Any], segments: dict[str, Any]) -> ErrorCode | None:
    """The first reference to a segment that is missing or malformed, in rule, clause, value order."""
    for rule in flag["rules"]:
        for clause in rule["clauses"]:
            if clause["op"] != "segmentMatch":
                continue
            for key in clause["values"]:
                segment = segments.get(key)
                if segment is None:
                    return ErrorCode.SEGMENT_NOT_FOUND
                if segment == MALFORMED:
                    return ErrorCode.MALFORMED_SEGMENT
    return None


def _target_index(flag: Mapping[str, Any]) -> dict[str, dict[str, tuple[int, int]]]:
    index: dict[str, dict[str, tuple[int, int]]] = {}
    for order, target in enumerate(flag["targets"]):
        kind = target.get("contextKind") or DEFAULT_KIND
        keys = index.setdefault(kind, {})
        for context_key in target["contextKeys"]:
            keys.setdefault(context_key, (order, int(target["variation"])))
    return index


def targeted_variation(targets: dict[str, dict[str, tuple[int, int]]], contexts: Contexts) -> int | None:
    """The variation of the first target in array order that matches the context, or None."""
    first: tuple[int, int] | None = None
    for kind, context in contexts.items():
        hit = targets.get(kind, {}).get(context.key)
        if hit is not None and (first is None or hit[0] < first[0]):
            first = hit
    return None if first is None else first[1]


def _compile_flag(value: Mapping[str, Any], segments: dict[str, Any]) -> FlagEntry:
    if not is_flag_header(value):
        return FlagEntry(MALFORMED)
    if not is_flag_body(value, len(value["variations"])):
        return FlagEntry("broken", value, ErrorCode.MALFORMED_FLAG)
    code = _segment_problem(value, segments)
    if code is not None:
        return FlagEntry("broken", value, code)
    return FlagEntry("ready", value, None, _target_index(value))


def create_ruleset(data: object) -> Ruleset:
    """Validates and indexes ``{flags, segments}`` as JSON hands it over. It never raises."""
    raw_flags = data.get("flags") if isinstance(data, Mapping) else None
    raw_segments = data.get("segments") if isinstance(data, Mapping) else None
    segments = _index_by_key(raw_segments, _compile_segment)
    flags = _index_by_key(raw_flags, lambda item: _compile_flag(item, segments))
    return Ruleset(flags, segments)
