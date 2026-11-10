"""Semantic versions: a hand-written walk over the characters (evaluation.md, "Semantic versions")."""

from __future__ import annotations

from functools import lru_cache

_DIGITS = frozenset("0123456789")
_IDENT = frozenset("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-")

# (major, minor, patch, prerelease identifiers)
Version = tuple[str, str, str, tuple[str, ...]]


def _numeric(part: str) -> bool:
    return part != "" and all(ch in _DIGITS for ch in part) and (part == "0" or part[0] != "0")


def _identifiers(text: str) -> list[str] | None:
    parts = text.split(".")
    for part in parts:
        if part == "" or not all(ch in _IDENT for ch in part):
            return None
    return parts


@lru_cache(maxsize=2048)
def parse_version(text: str) -> Version | None:
    """The parsed version, or ``None`` when ``text`` is not one."""
    rest = text
    build = None
    plus = rest.find("+")
    if plus >= 0:
        build = rest[plus + 1 :]
        rest = rest[:plus]
        if _identifiers(build) is None:
            return None
    pre: list[str] | None = None
    dash = rest.find("-")
    if dash >= 0:
        pre = _identifiers(rest[dash + 1 :])
        rest = rest[:dash]
        if pre is None:
            return None
        for ident in pre:
            if all(ch in _DIGITS for ch in ident) and len(ident) > 1 and ident[0] == "0":
                return None
    core = rest.split(".")
    has_suffix = build is not None or pre is not None
    if len(core) == 3:
        pass
    elif len(core) in (1, 2) and not has_suffix:
        core = core + ["0"] * (3 - len(core))
    else:
        return None
    if not all(_numeric(part) for part in core):
        return None
    return core[0], core[1], core[2], tuple(pre or ())


def _compare_numeric(a: str, b: str) -> int:
    if len(a) != len(b):
        return -1 if len(a) < len(b) else 1
    if a == b:
        return 0
    return -1 if a < b else 1


def _is_digits(text: str) -> bool:
    return all(ch in _DIGITS for ch in text)


def compare_versions(a: Version, b: Version) -> int:
    """-1, 0 or 1 by SemVer 2.0.0 section 11 (build is not part of a ``Version``)."""
    for x, y in zip(a[:3], b[:3], strict=True):
        c = _compare_numeric(x, y)
        if c != 0:
            return c
    pa, pb = a[3], b[3]
    if not pa and not pb:
        return 0
    if not pa:
        return 1
    if not pb:
        return -1
    for x, y in zip(pa, pb, strict=False):
        xd, yd = _is_digits(x), _is_digits(y)
        if xd and yd:
            c = _compare_numeric(x, y)
        elif xd:
            c = -1
        elif yd:
            c = 1
        else:
            c = 0 if x == y else (-1 if x < y else 1)
        if c != 0:
            return c
    if len(pa) == len(pb):
        return 0
    return -1 if len(pa) < len(pb) else 1
