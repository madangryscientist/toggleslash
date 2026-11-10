import unittest

from toggleslash._retry import MAX_RETRY_AFTER, retry_after
from toggleslash._sse import SseParser


def parse(*chunks: bytes) -> list[tuple[str, str]]:
    parser = SseParser()
    events: list[tuple[str, str]] = []
    for chunk in chunks:
        events.extend(parser.feed(chunk))
    return events


class SseTests(unittest.TestCase):
    def test_an_event_and_a_comment(self) -> None:
        self.assertEqual(parse(b': keep-alive\n\nevent: changed\ndata: "v2"\n\n'), [("changed", '"v2"')])

    def test_the_name_defaults_to_message(self) -> None:
        self.assertEqual(parse(b"data: x\n\n"), [("message", "x")])

    def test_data_lines_join_with_lf_and_one_leading_space_goes(self) -> None:
        self.assertEqual(parse(b"data: a\ndata:  b\ndata\n\n"), [("message", "a\n b\n")])

    def test_an_event_without_data_is_dropped_and_does_not_leak_its_name(self) -> None:
        self.assertEqual(parse(b"event: changed\n\ndata: x\n\n"), [("message", "x")])

    def test_every_line_end(self) -> None:
        self.assertEqual(
            parse(b"data: a\r\n\r\ndata: b\r\rdata: c\n\n"), [("message", "a"), ("message", "b"), ("message", "c")]
        )

    def test_a_crlf_split_between_reads_is_one_line_end(self) -> None:
        self.assertEqual(parse(b"data: a\r", b"\n\r", b"\ndata: b\n\n"), [("message", "a"), ("message", "b")])

    def test_a_lone_cr_at_the_end_of_a_read_ends_the_line(self) -> None:
        self.assertEqual(parse(b"data: a\r", b"data: b\r", b"\r"), [("message", "a\nb")])

    def test_any_cut_of_the_bytes_gives_the_same_events(self) -> None:
        whole = 'event: changed\r\ndata: "é€😀"\r\n\r\n: ping\n\ndata: x\n\n'.encode()
        expected = parse(whole)
        self.assertEqual(len(expected), 2)
        for size in range(1, 8):
            pieces = [whole[i : i + size] for i in range(0, len(whole), size)]
            self.assertEqual(parse(*pieces), expected, f"chunks of {size}")

    def test_an_event_still_incomplete_when_the_bytes_end_is_never_delivered(self) -> None:
        self.assertEqual(parse(b"data: a\n\ndata: b\n"), [("message", "a")])

    def test_a_leading_byte_order_mark_is_dropped(self) -> None:
        self.assertEqual(parse(b"\xef\xbb\xbfdata: a\n\n"), [("message", "a")])

    def test_fields_other_than_event_and_data_are_ignored(self) -> None:
        self.assertEqual(parse(b"id: 4\nretry: 10\nfoo: bar\ndata: a\n\n"), [("message", "a")])


class RetryAfterTests(unittest.TestCase):
    def test_only_a_429_is_read(self) -> None:
        self.assertIsNone(retry_after(503, "5"))
        self.assertEqual(retry_after(429, "5"), 5.0)

    def test_whole_decimal_seconds_only_with_spaces_ignored(self) -> None:
        self.assertEqual(retry_after(429, " 7 "), 7.0)
        for bad in (None, "", "1.5", "-3", "+3", "Wed, 21 Oct 2026 07:28:00 GMT", "3s", "٣", "²"):
            self.assertIsNone(retry_after(429, bad), repr(bad))

    def test_it_is_capped_at_five_minutes(self) -> None:
        self.assertEqual(retry_after(429, "300"), MAX_RETRY_AFTER)
        self.assertEqual(retry_after(429, "301"), 300.0)
        self.assertEqual(retry_after(429, "99999999999"), 300.0)
        self.assertEqual(retry_after(429, "9" * 5000), 300.0)


if __name__ == "__main__":
    unittest.main()
