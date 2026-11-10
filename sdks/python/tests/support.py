"""Helpers the tests share."""

from __future__ import annotations

import json
import math
import os
from pathlib import Path
from typing import Any

from toggleslash import _json

ROOT = Path(__file__).resolve().parents[3]
FIXTURES = ROOT / "fixtures"


def load(path: Path) -> Any:
    """A fixture, through the parser the client uses."""
    return _json.parse(path.read_bytes())


def json_equal(left: Any, right: Any) -> bool:
    """Deep equality by the spec: numbers by IEEE-754 (so -0 equals 0), and a bool is not a number."""
    if isinstance(left, bool) or isinstance(right, bool):
        return isinstance(left, bool) and isinstance(right, bool) and left == right
    if type(left) in (int, float) and type(right) in (int, float):
        if isinstance(left, float) and math.isnan(left):
            return isinstance(right, float) and math.isnan(right)
        return float(left) == float(right)
    if isinstance(left, str) and isinstance(right, str):
        return left == right
    if isinstance(left, list) and isinstance(right, list):
        return len(left) == len(right) and all(json_equal(a, b) for a, b in zip(left, right))
    if isinstance(left, dict) and isinstance(right, dict):
        return left.keys() == right.keys() and all(json_equal(left[k], right[k]) for k in left)
    return left is None and right is None


def dumps(value: Any) -> str:
    return json.dumps(value, ensure_ascii=True)


def under_load() -> bool:
    return os.environ.get("TOGGLESLASH_TEST_LOAD") == "1"
