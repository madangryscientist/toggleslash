"""Contexts: validation and the map from kind to ``(key, attributes)`` (evaluation.md, "Context")."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

_KIND_CHARS = frozenset("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-")

Normalised = dict[str, tuple[str, "Mapping[str, Any] | None"]]


def is_kind(value: Any) -> bool:
    """A kind is one or more ASCII letters, digits, ``.``, ``_`` or ``-``, and not ``kind`` or ``multi``.

    Checked by walking the characters: the evaluator uses no regular expression of the platform.
    """
    if not isinstance(value, str) or value == "" or value in ("kind", "multi"):
        return False
    return all(ch in _KIND_CHARS for ch in value)


def _entry(raw: Any) -> tuple[str, Mapping[str, Any] | None] | None:
    if not isinstance(raw, Mapping):
        return None
    key = raw.get("key")
    if not isinstance(key, str) or key == "":
        return None
    attributes = raw.get("attributes")
    if attributes is not None and not isinstance(attributes, Mapping):
        return None
    return key, attributes


def normalise(context: Any) -> Normalised | None:
    """The context as ``{kind: (key, attributes)}``, or ``None`` when it is not valid."""
    if not isinstance(context, Mapping):
        return None
    kind = context.get("kind")
    if kind is None:
        kind = "user"
    elif not isinstance(kind, str):
        return None
    if kind == "multi":
        if context.get("key") is not None or context.get("attributes") is not None:
            return None
        contexts = context.get("contexts")
        if not isinstance(contexts, Mapping) or len(contexts) == 0:
            return None
        out: Normalised = {}
        for name, raw in contexts.items():
            if not is_kind(name) or not isinstance(raw, Mapping) or raw.get("kind") is not None:
                return None
            entry = _entry(raw)
            if entry is None:
                return None
            out[name] = entry
        return out
    if not is_kind(kind):
        return None
    entry = _entry(context)
    if entry is None:
        return None
    return {kind: entry}
