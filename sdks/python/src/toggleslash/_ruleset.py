"""Validating and indexing a ruleset once, so each evaluation skips that work. Nothing here raises."""

from __future__ import annotations

from collections.abc import Callable, Mapping
from typing import Any, NamedTuple

from ._context import DEFAULT_KIND, Contexts
from ._types import ErrorCode
from ._validate import is_flag_body, is_flag_header, is_segment

KeysByKind = dict[str, frozenset[str]]


class ReadySegment(NamedTuple):
    segment: Mapping[str, Any]
    included: KeysByKind
    excluded: KeysByKind


class TargetHit(NamedTuple):
    order: int
    variation: int


TargetIndex = dict[str, dict[str, TargetHit]]


class FlagEntry:
    """status: malformed | broken | ready."""

    __slots__ = ("code", "header", "status", "targets")

    def __init__(
        self,
        status: str,
        header: Mapping[str, Any] | None = None,
        code: ErrorCode | None = None,
        targets: TargetIndex | None = None,
    ) -> None:
        self.status = status
        self.header = header
        self.code = code
        self.targets = targets


MALFORMED_SEGMENT = "malformed"


class Ruleset:
    __slots__ = ("flags", "segments")

    def __init__(self, flags: dict[str, FlagEntry], segments: dict[str, ReadySegment | str]) -> None:
        self.flags = flags
        self.segments = segments


def _index_by_key(items: object, compile_: Callable[[Any], Any]) -> dict[str, Any]:
    index: dict[str, Any] = {}
    if not isinstance(items, list):
        return index
    for item in items:
        key = item.get("key") if isinstance(item, Mapping) else None
        if isinstance(key, str) and key not in index:
            index[key] = compile_(item)
    return index


def _keys_by_kind(user_keys: list[str], lists: object) -> KeysByKind:
    index: dict[str, set[str]] = {DEFAULT_KIND: set(user_keys)}
    for entry in lists if isinstance(lists, list) else []:
        index.setdefault(entry["contextKind"], set()).update(entry["values"])
    return {kind: frozenset(keys) for kind, keys in index.items()}


def _compile_segment(value: Any) -> ReadySegment | str:
    if not is_segment(value):
        return MALFORMED_SEGMENT
    return ReadySegment(
        value,
        _keys_by_kind(value["included"], value.get("includedContexts")),
        _keys_by_kind(value["excluded"], value.get("excludedContexts")),
    )


def lists_context(keys: KeysByKind, contexts: Contexts) -> bool:
    """Does the context have, in any of its kinds, a key that `keys` lists for that kind?"""
    for kind, context in contexts.items():
        listed = keys.get(kind)
        if listed is not None and context.key in listed:
            return True
    return False


def _segment_problem(flag: Mapping[str, Any], segments: dict[str, Any]) -> ErrorCode | None:
    for rule in flag["rules"]:
        for clause in rule["clauses"]:
            if clause["op"] != "segmentMatch":
                continue
            for key in clause["values"]:
                segment = segments.get(key)
                if segment is None:
                    return ErrorCode.SEGMENT_NOT_FOUND
                if segment == MALFORMED_SEGMENT:
                    return ErrorCode.MALFORMED_SEGMENT
    return None


def _target_index(flag: Mapping[str, Any]) -> TargetIndex:
    index: TargetIndex = {}
    for order, target in enumerate(flag["targets"]):
        kind = target.get("contextKind") or DEFAULT_KIND
        keys = index.setdefault(kind, {})
        for context_key in target["contextKeys"]:
            if context_key not in keys:
                keys[context_key] = TargetHit(order, int(target["variation"]))
    return index


def targeted_variation(targets: TargetIndex, contexts: Contexts) -> int | None:
    """The variation of the first target in array order that matches the context, or None."""
    first: TargetHit | None = None
    for kind, context in contexts.items():
        hit = targets.get(kind, {}).get(context.key)
        if hit is not None and (first is None or hit.order < first.order):
            first = hit
    return None if first is None else first.variation


def _compile_flag(value: Any, segments: dict[str, Any]) -> FlagEntry:
    if not is_flag_header(value):
        return FlagEntry("malformed")
    if not is_flag_body(value, len(value["variations"])):
        return FlagEntry("broken", header=value, code=ErrorCode.MALFORMED_FLAG)
    code = _segment_problem(value, segments)
    if code is not None:
        return FlagEntry("broken", header=value, code=code)
    return FlagEntry("ready", header=value, targets=_target_index(value))


def is_ruleset_shape(value: object) -> bool:
    """Only an object with array `flags` and `segments` may replace a held ruleset."""
    return (
        isinstance(value, Mapping) and isinstance(value.get("flags"), list) and isinstance(value.get("segments"), list)
    )


def create_ruleset(value: object) -> Ruleset:
    flags_raw = value.get("flags") if isinstance(value, Mapping) else None
    segments_raw = value.get("segments") if isinstance(value, Mapping) else None
    segments = _index_by_key(segments_raw, _compile_segment)
    flags = _index_by_key(flags_raw, lambda item: _compile_flag(item, segments))
    return Ruleset(flags, segments)
