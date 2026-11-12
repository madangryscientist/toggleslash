"""Strings the way docs/evaluation.md reads them: by UTF-16 code unit, not by Python code point.

Python compares strings by code point, which orders U+E000 to U+FFFF after a supplementary character, and
which treats a pair of lone surrogates as two characters. Anything that is not pure ASCII is therefore
compared as its UTF-16 big-endian bytes (lone surrogates kept), which is the code unit order and spells a
pair of lone surrogates as the character they make. Pure ASCII, the common case, is compared directly.
"""

from __future__ import annotations


def units(text: str) -> bytes:
    """The UTF-16 big-endian code units of `text`; a lone surrogate stays one unit."""
    return text.encode("utf-16-be", "surrogatepass")


def compare(a: str, b: str) -> int:
    """-1, 0 or 1, by UTF-16 code unit."""
    if not (a.isascii() and b.isascii()):
        a, b = units(a), units(b)
    if a < b:
        return -1
    return 1 if a > b else 0


def equals(a: str, b: str) -> bool:
    """Ordinal equality by UTF-16 code unit."""
    if a == b:
        return True
    if a.isascii() and b.isascii():
        return False
    return units(a) == units(b)


def contains(haystack: str, needle: str) -> bool:
    """Is `needle` a run of code units in `haystack`? An empty needle is."""
    if haystack.isascii() and needle.isascii():
        return needle in haystack
    h, n = units(haystack), units(needle)
    start = 0
    while True:
        at = h.find(n, start)
        if at < 0:
            return False
        if at % 2 == 0:
            return True
        start = at + 1


def starts_with(text: str, prefix: str) -> bool:
    if text.isascii() and prefix.isascii():
        return text.startswith(prefix)
    return units(text).startswith(units(prefix))


def ends_with(text: str, suffix: str) -> bool:
    if text.isascii() and suffix.isascii():
        return text.endswith(suffix)
    return units(text).endswith(units(suffix))


def code_points(text: str) -> list[int]:
    """The code points of a UTF-16 string: a valid surrogate pair is one, a lone surrogate is itself."""
    if text.isascii():
        return list(text.encode("ascii"))
    joined = text.encode("utf-16-le", "surrogatepass").decode("utf-16-le", "surrogatepass")
    return [ord(char) for char in joined]


def hashable(text: str) -> bytes:
    """The UTF-8 bytes of `text`, with each lone surrogate written as U+FFFD."""
    try:
        return text.encode("utf-8")
    except UnicodeEncodeError:
        pass
    repaired = text.encode("utf-16-le", "surrogatepass").decode("utf-16-le", "replace")
    return repaired.encode("utf-8")
