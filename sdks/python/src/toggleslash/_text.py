"""Strings as the spec reads them: UTF-16 code units to compare, code points for patterns, UTF-8 to hash.

A Python `str` is a sequence of code points that may also hold a lone surrogate. The spec compares by
UTF-16 code unit, so a character above U+FFFF counts as its two surrogates; `u16` rewrites a string that
way, after which Python's own `==`, `in`, `startswith`, `endswith` and `<` are code-unit operations.
"""

from __future__ import annotations


def u16(text: str) -> str:
    """`text` with every character above U+FFFF replaced by its surrogate pair."""
    if text.isascii() or max(text) <= "￿":
        return text
    out: list[str] = []
    for char in text:
        point = ord(char)
        if point > 0xFFFF:
            point -= 0x10000
            out.append(chr(0xD800 + (point >> 10)))
            out.append(chr(0xDC00 + (point & 0x3FF)))
        else:
            out.append(char)
    return "".join(out)


def code_points(text: str) -> list[int]:
    """The code points of `text`: a valid surrogate pair is one, a lone surrogate is itself."""
    points = [ord(char) for char in text]
    if text.isascii() or max(points) < 0xD800:
        return points
    merged: list[int] = []
    index = 0
    count = len(points)
    while index < count:
        high = points[index]
        if 0xD800 <= high <= 0xDBFF and index + 1 < count and 0xDC00 <= points[index + 1] <= 0xDFFF:
            merged.append(((high - 0xD800) << 10) + (points[index + 1] - 0xDC00) + 0x10000)
            index += 2
        else:
            merged.append(high)
            index += 1
    return merged


def utf8(text: str) -> bytes:
    """UTF-8 bytes of `text`, a lone surrogate becoming U+FFFD (what a UTF-16 encoder does)."""
    try:
        return text.encode("utf-8")
    except UnicodeEncodeError:
        units = text.encode("utf-16-le", "surrogatepass")
        return units.decode("utf-16-le", "replace").encode("utf-8")
