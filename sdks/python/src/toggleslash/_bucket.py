"""Where a context lands for a flag or segment: SHA-256 of `<key>.<salt>.<contextKey>`, first 4 bytes."""

from __future__ import annotations

import hashlib

from ._text import hashable

BUCKET_COUNT = 100_000


def bucket_of(key: str, salt: str, context_key: str) -> int:
    digest = hashlib.sha256(hashable(f"{key}.{salt}.{context_key}")).digest()
    return int.from_bytes(digest[:4], "big") % BUCKET_COUNT


def uint32_of(key: str, salt: str, context_key: str) -> int:
    digest = hashlib.sha256(hashable(f"{key}.{salt}.{context_key}")).digest()
    return int.from_bytes(digest[:4], "big")
