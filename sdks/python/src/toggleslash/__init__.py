"""The ToggleSlash Python server SDK: ``toggleslash.create(url=..., sdk_key=...)``.

It keeps the ruleset the server serves, evaluates flags locally and sends usage counts. It never raises
at a caller. See ``README.md`` and ``docs/sdk-contract.md``.
"""

from __future__ import annotations

from ._client import Client, create
from ._types import (
    Detail,
    ErrorCode,
    EvaluationEvent,
    Hooks,
    LastError,
    Reason,
    ReasonKind,
    Status,
    StreamState,
    ToggleSlashError,
)

__all__ = [
    "Client",
    "Detail",
    "ErrorCode",
    "EvaluationEvent",
    "Hooks",
    "LastError",
    "Reason",
    "ReasonKind",
    "Status",
    "StreamState",
    "ToggleSlashError",
    "create",
]
