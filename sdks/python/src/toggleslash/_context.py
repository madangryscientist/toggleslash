"""Contexts, to the "Contexts" section of docs/evaluation.md: one context, or several of different kinds."""

from __future__ import annotations

import math
import numbers
from collections.abc import Mapping
from decimal import Decimal
from typing import Any, NamedTuple

from ._bucket import bucket_of

DEFAULT_KIND = "user"
MAX_SAFE = 2**53

_KIND_CHARS = frozenset("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-")


class KindContext(NamedTuple):
    """The key and attributes of one kind, after normalising."""

    key: str
    attributes: Mapping[str, Any] | None


def is_kind(value: Any) -> bool:
    """`^[A-Za-z0-9._-]+$` over ASCII, and neither `kind` nor `multi`."""
    return (
        isinstance(value, str)
        and value != ""
        and value not in ("kind", "multi")
        and all(char in _KIND_CHARS for char in value)
    )


def _read_entry(value: Mapping[str, Any]) -> KindContext | None:
    key = value.get("key")
    attributes = value.get("attributes")
    if not isinstance(key, str) or key == "":
        return None
    if attributes is None:
        return KindContext(key, None)
    return KindContext(key, attributes) if isinstance(attributes, Mapping) else None


def _read_multi(value: Mapping[str, Any]) -> dict[str, KindContext] | None:
    contexts = value.get("contexts")
    if value.get("key") is not None or value.get("attributes") is not None:
        return None
    if not isinstance(contexts, Mapping):
        return None
    normalised: dict[str, KindContext] = {}
    for kind, raw in contexts.items():
        if not is_kind(kind) or not isinstance(raw, Mapping) or raw.get("kind") is not None:
            return None
        entry = _read_entry(raw)
        if entry is None:
            return None
        normalised[kind] = entry
    return normalised or None


def normalise_context(value: Any) -> dict[str, KindContext] | None:
    """The context as a map from kind to its key and attributes, or None when it is not valid."""
    if not isinstance(value, Mapping):
        return None
    kind = value.get("kind")
    if kind is None:
        kind = DEFAULT_KIND
    if kind == "multi":
        return _read_multi(value)
    if not is_kind(kind):
        return None
    entry = _read_entry(value)
    return None if entry is None else {kind: entry}


def kind_bucket(key: str, salt: str, kind: str | None, contexts: Mapping[str, KindContext]) -> int:
    """The bucket for the key of `kind` (None is `user`); 0 when the context does not have that kind."""
    chosen = DEFAULT_KIND if kind is None else kind
    context = contexts.get(chosen)
    if context is None:
        return 0
    context_key = context.key if chosen == DEFAULT_KIND else f"{chosen}:{context.key}"
    return bucket_of(key, salt, context_key)


def to_float(value: Any) -> float:
    """A number the way binary64 holds it: an integer beyond 2**53 rounds, one too large is infinite."""
    try:
        return float(value)
    except OverflowError:
        return math.inf if value > 0 else -math.inf
    except (ValueError, TypeError):
        return math.nan


def number_of(value: Any) -> float | int | None:
    """`value` as a number the evaluator compares, or None when it is not one (a bool is not)."""
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value if -MAX_SAFE <= value <= MAX_SAFE else to_float(value)
    if isinstance(value, float):
        return value
    if isinstance(value, (numbers.Real, Decimal)):
        return to_float(value)
    return None
