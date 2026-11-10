import unittest

from toggleslash._sse import SseEvent, SseParser


def parse(*chunks):
    parser = SseParser()
    out = []
    for chunk in chunks:
        out.extend(parser.feed(chunk))
    return out


class SseParserTest(unittest.TestCase):
    def test_a_plain_event(self):
        self.assertEqual(parse("event: changed\ndata: x\n\n"), [SseEvent("changed", "x")])

    def test_default_name_is_message(self):
        self.assertEqual(parse("data: x\n\n"), [SseEvent("message", "x")])

    def test_several_data_lines_join_with_lf(self):
        self.assertEqual(parse("data: a\ndata: b\n\n"), [SseEvent("message", "a\nb")])

    def test_an_event_without_data_is_dropped(self):
        self.assertEqual(parse("event: changed\n\n"), [])

    def test_comments_are_ignored(self):
        self.assertEqual(parse(": keep-alive\n\ndata: x\n\n"), [SseEvent("message", "x")])

    def test_one_space_after_the_colon_is_dropped(self):
        self.assertEqual(parse("data:  two\n\n"), [SseEvent("message", " two")])
        self.assertEqual(parse("data:none\n\n"), [SseEvent("message", "none")])

    def test_crlf_and_cr_end_lines(self):
        self.assertEqual(parse("data: a\r\n\r\n"), [SseEvent("message", "a")])
        self.assertEqual(parse("data: a\r\r"), [SseEvent("message", "a")])

    def test_a_crlf_split_across_reads_is_one_line_end(self):
        self.assertEqual(parse("data: a\r", "\n\r", "\n"), [SseEvent("message", "a")])

    def test_an_event_split_across_reads(self):
        self.assertEqual(parse("eve", "nt: chan", "ged\nda", "ta: x\n", "\n"), [SseEvent("changed", "x")])

    def test_an_unfinished_event_is_never_delivered(self):
        self.assertEqual(parse("data: a\n"), [])
        self.assertEqual(parse("data: a\n\ndata: b"), [SseEvent("message", "a")])

    def test_a_leading_bom_is_skipped(self):
        self.assertEqual(parse("﻿data: x\n\n"), [SseEvent("message", "x")])

    def test_name_resets_after_each_event(self):
        self.assertEqual(parse("event: a\ndata: 1\n\ndata: 2\n\n"), [SseEvent("a", "1"), SseEvent("message", "2")])

    def test_unknown_fields_are_ignored(self):
        self.assertEqual(parse("id: 1\nretry: 5\ndata: x\n\n"), [SseEvent("message", "x")])


if __name__ == "__main__":
    unittest.main()
