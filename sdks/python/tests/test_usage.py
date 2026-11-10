import calendar
import json
import unittest

import toggleslash
from support import CTX, FakeClock, FakeServer, bool_flag, wait_until
from toggleslash import Seams

EVENTS = "/sdk/v1/events"


def at(year, month, day, hour, minute, second):
    return calendar.timegm((year, month, day, hour, minute, second)) * 1000


class UsageTest(unittest.TestCase):
    def setUp(self):
        self.server = FakeServer()
        self.addCleanup(self.server.stop)
        self.clock = FakeClock(at(2026, 11, 10, 12, 0, 0))

    def made(self, flush=10, **options):
        errors = []
        options.setdefault("stream", False)
        options.setdefault("poll_interval", 1_000_000)
        client = toggleslash.create(
            url=self.server.url,
            sdk_key="k",
            flush_interval=flush,
            request_timeout=2,
            on_error=errors.append,
            _seams=Seams(clock=self.clock, jitter=lambda: 0.5),
            **options,
        )
        self.addCleanup(client.close)
        self.assertTrue(client.ready())
        return client, errors

    def tick(self, seconds):
        """Moves time once the flush loop is waiting, and returns after it has run and waited again."""
        wait_until(lambda: self.clock.armed() >= 1, what="a waiting loop")
        started = self.clock.started()
        self.clock.advance(seconds)
        self.clock.wait_started(started + 1)

    def bodies(self):
        return [json.loads(r["body"]) for r in self.server.requests_to("POST", EVENTS)]

    def test_counts_are_summed_per_flag_variation_and_day_and_sent_after_one_interval(self):
        client, _ = self.made()
        for _ in range(3):
            client.variation("checkout", CTX, False)
        client.variation("legacy", CTX, True)
        self.assertEqual(self.server.count("POST", EVENTS), 0)  # nothing before the first interval
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 1, what="a post")
        events = {(e["flag"], e["variationIndex"]): e for e in self.bodies()[0]["events"]}
        self.assertEqual(events[("checkout", 1)]["count"], 3)
        self.assertEqual(events[("legacy", 0)]["count"], 1)
        self.assertEqual(events[("checkout", 1)]["day"], "2026-11-10")
        request = self.server.requests_to("POST", EVENTS)[0]
        self.assertEqual(request["headers"]["content-type"], "application/json")
        self.assertEqual(request["headers"]["authorization"], "Bearer k")

    def test_a_flush_with_nothing_pending_sends_nothing(self):
        self.made()
        self.tick(10)
        self.tick(10)
        self.assertEqual(self.server.count("POST", EVENTS), 0)

    def test_fallbacks_and_errors_are_not_counted(self):
        client, _ = self.made()
        client.variation("nope", CTX, 1)
        client.variation("checkout", CTX, "wrong kind")
        self.tick(10)
        self.assertEqual(self.server.count("POST", EVENTS), 0)

    def test_counts_either_side_of_utc_midnight_go_out_as_two_days(self):
        self.clock = FakeClock(at(2026, 11, 10, 23, 59, 59))
        client, _ = self.made(flush=100)
        client.variation("checkout", CTX, False)
        self.clock.advance(2)
        client.variation("checkout", CTX, False)
        self.tick(100)
        wait_until(lambda: self.server.count("POST", EVENTS) == 1, what="a post")
        days = sorted((e["day"], e["count"]) for e in self.bodies()[0]["events"])
        self.assertEqual(days, [("2026-11-10", 1), ("2026-11-11", 1)])

    def test_batches_of_at_most_500(self):
        self.server.set({"flags": [bool_flag(f"f{i}", True) for i in range(501)], "segments": []}, '"v1"')
        client, _ = self.made()
        for i in range(501):
            client.variation(f"f{i}", CTX, False)
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 2, what="two posts")
        self.assertEqual(sorted(len(b["events"]) for b in self.bodies()), [1, 500])

    def test_a_server_error_keeps_the_counts_and_stops_the_flush(self):
        self.server.set({"flags": [bool_flag(f"f{i}", True) for i in range(501)], "segments": []}, '"v1"')
        self.server.script(EVENTS, (503, {}))
        client, errors = self.made()
        for i in range(501):
            client.variation(f"f{i}", CTX, False)
        self.tick(10)
        wait_until(lambda: len(errors) == 1, what="the error")
        self.assertEqual(self.server.count("POST", EVENTS), 1)  # the second batch was not tried
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 3, what="the retry in two batches")
        total = sum(e["count"] for r in self.bodies()[1:] for e in r["events"])
        self.assertEqual(total, 501)

    def test_a_client_error_drops_the_batch_and_the_flush_goes_on(self):
        self.server.set({"flags": [bool_flag(f"f{i}", True) for i in range(501)], "segments": []}, '"v1"')
        self.server.script(EVENTS, (400, {}))
        client, errors = self.made()
        for i in range(501):
            client.variation(f"f{i}", CTX, False)
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 2, what="both batches tried")
        self.assertEqual(len(errors), 1)
        self.tick(10)
        self.assertEqual(self.server.count("POST", EVENTS), 2)  # the dropped 500 are not retried

    def test_408_and_5xx_are_kept(self):
        for status in (408, 500, 502):
            with self.subTest(status=status):
                self.setUp()
                self.server.script(EVENTS, (status, {}))
                client, _ = self.made()
                client.variation("checkout", CTX, False)
                self.tick(10)
                wait_until(lambda: self.server.count("POST", EVENTS) == 1, what="a post")
                self.tick(10)
                wait_until(lambda: self.server.count("POST", EVENTS) == 2, what="the retry")
                self.assertEqual(self.bodies()[1]["events"][0]["count"], 1)
                client.close()
                self.server.stop()

    def test_a_429_without_a_retry_after_is_kept_with_no_hold(self):
        self.server.script(EVENTS, (429, {}))
        client, _ = self.made()
        client.variation("checkout", CTX, False)
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 1, what="a post")
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 2, what="the next flush is not held")

    def test_a_429_with_a_retry_after_holds_the_next_flushes(self):
        self.server.script(EVENTS, (429, {"Retry-After": "35"}))
        client, _ = self.made()
        client.variation("checkout", CTX, False)
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 1, what="a post")
        for _ in range(3):
            self.tick(10)  # 10, 20, 30 s into a 35 s hold
        self.assertEqual(self.server.count("POST", EVENTS), 1)
        self.tick(10)  # 40 s
        wait_until(lambda: self.server.count("POST", EVENTS) == 2, what="a post after the hold")

    def test_a_retry_after_above_300_is_held_for_300_on_the_events_route(self):
        self.server.script(EVENTS, (429, {"Retry-After": "100000"}))
        client, _ = self.made(flush=100)
        client.variation("checkout", CTX, False)
        self.tick(100)
        wait_until(lambda: self.server.count("POST", EVENTS) == 1, what="a post")
        self.tick(100)
        self.tick(100)  # 200 s in
        self.assertEqual(self.server.count("POST", EVENTS), 1)
        self.tick(100)  # 300 s in: the hold is over
        wait_until(lambda: self.server.count("POST", EVENTS) == 2, what="a post at 300 s")

    def test_every_failed_request_calls_on_error(self):
        self.server.script(EVENTS, (500, {}), (500, {}))
        client, errors = self.made()
        client.variation("checkout", CTX, False)
        self.tick(10)
        self.tick(10)
        wait_until(lambda: len(errors) == 2, what="two errors")
        self.assertIn("500", str(errors[0]))

    def test_close_sends_the_pending_counts(self):
        client, _ = self.made(flush=1000)
        client.variation("checkout", CTX, False)
        client.close()
        self.assertEqual(self.server.count("POST", EVENTS), 1)

    def test_close_does_not_wait_out_a_hold_and_reports_the_unsent_counts(self):
        self.server.script(EVENTS, (429, {"Retry-After": "250"}))
        client, errors = self.made()
        client.variation("checkout", CTX, False)
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 1, what="a post")
        client.variation("legacy", CTX, True)
        client.variation("checkout", CTX, False)
        errors.clear()
        client.close()
        self.assertEqual(self.server.count("POST", EVENTS), 1)
        self.assertEqual([str(e) for e in errors], ["closed with 2 usage counts unsent: the server asked to wait"])

    def test_the_body_holds_counts_and_nothing_about_a_user(self):
        client, _ = self.made()
        client.variation("checkout", {"key": "SENTINEL-USER", "email": "a@b.c"}, False)
        self.tick(10)
        wait_until(lambda: self.server.count("POST", EVENTS) == 1, what="a post")
        body = self.server.requests_to("POST", EVENTS)[0]["body"].decode()
        self.assertNotIn("SENTINEL", body)
        self.assertEqual(set(json.loads(body)), {"events"})
        self.assertEqual(set(json.loads(body)["events"][0]), {"flag", "variationIndex", "count", "day"})


if __name__ == "__main__":
    unittest.main()
