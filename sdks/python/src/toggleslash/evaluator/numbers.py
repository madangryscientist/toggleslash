"""Numbers, as the evaluation spec reads them: IEEE-754 binary64."""

from __future__ import annotations

import math

INFINITY = math.inf


def is_number(value: object) -> bool:
    """A JSON number. ``bool`` is a subclass of ``int`` in Python and is not one."""
    kind = type(value)
    return kind is int or kind is float


def as_float(value: int | float) -> float:
    """The binary64 a JSON number reads as. An integer too large for one is infinity."""
    if type(value) is float:
        return value
    try:
        return float(value)
    except OverflowError:
        return INFINITY if value > 0 else -INFINITY


def is_finite_number(value: object) -> bool:
    return is_number(value) and math.isfinite(as_float(value))  # type: ignore[arg-type]


def is_count(value: object) -> bool:
    """An integer that is 0 or more: a finite number with no fractional part."""
    if type(value) is int:
        return value >= 0
    return type(value) is float and math.isfinite(value) and value.is_integer() and value >= 0
