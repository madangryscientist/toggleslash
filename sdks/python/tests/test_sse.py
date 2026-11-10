from __future__ import annotations

import unittest

from toggleslash._sse import SseParser


def feed_all(chunks: list[bytes]) -> list[tuple[str, str]]:
    parser = SseParser()
    out: list[tuple[str, str]] = []
    for chunk in chunks:
        out.extend(parser.feed(chunk))
    return out


class SseTests(unittest.TestCase):
    def test_basic_event(self) -> None:
        self.assertEqual(feed_all([b'event: changed\ndata: "v2"\n\n']), [("changed", '"v2"')])

    def test_comment_and_keep_alive_are_ignored(self) -> None:
        self.assertEqual(feed_all([b": keep-alive\n\n"]), [])

    def test_nameless_event_is_message(self) -> None:
        self.assertEqual(feed_all([b"data: x\n\n"]), [("message", "x")])

    def test_event_without_data_is_dropped_and_resets_the_name(self) -> None:
        self.assertEqual(feed_all([b"event: changed\n\ndata: x\n\n"]), [("message", "x")])

    def test_data_lines_join_with_lf(self) -> None:
        self.assertEqual(feed_all([b"data: a\ndata: b\ndata\n\n"]), [("message", "a\nb\n")])

    def test_only_one_leading_space_is_removed(self) -> None:
        self.assertEqual(feed_all([b"data:  two\n\n"]), [("message", " two")])
        self.assertEqual(feed_all([b"data:none\n\n"]), [("message", "none")])

    def test_crlf_split_between_reads(self) -> None:
        self.assertEqual(
            feed_all([b"event: connected\r", b"\ndata: a\r", b"\n\r", b"\n"]), [("connected", "a")]
        )

    def test_lone_cr_ends_a_line(self) -> None:
        self.assertEqual(feed_all([b"event: changed\rdata: a\r\r"]), [("changed", "a")])

    def test_cr_then_non_lf_in_the_next_read(self) -> None:
        self.assertEqual(feed_all([b"data: a\r", b"data: b\n\n"]), [("message", "a\nb")])

    def test_utf8_split_across_reads(self) -> None:
        raw = "data: café \U0001f600\n\n".encode()
        for cut in range(1, len(raw)):
            self.assertEqual(feed_all([raw[:cut], raw[cut:]]), [("message", "café \U0001f600")], cut)

    def test_incomplete_event_is_discarded(self) -> None:
        parser = SseParser()
        self.assertEqual(parser.feed(b"event: changed\ndata: a\n"), [])

    def test_unknown_fields_are_ignored(self) -> None:
        self.assertEqual(feed_all([b"id: 7\nretry: 5\nfoo: bar\ndata: a\n\n"]), [("message", "a")])

    def test_two_events_in_one_read(self) -> None:
        self.assertEqual(
            feed_all([b"event: connected\ndata: a\n\nevent: changed\ndata: b\n\n"]),
            [("connected", "a"), ("changed", "b")],
        )


if __name__ == "__main__":
    unittest.main()
