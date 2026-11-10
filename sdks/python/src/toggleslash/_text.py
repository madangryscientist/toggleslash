"""Code point and UTF-16 helpers.

A Python `str` is a sequence of code points and can hold a lone surrogate. The spec's string rules are over
UTF-16 code units, so ordering and the substring ops go through UTF-16 bytes (big-endian, so a byte compare is
a code unit compare), and hashing replaces a lone surrogate with U+FFFD.
"""

from __future__ import annotations


def normalise(text: str) -> str:
    """Joins a high and a low surrogate that sit side by side into the one code point they spell."""
    if text.isascii():
        return text
    return text.encode("utf-16-be", "surrogatepass").decode("utf-16-be", "surrogatepass")


def utf16(text: str) -> bytes:
    return text.encode("utf-16-be", "surrogatepass")


def code_points(text: str) -> list[int]:
    return [ord(c) for c in normalise(text)]


def utf8_for_hash(text: str) -> bytes:
    """UTF-8 of `text` with each lone surrogate replaced by U+FFFD."""
    if text.isascii():
        return text.encode("ascii")
    return text.encode("utf-16-be", "surrogatepass").decode("utf-16-be", "replace").encode("utf-8")


def compare(a: str, b: str) -> int:
    """-1, 0 or 1 by UTF-16 code unit."""
    if a.isascii() and b.isascii():
        return -1 if a < b else (1 if a > b else 0)
    x, y = utf16(a), utf16(b)
    return -1 if x < y else (1 if x > y else 0)


def contains(text: str, part: str) -> bool:
    if text.isascii() and part.isascii():
        return part in text
    x, y = utf16(text), utf16(part)
    start = 0
    while True:
        at = x.find(y, start)
        if at < 0:
            return False
        if at % 2 == 0:
            return True
        start = at + 1


def starts_with(text: str, part: str) -> bool:
    if text.isascii() and part.isascii():
        return text.startswith(part)
    return utf16(text).startswith(utf16(part))


def ends_with(text: str, part: str) -> bool:
    if text.isascii() and part.isascii():
        return text.endswith(part)
    return utf16(text).endswith(utf16(part))


def equal(a: str, b: str) -> bool:
    return a == b or (not (a.isascii() and b.isascii()) and utf16(a) == utf16(b))
