"""Strings by UTF-16 code unit, and JSON read as binary64."""

from __future__ import annotations

import math
import unittest

from toggleslash import _json, _text
from toggleslash._bucket import bucket_of, uint32_of


class TextTests(unittest.TestCase):
    def test_order_is_by_utf16_code_unit(self) -> None:
        # U+FF5E is one unit 0xFF5E; U+1F600 is the pair 0xD83D 0xDE00, which sorts below it.
        self.assertEqual(_text.compare("\uff5e", "\U0001f600"), 1)
        self.assertEqual(_text.compare("\U0001f600", "\uff5e"), -1)
        self.assertEqual(_text.compare("a", "b"), -1)
        self.assertEqual(_text.compare("b", "b"), 0)

    def test_a_pair_of_lone_surrogates_is_the_character_it_spells(self) -> None:
        pair = "😀"
        self.assertTrue(_text.equals(pair, "\U0001f600"))
        self.assertTrue(_text.contains("x\U0001f600y", pair))
        self.assertTrue(_text.starts_with("\U0001f600y", pair))
        self.assertTrue(_text.ends_with("y\U0001f600", pair))

    def test_substring_ops_see_code_units(self) -> None:
        # A lone high surrogate is a run of units inside a pair: UTF-16 `includes` finds it.
        self.assertTrue(_text.contains("\U0001f600", "\ud83d"))
        self.assertTrue(_text.starts_with("\U0001f600", "\ud83d"))
        self.assertTrue(_text.ends_with("\U0001f600", "\ude00"))
        self.assertFalse(_text.contains("㴀", "\ud83d"))
        self.assertTrue(_text.contains("anything", ""))

    def test_code_points_join_a_pair_and_keep_a_lone_surrogate(self) -> None:
        self.assertEqual(_text.code_points("a\U0001f600"), [0x61, 0x1F600])
        self.assertEqual(_text.code_points("😀"), [0x1F600])
        self.assertEqual(_text.code_points("\ud83dx"), [0xD83D, 0x78])

    def test_hashing_turns_a_lone_surrogate_into_u_fffd(self) -> None:
        self.assertEqual(bucket_of("f", "s", "\ud800"), bucket_of("f", "s", "�"))
        self.assertEqual(uint32_of("f", "s", "😀"), uint32_of("f", "s", "\U0001f600"))


class JsonTests(unittest.TestCase):
    def test_numbers_are_binary64(self) -> None:
        self.assertEqual(_json.loads("9007199254740993"), 9007199254740992)
        self.assertEqual(_json.loads("[9007199254740993.0]"), [9007199254740992.0])
        self.assertEqual(_json.loads("1e999"), math.inf)
        self.assertEqual(_json.loads("-1e999"), -math.inf)
        self.assertEqual(math.copysign(1, _json.loads("-0.0")), -1)
        self.assertEqual(_json.loads("-0"), 0)
        self.assertIsInstance(_json.loads("12"), int)
        self.assertIsInstance(_json.loads("9007199254740993"), int)
        self.assertIsInstance(_json.loads("9007199254740994"), float)

    def test_a_literal_of_thousands_of_digits_is_read(self) -> None:
        self.assertEqual(_json.loads("1" + "0" * 5000), math.inf)
        self.assertEqual(_json.loads("-1" + "0" * 5000), -math.inf)

    def test_names_that_are_not_json_numbers_are_refused(self) -> None:
        for text in ("NaN", "Infinity", "-Infinity", "[1,]", "{", "", "01"):
            with self.subTest(text=text), self.assertRaises(_json.JSONError):
                _json.loads(text)

    def test_nesting_512_deep_parses_and_absurd_nesting_is_refused(self) -> None:
        deep = "[" * 512 + "]" * 512
        value = _json.loads(deep)
        for _ in range(511):
            value = value[0]
        self.assertEqual(value, [])
        with self.assertRaises(_json.JSONError):
            _json.loads("[" * 100_000)

    def test_bytes_decode_with_replacement_and_a_bom_is_skipped(self) -> None:
        self.assertEqual(_json.loads(b'"a\xffb"'), "a�b")
        self.assertEqual(_json.loads(b'\xef\xbb\xbf{"a":1}'), {"a": 1})
        self.assertEqual(_json.loads(b'"\xed\xa0\x80"'), "���")

    def test_lone_surrogate_escapes_survive_and_a_pair_joins(self) -> None:
        self.assertEqual(_json.loads('"\\ud800"'), "\ud800")
        self.assertEqual(_json.loads('"\\ud83d\\ude00"'), "\U0001f600")

    def test_a_duplicate_key_keeps_the_last(self) -> None:
        self.assertEqual(_json.loads('{"a":1,"a":2}'), {"a": 2})

    def test_dumps_writes_a_non_finite_number_as_null(self) -> None:
        self.assertEqual(
            _json.dumps({"a": math.nan, "b": [math.inf, 1.5]}), '{"a":null,"b":[null,1.5]}'
        )
        self.assertEqual(_json.dumps("é\ud800"), '"\\u00e9\\ud800"')

    def test_copy_is_deep_and_does_not_recurse(self) -> None:
        value = _json.loads("[" * 512 + "]" * 512)
        copy = _json.copy(value)
        self.assertEqual(copy, value)
        self.assertIsNot(copy, value)
        original = {"a": [1, {"b": 2}]}
        clone = _json.copy(original)
        clone["a"][1]["b"] = 3
        self.assertEqual(original["a"][1]["b"], 2)


if __name__ == "__main__":
    unittest.main()
