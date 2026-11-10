"""The JSON reader: binary64 numbers, no NaN or Infinity literals, lone surrogates kept."""

from __future__ import annotations

import json
from typing import Any

_TWO_53 = 9007199254740992.0


def _parse_int(text: str) -> int | float:
    # The spec reads every number as binary64. An integer literal below 2**53 is exact as an int;
    # beyond it the value is the nearest double. float() on the text never meets the interpreter's
    # integer-digit limit, which a literal of thousands of digits would hit in int().
    if text == "-0":
        return -0.0
    if len(text) <= 15:
        return int(text)
    value = float(text)
    if abs(value) < _TWO_53:
        return int(text)
    return value


def _reject_constant(name: str) -> Any:
    raise ValueError(f"not JSON: {name}")


def loads(data: str | bytes | bytearray) -> Any:
    """Parse JSON text. Raises ``ValueError`` (or ``RecursionError``) on anything that is not JSON.

    Bytes are UTF-8 and a leading byte order mark is skipped. A duplicate key keeps the last value.
    ``1e999`` is infinity and a lone surrogate escape stays a lone surrogate in the ``str``.
    """
    if isinstance(data, (bytes, bytearray)):
        data = bytes(data).decode("utf-8-sig")
    elif data.startswith("﻿"):
        data = data[1:]
    return json.loads(data, parse_int=_parse_int, parse_constant=_reject_constant)
