"""JSON in and out, to the numbers and strings docs/evaluation.md describes.

Numbers are binary64: an integer beyond 2**53 reads as the nearest float, `1e999` is infinity and `-0` is
zero. Python's reader is told so through its hooks; the rest of its behaviour is kept: a lone surrogate
escape is a lone surrogate, and a duplicate key keeps the last value.
"""

from __future__ import annotations

import json
import math
from typing import Any

_MAX_SAFE = 2**53


class JSONError(ValueError):
    """The text is not JSON this SDK reads."""


def _parse_int(text: str) -> int | float:
    # float() first: int() refuses more than 4300 digits, float() does not and rounds like binary64.
    value = float(text)
    if abs(value) <= _MAX_SAFE:
        return int(value)
    return value


def _reject(name: str) -> Any:
    raise JSONError(f"{name} is not JSON")


def loads(data: str | bytes | bytearray) -> Any:
    """Parses JSON text or UTF-8 bytes (invalid bytes become U+FFFD, a leading BOM is skipped)."""
    text = bytes(data).decode("utf-8", "replace") if isinstance(data, (bytes, bytearray)) else data
    if text.startswith("﻿"):
        text = text[1:]
    try:
        return json.loads(text, parse_float=float, parse_int=_parse_int, parse_constant=_reject)
    except RecursionError as error:
        raise JSONError("nested too deeply") from error
    except ValueError as error:
        raise JSONError(str(error)) from error


def _rebuild(value: Any, repair: bool) -> Any:
    """A deep copy of dicts, lists and tuples without recursion; `repair` turns a non-finite float into None."""

    def shell(item: Any) -> Any:
        if isinstance(item, dict):
            return {}
        if isinstance(item, (list, tuple)):
            return []
        if repair and isinstance(item, float) and not math.isfinite(item):
            return None
        return item

    result = shell(value)
    pending = [(value, result)] if isinstance(value, (dict, list, tuple)) else []
    while pending:
        source, target = pending.pop()
        items = source.items() if isinstance(source, dict) else enumerate(source)
        for key, item in items:
            made = shell(item)
            if isinstance(target, dict):
                target[key] = made
            else:
                target.append(made)
            if isinstance(item, (dict, list, tuple)):
                pending.append((item, made))
    return result


def finite(value: Any) -> Any:
    """A copy of `value` with every non-finite float replaced by None (JSON has no such number)."""
    return _rebuild(value, True)


def copy(value: Any) -> Any:
    """A deep copy of JSON data (dicts, lists, scalars), without recursion."""
    return _rebuild(value, False)


def dumps(value: Any) -> str:
    """Compact ASCII JSON; a non-finite number is written as null."""
    try:
        return json.dumps(value, ensure_ascii=True, separators=(",", ":"), allow_nan=False)
    except ValueError:
        return json.dumps(finite(value), ensure_ascii=True, separators=(",", ":"), allow_nan=False)
