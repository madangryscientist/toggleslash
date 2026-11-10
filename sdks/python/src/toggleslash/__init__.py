"""The ToggleSlash server SDK for Python: feature flags evaluated locally from a ruleset the server keeps
current. Start with `create_client`; see `README.md`."""

from ._client import Client, create_client
from ._types import (
    BAD_RULESET,
    NETWORK,
    TIMEOUT,
    ClientStatus,
    ErrorCode,
    EvaluationEvent,
    Hooks,
    Reason,
    ReasonKind,
    StreamState,
    ToggleSlashError,
    VariationDetail,
)

__all__ = [
    "BAD_RULESET",
    "NETWORK",
    "TIMEOUT",
    "Client",
    "ClientStatus",
    "ErrorCode",
    "EvaluationEvent",
    "Hooks",
    "Reason",
    "ReasonKind",
    "StreamState",
    "ToggleSlashError",
    "VariationDetail",
    "create_client",
]
