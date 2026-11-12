"""The Server-Sent Events reader, as the WHATWG standard reads a stream."""

from __future__ import annotations

import unittest

from toggleslash._sse import SseParser


def parse(*pieces: bytes) -> list[tuple[str, str]]:
    parser = SseParser()
    events = []
    for piece in pieces:
        events.extend((e.name, e.data) for e in parser.feed(piece))
    return events


class SseTests(unittest.TestCase):
    def test_line_endings_lf_crlf_and_lone_cr(self) -> None:
        for end in (b"\n", b"\r\n", b"\r"):
            with self.subTest(end=end):
                self.assertEqual(parse(b"event: a" + end + b"data: x" + end + end), [("a", "x")])

    def test_a_crlf_split_across_reads_is_one_line_end(self) -> None:
        self.assertEqual(parse(b"event: a\r", b"\ndata: x\r\n\r", b"\n"), [("a", "x")])
        self.assertEqual(
            parse(b"data: x\r\n\r\ndata: y\r\n\r\n"), [("message", "x"), ("message", "y")]
        )

    def test_one_leading_space_is_dropped_and_only_one(self) -> None:
        self.assertEqual(parse(b"data:  x\n\n"), [("message", " x")])
        self.assertEqual(parse(b"data:x\n\n"), [("message", "x")])

    def test_multiple_data_lines_join_with_lf(self) -> None:
        self.assertEqual(parse(b"data: a\ndata: b\ndata\n\n"), [("message", "a\nb\n")])

    def test_comments_and_unknown_fields_are_ignored(self) -> None:
        self.assertEqual(
            parse(b": ka\nid: 4\nretry: 10\nfoo: bar\ndata: x\n\n"), [("message", "x")]
        )

    def test_an_event_with_no_data_is_dropped(self) -> None:
        self.assertEqual(parse(b"event: a\n\n"), [])

    def test_the_name_resets_after_each_event(self) -> None:
        self.assertEqual(parse(b"event: a\ndata: 1\n\ndata: 2\n\n"), [("a", "1"), ("message", "2")])

    def test_a_bom_at_the_start_is_stripped(self) -> None:
        self.assertEqual(parse(b"\xef\xbb\xbfdata: x\n\n"), [("message", "x")])
        self.assertEqual(parse(b"\xef\xbb", b"\xbfdata: x\n\n"), [("message", "x")])

    def test_a_multi_byte_character_split_across_reads(self) -> None:
        text = "data: é😀\n\n".encode()
        for cut in range(1, len(text)):
            with self.subTest(cut=cut):
                self.assertEqual(parse(text[:cut], text[cut:]), [("message", "é😀")])

    def test_invalid_utf8_is_replaced_not_raised(self) -> None:
        self.assertEqual(parse(b"data: \xff\n\n"), [("message", "�")])

    def test_an_unterminated_event_is_not_dispatched(self) -> None:
        self.assertEqual(parse(b"data: x\n"), [])

    def test_byte_at_a_time(self) -> None:
        raw = b'event: connected\ndata: "v1"\n\n: ka\n\nevent: changed\ndata: "v2"\n\n'
        self.assertEqual(
            parse(*(raw[i : i + 1] for i in range(len(raw)))),
            [("connected", '"v1"'), ("changed", '"v2"')],
        )


if __name__ == "__main__":
    unittest.main()
