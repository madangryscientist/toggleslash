"""Semantic versions, to the "Semantic versions" section of docs/evaluation.md.

A hand-written walk over the string: no regular expression. No numeric part is converted to a number.
"""

from __future__ import annotations

from typing import NamedTuple

_DIGITS = frozenset("0123456789")
_IDENTIFIER_CHARS = frozenset("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-")


class SemanticVersion(NamedTuple):
    major: str
    minor: str
    patch: str
    prerelease: tuple[str, ...]


def _all_digits(text: str) -> bool:
    return all(char in _DIGITS for char in text)


def _is_numeric_part(text: str) -> bool:
    return text != "" and _all_digits(text) and (text == "0" or text[0] != "0")


def _identifiers(text: str) -> list[str] | None:
    parts = text.split(".")
    for part in parts:
        if part == "" or not all(char in _IDENTIFIER_CHARS for char in part):
            return None
    return parts


def _split_at(text: str, char: str) -> tuple[str, str | None]:
    at = text.find(char)
    return (text, None) if at < 0 else (text[:at], text[at + 1 :])


def _prerelease_identifiers(text: str) -> list[str] | None:
    parts = _identifiers(text)
    if parts is None:
        return None
    return parts if all(not _all_digits(part) or _is_numeric_part(part) for part in parts) else None


def _numeric_parts(text: str, short: bool) -> list[str] | None:
    numbers = text.split(".")
    if len(numbers) > 3 or (len(numbers) < 3 and not short):
        return None
    return numbers if all(_is_numeric_part(number) for number in numbers) else None


def parse_semantic_version(text: str) -> SemanticVersion | None:
    without_build, build = _split_at(text, "+")
    if build is not None and _identifiers(build) is None:
        return None
    core, prerelease = _split_at(without_build, "-")
    parsed_prerelease: list[str] | None = [] if prerelease is None else _prerelease_identifiers(prerelease)
    if parsed_prerelease is None:
        return None
    numbers = _numeric_parts(core, build is None and prerelease is None)
    if numbers is None:
        return None
    padded = [*numbers, "0", "0"]
    return SemanticVersion(padded[0], padded[1], padded[2], tuple(parsed_prerelease))


def _sign(order: int) -> int:
    return -1 if order < 0 else (1 if order > 0 else 0)


def _compare_text(a: str, b: str) -> int:
    return -1 if a < b else (1 if a > b else 0)


def _compare_numeric(a: str, b: str) -> int:
    if len(a) != len(b):
        return -1 if len(a) < len(b) else 1
    return _compare_text(a, b)


def _compare_identifier(a: str, b: str) -> int:
    a_numeric = _all_digits(a)
    b_numeric = _all_digits(b)
    if a_numeric and b_numeric:
        return _compare_numeric(a, b)
    if a_numeric:
        return -1
    if b_numeric:
        return 1
    return _compare_text(a, b)


def _compare_prerelease(a: tuple[str, ...], b: tuple[str, ...]) -> int:
    if not a or not b:
        return _sign(len(b) - len(a))
    for left, right in zip(a, b, strict=False):
        order = _compare_identifier(left, right)
        if order != 0:
            return order
    return _sign(len(a) - len(b))


def compare_semantic_versions(a: SemanticVersion, b: SemanticVersion) -> int:
    core = (
        _compare_numeric(a.major, b.major) or _compare_numeric(a.minor, b.minor) or _compare_numeric(a.patch, b.patch)
    )
    return core if core != 0 else _compare_prerelease(a.prerelease, b.prerelease)
