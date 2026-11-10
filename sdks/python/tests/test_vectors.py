"""Every shared evaluation vector and every bucket pin (docs/evaluation.md, "The vectors")."""

from __future__ import annotations

import math
import unittest
from pathlib import Path
from typing import Any

from toggleslash import _json
from toggleslash._bucket import bucket, uint32
from toggleslash._evaluate import evaluate, load_ruleset

FIXTURES = Path(__file__).resolve().parents[3] / "fixtures" / "evaluation"

counts = {"files": 0, "vectors": 0, "pins": 0}


def same(a: Any, b: Any) -> bool:
    """Deep equality that tells ``True`` from ``1`` and compares numbers as binary64."""
    if isinstance(a, bool) or isinstance(b, bool):
        return isinstance(a, bool) and isinstance(b, bool) and a == b
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return a == b or (math.isnan(a) and math.isnan(b))
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(same(a[k], b[k]) for k in a)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(same(x, y) for x, y in zip(a, b, strict=True))
    return type(a) is type(b) and a == b


def reason_dict(reason: Any) -> dict[str, Any]:
    out: dict[str, Any] = {"kind": str(reason.kind)}
    if reason.code is not None:
        out["code"] = str(reason.code)
    if reason.rule_index is not None:
        out["ruleIndex"] = reason.rule_index
    if reason.flag_key is not None:
        out["flagKey"] = reason.flag_key
    return out


def load(path: Path) -> Any:
    return _json.loads(path.read_bytes())


class VectorTests(unittest.TestCase):
    def test_vectors(self) -> None:
        paths = sorted(p for p in FIXTURES.glob("*.json") if p.name != "buckets.json")
        self.assertTrue(paths, "no vector files found")
        for path in paths:
            counts["files"] += 1
            vectors = load(path)["vectors"]
            self.assertTrue(vectors, f"no vectors in {path.name}")
            for vector in vectors:
                with self.subTest(f"{path.name}: {vector['name']}"):
                    counts["vectors"] += 1
                    ruleset = load_ruleset({"flags": vector["flags"], "segments": vector["segments"]})
                    value, index, reason = evaluate(ruleset, vector["flagKey"], vector["context"])
                    expected = vector["expected"]
                    self.assertTrue(same(value, expected["value"]), f"value {value!r}")
                    self.assertEqual(index, expected["variationIndex"])
                    self.assertTrue(same(reason_dict(reason), expected["reason"]), f"reason {reason!r}")

    def test_pins(self) -> None:
        pins = load(FIXTURES / "buckets.json")["buckets"]
        self.assertTrue(pins, "no pins found")
        for pin in pins:
            with self.subTest(f"{pin['flagKey']}.{pin['salt']}.{pin['contextKey']}"):
                counts["pins"] += 1
                got = uint32(pin["flagKey"], pin["salt"], pin["contextKey"])
                self.assertEqual(got, pin["uint32"])
                self.assertEqual(bucket(pin["flagKey"], pin["salt"], pin["contextKey"]), pin["bucket"])


def tearDownModule() -> None:
    print(f"vectors: {counts['vectors']} pins: {counts['pins']}")


if __name__ == "__main__":
    unittest.main()
