"""`Retry-After` (contract 7.4): only a 429 is read, only whole decimal seconds count, and 5 minutes is the cap."""

from __future__ import annotations

MAX_RETRY_AFTER = 300.0


def retry_after(status: int, value: str | None) -> float | None:
    """Seconds a 429 asks to wait, capped; `None` for any other answer or a header that is not whole seconds."""
    if status != 429 or value is None:
        return None
    text = value.strip(" \t")
    if not text or not (text.isascii() and text.isdigit()):
        return None
    if len(text) > 6:  # far past the cap; `int()` of thousands of digits would raise
        return MAX_RETRY_AFTER
    return min(float(int(text)), MAX_RETRY_AFTER)
