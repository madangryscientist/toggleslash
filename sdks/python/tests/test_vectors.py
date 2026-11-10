"""Runs every shared evaluation vector and every bucket pin, through the client's own JSON parser."""

from __future__ import annotations

import unittest

from support import FIXTURES, deep_equal
from toggleslash import _json
from toggleslash._bucket import bucket, uint32
from toggleslash._evaluate import evaluate
from toggleslash._ruleset import create_ruleset

COUNTS = {"vectors": 0, "pins": 0}


def _read(name: str):
    return _json.loads((FIXTURES / name).read_bytes())


class VectorTests(unittest.TestCase):
    def test_every_vector(self) -> None:
        files = sorted(p.name for p in FIXTURES.glob("*.json") if p.name != "buckets.json")
        self.assertGreater(len(files), 0, "no vector files found")
        total = 0
        for name in files:
            vectors = _read(name)["vectors"]
            self.assertGreater(len(vectors), 0, f"{name} has no vectors")
            for vector in vectors:
                total += 1
                with self.subTest(file=name, vector=vector["name"]):
                    ruleset = create_ruleset({"flags": vector["flags"], "segments": vector["segments"]})
                    result = evaluate(ruleset, vector["flagKey"], vector["context"])
                    actual = {
                        "value": result.value,
                        "variationIndex": result.variation_index,
                        "reason": result.reason.to_json(),
                    }
                    self.assertTrue(
                        deep_equal(actual, vector["expected"]),
                        f"{actual!r} != {vector['expected']!r}",
                    )
        COUNTS["vectors"] = total

    def test_every_pin(self) -> None:
        pins = _read("buckets.json")["buckets"]
        self.assertGreater(len(pins), 0, "no bucket pins found")
        for pin in pins:
            with self.subTest(pin=pin):
                self.assertEqual(uint32(pin["flagKey"], pin["salt"], pin["contextKey"]), pin["uint32"])
                self.assertEqual(bucket(pin["flagKey"], pin["salt"], pin["contextKey"]), pin["bucket"])
        COUNTS["pins"] = len(pins)


def tearDownModule() -> None:
    print(f"vectors: {COUNTS['vectors']} pins: {COUNTS['pins']}", flush=True)
