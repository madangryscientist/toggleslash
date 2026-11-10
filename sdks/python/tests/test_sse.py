"""The server-sent-events parser, by the WHATWG rules the contract points at."""

import unittest

from toggleslash.sse import SseParser


def parse(*chunks: bytes) -> list[tuple[str, str]]:
    parser = SseParser()
    out = []
    for chunk in chunks:
        out.extend((e.event, e.data) for e in parser.feed(chunk))
    return out


class SseParserTests(unittest.TestCase):
    def test_one_event(self) -> None:
        self.assertEqual(parse(b'event: changed\ndata: "abc"\n\n'), [("changed", '"abc"')])

    def test_default_name_is_message(self) -> None:
        self.assertEqual(parse(b"data: x\n\n"), [("message", "x")])

    def test_an_event_without_data_is_dropped(self) -> None:
        self.assertEqual(parse(b"event: connected\n\n"), [])

    def test_data_lines_join_with_lf(self) -> None:
        self.assertEqual(parse(b"data: a\ndata: b\n\n"), [("message", "a\nb")])

    def test_crlf_and_cr_end_lines(self) -> None:
        self.assertEqual(parse(b"event: e\r\ndata: 1\r\n\r\n"), [("e", "1")])
        self.assertEqual(parse(b"event: e\rdata: 1\r\r"), [("e", "1")])

    def test_crlf_split_across_chunks(self) -> None:
        self.assertEqual(parse(b"data: 1\r", b"\n\r", b"\n"), [("message", "1")])

    def test_comments_ids_and_retry_are_ignored(self) -> None:
        self.assertEqual(parse(b": keep-alive\nid: 7\nretry: 5\ndata: z\n\n"), [("message", "z")])

    def test_one_leading_space_is_dropped_only(self) -> None:
        self.assertEqual(parse(b"data:  two\n\n"), [("message", " two")])
        self.assertEqual(parse(b"data:none\n\n"), [("message", "none")])

    def test_a_field_with_no_colon_is_a_name_with_empty_value(self) -> None:
        self.assertEqual(parse(b"data\n\n"), [("message", "")])

    def test_byte_order_mark_is_dropped(self) -> None:
        self.assertEqual(parse(b"\xef\xbb\xbfdata: q\n\n"), [("message", "q")])

    def test_multibyte_character_split_across_chunks(self) -> None:
        raw = "data: café\n\n".encode()
        self.assertEqual(parse(raw[:10], raw[10:]), [("message", "café")])

    def test_byte_at_a_time(self) -> None:
        raw = b"event: changed\ndata: 1\n\nevent: connected\ndata: 2\n\n"
        self.assertEqual(
            parse(*(raw[i : i + 1] for i in range(len(raw)))),
            [("changed", "1"), ("connected", "2")],
        )

    def test_an_event_is_not_released_before_its_blank_line(self) -> None:
        self.assertEqual(parse(b"data: half\n"), [])

    def test_invalid_utf8_becomes_a_replacement_character(self) -> None:
        self.assertEqual(parse(b"data: \xff\n\n"), [("message", "�")])


if __name__ == "__main__":
    unittest.main()
