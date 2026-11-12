"""Usage counts: grouping, batching, retry rules, Retry-After, the final flush and what is never sent."""

from __future__ import annotations

import json
import time
import unittest
from typing import Any

from support import (
    FakeServer,
    flag_json,
    make_client,
    reply,
    ruleset_json,
    stalled,
    wait_for,
)
from toggleslash._client import Client
from toggleslash._usage import MAX_BATCH, retryable

RULESET = "/sdk/v1/ruleset"
EVENTS = "/sdk/v1/events"
CONTEXT = {"key": "private-user-key", "attributes": {"email": "private@example.com"}}


class UsageTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.server = FakeServer()
        self.errors: list[Exception] = []
        self.addCleanup(self.server.stop)

    def client(self, *flags: dict[str, Any], **changes: Any) -> Client:
        self.server.on(
            "GET", RULESET, reply(200, ruleset_json(*(flags or (flag_json(),))), ETag='"v1"')
        )
        changes.setdefault("on_error", self.errors.append)
        client = make_client(self.server, **changes)
        self.addCleanup(client.close)
        self.assertTrue(client.ready())
        return client

    def sent(self) -> list[dict[str, Any]]:
        return [event for r in self.server.seen("POST", EVENTS) for event in r.json()["events"]]


class RecordingTests(UsageTestCase):
    def test_counts_are_summed_per_flag_variation_and_day(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client(flag_json("a", fallthrough=1), flag_json("b", fallthrough=0))
        for _ in range(3):
            client.variation("a", CONTEXT, False)
        client.variation("b", CONTEXT, True)
        client.variation("missing", CONTEXT, True)
        client.variation("a", CONTEXT, "wrong kind")
        client._usage.flush()
        events = sorted(self.sent(), key=lambda e: e["flag"])
        today = time.strftime("%Y-%m-%d", time.gmtime())
        self.assertEqual(
            events,
            [
                {"flag": "a", "variationIndex": 1, "count": 3, "day": today},
                {"flag": "b", "variationIndex": 0, "count": 1, "day": today},
            ],
        )
        self.assertEqual(client._usage.pending_count(), 0)

    def test_utc_midnight_makes_two_days(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        now = [1_700_000_000.0 - (1_700_000_000 % 86400) + 86399.5]
        client = self.client(now=lambda: now[0])
        client.variation("f", CONTEXT, False)
        now[0] += 1.0
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        days = sorted(e["day"] for e in self.sent())
        self.assertEqual(len(days), 2)
        self.assertLess(days[0], days[1])

    def test_exactly_four_fields_and_the_headers_the_contract_allows(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        request = self.server.seen("POST", EVENTS)[0]
        self.assertEqual(set(request.json()), {"events"})
        self.assertEqual(
            set(request.json()["events"][0]), {"flag", "variationIndex", "count", "day"}
        )
        self.assertEqual(request.headers["content-type"], "application/json")
        self.assertEqual(request.headers["authorization"], "Bearer test-key")
        for private in ("private-user-key", "private@example.com"):
            self.assertNotIn(private.encode(), request.body)
            self.assertNotIn(private, str(request.raw_headers))

    def test_a_flush_with_nothing_pending_sends_nothing(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client()
        client._usage.flush()
        client.close()
        self.assertEqual(self.server.seen("POST", EVENTS), [])

    def test_the_flush_loop_sends_one_interval_after_create(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client(flush_interval=0.3)
        client.variation("f", CONTEXT, False)
        time.sleep(0.1)
        self.assertEqual(self.server.seen("POST", EVENTS), [])
        self.assertTrue(wait_for(lambda: len(self.server.seen("POST", EVENTS)) == 1))

    def test_batches_of_at_most_500(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client()
        for i in range(MAX_BATCH + 7):
            client._usage.record(f"flag-{i}", 0)
        client._usage.flush()
        sizes = [len(r.json()["events"]) for r in self.server.seen("POST", EVENTS)]
        self.assertEqual(sizes, [MAX_BATCH, 7])


class RetryRuleTests(UsageTestCase):
    def test_the_status_alone_decides(self) -> None:
        for status in (500, 502, 503, 599, 408, 429):
            self.assertTrue(retryable(status), status)
        for status in (400, 401, 403, 404, 413, 422, 301):
            self.assertFalse(retryable(status), status)

    def test_a_retryable_failure_keeps_the_batch_and_stops_the_flush(self) -> None:
        self.server.on("POST", EVENTS, reply(503), reply(202))
        client = self.client()
        for i in range(MAX_BATCH + 3):
            client._usage.record(f"flag-{i}", 0)
        client._usage.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 1)
        self.assertEqual(client._usage.pending_count(), MAX_BATCH + 3)
        self.assertEqual(getattr(self.errors[-1], "code", None), "http_503")
        client._usage.flush()
        self.assertEqual(client._usage.pending_count(), 0)

    def test_kept_counts_merge_with_new_ones(self) -> None:
        self.server.on("POST", EVENTS, reply(500), reply(202))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        self.assertEqual([e["count"] for e in self.sent()][-1], 2)

    def test_other_4xx_drops_the_batch_and_continues(self) -> None:
        self.server.on("POST", EVENTS, reply(400), reply(202))
        client = self.client()
        for i in range(MAX_BATCH + 3):
            client._usage.record(f"flag-{i}", 0)
        client._usage.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 2)
        self.assertEqual(client._usage.pending_count(), 0)

    def test_a_network_failure_keeps_the_counts(self) -> None:
        self.server.on("POST", EVENTS, lambda _r, c: c.close(), reply(202))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        self.assertEqual(client._usage.pending_count(), 1)
        self.assertEqual(getattr(self.errors[-1], "code", None), "network")

    def test_a_timeout_keeps_the_counts(self) -> None:
        self.server.on("POST", EVENTS, stalled())
        client = self.client(request_timeout=0.3)
        client.variation("f", CONTEXT, False)
        started = time.monotonic()
        client._usage.flush()
        self.assertLess(time.monotonic() - started, 1.5)
        self.assertEqual(client._usage.pending_count(), 1)
        self.assertEqual(getattr(self.errors[-1], "code", None), "timeout")


class HoldTests(UsageTestCase):
    def test_a_429_with_retry_after_holds_usage_only(self) -> None:
        self.server.on("POST", EVENTS, reply(429, Retry_After="60"), reply(202))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        self.assertEqual(client._usage.pending_count(), 1)
        self.assertEqual(client._hold.remaining(), 0)
        client._usage.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 1)
        self.assertEqual(client._usage.pending_count(), 1)

    def test_the_events_routes_hold_is_capped_at_300_seconds(self) -> None:
        self.server.on("POST", EVENTS, reply(429, Retry_After="1000"))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        self.assertAlmostEqual(client._usage._hold.remaining(), 300, delta=2)

    def test_a_429_without_a_valid_retry_after_keeps_the_counts_with_no_hold(self) -> None:
        self.server.on("POST", EVENTS, reply(429), reply(202))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        self.assertEqual(client._usage.pending_count(), 1)
        client._usage.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 2)
        self.assertEqual(client._usage.pending_count(), 0)

    def test_the_final_flush_does_not_wait_out_a_hold_and_says_so(self) -> None:
        self.server.on("POST", EVENTS, reply(429, Retry_After="1000"))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client.variation("g", CONTEXT, False)
        client._usage.flush()
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 1.0)
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 1)
        message = str(self.errors[-1])
        self.assertEqual(message, "closed with 1 usage counts unsent: the server asked to wait")

    def test_the_events_hold_is_separate_from_the_ruleset_hold(self) -> None:
        self.server.on("POST", EVENTS, reply(429, Retry_After="60"))
        client = self.client(poll_interval=0.05)
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        before = len(self.server.seen("GET", RULESET))
        self.assertTrue(wait_for(lambda: len(self.server.seen("GET", RULESET)) > before + 1))


class CloseTests(UsageTestCase):
    def test_close_sends_the_final_flush(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client.close()
        self.assertEqual(len(self.sent()), 1)

    def test_nothing_is_counted_after_close_begins(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client()
        client.close()
        client.variation("f", CONTEXT, False)
        self.assertEqual(client._usage.pending_count(), 0)
        client.close()
        self.assertEqual(self.server.seen("POST", EVENTS), [])

    def test_a_flush_failure_at_close_is_reported_once(self) -> None:
        self.server.on("POST", EVENTS, reply(500))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client.close()
        self.assertEqual([getattr(e, "code", None) for e in self.errors], ["http_500"])

    def test_close_is_bounded_while_the_events_route_hangs(self) -> None:
        self.server.on("POST", EVENTS, stalled())
        client = self.client(request_timeout=0.4)
        client.variation("f", CONTEXT, False)
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 1.5)

    def test_the_body_is_valid_json_with_integers(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client()
        client.variation("f", CONTEXT, False)
        client._usage.flush()
        text = self.server.seen("POST", EVENTS)[0].body.decode("ascii")
        self.assertEqual(json.loads(text)["events"][0]["count"], 1)
        self.assertNotIn("1.0", text)


if __name__ == "__main__":
    unittest.main()
