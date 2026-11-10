"""The ToggleSlash server SDK for Python: ``create_client``, then ``variation``."""

from .client import Client, create_client
from .types import (
    Detail,
    ErrorCode,
    EvaluationEvent,
    Hooks,
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
    "Reason",
    "ReasonKind",
    "Status",
    "StreamState",
    "ToggleSlashError",
    "create_client",
]
