"""The JSON parser, over the standard library's, to the spec's "Reading JSON".

Numbers are IEEE-754 binary64: an integer literal beyond 2^53 reads as the float it rounds to
(``9007199254740993`` is ``9007199254740992.0``), a literal of thousands of digits is infinity
rather than the interpreter's integer-digit-limit error, ``1e999`` is infinity and ``-0.0`` keeps its
sign. ``NaN``, ``Infinity`` and ``-Infinity`` are not JSON and are refused. A lone surrogate escape
stays in the ``str``, which can hold one.
"""

from __future__ import annotations

import json
from typing import Any

_EXACT = 2**53


class JSONError(ValueError):
    """The text is not JSON."""


def _parse_int(text: str) -> int | float:
    if text == "-0":
        return -0.0
    digits = len(text.lstrip("-"))
    if digits < 16:
        return int(text)
    if digits == 16 and abs(int(text)) <= _EXACT:
        return int(text)
    return float(text)


def _parse_constant(text: str) -> Any:
    raise JSONError(f"{text} is not JSON")


_DECODER = json.JSONDecoder(
    parse_int=_parse_int, parse_float=float, parse_constant=_parse_constant, strict=True
)


def parse(data: bytes | str) -> Any:
    """Parses a JSON document. Raises ``JSONError`` for anything that is not one."""
    try:
        text = data.decode("utf-8") if isinstance(data, (bytes, bytearray)) else data
    except UnicodeDecodeError as exc:
        raise JSONError("not valid UTF-8") from exc
    if text.startswith("﻿"):
        text = text[1:]
    try:
        return _DECODER.decode(text) if text.strip(" \t\n\r") else _empty()
    except RecursionError as exc:
        raise JSONError("nested too deeply") from exc
    except json.JSONDecodeError as exc:
        raise JSONError(str(exc)) from exc


def _empty() -> Any:
    raise JSONError("empty document")
