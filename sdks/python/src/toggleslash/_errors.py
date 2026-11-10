"""The failures the SDK reports to `on_error`, and the `Status.last_error` spelling of each."""

from __future__ import annotations

from ._http import HttpError
from ._types import LastError


class SdkError(Exception):
    """An error the SDK built itself. Its message never holds a context, a key or a flag value."""


class HttpStatusError(SdkError):
    def __init__(self, status: int, message: str) -> None:
        super().__init__(message)
        self.status = status


class BadRulesetError(SdkError):
    pass


def error_code(error: BaseException) -> str:
    if isinstance(error, HttpStatusError):
        return LastError.http(error.status)
    if isinstance(error, HttpError):
        return LastError.TIMEOUT if error.code == "timeout" else LastError.NETWORK
    if isinstance(error, BadRulesetError):
        return LastError.BAD_RULESET
    return LastError.NETWORK
