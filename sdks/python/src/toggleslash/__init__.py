"""The ToggleSlash server SDK for Python: evaluates feature flags locally from a ruleset it keeps current.

    client = toggleslash.create(url="https://flags.example.com", sdk_key="...")
    if client.variation("new-checkout", {"key": "user-1"}, False):
        ...
    client.close()

It never raises at a caller and sends nothing about an end user. See README.md in the repository.
"""

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
