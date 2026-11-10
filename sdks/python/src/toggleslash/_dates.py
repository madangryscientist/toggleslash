"""Instants: a hand-written walk over the characters (evaluation.md, "Dates")."""

from __future__ import annotations

import math
from functools import lru_cache
from typing import Any

_DIGITS = frozenset("0123456789")


def _digits(text: str) -> bool:
    return text != "" and all(ch in _DIGITS for ch in text)


def _is_leap(year: int) -> bool:
    return year % 4 == 0 and (year % 100 != 0 or year % 400 == 0)


def _days_in_month(year: int, month: int) -> int:
    if month == 2:
        return 29 if _is_leap(year) else 28
    return 30 if month in (4, 6, 9, 11) else 31


def _days_from_civil(year: int, month: int, day: int) -> int:
    y = year - 1 if month <= 2 else year
    era = y // 400
    yoe = y - era * 400
    doy = (153 * (month + (-3 if month > 2 else 9)) + 2) // 5 + day - 1
    doe = yoe * 365 + yoe // 4 - yoe // 100 + doy
    return era * 146097 + doe - 719468


@lru_cache(maxsize=2048)
def parse_instant_string(text: str) -> float | None:
    """Unix milliseconds of a strict ``YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)`` string, or ``None``."""
    if len(text) < 20:
        return None
    if text[4] != "-" or text[7] != "-" or text[10] != "T" or text[13] != ":" or text[16] != ":":
        return None
    fields = (text[0:4], text[5:7], text[8:10], text[11:13], text[14:16], text[17:19])
    if not all(_digits(f) for f in fields):
        return None
    year, month, day, hour, minute, second = (int(f) for f in fields)
    if not (1 <= year <= 9999 and 1 <= month <= 12):
        return None
    if not (1 <= day <= _days_in_month(year, month)):
        return None
    if hour > 23 or minute > 59 or second > 59:
        return None
    i = 19
    fraction_ms = 0
    if text[i] == ".":
        j = i + 1
        while j < len(text) and text[j] in _DIGITS:
            j += 1
        digits = text[i + 1 : j]
        if digits == "":
            return None
        fraction_ms = int(digits[:3].ljust(3, "0"))
        i = j
    rest = text[i:]
    if rest == "Z":
        offset_minutes = 0
    elif len(rest) == 6 and rest[0] in "+-" and rest[3] == ":":
        oh, om = rest[1:3], rest[4:6]
        if not (_digits(oh) and _digits(om)):
            return None
        hours, minutes = int(oh), int(om)
        if hours > 23 or minutes > 59:
            return None
        offset_minutes = (hours * 60 + minutes) * (1 if rest[0] == "+" else -1)
    else:
        return None
    days = _days_from_civil(year, month, day)
    millis = days * 86400000 + ((hour * 60 + minute) * 60 + second) * 1000 + fraction_ms
    return float(millis - offset_minutes * 60000)


def to_instant(value: Any) -> float | None:
    """Unix milliseconds of a finite number or a valid date string; ``None`` for anything else."""
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        try:
            number = float(value)
        except OverflowError:
            return None
        return number if math.isfinite(number) else None
    if isinstance(value, str):
        return parse_instant_string(value)
    return None
