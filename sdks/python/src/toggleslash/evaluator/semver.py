"""Semantic versions, to "Semantic versions" in docs/evaluation.md.

A hand-written walk over the characters: no regular expression (``\\d`` would accept Unicode
digits) and no library. Nothing here converts a numeric part to a number.
"""

from __future__ import annotations

from typing import NamedTuple


class Version(NamedTuple):
    major: str
    minor: str
    patch: str
    prerelease: tuple[str, ...]


def _digit(char: str) -> bool:
    return "0" <= char <= "9"


def _identifier_char(char: str) -> bool:
    return _digit(char) or "A" <= char <= "Z" or "a" <= char <= "z" or char == "-"


def _all_digits(text: str) -> bool:
    return all(_digit(char) for char in text)


def _is_numeric_part(text: str) -> bool:
    return text != "" and _all_digits(text) and (text == "0" or text[0] != "0")


def _identifiers(text: str) -> list[str] | None:
    parts = text.split(".")
    for part in parts:
        if part == "" or not all(_identifier_char(char) for char in part):
            return None
    return parts


def _split_at(text: str, char: str) -> tuple[str, str | None]:
    at = text.find(char)
    if at < 0:
        return text, None
    return text[:at], text[at + 1 :]


def _prerelease_identifiers(text: str) -> list[str] | None:
    parts = _identifiers(text)
    if parts is None:
        return None
    if all(not _all_digits(part) or _is_numeric_part(part) for part in parts):
        return parts
    return None


def _numeric_parts(text: str, short: bool) -> list[str] | None:
    numbers = text.split(".")
    if len(numbers) > 3 or (len(numbers) < 3 and not short):
        return None
    return numbers if all(_is_numeric_part(number) for number in numbers) else None


def parse_version(text: str) -> Version | None:
    """The version ``text`` spells, or None when it is not one."""
    without_build, build = _split_at(text, "+")
    if build is not None and _identifiers(build) is None:
        return None
    core, prerelease = _split_at(without_build, "-")
    parsed: list[str] | None = []
    if prerelease is not None:
        parsed = _prerelease_identifiers(prerelease)
    if parsed is None:
        return None
    numbers = _numeric_parts(core, build is None and prerelease is None)
    if numbers is None:
        return None
    padded = [*numbers, "0", "0", "0"][:3]
    return Version(padded[0], padded[1], padded[2], tuple(parsed))


def _sign(left: int, right: int) -> int:
    if left < right:
        return -1
    return 1 if left > right else 0


def _compare_numeric(left: str, right: str) -> int:
    """Digit strings with no leading zero: the longer is larger, else by code unit."""
    if len(left) != len(right):
        return _sign(len(left), len(right))
    if left < right:
        return -1
    return 1 if left > right else 0


def _compare_identifier(left: str, right: str) -> int:
    left_numeric = _all_digits(left)
    right_numeric = _all_digits(right)
    if left_numeric and right_numeric:
        return _compare_numeric(left, right)
    if left_numeric:
        return -1
    if right_numeric:
        return 1
    if left < right:
        return -1
    return 1 if left > right else 0


def _compare_prerelease(left: tuple[str, ...], right: tuple[str, ...]) -> int:
    if not left or not right:
        return _sign(len(right), len(left))
    for one, other in zip(left, right, strict=False):
        order = _compare_identifier(one, other)
        if order != 0:
            return order
    return _sign(len(left), len(right))


def compare_versions(left: Version, right: Version) -> int:
    """-1, 0 or 1 by SemVer 2.0.0 section 11; build metadata is already gone."""
    core = (
        _compare_numeric(left.major, right.major)
        or _compare_numeric(left.minor, right.minor)
        or _compare_numeric(left.patch, right.patch)
    )
    return core if core != 0 else _compare_prerelease(left.prerelease, right.prerelease)
