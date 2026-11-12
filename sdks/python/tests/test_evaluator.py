"""The evaluator's edges the shared vectors do not reach, from Python callers' values."""

from __future__ import annotations

import math
import unittest
from decimal import Decimal
from fractions import Fraction
from typing import Any

from toggleslash import _json
from toggleslash._evaluate import Outcome, evaluate
from toggleslash._pattern import compile_pattern, pattern_matches
from toggleslash._ruleset import create_ruleset


def flag(rules: list[dict[str, Any]], **extra: Any) -> dict[str, Any]:
    base: dict[str, Any] = {
        "key": "f",
        "on": True,
        "variations": [False, True],
        "offVariation": 0,
        "salt": "s",
        "version": 7,
        "targets": [],
        "prerequisites": [],
        "rules": rules,
        "fallthrough": {"variation": 0},
    }
    base.update(extra)
    return base


def rule(op: str, values: list[Any], attribute: str = "a", **extra: Any) -> dict[str, Any]:
    clause = {"op": op, "attribute": attribute, "values": values, **extra}
    return {"clauses": [clause], "variation": 1}


def run(rules: list[dict[str, Any]], context: Any, **extra: Any) -> Outcome:
    ruleset = create_ruleset({"flags": [flag(rules, **extra)], "segments": []})
    return evaluate(ruleset, "f", context)


def served(rules: list[dict[str, Any]], attributes: dict[str, Any]) -> bool:
    return run(rules, {"key": "k", "attributes": attributes}).variation_index == 1


class AttributeValueTests(unittest.TestCase):
    def test_a_bool_is_not_a_number(self) -> None:
        self.assertFalse(served([rule("in", [1])], {"a": True}))
        self.assertFalse(served([rule("in", [True])], {"a": 1}))
        self.assertTrue(served([rule("in", [True])], {"a": True}))
        self.assertFalse(served([rule("gt", [0])], {"a": True}))

    def test_numbers_a_python_caller_hands_over(self) -> None:
        self.assertTrue(served([rule("in", [9007199254740992])], {"a": 9007199254740993}))
        self.assertTrue(served([rule("gt", [1e299])], {"a": 10**300}))
        self.assertFalse(served([rule("gt", [1e300])], {"a": 10**400}))
        self.assertFalse(served([rule("gt", [1])], {"a": math.nan}))
        self.assertFalse(served([rule("lt", [1])], {"a": -math.inf}))
        self.assertTrue(served([rule("in", [0.5])], {"a": Decimal("0.5")}))
        self.assertTrue(served([rule("in", [0.5])], {"a": Fraction(1, 2)}))
        self.assertTrue(served([rule("in", [0])], {"a": -0.0}))

    def test_arrays_are_one_candidate_per_element(self) -> None:
        self.assertTrue(served([rule("in", ["b"])], {"a": ["a", "b"]}))
        self.assertTrue(served([rule("in", ["b"])], {"a": ("a", "b")}))
        self.assertFalse(served([rule("in", ["b"])], {"a": [["b"]]}))
        self.assertFalse(served([rule("in", ["b"])], {"a": []}))

    def test_a_missing_or_null_attribute_fails_whatever_negate_says(self) -> None:
        self.assertFalse(served([rule("in", ["b"], negate=True)], {}))
        self.assertFalse(served([rule("in", ["b"], negate=True)], {"a": None}))
        self.assertTrue(served([rule("in", ["b"], negate=True)], {"a": "c"}))

    def test_an_object_attribute_is_present_and_never_matches(self) -> None:
        self.assertFalse(served([rule("in", ["b"])], {"a": {"x": "b"}}))
        self.assertTrue(served([rule("in", ["b"], negate=True)], {"a": {"x": "b"}}))

    def test_references_read_own_keys_only(self) -> None:
        self.assertTrue(served([rule("in", ["b"], attribute="/a/x")], {"a": {"x": "b"}}))
        self.assertTrue(served([rule("in", ["b"], attribute="/a~1b")], {"a/b": "b"}))
        self.assertFalse(served([rule("in", ["b"], attribute="/a/0")], {"a": ["b"]}))
        self.assertTrue(served([rule("in", ["k"], attribute="/key")], {}))
        self.assertTrue(served([rule("in", ["k"], attribute="key")], {}))
        self.assertFalse(served([rule("in", ["b"], attribute="/a/__class__")], {"a": {}}))

    def test_string_order_is_by_code_unit(self) -> None:
        self.assertTrue(served([rule("lt", ["￿"])], {"a": "\U0001f600"}))
        self.assertTrue(served([rule("in", ["\U0001f600"])], {"a": "😀"}))

    def test_dates_and_versions(self) -> None:
        self.assertTrue(
            served([rule("before", ["2024-01-02T00:00:00Z"])], {"a": "2024-01-01T23:59:59.999Z"})
        )
        self.assertTrue(served([rule("after", [0])], {"a": "1970-01-01T00:00:00.001Z"}))
        self.assertFalse(served([rule("before", [0])], {"a": "2024-13-01T00:00:00Z"}))
        self.assertFalse(served([rule("before", [0])], {"a": "٢024-01-01T00:00:00Z"}))
        self.assertTrue(served([rule("semverGt", ["1.2.3-rc.1"])], {"a": "1.2.3"}))
        self.assertTrue(served([rule("semverEq", ["1.2.0"])], {"a": "1.2"}))
        self.assertFalse(served([rule("semverEq", ["1.2.0"])], {"a": "1.2-rc.1"}))
        self.assertTrue(served([rule("semverLt", ["1" + "0" * 30])], {"a": "9" * 30}))


class ContextTests(unittest.TestCase):
    def test_invalid_contexts(self) -> None:
        for context in (
            None,
            "k",
            [],
            {},
            {"key": ""},
            {"key": 1},
            {"key": "k", "attributes": []},
            {"key": "k", "kind": "multi"},
            {"kind": "multi", "contexts": {}},
            {"kind": "multi", "key": "k", "contexts": {"org": {"key": "o"}}},
            {"kind": "multi", "contexts": {"kind": {"key": "o"}}},
            {"kind": "multi", "contexts": {"org": {"key": "o", "kind": "org"}}},
            {"kind": "bad kind", "key": "k"},
            {"kind": True, "key": "k"},
        ):
            with self.subTest(context=context):
                outcome = run([], context)
                self.assertEqual((outcome.kind, outcome.code), ("ERROR", "INVALID_CONTEXT"))

    def test_a_null_kind_is_user_and_a_multi_context_is_read_per_kind(self) -> None:
        self.assertEqual(run([], {"kind": None, "key": "k"}).kind, "FALLTHROUGH")
        context = {"kind": "multi", "contexts": {"user": {"key": "u"}, "org": {"key": "o"}}}
        self.assertEqual(run([], context).kind, "FALLTHROUGH")


class RulesetTests(unittest.TestCase):
    def test_loading_never_fails(self) -> None:
        for data in (
            None,
            1,
            "x",
            [],
            {"flags": 3},
            {"flags": [1, None, {"key": 4}]},
            {"segments": {}},
        ):
            with self.subTest(data=data):
                outcome = evaluate(create_ruleset(data), "f", {"key": "k"})
                self.assertEqual((outcome.kind, outcome.code), ("ERROR", "FLAG_NOT_FOUND"))

    def test_the_first_duplicate_wins_even_when_malformed(self) -> None:
        ruleset = create_ruleset({"flags": [{"key": "f", "on": "yes"}, flag([])]})
        outcome = evaluate(ruleset, "f", {"key": "k"})
        self.assertEqual((outcome.kind, outcome.code), ("ERROR", "MALFORMED_FLAG"))

    def test_an_off_flag_with_a_bad_body_still_serves_off(self) -> None:
        outcome = evaluate(
            create_ruleset({"flags": [flag([], on=False) | {"rules": "nope"}], "segments": []}),
            "f",
            {"key": "k"},
        )
        self.assertEqual((outcome.kind, outcome.value), ("OFF", False))

    def test_float_counts_read_as_integers(self) -> None:
        ruleset = create_ruleset(
            _json.loads(_json.dumps({"flags": [flag([], version=7.0)], "segments": []}))
        )
        self.assertEqual(ruleset.flags["f"].version, 7)
        self.assertIsNone(create_ruleset({"flags": [{"key": "f"}]}).flags["f"].version)

    def test_a_clause_value_that_is_not_finite_makes_the_flag_malformed(self) -> None:
        ruleset = create_ruleset(
            _json.loads(
                '{"flags": ['
                + _json.dumps(flag([rule("lt", [1])])).replace("[1]", "[1e999]", 1)
                + "]}"
            )
        )
        self.assertEqual(evaluate(ruleset, "f", {"key": "k"}).code, "MALFORMED_FLAG")


class PatternTests(unittest.TestCase):
    def matches(self, pattern: str, text: str) -> bool:
        compiled = compile_pattern(pattern)
        return compiled is not None and pattern_matches(compiled, text)

    def test_nested_groups_do_not_overflow_the_stack(self) -> None:
        for depth in (100, 250, 340):
            with self.subTest(depth=depth):
                self.assertTrue(self.matches("(" * depth + "a" + ")" * depth, "xax"))
                self.assertTrue(self.matches("(" * depth + "a" + ")*" * depth, "xax"))
                self.assertTrue(self.matches("(a|" * 150 + "b" + ")" * 150, "b"))

    def test_limits(self) -> None:
        self.assertIsNone(compile_pattern("a" * 1025))
        self.assertIsNotNone(compile_pattern("a" * 1024))
        self.assertIsNone(compile_pattern("a{101}"))
        self.assertIsNotNone(compile_pattern("(a{100}){100}"))
        self.assertIsNone(compile_pattern("(a{100}){100}b"))
        self.assertIsNone(compile_pattern("(" * 600 + "a" + ")" * 600))
        self.assertIsNotNone(compile_pattern("(a{100}){99}"))
        self.assertIsNone(compile_pattern("((a{100}){100}){2}"))

    def test_invalid_patterns(self) -> None:
        for pattern in (
            "(",
            ")",
            "a)",
            "*",
            "a**",
            "[",
            "[]",
            "[a",
            "a{2,1}",
            "a{01}",
            "\\q",
            "[z-a]",
            "a{",
            "[a-z-9]",
        ):
            with self.subTest(pattern=pattern):
                self.assertIsNone(compile_pattern(pattern), pattern)

    def test_matching(self) -> None:
        cases = [
            ("^a.c$", "abc", True),
            ("^a.c$", "a\nc", False),
            ("a|b", "xb", True),
            ("^(ab)+$", "ababab", True),
            ("^a{2,3}$", "aaaa", False),
            ("^[^a-c]+$", "xyz", True),
            ("\\d+", "ab12", True),
            ("\\D", "123", False),
            ("^$", "", True),
            ("^\\w+$", "café", False),
            ("^.$", "\U0001f600", True),
            ("^.$", "😀", True),
            ("(^a)*b", "ab", True),
            ("^(a*)*$", "a" * 40 + "b", False),
        ]
        for pattern, text, expected in cases:
            with self.subTest(pattern=pattern, text=text):
                self.assertEqual(self.matches(pattern, text), expected)


if __name__ == "__main__":
    unittest.main()
