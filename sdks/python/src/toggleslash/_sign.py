"""Secure-mode signing (contract 14): `sign_context`, which an application's server calls to sign a context.

The bytes are docs/sdk-browser.md's "What is signed": the normalised context's kinds in byte order, each kind and
then its key as a netstring, keys only. The signature is ECDSA P-256 with SHA-256, the 64-byte IEEE P1363
`r || s`, base64url with no padding. The standard library cannot read a PKCS#8 EC key or sign with ECDSA, so this
needs `cryptography`, the optional extra `toggleslash[signing]`. It is imported inside `sign_context` only, so the
client and the evaluator never load it.
"""

from __future__ import annotations

import base64
from typing import Any

from ._context import normalise_context
from ._text import hashable


def _netstring(text: str) -> bytes:
    # A lone surrogate is written as U+FFFD, as the other SDKs and the server write it.
    data = hashable(text)
    return str(len(data)).encode("ascii") + b":" + data + b","


def context_message(context: Any) -> bytes:
    """The bytes a signature covers. Raises ValueError when the context is not valid (docs/evaluation.md)."""
    contexts = normalise_context(context)
    if contexts is None:
        raise ValueError("sign_context: the context is not valid; see docs/evaluation.md")
    # A kind is ASCII, so sorting the str sorts the bytes.
    return b"".join(_netstring(kind) + _netstring(contexts[kind].key) for kind in sorted(contexts))


def sign_context(private_key_pem: str, context: Any) -> str:
    """Signs `context` for secure mode with an environment's signing key, the PKCS#8 PEM that
    `POST .../environments/{env}/signing-key` answered once. Returns 86 base64url characters, which go to the
    browser with the context.

    Unlike the client this raises: ValueError for a context that is not valid or a key that is not an ECDSA
    P-256 private key, and ImportError when `cryptography` (`pip install toggleslash[signing]`) is missing.
    Both are mistakes at the call site, not conditions at run time. It needs no client.
    """
    message = context_message(context)
    try:
        from cryptography.exceptions import UnsupportedAlgorithm
        from cryptography.hazmat.primitives import hashes, serialization
        from cryptography.hazmat.primitives.asymmetric import ec
        from cryptography.hazmat.primitives.asymmetric.utils import decode_dss_signature
    except ImportError as error:
        raise ImportError(
            "sign_context needs the cryptography package: pip install toggleslash[signing]"
        ) from error
    if not isinstance(private_key_pem, str):
        raise ValueError("sign_context: the key is not an ECDSA P-256 private key")
    try:
        key = serialization.load_pem_private_key(private_key_pem.encode("utf-8"), password=None)
    except (ValueError, TypeError, UnsupportedAlgorithm) as error:
        raise ValueError("sign_context: the key is not an ECDSA P-256 private key") from error
    if not isinstance(key, ec.EllipticCurvePrivateKey) or not isinstance(key.curve, ec.SECP256R1):
        raise ValueError("sign_context: the key is not an ECDSA P-256 private key")
    r, s = decode_dss_signature(key.sign(message, ec.ECDSA(hashes.SHA256())))
    raw = r.to_bytes(32, "big") + s.to_bytes(32, "big")
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")
