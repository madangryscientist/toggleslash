"""Instants, to the "Dates" section of docs/evaluation.md.

A hand-written walk over the string: no `datetime.fromisoformat` and no regular expression. The result is
Unix milliseconds, or `None` when the text is not an instant.
"""

from __future__ import annotations

import math

_ZERO = 0x30


def _digits(text: str, start: int, length: int) -> int | None:
    if start + length > len(text):
        return None
    value = 0
    for char in text[start : start + length]:
        code = ord(char)
        if not _ZERO <= code <= _ZERO + 9:
            return None
        value = value * 10 + (code - _ZERO)
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
    year = _digits(text, 0, 4)
    month = _digits(text, 5, 2)
    day = _digits(text, 8, 2)
    if year is None or month is None or day is None:
        return None
    if year < 1 or month < 1 or month > 12:
        return None
    if day < 1 or day > _days_in_month(year, month):
        return None
    return _days_from_civil(year, month, day)


def _parse_time(text: str) -> int | None:
    hour = _digits(text, 0, 2)
    minute = _digits(text, 3, 2)
    second = _digits(text, 6, 2)
    if hour is None or minute is None or second is None:
        return None
    if hour > 23 or minute > 59 or second > 59:
        return None
    return (hour * 60 + minute) * 60 + second


def _parse_fraction(text: str, start: int) -> tuple[int, int] | None:
    end = start + 1
    while end < len(text) and ord(text[end]) in range(_ZERO, _ZERO + 10):
        end += 1
    if end == start + 1:
        return None
    kept = min(end - start - 1, 3)
    scale = (1, 100, 10, 1)[kept]
    return (_digits(text, start + 1, kept) or 0) * scale, end


def _parse_offset(text: str) -> int | None:
    if text == "Z":
        return 0
    sign = {"+": 1, "-": -1}.get(text[:1])
    if sign is None or len(text) != 6 or text[3] != ":":
        return None
    hour = _digits(text, 1, 2)
    minute = _digits(text, 4, 2)
    if hour is None or minute is None:
        return None
    return None if hour > 23 or minute > 59 else sign * (hour * 60 + minute)


def _has_shape(text: str) -> bool:
    return (
        len(text) >= 20
        and text[4] == "-"
        and text[7] == "-"
        and text[10] == "T"
        and text[13] == ":"
        and text[16] == ":"
    )


def _parse_instant(text: str) -> int | None:
    if not _has_shape(text):
        return None
    days = _parse_date(text)
    seconds = _parse_time(text[11:])
    if days is None or seconds is None:
        return None
    fraction = _parse_fraction(text, 19) if text[19] == "." else (0, 19)
    if fraction is None:
        return None
    millis, end = fraction
    offset = _parse_offset(text[end:])
    if offset is None:
        return None
    return days * 86_400_000 + seconds * 1000 + millis - offset * 60_000


def to_instant(value: object) -> float | int | None:
    """A finite number is Unix milliseconds, a string is parsed, anything else is not an instant."""
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        try:
            number = float(value)
        except OverflowError:
            return None
        return number if math.isfinite(number) else None
    return _parse_instant(value) if isinstance(value, str) else None
