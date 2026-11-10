"""The evaluator of evaluation.md: ``load_ruleset`` and ``evaluate``. Pure: no I/O, no clock, no raise."""

from __future__ import annotations

import math
from collections.abc import Mapping
from typing import Any

from ._bucket import bucket
from ._context import Normalised, is_kind, normalise
from ._dates import to_instant
from ._pattern import code_points, compile_pattern, search
from ._reference import attribute_path
from ._semver import compare_versions, parse_version
from ._types import ErrorCode, Reason, ReasonKind

DEFAULT_KIND = "user"
BUCKET_COUNT = 100000
MAX_DEPTH = 32

Result = tuple[Any, "int | None", Reason]

_ATTRIBUTE_OPS = frozenset(
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

_MET = (ReasonKind.TARGET_MATCH, ReasonKind.RULE_MATCH, ReasonKind.FALLTHROUGH)


class _Invalid(Exception):
    """A flag or segment does not fit evaluation.md."""


def _error(code: ErrorCode) -> Result:
    return None, None, Reason(ReasonKind.ERROR, code=code)


# ---------------------------------------------------------------------------------------------
# Small predicates over parsed JSON


def _is_object(value: Any) -> bool:
    return isinstance(value, dict)


def _is_number(value: Any) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _as_float(value: Any) -> float:
    """The binary64 of a number; an integer too large for it reads as infinity."""
    try:
        return float(value)
    except OverflowError:
        return math.inf if value > 0 else -math.inf


def _is_count(value: Any) -> bool:
    if isinstance(value, bool):
        return False
    if isinstance(value, int):
        return value >= 0
    if isinstance(value, float):
        return math.isfinite(value) and value.is_integer() and value >= 0
    return False


def _is_key(value: Any) -> bool:
    return isinstance(value, str) and value != ""


def _is_string_list(value: Any) -> bool:
    return isinstance(value, list) and all(isinstance(item, str) for item in value)


def _optional_kind(value: Any) -> str:
    """The kind of an optional kind field; raises when it is present and not valid."""
    if value is None:
        return DEFAULT_KIND
    if not is_kind(value):
        raise _Invalid
    return value


def _index(value: Any, count: int) -> int:
    if not _is_count(value) or value >= count:
        raise _Invalid
    return int(value)


def _units(text: str) -> str:
    """``text`` as UTF-16 code units, each held in one ``str`` character, so comparison is by unit."""
    if text.isascii() or max(text) <= "￿":
        return text
    out = []
    for ch in text:
        cp = ord(ch)
        if cp > 0xFFFF:
            cp -= 0x10000
            out.append(chr(0xD800 + (cp >> 10)))
            out.append(chr(0xDC00 + (cp & 0x3FF)))
        else:
            out.append(ch)
    return "".join(out)


# ---------------------------------------------------------------------------------------------
# The compiled forms


class _Clause:
    __slots__ = ("kind", "negate", "op", "path", "segments", "values")

    def __init__(self) -> None:
        self.path: tuple[str, ...] = ()
        self.kind = DEFAULT_KIND
        self.op = ""
        self.values: list[Any] = []
        self.negate = False
        self.segments: list[str] | None = None  # set for segmentMatch


class _Serve:
    __slots__ = ("entries", "kind", "variation")

    def __init__(self, variation: int | None, kind: str, entries: list[tuple[int, int]]) -> None:
        self.variation = variation
        self.kind = kind
        self.entries = entries


class _Rule:
    __slots__ = ("clauses", "serve")

    def __init__(self, clauses: list[_Clause], serve: _Serve) -> None:
        self.clauses = clauses
        self.serve = serve


class _Flag:
    __slots__ = (
        "fallthrough",
        "key",
        "off",
        "on",
        "prerequisites",
        "rules",
        "salt",
        "targets",
        "variations",
        "version",
    )

    def __init__(self) -> None:
        self.key = ""
        self.salt = ""
        self.version = 0
        self.on = False
        self.variations: list[Any] = []
        self.off: int | None = None
        self.targets: dict[str, dict[str, tuple[int, int]]] = {}
        self.prerequisites: list[tuple[str, int]] = []
        self.rules: list[_Rule] = []
        self.fallthrough = _Serve(0, DEFAULT_KIND, [])


class _Entry:
    """A flag as loaded: ``status`` is ``malformed``, ``broken`` (carrying ``code``) or ``ready``."""

    __slots__ = ("code", "flag", "status")

    def __init__(self, status: str, flag: _Flag | None, code: ErrorCode | None) -> None:
        self.status = status
        self.flag = flag
        self.code = code


class _SegmentRule:
    __slots__ = ("clauses", "kind", "weight")

    def __init__(self, clauses: list[_Clause], weight: int | None, kind: str) -> None:
        self.clauses = clauses
        self.weight = weight
        self.kind = kind


class _Segment:
    __slots__ = ("excluded", "included", "key", "rules", "salt")

    def __init__(self) -> None:
        self.key = ""
        self.salt = ""
        self.included: dict[str, set[str]] = {}
        self.excluded: dict[str, set[str]] = {}
        self.rules: list[_SegmentRule] = []


class Ruleset:
    """A loaded ruleset: immutable once built, so one evaluation and another never interfere."""

    __slots__ = ("flags", "segments")

    def __init__(self, flags: dict[str, _Entry], segments: dict[str, _Segment | None]) -> None:
        self.flags = flags
        self.segments = segments

    def flag_version(self, key: str) -> int | None:
        """The ``version`` of a flag that is fully valid, or ``None``."""
        entry = self.flags.get(key)
        if entry is None or entry.status != "ready" or entry.flag is None:
            return None
        return entry.flag.version

    def __len__(self) -> int:
        return len(self.flags)


# ---------------------------------------------------------------------------------------------
# Loading


def _clause_value_ok(value: Any) -> bool:
    if isinstance(value, (bool, str)):
        return True
    return _is_number(value) and math.isfinite(_as_float(value))


def _attribute_clause(raw: Any) -> _Clause:
    if not _is_object(raw) or raw.get("op") not in _ATTRIBUTE_OPS:
        raise _Invalid
    values = raw.get("values")
    negate = raw.get("negate")
    if negate is not None and not isinstance(negate, bool):
        raise _Invalid
    path = attribute_path(raw.get("attribute"))
    if path is None:
        raise _Invalid
    clause = _Clause()
    clause.path = path
    clause.kind = _optional_kind(raw.get("contextKind"))
    clause.op = raw["op"]
    if not isinstance(values, list) or not all(_clause_value_ok(v) for v in values):
        raise _Invalid
    clause.values = values
    clause.negate = negate is True
    return clause


def _segment_clause(raw: Any) -> _Clause:
    negate = raw.get("negate")
    values = raw.get("values")
    if (negate is not None and not isinstance(negate, bool)) or not _is_string_list(values):
        raise _Invalid
    clause = _Clause()
    clause.op = "segmentMatch"
    clause.negate = negate is True
    clause.segments = values
    return clause


def _rule_clause(raw: Any) -> _Clause:
    if not _is_object(raw):
        raise _Invalid
    if raw.get("op") == "segmentMatch":
        return _segment_clause(raw)
    return _attribute_clause(raw)


def _serve(raw: Any, count: int) -> _Serve:
    if not _is_object(raw):
        raise _Invalid
    rollout = raw.get("rollout")
    variation = raw.get("variation")
    if rollout is None:
        return _Serve(_index(variation, count), DEFAULT_KIND, [])
    if variation is not None or not _is_object(rollout):
        raise _Invalid
    kind = _optional_kind(rollout.get("contextKind"))
    items = rollout.get("variations")
    if not isinstance(items, list) or len(items) == 0:
        raise _Invalid
    entries = []
    total = 0
    for item in items:
        if not _is_object(item):
            raise _Invalid
        weight = item.get("weight")
        if not _is_count(weight) or weight > BUCKET_COUNT:
            raise _Invalid
        entries.append((_index(item.get("variation"), count), int(weight)))
        total += int(weight)
    if total != BUCKET_COUNT:
        raise _Invalid
    return _Serve(None, kind, entries)


def _rule(raw: Any, count: int) -> _Rule:
    if not _is_object(raw):
        raise _Invalid
    clauses = raw.get("clauses")
    if not isinstance(clauses, list):
        raise _Invalid
    return _Rule([_rule_clause(c) for c in clauses], _serve(raw, count))


def _header_ok(raw: Any) -> bool:
    if not _is_object(raw) or not isinstance(raw.get("on"), bool):
        return False
    variations = raw.get("variations")
    if not _is_key(raw.get("key")) or not isinstance(variations, list) or len(variations) == 0:
        return False
    off = raw.get("offVariation")
    return off is None or (_is_count(off) and off < len(variations))


def _load_flag(raw: Any) -> _Entry:
    """Check one flag. A bad header is ``malformed``; a bad body is ``broken`` with MALFORMED_FLAG."""
    if not _header_ok(raw):
        return _Entry("malformed", None, ErrorCode.MALFORMED_FLAG)
    flag = _Flag()
    flag.key = raw["key"]
    flag.on = raw["on"]
    flag.variations = raw["variations"]
    off = raw.get("offVariation")
    flag.off = None if off is None else int(off)
    count = len(flag.variations)
    try:
        salt = raw.get("salt")
        if not isinstance(salt, str) or not _is_count(raw.get("version")):
            raise _Invalid
        flag.salt = salt
        flag.version = int(raw["version"])
        targets = raw.get("targets")
        prerequisites = raw.get("prerequisites")
        rules = raw.get("rules")
        if not isinstance(targets, list) or not isinstance(prerequisites, list):
            raise _Invalid
        if not isinstance(rules, list):
            raise _Invalid
        for order, target in enumerate(targets):
            if not _is_object(target):
                raise _Invalid
            variation = _index(target.get("variation"), count)
            keys = target.get("contextKeys")
            if not _is_string_list(keys):
                raise _Invalid
            kind = _optional_kind(target.get("contextKind"))
            by_key = flag.targets.setdefault(kind, {})
            for key in keys:
                by_key.setdefault(key, (variation, order))
        for prerequisite in prerequisites:
            if not _is_object(prerequisite):
                raise _Invalid
            if not _is_key(prerequisite.get("flagKey")) or not _is_count(prerequisite.get("variation")):
                raise _Invalid
            flag.prerequisites.append((prerequisite["flagKey"], int(prerequisite["variation"])))
        flag.rules = [_rule(r, count) for r in rules]
        flag.fallthrough = _serve(raw.get("fallthrough"), count)
    except _Invalid:
        return _Entry("broken", flag, ErrorCode.MALFORMED_FLAG)
    return _Entry("ready", flag, None)


def _context_lists(raw: Any) -> list[tuple[str, list[str]]]:
    if raw is None:
        return []
    if not isinstance(raw, list):
        raise _Invalid
    out = []
    for item in raw:
        if not _is_object(item) or not is_kind(item.get("contextKind")):
            raise _Invalid
        values = item.get("values")
        if not _is_string_list(values):
            raise _Invalid
        out.append((item["contextKind"], values))
    return out


def _key_sets(users: Any, lists: Any) -> dict[str, set[str]]:
    if not _is_string_list(users):
        raise _Invalid
    sets: dict[str, set[str]] = {DEFAULT_KIND: set(users)}
    for kind, values in _context_lists(lists):
        sets.setdefault(kind, set()).update(values)
    return sets


def _load_segment(raw: Any) -> _Segment | None:
    try:
        if not _is_object(raw) or not _is_key(raw.get("key")) or not isinstance(raw.get("salt"), str):
            raise _Invalid
        segment = _Segment()
        segment.key = raw["key"]
        segment.salt = raw["salt"]
        segment.included = _key_sets(raw.get("included"), raw.get("includedContexts"))
        segment.excluded = _key_sets(raw.get("excluded"), raw.get("excludedContexts"))
        rules = raw.get("rules")
        if not isinstance(rules, list):
            raise _Invalid
        for rule in rules:
            if not _is_object(rule):
                raise _Invalid
            weight = rule.get("weight")
            if weight is not None and (not _is_count(weight) or weight > BUCKET_COUNT):
                raise _Invalid
            kind = _optional_kind(rule.get("rolloutContextKind"))
            clauses = rule.get("clauses")
            if not isinstance(clauses, list):
                raise _Invalid
            segment.rules.append(
                _SegmentRule(
                    [_attribute_clause(c) for c in clauses],
                    None if weight is None else int(weight),
                    kind,
                )
            )
        return segment
    except _Invalid:
        return None


def _segment_error(flag: _Flag, segments: dict[str, _Segment | None]) -> ErrorCode | None:
    """The first bad ``segmentMatch`` reference in rule, clause and value order."""
    for rule in flag.rules:
        for clause in rule.clauses:
            for key in clause.segments or ():
                if key not in segments:
                    return ErrorCode.SEGMENT_NOT_FOUND
                if segments[key] is None:
                    return ErrorCode.MALFORMED_SEGMENT
    return None


def load_ruleset(raw: Any) -> Ruleset:
    """Load a parsed ruleset. Never fails: a bad entry becomes an ``ERROR`` when it is evaluated."""
    flags_raw = raw.get("flags") if isinstance(raw, dict) else None
    segments_raw = raw.get("segments") if isinstance(raw, dict) else None
    segments: dict[str, _Segment | None] = {}
    if isinstance(segments_raw, list):
        for item in segments_raw:
            key = item.get("key") if isinstance(item, dict) else None
            if isinstance(key, str) and key not in segments:
                segments[key] = _load_segment(item)
    flags: dict[str, _Entry] = {}
    if isinstance(flags_raw, list):
        for item in flags_raw:
            key = item.get("key") if isinstance(item, dict) else None
            if isinstance(key, str) and key not in flags:
                entry = _load_flag(item)
                if entry.status == "ready" and entry.flag is not None:
                    code = _segment_error(entry.flag, segments)
                    if code is not None:
                        entry = _Entry("broken", entry.flag, code)
                flags[key] = entry
    return Ruleset(flags, segments)


# ---------------------------------------------------------------------------------------------
# Clauses


def _read(path: tuple[str, ...], kind: str, context: Normalised) -> Any:
    """The value an attribute reads, or ``None`` when it is missing."""
    entry = context.get(kind)
    if entry is None:
        return None
    key, attributes = entry
    first = path[0]
    if first == "key":
        current: Any = key
    elif attributes is not None and first in attributes:
        current = attributes[first]
    else:
        return None
    for name in path[1:]:
        if not isinstance(current, Mapping) or name not in current:
            return None
        current = current[name]
    return current


def _compare(op: str, a: float | str, v: float | str) -> bool:
    if op == "lt":
        return a < v  # type: ignore[operator]
    if op == "lte":
        return a <= v  # type: ignore[operator]
    if op == "gt":
        return a > v  # type: ignore[operator]
    return a >= v  # type: ignore[operator]


_SEMVER = {
    "semverEq": lambda c: c == 0,
    "semverLt": lambda c: c < 0,
    "semverLte": lambda c: c <= 0,
    "semverGt": lambda c: c > 0,
    "semverGte": lambda c: c >= 0,
}


def _match_one(op: str, a: Any, v: Any) -> bool:
    """One candidate against one clause value."""
    if isinstance(a, bool):
        return op == "in" and isinstance(v, bool) and a == v
    if _is_number(a):
        if not _is_number(v):
            return False
        fa, fv = _as_float(a), _as_float(v)
        if not (math.isfinite(fa) and math.isfinite(fv)):
            return False
        if op == "in":
            return fa == fv
        if op in ("lt", "lte", "gt", "gte"):
            return _compare(op, fa, fv)
        if op in ("before", "after"):
            return (fa < fv) if op == "before" else (fa > fv)
        return False
    if isinstance(a, str):
        return _match_string(op, a, v)
    return False


def _match_string(op: str, a: str, v: Any) -> bool:
    if op in ("before", "after"):
        ia, iv = to_instant(a), to_instant(v)
        if ia is None or iv is None:
            return False
        return (ia < iv) if op == "before" else (ia > iv)
    if not isinstance(v, str):
        return False
    if op == "in":
        return _units(a) == _units(v)
    if op == "contains":
        return _units(v) in _units(a)
    if op == "startsWith":
        return _units(a).startswith(_units(v))
    if op == "endsWith":
        return _units(a).endswith(_units(v))
    if op in ("lt", "lte", "gt", "gte"):
        return _compare(op, _units(a), _units(v))
    if op in _SEMVER:
        left, right = parse_version(a), parse_version(v)
        if left is None or right is None:
            return False
        return _SEMVER[op](compare_versions(left, right))
    if op == "matches":
        pattern = compile_pattern(code_points(v))
        return pattern is not None and search(pattern, code_points(a))
    return False


def _attribute_match(clause: _Clause, context: Normalised) -> bool:
    value = _read(clause.path, clause.kind, context)
    if value is None:
        return False
    candidates = value if isinstance(value, (list, tuple)) else (value,)
    op = clause.op
    base = False
    if op in ("before", "after"):
        # a number or a string on the candidate side; the value side is read inside the comparison
        for a in candidates:
            for v in clause.values:
                if _instant_match(op, a, v):
                    base = True
                    break
            if base:
                break
    else:
        for a in candidates:
            if any(_match_one(op, a, v) for v in clause.values):
                base = True
                break
    return base != clause.negate


def _instant_match(op: str, a: Any, v: Any) -> bool:
    ia, iv = to_instant(a), to_instant(v)
    if ia is None or iv is None:
        return False
    return (ia < iv) if op == "before" else (ia > iv)


# ---------------------------------------------------------------------------------------------
# Bucketing and serving


def _bucket_of(key: str, salt: str, context: Normalised, kind: str) -> int:
    entry = context.get(kind)
    if entry is None:
        return 0
    context_key = entry[0] if kind == DEFAULT_KIND else f"{kind}:{entry[0]}"
    return bucket(key, salt, context_key)


def _serve_index(serve: _Serve, flag: _Flag, context: Normalised) -> int:
    if serve.variation is not None:
        return serve.variation
    value = _bucket_of(flag.key, flag.salt, context, serve.kind)
    total = 0
    for variation, weight in serve.entries:
        total += weight
        if value < total:
            return variation
    return serve.entries[-1][0]  # unreachable: the weights sum to 100000


def _in_keys(sets: dict[str, set[str]], context: Normalised) -> bool:
    for kind, keys in sets.items():
        entry = context.get(kind)
        if entry is not None and entry[0] in keys:
            return True
    return False


def _in_segment(segment: _Segment, context: Normalised) -> bool:
    if _in_keys(segment.excluded, context):
        return False
    if _in_keys(segment.included, context):
        return True
    for rule in segment.rules:
        if not all(_attribute_match(c, context) for c in rule.clauses):
            continue
        if rule.weight is None:
            return True
        if rule.weight > _bucket_of(segment.key, segment.salt, context, rule.kind):
            return True
    return False


def _clause_matches(clause: _Clause, context: Normalised, segments: dict[str, _Segment | None]) -> bool:
    if clause.segments is None:
        return _attribute_match(clause, context)
    base = False
    for key in clause.segments:
        segment = segments.get(key)
        if segment is not None and _in_segment(segment, context):
            base = True
            break
    return base != clause.negate


# ---------------------------------------------------------------------------------------------
# Evaluation


class _Run:
    __slots__ = ("active", "context", "done", "ruleset")

    def __init__(self, ruleset: Ruleset, context: Normalised) -> None:
        self.ruleset = ruleset
        self.context = context
        self.done: dict[str, Result] = {}
        self.active: set[str] = set()

    def enter(self, key: str, depth: int) -> Result:
        finished = self.done.get(key)
        if finished is not None:
            return finished
        if key in self.active:
            return _error(ErrorCode.PREREQUISITE_CYCLE)
        if depth > MAX_DEPTH:
            return _error(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED)
        entry = self.ruleset.flags.get(key)
        if entry is None:
            return _error(ErrorCode.FLAG_NOT_FOUND)
        self.active.add(key)
        try:
            result = self.run(entry, depth)
        finally:
            self.active.discard(key)
        self.done[key] = result
        return result

    def run(self, entry: _Entry, depth: int) -> Result:
        if entry.status == "malformed" or entry.flag is None:
            return _error(ErrorCode.MALFORMED_FLAG)
        flag = entry.flag
        if not flag.on:
            return self.off(flag, Reason(ReasonKind.OFF))
        if entry.status == "broken":
            return _error(entry.code or ErrorCode.MALFORMED_FLAG)
        for prerequisite_key, required in flag.prerequisites:
            outcome = self.enter(prerequisite_key, depth + 1)
            kind = outcome[2].kind
            if kind == ReasonKind.ERROR:
                return outcome
            if kind not in _MET or outcome[1] != required:
                return self.off(flag, Reason(ReasonKind.PREREQUISITE_FAILED, flag_key=prerequisite_key))
        context = self.context
        hit = self.target(flag)
        if hit is not None:
            return flag.variations[hit], hit, Reason(ReasonKind.TARGET_MATCH)
        segments = self.ruleset.segments
        for index, rule in enumerate(flag.rules):
            if all(_clause_matches(c, context, segments) for c in rule.clauses):
                chosen = _serve_index(rule.serve, flag, context)
                return flag.variations[chosen], chosen, Reason(ReasonKind.RULE_MATCH, rule_index=index)
        chosen = _serve_index(flag.fallthrough, flag, context)
        return flag.variations[chosen], chosen, Reason(ReasonKind.FALLTHROUGH)

    def target(self, flag: _Flag) -> int | None:
        """The variation of the first target, in array order, that matches the context."""
        best: tuple[int, int] | None = None
        for kind, by_key in flag.targets.items():
            entry = self.context.get(kind)
            if entry is None:
                continue
            hit = by_key.get(entry[0])
            if hit is not None and (best is None or hit[1] < best[1]):
                best = hit
        return None if best is None else best[0]

    @staticmethod
    def off(flag: _Flag, reason: Reason) -> Result:
        if flag.off is None:
            return None, None, reason
        return flag.variations[flag.off], flag.off, reason


def evaluate(ruleset: Ruleset, flag_key: str, context: Any) -> Result:
    """Evaluate one flag for a context: ``(value, variation_index, reason)``. Never raises."""
    normalised = normalise(context)
    if normalised is None:
        return _error(ErrorCode.INVALID_CONTEXT)
    return _Run(ruleset, normalised).enter(flag_key, 0)
