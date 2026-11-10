import json
import unittest
from datetime import UTC, datetime, timedelta, timezone
from typing import Any

from support import FakeReply, FakeTime, ScriptedTransport
from toggleslash._http import TransportError
from toggleslash._types import NETWORK
from toggleslash._usage import MAX_BATCH, Usage


def status(code: int, retry_after: str | None = None):
    headers = {"Retry-After": retry_after} if retry_after is not None else None
    return lambda *_: FakeReply(code, headers=headers)


def broken(*_: Any):
    raise TransportError(NETWORK, "the connection failed")


class Rig:
    def __init__(self, events: Any, start: datetime | None = None) -> None:
        self.time = FakeTime(start)
        self.transport = ScriptedTransport(events=events)
        self.errors: list[BaseException] = []
        self.usage = Usage(self.transport, "sdk-key", 5.0, self.errors.append, self.time.monotonic, self.time.wall)

    def sent(self) -> list[dict[str, Any]]:
        return [entry for request in self.transport.requests for entry in json.loads(request["body"])["events"]]


class UsageTests(unittest.TestCase):
    def test_counts_are_summed_per_flag_index_and_day_and_sent_in_the_documented_shape(self) -> None:
        rig = Rig(status(202))
        for _ in range(3):
            rig.usage.record("a", 1)
        rig.usage.record("a", 0)
        rig.usage.record("b", 1)
        rig.usage.flush()
        self.assertEqual(len(rig.transport.requests), 1)
        request = rig.transport.requests[0]
        self.assertEqual((request["method"], request["route"]), ("POST", "/sdk/v1/events"))
        self.assertEqual(request["headers"], {"Authorization": "Bearer sdk-key", "Content-Type": "application/json"})
        entries = sorted(rig.sent(), key=lambda e: (e["flag"], e["variationIndex"]))
        self.assertEqual(
            entries,
            [
                {"flag": "a", "variationIndex": 0, "count": 1, "day": "2026-11-10"},
                {"flag": "a", "variationIndex": 1, "count": 3, "day": "2026-11-10"},
                {"flag": "b", "variationIndex": 1, "count": 1, "day": "2026-11-10"},
            ],
        )
        self.assertEqual(rig.errors, [])

    def test_nothing_pending_sends_nothing(self) -> None:
        rig = Rig(status(202))
        rig.usage.flush()
        self.assertEqual(rig.transport.requests, [])

    def test_the_day_is_the_utc_day_and_splits_at_midnight(self) -> None:
        rig = Rig(status(202), datetime(2026, 11, 10, 23, 59, 59, 500_000, tzinfo=UTC))
        rig.usage.record("a", 0)
        rig.time.advance(1.0)
        rig.usage.record("a", 0)
        rig.usage.flush()
        days = sorted(entry["day"] for entry in rig.sent())
        self.assertEqual(days, ["2026-11-10", "2026-11-11"])

    def test_a_clock_in_another_zone_still_counts_the_utc_day(self) -> None:
        zone = timezone(timedelta(hours=-5))
        rig = Rig(status(202))
        rig.usage = Usage(
            rig.transport,
            "k",
            5.0,
            rig.errors.append,
            rig.time.monotonic,
            lambda: datetime(2026, 11, 10, 20, 0, tzinfo=zone),
        )
        rig.usage.record("a", 0)
        rig.usage.flush()
        self.assertEqual(rig.sent()[0]["day"], "2026-11-11")

    def test_at_most_500_entries_a_request(self) -> None:
        rig = Rig(status(202))
        for n in range(MAX_BATCH * 2 + 1):
            rig.usage.record(f"f{n}", 0)
        rig.usage.flush()
        sizes = [len(json.loads(request["body"])["events"]) for request in rig.transport.requests]
        self.assertEqual(sizes, [500, 500, 1])

    def test_a_server_failure_keeps_the_counts_and_stops_the_flush(self) -> None:
        for code in (500, 503, 408, 429):
            with self.subTest(code=code):
                rig = Rig(status(code))
                for n in range(MAX_BATCH + 1):
                    rig.usage.record(f"f{n}", 0)
                rig.usage.flush()
                self.assertEqual(len(rig.transport.requests), 1, "the flush stops at the first failure")
                self.assertEqual(len(rig.errors), 1)
                rig.transport.routes["events"] = status(202)
                rig.time.advance(1000)
                rig.usage.flush()
                self.assertEqual(sum(e["count"] for e in rig.sent()[MAX_BATCH:]), MAX_BATCH + 1)

    def test_a_network_failure_keeps_the_counts(self) -> None:
        rig = Rig(broken)
        rig.usage.record("a", 0)
        rig.usage.flush()
        self.assertEqual(len(rig.errors), 1)
        rig.transport.routes["events"] = status(202)
        rig.usage.flush()
        self.assertEqual(rig.sent()[-1], {"flag": "a", "variationIndex": 0, "count": 1, "day": "2026-11-10"})

    def test_a_refusal_drops_that_batch_and_the_flush_goes_on(self) -> None:
        rig = Rig([status(400), status(202)])
        for n in range(MAX_BATCH + 1):
            rig.usage.record(f"f{n}", 0)
        rig.usage.flush()
        self.assertEqual(len(rig.transport.requests), 2)
        self.assertEqual(len(rig.errors), 1)
        rig.usage.flush()
        self.assertEqual(len(rig.transport.requests), 2, "the refused batch is gone")

    def test_counts_recorded_during_a_failed_send_are_added_to_the_ones_put_back(self) -> None:
        rig = Rig(status(500))
        rig.usage.record("a", 0)

        def during(*_: Any):
            rig.usage.record("a", 0)
            return FakeReply(500)

        rig.transport.routes["events"] = during
        rig.usage.flush()
        rig.transport.routes["events"] = status(202)
        rig.usage.flush()
        self.assertEqual(rig.sent()[-1]["count"], 2)


class HoldTests(unittest.TestCase):
    def test_a_429_holds_the_next_send_until_retry_after_has_passed(self) -> None:
        rig = Rig([status(429, "10"), status(202)])
        rig.usage.record("a", 0)
        rig.usage.flush()
        rig.time.advance(9.9)
        rig.usage.flush()
        self.assertEqual(len(rig.transport.requests), 1)
        rig.time.advance(0.2)
        rig.usage.flush()
        self.assertEqual(len(rig.transport.requests), 2)

    def test_the_hold_is_capped_at_300_seconds(self) -> None:
        rig = Rig([status(429, "86400"), status(202)])
        rig.usage.record("a", 0)
        rig.usage.flush()
        rig.time.advance(299.0)
        rig.usage.flush()
        self.assertEqual(len(rig.transport.requests), 1)
        rig.time.advance(1.5)
        rig.usage.flush()
        self.assertEqual(len(rig.transport.requests), 2)

    def test_only_digits_count_and_only_on_a_429(self) -> None:
        for code, value in ((429, "1.5"), (429, "Wed, 21 Oct 2026 07:28:00 GMT"), (503, "100"), (500, "100")):
            with self.subTest(code=code, value=value):
                rig = Rig([status(code, value), status(202)])
                rig.usage.record("a", 0)
                rig.usage.flush()
                rig.usage.flush()
                self.assertEqual(len(rig.transport.requests), 2)

    def test_the_final_flush_does_not_wait_and_says_what_it_lost(self) -> None:
        rig = Rig([status(429, "60"), status(202)])
        for flag in ("a", "b"):
            rig.usage.record(flag, 0)
        rig.usage.flush()
        rig.usage.flush(final=True)
        self.assertEqual(len(rig.transport.requests), 1)
        self.assertEqual(
            [str(error) for error in rig.errors][-1], "closed with 2 usage counts unsent: the server asked to wait"
        )

    def test_recording_stops_after_stop_recording(self) -> None:
        rig = Rig(status(202))
        rig.usage.stop_recording()
        rig.usage.record("a", 0)
        rig.usage.flush()
        self.assertEqual(rig.transport.requests, [])


if __name__ == "__main__":
    unittest.main()
