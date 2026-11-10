"""Contexts: validation, normalising and the bucket of a kind's key."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any, NamedTuple

from .bucket import bucket
from .validate import is_kind

DEFAULT_KIND = "user"


class KindContext(NamedTuple):
    """The key and attributes of one kind, after normalising."""

    key: str
    attributes: Mapping[str, Any] | None


Contexts = dict[str, KindContext]


def _read_entry(value: Mapping[str, Any]) -> KindContext | None:
    key = value.get("key")
    attributes = value.get("attributes")
    if not isinstance(key, str) or key == "":
        return None
    if attributes is None:
        return KindContext(key, None)
    return KindContext(key, attributes) if isinstance(attributes, Mapping) else None


def _read_multi(value: Mapping[str, Any]) -> Contexts | None:
    contexts = value.get("contexts")
    if value.get("key") is not None or value.get("attributes") is not None:
        return None
    if not isinstance(contexts, Mapping):
        return None
    normalised: Contexts = {}
    for kind, raw in contexts.items():
        if not is_kind(kind) or not isinstance(raw, Mapping) or raw.get("kind") is not None:
            return None
        entry = _read_entry(raw)
        if entry is None:
            return None
        normalised[kind] = entry
    return normalised or None


def normalise_context(value: object) -> Contexts | None:
    """The context as a map from kind to key and attributes, or None when it is not valid."""
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


def kind_bucket(key: str, salt: str, kind: str | None, contexts: Contexts) -> int:
    """The bucket for the key of ``kind`` (absent is ``user``), or 0 without that kind."""
    chosen = DEFAULT_KIND if kind is None else kind
    context = contexts.get(chosen)
    if context is None:
        return 0
    return bucket(key, salt, context.key if chosen == DEFAULT_KIND else f"{chosen}:{context.key}")
