"""Bucketing: the first four bytes of SHA-256 over `<key>.<salt>.<contextKey>`, big-endian, mod 100000."""

from __future__ import annotations

import hashlib

from ._text import utf8

BUCKET_COUNT = 100_000


def uint32(key: str, salt: str, context_key: str) -> int:
    digest = hashlib.sha256(utf8(f"{key}.{salt}.{context_key}")).digest()
    return int.from_bytes(digest[:4], "big")


def bucket(key: str, salt: str, context_key: str) -> int:
    return uint32(key, salt, context_key) % BUCKET_COUNT
