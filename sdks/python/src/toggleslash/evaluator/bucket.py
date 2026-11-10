"""Bucketing: SHA-256 of ``<key>.<salt>.<contextKey>``, the first four bytes, modulo 100000."""

from __future__ import annotations

import hashlib

from .text import utf8

BUCKET_COUNT = 100_000


def bucket(key: str, salt: str, context_key: str) -> int:
    digest = hashlib.sha256(utf8(f"{key}.{salt}.{context_key}")).digest()
    return int.from_bytes(digest[:4], "big") % BUCKET_COUNT
