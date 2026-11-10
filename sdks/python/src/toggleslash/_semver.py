"""Semantic versions, to the "Semantic versions" section of docs/evaluation.md.

A hand-written walk over the string: no regular expression, and no numeric part is ever converted to a number
(a part of any length compares as a digit string).
"""

from __future__ import annotations

from typing import NamedTuple


class SemVer(NamedTuple):
    major: str
    minor: str
    patch: str
    prerelease: tuple[str, ...]


def _all_digits(text: str) -> bool:
    return all("0" <= c <= "9" for c in text)


def _is_numeric_part(text: str) -> bool:
    return text != "" and _all_digits(text) and (text == "0" or text[0] != "0")


def _is_identifier_char(c: str) -> bool:
    return "0" <= c <= "9" or "A" <= c <= "Z" or "a" <= c <= "z" or c == "-"


def _identifiers(text: str) -> list[str] | None:
    parts = text.split(".")
    for part in parts:
        if part == "":
            return None
        if not all(_is_identifier_char(c) for c in part):
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
    return parts if all(not _all_digits(p) or _is_numeric_part(p) for p in parts) else None


def _numeric_parts(text: str, short: bool) -> list[str] | None:
    numbers = text.split(".")
    if len(numbers) > 3 or (len(numbers) < 3 and not short):
        return None
    return numbers if all(_is_numeric_part(n) for n in numbers) else None


def parse_semver(text: str) -> SemVer | None:
    without_build, build = _split_at(text, "+")
    if build is not None and _identifiers(build) is None:
        return None
    core, prerelease = _split_at(without_build, "-")
    parsed: list[str] | None = [] if prerelease is None else _prerelease_identifiers(prerelease)
    if parsed is None:
        return None
    numbers = _numeric_parts(core, build is None and prerelease is None)
    if numbers is None:
        return None
    return SemVer(
        numbers[0],
        numbers[1] if len(numbers) > 1 else "0",
        numbers[2] if len(numbers) > 2 else "0",
        tuple(parsed),
    )


def _sign(n: int) -> int:
    return -1 if n < 0 else (1 if n > 0 else 0)


def _compare_numeric(a: str, b: str) -> int:
    if len(a) != len(b):
        return -1 if len(a) < len(b) else 1
    return -1 if a < b else (1 if a > b else 0)


def _compare_identifier(a: str, b: str) -> int:
    a_num, b_num = _all_digits(a), _all_digits(b)
    if a_num and b_num:
        return _compare_numeric(a, b)
    if a_num:
        return -1
    if b_num:
        return 1
    return -1 if a < b else (1 if a > b else 0)


def _compare_prerelease(a: tuple[str, ...], b: tuple[str, ...]) -> int:
    if not a or not b:
        return _sign(len(b) - len(a))
    for x, y in zip(a, b, strict=False):
        order = _compare_identifier(x, y)
        if order != 0:
            return order
    return _sign(len(a) - len(b))


def compare_semver(a: SemVer, b: SemVer) -> int:
    core = (
        _compare_numeric(a.major, b.major) or _compare_numeric(a.minor, b.minor) or _compare_numeric(a.patch, b.patch)
    )
    return core if core != 0 else _compare_prerelease(a.prerelease, b.prerelease)
