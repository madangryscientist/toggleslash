"""Strings, as the evaluation spec reads them.

A Python ``str`` is a sequence of Unicode code points and can hold a lone surrogate, which the spec
keeps (a lone surrogate compares equal only to the same lone surrogate). The spec compares and
orders by UTF-16 code unit, matches patterns by code point and hashes the UTF-8 encoding in which
a lone surrogate becomes U+FFFD. Strings that come from a JSON parser are already canonical: a
valid escaped pair is one astral character. The helpers here also make a string built by hand,
with the two halves of a pair side by side, read as the pair.
"""

from __future__ import annotations

_HIGH_FIRST, _HIGH_LAST = 0xD800, 0xDBFF
_LOW_FIRST, _LOW_LAST = 0xDC00, 0xDFFF
_ASTRAL = 0x10000


def units(text: str) -> str:
    """The text as UTF-16 code units, each held as one character at or below U+FFFF.

    Two such strings compare, search and order by code unit with Python's own operators.
    """
    if text.isascii() or max(text) <= "￿":
        return text
    out: list[str] = []
    for char in text:
        point = ord(char)
        if point < _ASTRAL:
            out.append(char)
        else:
            point -= _ASTRAL
            out.append(chr(_HIGH_FIRST + (point >> 10)))
            out.append(chr(_LOW_FIRST + (point & 0x3FF)))
    return "".join(out)


def scalars(text: str) -> str:
    """The text with each adjacent high and low surrogate joined into one code point.

    A lone surrogate stays as it is.
    """
    if text.isascii() or not any("\ud800" <= char <= "\udfff" for char in text):
        return text
    out: list[str] = []
    index = 0
    length = len(text)
    while index < length:
        point = ord(text[index])
        nxt = ord(text[index + 1]) if index + 1 < length else 0
        if _HIGH_FIRST <= point <= _HIGH_LAST and _LOW_FIRST <= nxt <= _LOW_LAST:
            out.append(chr(((point - _HIGH_FIRST) << 10) + (nxt - _LOW_FIRST) + _ASTRAL))
            index += 2
        else:
            out.append(text[index])
            index += 1
    return "".join(out)


def code_points(text: str) -> list[int]:
    """The code points of a string: a valid pair is one, a lone surrogate is itself."""
    return [ord(char) for char in scalars(text)]


def same(left: str, right: str) -> bool:
    """Equal by UTF-16 code unit."""
    return left == right or units(left) == units(right)


def utf8(text: str) -> bytes:
    """The UTF-8 bytes of a string, a lone surrogate encoded as U+FFFD."""
    joined = scalars(text)
    try:
        return joined.encode("utf-8")
    except UnicodeEncodeError:
        replaced = "".join("�" if "\ud800" <= char <= "\udfff" else char for char in joined)
        return replaced.encode("utf-8")
