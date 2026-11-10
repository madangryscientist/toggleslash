"""The JSON reader. Numbers are IEEE-754 binary64 and `NaN`/`Infinity` are not JSON."""

from __future__ import annotations

import json
from typing import Any

_EXACT_DIGITS = 18


def _parse_int(literal: str) -> int | float:
    # `int()` of a very long literal raises (the interpreter's digit limit); `float()` of one is Infinity.
    if len(literal) > _EXACT_DIGITS:
        return float(literal)
    number = int(literal)
    return number if -(2**53) <= number <= 2**53 else float(number)


def _reject_constant(name: str) -> Any:
    raise ValueError(f"{name} is not JSON")


def loads(data: bytes | bytearray | str) -> Any:
    """Parses a JSON document. Raises on bad UTF-8, bad JSON or a document nested too deep."""
    text = bytes(data).decode("utf-8") if isinstance(data, (bytes, bytearray)) else data
    return json.loads(text, parse_int=_parse_int, parse_constant=_reject_constant)
