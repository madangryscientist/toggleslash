"""Bucketing: the first four bytes of SHA-256 over ``<key>.<salt>.<contextKey>``, modulo 100000."""

from __future__ import annotations

import hashlib


def utf8(text: str) -> bytes:
    """UTF-8 bytes of ``text``, with a lone surrogate hashed as U+FFFD."""
    try:
        return text.encode("utf-8")
    except UnicodeEncodeError:
        cleaned = "".join("�" if "\ud800" <= ch <= "\udfff" else ch for ch in text)
        return cleaned.encode("utf-8")


def uint32(key: str, salt: str, context_key: str) -> int:
    digest = hashlib.sha256(utf8(f"{key}.{salt}.{context_key}")).digest()
    return int.from_bytes(digest[:4], "big")


def bucket(key: str, salt: str, context_key: str) -> int:
    return uint32(key, salt, context_key) % 100000
