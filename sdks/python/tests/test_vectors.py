"""Runs every shared evaluation vector (fixtures/evaluation/*.json) through the evaluator.

The fixtures are read from the repository at test time, never copied. `buckets.json` is checked for both
`uint32` and `bucket`; every other file holds vectors with a ruleset, a context and an expected result.
"""

from __future__ import annotations

import math
import unittest
from pathlib import Path
from typing import Any

from toggleslash import _json
from toggleslash._bucket import bucket, uint32
from toggleslash._evaluate import evaluate
from toggleslash._ruleset import create_ruleset
from toggleslash._types import Reason

FIXTURES = Path(__file__).resolve().parents[3] / "fixtures" / "evaluation"


def deep_equal(a: Any, b: Any) -> bool:
    """JSON equality by the spec: numbers by IEEE-754 (-0 equals 0), and a boolean is never a number."""
    if isinstance(a, bool) or isinstance(b, bool):
        return isinstance(a, bool) and isinstance(b, bool) and a == b
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return a == b or (math.isnan(a) and math.isnan(b))
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(deep_equal(a[key], b[key]) for key in a)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(deep_equal(x, y) for x, y in zip(a, b, strict=True))
    return type(a) is type(b) and a == b


def reason_json(reason: Reason) -> dict[str, Any]:
    out: dict[str, Any] = {"kind": str(reason.kind)}
    if reason.rule_index is not None:
        out["ruleIndex"] = reason.rule_index
    if reason.flag_key is not None:
        out["flagKey"] = reason.flag_key
    if reason.code is not None:
        out["code"] = str(reason.code)
    return out


class VectorTests(unittest.TestCase):
    def test_evaluation_vectors(self) -> None:
        files = sorted(path for path in FIXTURES.glob("*.json") if path.name != "buckets.json")
        self.assertTrue(files, f"no vector files in {FIXTURES}")
        ran = 0
        for path in files:
            document = _json.loads(path.read_bytes())
            for vector in document["vectors"]:
                with self.subTest(file=path.name, vector=vector["name"]):
                    ruleset = create_ruleset({"flags": vector.get("flags"), "segments": vector.get("segments")})
                    got = evaluate(ruleset, vector["flagKey"], vector["context"])
                    expected = vector["expected"]
                    actual = {
                        "value": got.value,
                        "variationIndex": got.variation_index,
                        "reason": reason_json(got.reason),
                    }
                    self.assertTrue(deep_equal(actual, expected), f"got {actual!r}, want {expected!r}")
                ran += 1
        self.assertGreater(ran, 0)
        print(f"vectors: {ran}")

    def test_bucket_pins(self) -> None:
        pins = _json.loads((FIXTURES / "buckets.json").read_bytes())["buckets"]
        self.assertTrue(pins)
        for pin in pins:
            with self.subTest(pin=pin):
                self.assertEqual(uint32(pin["flagKey"], pin["salt"], pin["contextKey"]), pin["uint32"])
                self.assertEqual(bucket(pin["flagKey"], pin["salt"], pin["contextKey"]), pin["bucket"])
        print(f"pins: {len(pins)}")


if __name__ == "__main__":
    unittest.main()
