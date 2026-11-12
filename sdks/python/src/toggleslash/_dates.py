"""Instants, to the "Dates" section of docs/evaluation.md.

A hand-written walk over the string: no `datetime`, no regular expression. The result is Unix milliseconds
as a float, or None when the text is not an instant.
"""

from __future__ import annotations

import math
from typing import Any


def _is_digit(char: str) -> bool:
    return "0" <= char <= "9"


def _digits(text: str, start: int, length: int) -> int | None:
    value = 0
    for i in range(start, start + length):
        if i >= len(text) or not _is_digit(text[i]):
            return None
        value = value * 10 + (ord(text[i]) - 0x30)
    return value


def _is_leap_year(year: int) -> bool:
    return year % 4 == 0 and (year % 100 != 0 or year % 400 == 0)


def _days_in_month(year: int, month: int) -> int:
    if month == 2:
        return 29 if _is_leap_year(year) else 28
    return 30 if month in (4, 6, 9, 11) else 31


def _days_from_civil(year: int, month: int, day: int) -> int:
    y = year - 1 if month <= 2 else year
    era = y // 400
    year_of_era = y - era * 400
    month_index = month - 3 if month > 2 else month + 9
    day_of_year = (153 * month_index + 2) // 5 + day - 1
    day_of_era = year_of_era * 365 + year_of_era // 4 - year_of_era // 100 + day_of_year
    return era * 146097 + day_of_era - 719468


def _parse_date(text: str) -> int | None:
    year, month, day = _digits(text, 0, 4), _digits(text, 5, 2), _digits(text, 8, 2)
    if year is None or month is None or day is None:
        return None
    if year < 1 or month < 1 or month > 12:
        return None
    if day < 1 or day > _days_in_month(year, month):
        return None
    return _days_from_civil(year, month, day)


def _parse_time(text: str) -> int | None:
    hour, minute, second = _digits(text, 0, 2), _digits(text, 3, 2), _digits(text, 6, 2)
    if hour is None or minute is None or second is None:
        return None
    if hour > 23 or minute > 59 or second > 59:
        return None
    return (hour * 60 + minute) * 60 + second


def _parse_fraction(text: str, start: int) -> tuple[int, int] | None:
    end = start + 1
    while end < len(text) and _is_digit(text[end]):
        end += 1
    if end == start + 1:
        return None
    kept = min(end - start - 1, 3)
    scale = (1, 100, 10, 1)[kept]
    return (_digits(text, start + 1, kept) or 0) * scale, end


def _parse_offset(text: str) -> int | None:
    if text == "Z":
        return 0
    if text[:1] not in ("+", "-") or len(text) != 6 or text[3] != ":":
        return None
    sign = 1 if text[0] == "+" else -1
    hour, minute = _digits(text, 1, 2), _digits(text, 4, 2)
    if hour is None or minute is None or hour > 23 or minute > 59:
        return None
    return sign * (hour * 60 + minute)


def _has_shape(text: str) -> bool:
    return (
        len(text) >= 20
        and text[4] == "-"
        and text[7] == "-"
        and text[10] == "T"
        and text[13] == ":"
        and text[16] == ":"
    )


def _parse_instant(text: str) -> float | None:
    # Every character an instant holds is ASCII, so any other character settles it.
    if not text.isascii() or not _has_shape(text):
        return None
    days = _parse_date(text)
    seconds = _parse_time(text[11:])
    if days is None or seconds is None:
        return None
    fraction = _parse_fraction(text, 19) if text[19] == "." else (0, 19)
    if fraction is None:
        return None
    offset = _parse_offset(text[fraction[1] :])
    if offset is None:
        return None
    return float(days * 86_400_000 + seconds * 1000 + fraction[0] - offset * 60_000)


def to_instant(value: Any) -> float | None:
    """A finite number is Unix milliseconds, a string is parsed, anything else is not an instant."""
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        number = float(value)
        return number if math.isfinite(number) else None
    return _parse_instant(value) if isinstance(value, str) else None
