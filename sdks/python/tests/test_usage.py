"""Usage counts: what is kept, the batches, the answers, the holds and the last flush."""

import calendar
import json
import threading
import time
import unittest

from fakes import CONTEXT, ServerCase, flag, hang, ruleset, serve_ruleset, wait_until

EVENTS = "/sdk/v1/events"


def bodies(case: ServerCase) -> list[list[dict]]:
    return [json.loads(r.body)["events"] for r in case.server.seen("POST", EVENTS)]


class UsageTests(ServerCase):
    def start(self, flags=None, **options):
        flags = flags or [flag("f", [False, True], serve=1), flag("g", ["a", "b"], serve=0)]
        serve_ruleset(self.server, ruleset(*flags))
        client = self.make(fake_clock=True, **options)
        self.assertTrue(client.ready())
        return client

    def test_the_request_has_exactly_the_four_fields_and_the_headers_it_needs(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        client = self.start()
        self.clock.set_wall(calendar.timegm((2026, 11, 10, 12, 0, 0)))
        for _ in range(3):
            client.bool_variation("f", CONTEXT, False)
        client.flush()
        request = self.server.seen("POST", EVENTS)[0]
        self.assertEqual(request.headers["content-type"], "application/json")
        self.assertEqual(request.headers["authorization"], "Bearer sdk-test-key")
        self.assertEqual(
            json.loads(request.body),
            {"events": [{"flag": "f", "variationIndex": 1, "count": 3, "day": "2026-11-10"}]},
        )
        self.assertNotIn(b"user-1", request.body)

    def test_counts_are_summed_per_flag_variation_and_day(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        client = self.start()
        client.bool_variation("f", CONTEXT, False)
        client.bool_variation("f", {"key": "someone-else"}, False)
        client.string_variation("g", CONTEXT, "z")
        client.flush()
        (events,) = bodies(self)
        self.assertEqual(
            sorted((e["flag"], e["variationIndex"], e["count"]) for e in events),
            [("f", 1, 2), ("g", 0, 1)],
        )

    def test_a_utc_midnight_splits_one_flag_into_two_days(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        client = self.start()
        self.clock.set_wall(calendar.timegm((2026, 11, 10, 23, 59, 59)) + 0.5)
        client.bool_variation("f", CONTEXT, False)
        self.clock.advance(1.0)
        client.bool_variation("f", CONTEXT, False)
        client.flush()
        (events,) = bodies(self)
        self.assertEqual(
            sorted((e["day"], e["count"]) for e in events),
            [("2026-11-10", 1), ("2026-11-11", 1)],
        )

    def test_what_is_not_served_from_the_ruleset_is_not_counted(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        client = self.start()
        client.bool_variation("missing", CONTEXT, False)  # FLAG_NOT_FOUND
        client.string_variation("f", CONTEXT, "x")  # TYPE_MISMATCH
        client.bool_variation("f", {"key": ""}, False)  # INVALID_CONTEXT
        client.flush()
        self.assertEqual(self.server.seen("POST", EVENTS), [])

    def test_nothing_pending_sends_no_request(self) -> None:
        client = self.start()
        client.flush()
        client.close()
        self.assertEqual(self.server.seen("POST", EVENTS), [])

    def test_more_than_500_entries_go_out_500_at_a_time(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        flags = [flag(f"flag-{i}", [False, True], serve=1) for i in range(1203)]
        client = self.start(flags)
        for i in range(1203):
            client.bool_variation(f"flag-{i}", CONTEXT, False)
        client.flush()
        self.assertEqual([len(b) for b in bodies(self)], [500, 500, 203])

    def test_a_server_error_keeps_the_counts_and_stops_the_flush(self) -> None:
        answers = iter([204, 503, 204, 204, 204])
        self.server.route("POST", EVENTS, lambda r, o: o.send(next(answers)))
        flags = [flag(f"flag-{i}", [False, True], serve=1) for i in range(1203)]
        client = self.start(flags)
        for i in range(1203):
            client.bool_variation(f"flag-{i}", CONTEXT, False)
        client.flush()
        self.assertEqual([len(b) for b in bodies(self)], [500, 500])  # the third was not tried
        self.assertEqual(self.errors[-1].args[0], "POST /sdk/v1/events answered 503")
        client.bool_variation("flag-0", CONTEXT, False)  # recorded since: merged with what was kept
        client.flush()
        sizes = [len(b) for b in bodies(self)]
        self.assertEqual(sizes[2:], [500, 204])
        self.assertEqual(sum(e["count"] for b in bodies(self)[2:] for e in b), 704)

    def test_a_refused_batch_is_dropped_and_the_flush_goes_on(self) -> None:
        answers = iter([400, 204])
        self.server.route("POST", EVENTS, lambda r, o: o.send(next(answers)))
        flags = [flag(f"flag-{i}", [False, True], serve=1) for i in range(600)]
        client = self.start(flags)
        for i in range(600):
            client.bool_variation(f"flag-{i}", CONTEXT, False)
        client.flush()
        self.assertEqual([len(b) for b in bodies(self)], [500, 100])
        client.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 2)  # nothing kept
        self.assertEqual(len(self.errors), 1)

    def test_408_and_network_failures_keep_the_counts(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(408))
        client = self.start()
        client.bool_variation("f", CONTEXT, False)
        client.flush()
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        client.flush()
        self.assertEqual([sum(e["count"] for e in b) for b in bodies(self)], [1, 1])

    def test_a_timer_flushes_every_flush_interval(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        client = self.start(flush_interval=10)
        client.bool_variation("f", CONTEXT, False)
        self.assertEqual(self.server.seen("POST", EVENTS), [])
        self.clock.advance(11)
        wait_until(lambda: len(self.server.seen("POST", EVENTS)) == 1, what="the timed flush")

    def test_a_429_retry_after_holds_the_flushes_and_is_capped_at_300(self) -> None:
        answers = {"n": 0}

        def handle(request, out) -> None:
            answers["n"] += 1
            if answers["n"] == 1:
                out.send(429, b"", {"Retry-After": "99999"})
            else:
                out.send(204)

        self.server.route("POST", EVENTS, handle)
        client = self.start()
        client.bool_variation("f", CONTEXT, False)
        client.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 1)
        self.clock.advance(299)
        client.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 1, "sent inside the hold")
        self.clock.advance(2)
        client.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 2)
        self.assertEqual(bodies(self)[1][0]["count"], 1)

    def test_a_429_on_events_does_not_hold_the_ruleset(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(429, b"", {"Retry-After": "250"}))
        client = self.start(poll_interval=30)
        client.bool_variation("f", CONTEXT, False)
        client.flush()
        before = len(self.server.seen("GET", "/sdk/v1/ruleset"))
        self.clock.advance(31)
        wait_until(
            lambda: len(self.server.seen("GET", "/sdk/v1/ruleset")) > before,
            what="a poll in the hold",
        )

    def test_an_invalid_retry_after_keeps_the_counts_without_a_hold(self) -> None:
        answers = iter([429, 204])
        self.server.route("POST", EVENTS, lambda r, o: o.send(next(answers), b"", {"Retry-After": "soon"}))
        client = self.start()
        client.bool_variation("f", CONTEXT, False)
        client.flush()
        client.flush()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 2)

    def test_close_sends_the_last_counts(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        client = self.start()
        client.bool_variation("f", CONTEXT, False)
        client.close()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 1)
        self.assertEqual(self.errors, [])

    def test_close_in_a_hold_loses_the_counts_and_says_so_with_the_entry_count(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(429, b"", {"Retry-After": "100"}))
        client = self.start()
        client.bool_variation("f", CONTEXT, False)
        client.string_variation("g", CONTEXT, "z")
        client.flush()
        client.bool_variation("f", CONTEXT, False)
        client.close()
        self.assertEqual(len(self.server.seen("POST", EVENTS)), 1)
        self.assertEqual(
            self.errors[-1].args[0],
            "closed with 2 usage counts unsent: the server asked to wait",
        )

    def test_nothing_is_counted_after_close_begins_and_evaluation_still_answers(self) -> None:
        self.server.route("POST", EVENTS, lambda r, o: o.send(204))
        client = self.start()
        client.close()
        self.assertTrue(client.bool_variation("f", CONTEXT, False))
        client.flush()
        self.assertEqual(self.server.seen("POST", EVENTS), [])

    def test_a_hung_usage_flush_does_not_slow_evaluation_and_is_bounded(self) -> None:
        self.server.route("POST", EVENTS, hang)
        client = self.start(request_timeout=0.5)
        client.bool_variation("f", CONTEXT, False)
        flusher = threading.Thread(target=client.flush, daemon=True)
        started = time.monotonic()
        flusher.start()
        wait_until(lambda: len(self.server.seen("POST", EVENTS)) == 1, what="the flush request")
        evaluating = time.monotonic()
        for _ in range(1000):
            client.bool_variation("f", CONTEXT, False)
        client.status()
        self.assertLess(time.monotonic() - evaluating, 1.0, "evaluation waited on the flush")
        flusher.join(5)
        self.assertFalse(flusher.is_alive(), "a hung flush was not bounded by the request timeout")
        self.assertLess(time.monotonic() - started, 4.0)
        self.assertEqual(self.errors[-1].args[0], "POST /sdk/v1/events failed: timeout")

    def test_close_returns_while_the_last_flush_hangs(self) -> None:
        self.server.route("POST", EVENTS, hang)
        client = self.start(request_timeout=0.5)
        client.bool_variation("f", CONTEXT, False)
        closer = threading.Thread(target=client.close, daemon=True)
        closer.start()
        closer.join(6)
        self.assertFalse(closer.is_alive())

    def test_flushes_are_serial(self) -> None:
        active = {"now": 0, "max": 0}
        lock = threading.Lock()

        def handle(request, out) -> None:
            with lock:
                active["now"] += 1
                active["max"] = max(active["max"], active["now"])
            time.sleep(0.1)
            with lock:
                active["now"] -= 1
            out.send(204)

        self.server.route("POST", EVENTS, handle)
        client = self.start()
        for _ in range(3):
            client.bool_variation("f", CONTEXT, False)
        threads = [threading.Thread(target=client.flush, daemon=True) for _ in range(4)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(10)
        self.assertEqual(active["max"], 1)


if __name__ == "__main__":
    unittest.main()
