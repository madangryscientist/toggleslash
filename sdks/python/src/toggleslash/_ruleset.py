"""Validating and indexing a ruleset once, so each evaluation skips that work."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from ._context import DEFAULT_KIND, Contexts
from ._types import ErrorCode
from ._validate import is_array, is_flag_body, is_flag_header, is_segment

# kind -> keys of that kind
KeysByKind = dict[str, set[str]]
# kind -> key -> (order of the target in the flag's `targets`, the variation it serves)
TargetIndex = dict[str, dict[str, tuple[int, int]]]


class ReadySegment:
    __slots__ = ("excluded", "included", "segment")

    def __init__(self, segment: Mapping[str, Any]) -> None:
        self.segment = segment
        self.included = _keys_by_kind(segment["included"], segment.get("includedContexts"))
        self.excluded = _keys_by_kind(segment["excluded"], segment.get("excludedContexts"))


class FlagEntry:
    """A flag after validation. `status` is `malformed`, `broken` (header fine, `code` says why) or `ready`."""

    __slots__ = ("code", "flag", "status", "targets")

    def __init__(
        self,
        status: str,
        flag: Mapping[str, Any] | None = None,
        code: ErrorCode | None = None,
        targets: TargetIndex | None = None,
    ) -> None:
        self.status = status
        self.flag = flag
        self.code = code
        self.targets = targets


class Ruleset:
    __slots__ = ("flags", "segments")

    def __init__(
        self,
        flags: dict[str, FlagEntry],
        segments: dict[str, ReadySegment | None],
    ) -> None:
        self.flags = flags
        # A segment that is present but malformed is `None`.
        self.segments = segments


def _keys_by_kind(user_keys: list[str], lists: list[Mapping[str, Any]] | None) -> KeysByKind:
    index: KeysByKind = {DEFAULT_KIND: set(user_keys)}
    for item in lists or ():
        index.setdefault(item["contextKind"], set()).update(item["values"])
    return index


def _compile_segment(value: object) -> ReadySegment | None:
    return ReadySegment(value) if is_segment(value) else None


def lists_context(keys: KeysByKind, contexts: Contexts) -> bool:
    """Does the context have, in any of its kinds, a key that `keys` lists for that kind?"""
    for kind, (context_key, _attributes) in contexts.items():
        listed = keys.get(kind)
        if listed is not None and context_key in listed:
            return True
    return False


def _segment_problem(flag: Mapping[str, Any], segments: dict[str, ReadySegment | None]) -> ErrorCode | None:
    """The first reference to a segment that is missing or malformed, in rule, clause, value order."""
    for rule in flag["rules"]:
        for clause in rule["clauses"]:
            if clause["op"] != "segmentMatch":
                continue
            for key in clause["values"]:
                if key not in segments:
                    return ErrorCode.SEGMENT_NOT_FOUND
                if segments[key] is None:
                    return ErrorCode.MALFORMED_SEGMENT
    return None


def _index(flag: Mapping[str, Any]) -> TargetIndex:
    index: TargetIndex = {}
    for order, target in enumerate(flag["targets"]):
        keys = index.setdefault(target.get("contextKind") or DEFAULT_KIND, {})
        for context_key in target["contextKeys"]:
            keys.setdefault(context_key, (order, int(target["variation"])))
    return index


def targeted_variation(targets: TargetIndex, contexts: Contexts) -> int | None:
    """The variation of the first target in array order that matches the context, or `None`."""
    first: tuple[int, int] | None = None
    for kind, (context_key, _attributes) in contexts.items():
        hit = targets.get(kind, {}).get(context_key)
        if hit is not None and (first is None or hit[0] < first[0]):
            first = hit
    return None if first is None else first[1]


def _compile_flag(value: object, segments: dict[str, ReadySegment | None]) -> FlagEntry:
    if not is_flag_header(value):
        return FlagEntry("malformed")
    flag: Mapping[str, Any] = value
    if not is_flag_body(flag, len(flag["variations"])):
        return FlagEntry("broken", flag, ErrorCode.MALFORMED_FLAG)
    code = _segment_problem(flag, segments)
    if code is not None:
        return FlagEntry("broken", flag, code)
    return FlagEntry("ready", flag, None, _index(flag))


def _index_by_key(items: object, compile_item: Any) -> dict[str, Any]:
    index: dict[str, Any] = {}
    for item in items if is_array(items) else ():
        key = item.get("key") if isinstance(item, Mapping) else None
        if isinstance(key, str) and key not in index:
            index[key] = compile_item(item)
    return index


def create_ruleset(value: object) -> Ruleset:
    """Validates and indexes `{flags, segments}` as JSON hands it over. It never raises: a bad flag becomes
    an `ERROR` result when it is evaluated."""
    raw_flags = value.get("flags") if isinstance(value, Mapping) else None
    raw_segments = value.get("segments") if isinstance(value, Mapping) else None
    segments = _index_by_key(raw_segments, _compile_segment)
    flags = _index_by_key(raw_flags, lambda item: _compile_flag(item, segments))
    return Ruleset(flags, segments)
