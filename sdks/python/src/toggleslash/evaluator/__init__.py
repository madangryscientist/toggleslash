"""The flag evaluator: ``docs/evaluation.md`` in Python. No network code, standard library only."""

from .evaluate import evaluate
from .result import ErrorCode, Reason, ReasonKind, Result
from .ruleset import Ruleset, create_ruleset

__all__ = [
    "ErrorCode",
    "Reason",
    "ReasonKind",
    "Result",
    "Ruleset",
    "create_ruleset",
    "evaluate",
]
