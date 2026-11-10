"""JSON as the spec reads it: numbers are binary64, and a context keeps what the standard library gives.

The standard parser is used with three hooks. An integer literal beyond plus or minus 2**53 becomes a float
(so `9007199254740993` is `9007199254740992.0`, and the interpreter's integer-digit limit is never reached, since
`float` is given the digits), `1e999` is infinity, and `NaN` and `Infinity` are errors.
"""

from __future__ import annotations

import json
import math
from typing import Any

_SAFE = 2**53


def _parse_int(text: str) -> int | float:
    if len(text) <= 15:
        return int(text)
    # A long literal never reaches `int()`, whose digit limit would raise on thousands of digits.
    return float(text)


def _parse_float(text: str) -> float:
    return float(text)


def _no_constant(name: str) -> Any:
    raise ValueError(f"{name} is not JSON")


def _fix_int(value: int | float) -> int | float:
    if isinstance(value, int) and not -_SAFE <= value <= _SAFE:
        return float(value)
    return value


def _parse_int_checked(text: str) -> int | float:
    return _fix_int(_parse_int(text))


_DECODER = json.JSONDecoder(
    parse_int=_parse_int_checked,
    parse_float=_parse_float,
    parse_constant=_no_constant,
)


def loads(data: bytes | str) -> Any:
    """Parses JSON; raises `ValueError` (or `RecursionError`) on anything that is not valid."""
    if isinstance(data, (bytes, bytearray)):
        data = bytes(data).decode("utf-8")
    if data.startswith("﻿"):
        data = data[1:]
    return _DECODER.decode(data)


def dumps(value: Any) -> str:
    """The JSON text of a value. Infinity is written as `1e999`, which reads back as infinity."""
    out: list[str] = []
    _write(value, out)
    return "".join(out)


def _write(value: Any, out: list[str]) -> None:
    if value is None:
        out.append("null")
    elif value is True:
        out.append("true")
    elif value is False:
        out.append("false")
    elif isinstance(value, str):
        out.append(json.dumps(value, ensure_ascii=True))
    elif isinstance(value, int):
        out.append(str(int(value)))
    elif isinstance(value, float):
        if math.isnan(value):
            raise ValueError("NaN is not JSON")
        out.append(("1e999" if value > 0 else "-1e999") if math.isinf(value) else repr(value))
    elif isinstance(value, (list, tuple)):
        out.append("[")
        for i, item in enumerate(value):
            if i:
                out.append(",")
            _write(item, out)
        out.append("]")
    elif isinstance(value, dict):
        out.append("{")
        for i, (key, item) in enumerate(value.items()):
            if i:
                out.append(",")
            out.append(json.dumps(str(key), ensure_ascii=True))
            out.append(":")
            _write(item, out)
        out.append("}")
    else:
        raise TypeError(f"{type(value).__name__} is not JSON")


def is_number(value: object) -> bool:
    """A JSON number: an `int` or a `float`, never a `bool`."""
    return type(value) is int or type(value) is float


def is_finite(value: object) -> bool:
    if type(value) is int:
        return True
    return type(value) is float and math.isfinite(value)


def to_float(value: int | float) -> float:
    try:
        return float(value)
    except OverflowError:
        return math.inf if value > 0 else -math.inf
