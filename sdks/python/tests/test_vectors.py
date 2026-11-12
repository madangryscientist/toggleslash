"""Every evaluation vector and bucket pin in fixtures/evaluation, run through the evaluator."""

from __future__ import annotations

import math
import pathlib
import sys
import unittest
from typing import Any

from toggleslash import _json
from toggleslash._bucket import bucket_of, uint32_of
from toggleslash._evaluate import Outcome, evaluate
from toggleslash._ruleset import create_ruleset

FIXTURES = pathlib.Path(__file__).resolve().parent.parent.parent.parent / "fixtures" / "evaluation"

counts = {"vectors": 0, "pins": 0}


def read(path: pathlib.Path) -> Any:
    return _json.loads(path.read_bytes())


def reason_of(outcome: Outcome) -> dict[str, Any]:
    reason: dict[str, Any] = {"kind": outcome.kind}
    if outcome.rule_index is not None:
        reason["ruleIndex"] = outcome.rule_index
    if outcome.flag_key is not None:
        reason["flagKey"] = outcome.flag_key
    if outcome.code is not None:
        reason["code"] = outcome.code
    return reason


def same(a: Any, b: Any) -> bool:
    """Deep equality by the specification: numbers by IEEE-754 (so -0 equals 0), a bool is not a number."""
    if isinstance(a, bool) or isinstance(b, bool):
        return isinstance(a, bool) and isinstance(b, bool) and a == b
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return a == b or (math.isnan(a) and math.isnan(b))
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(same(a[k], b[k]) for k in a)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(same(x, y) for x, y in zip(a, b, strict=True))
    return type(a) is type(b) and a == b


class VectorTests(unittest.TestCase):
    def test_vectors(self) -> None:
        files = sorted(p for p in FIXTURES.glob("*.json") if p.name != "buckets.json")
        self.assertTrue(files, f"no vector files under {FIXTURES}")
        for path in files:
            for vector in read(path)["vectors"]:
                counts["vectors"] += 1
                with self.subTest(file=path.name, vector=vector["name"]):
                    ruleset = create_ruleset(
                        {"flags": vector["flags"], "segments": vector["segments"]}
                    )
                    outcome = evaluate(ruleset, vector["flagKey"], vector["context"])
                    expected = vector["expected"]
                    actual = {
                        "value": outcome.value,
                        "variationIndex": outcome.variation_index,
                        "reason": reason_of(outcome),
                    }
                    self.assertTrue(
                        same(actual, expected),
                        f"{path.name}: {vector['name']}: {actual} != {expected}",
                    )
        self.assertGreater(counts["vectors"], 0, "no vectors were read")

    def test_bucket_pins(self) -> None:
        pins = read(FIXTURES / "buckets.json")["buckets"]
        self.assertTrue(pins, "no bucket pins were read")
        for pin in pins:
            counts["pins"] += 1
            args = (pin["flagKey"], pin["salt"], pin["contextKey"])
            with self.subTest(pin=args):
                self.assertEqual(uint32_of(*args), pin["uint32"])
                self.assertEqual(bucket_of(*args), pin["bucket"])


def tearDownModule() -> None:
    sys.stdout.write(f"vectors: {counts['vectors']} pins: {counts['pins']}\n")
    sys.stdout.flush()
