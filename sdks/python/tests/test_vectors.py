"""Every shared evaluation vector and bucket pin, read from ``fixtures/evaluation`` at test time."""

from __future__ import annotations

import hashlib
import unittest

from support import FIXTURES, json_equal, load
from toggleslash.evaluator import create_ruleset, evaluate
from toggleslash.evaluator.bucket import bucket
from toggleslash.evaluator.text import utf8

DIRECTORY = FIXTURES / "evaluation"
_counts = {"vectors": 0, "pins": 0}


class EvaluationVectors(unittest.TestCase):
    def test_every_vector(self) -> None:
        files = sorted(path for path in DIRECTORY.glob("*.json") if path.name != "buckets.json")
        self.assertTrue(files, "no vector files were read")
        for path in files:
            vectors = load(path)["vectors"]
            self.assertTrue(vectors, f"{path.name} holds no vectors")
            for vector in vectors:
                with self.subTest(file=path.name, vector=vector["name"]):
                    ruleset = create_ruleset({"flags": vector["flags"], "segments": vector["segments"]})
                    got = evaluate(ruleset, vector["flagKey"], vector["context"]).to_json()
                    self.assertTrue(
                        json_equal(got, vector["expected"]),
                        f"{path.name}: {vector['name']}: got {got!r}, expected {vector['expected']!r}",
                    )
                    _counts["vectors"] += 1
        self.assertGreater(_counts["vectors"], 0)

    def test_every_bucket_pin(self) -> None:
        pins = load(DIRECTORY / "buckets.json")["buckets"]
        self.assertTrue(pins, "no bucket pins were read")
        for pin in pins:
            with self.subTest(pin=pin):
                text = f"{pin['flagKey']}.{pin['salt']}.{pin['contextKey']}"
                value = int.from_bytes(hashlib.sha256(utf8(text)).digest()[:4], "big")
                self.assertEqual(value, pin["uint32"], text)
                self.assertEqual(bucket(pin["flagKey"], pin["salt"], pin["contextKey"]), pin["bucket"])
                _counts["pins"] += 1


def tearDownModule() -> None:
    print(f"vectors: {_counts['vectors']} pins: {_counts['pins']}", flush=True)


if __name__ == "__main__":
    unittest.main()
