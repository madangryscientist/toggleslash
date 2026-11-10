"""The sync loop on a fake clock: time moves only when the SDK is waiting on it."""

import unittest

import toggleslash
from support import CTX, FakeClock, FakeServer, wait_until
from toggleslash import Seams, StreamState


class SyncTest(unittest.TestCase):
    def setUp(self):
        self.server = FakeServer()
        self.addCleanup(self.server.stop)
        self.clock = FakeClock()

    def made(self, **options):
        errors = []
        options.setdefault("request_timeout", 2)
        options.setdefault("_seams", Seams(clock=self.clock, jitter=lambda: 0.5))
        client = toggleslash.create(url=self.server.url, sdk_key="k", on_error=errors.append, **options)
        self.addCleanup(client.close)
        return client, errors

    def test_polling_without_a_stream_waits_the_poll_interval(self):
        client, _ = self.made(stream=False, poll_interval=10)
        self.assertIs(client.ready(), True)
        self.clock.wait_started(2)  # the poll wait and the usage flush wait
        self.assertIn(10, self.clock.waits)
        before = self.server.count("GET", "/sdk/v1/ruleset")
        self.clock.advance(10)
        wait_until(lambda: self.server.count("GET", "/sdk/v1/ruleset") == before + 1, what="a poll")

    def test_last_sync_at_moves_on_a_304_and_comes_from_the_clock(self):
        client, _ = self.made(stream=False, poll_interval=10)
        client.ready()
        first = client.status().last_sync_at
        self.assertEqual(first, 1_700_000_000_000)
        self.clock.step(10, count=2)
        wait_until(lambda: client.status().last_sync_at == 1_700_000_010_000, what="lastSyncAt")

    def test_stream_failure_backs_off_with_jitter_then_polls(self):
        self.server.script("/sdk/v1/stream", (503, {}), (503, {}), (503, {}))
        client, errors = self.made(poll_interval=30, flush_interval=100000)
        client.ready()
        # failures=1: min(30, 2) * (0.5 + 0.5/2) = 1.5, then 4 * 0.75 = 3, then 8 * 0.75 = 6
        wait_until(lambda: 1.5 in self.clock.waits, what="the first backoff")
        polls = self.server.count("GET", "/sdk/v1/ruleset")
        self.clock.run_until(lambda: 6.0 in self.clock.waits, step=0.5, what="the third backoff")
        self.assertEqual(self.clock.waits.count(3.0), 1)
        self.assertGreater(self.server.count("GET", "/sdk/v1/ruleset"), polls)  # a poll after each failure
        self.assertEqual(client.status().last_error, "http_503")
        self.assertGreaterEqual(len(errors), 2)

    def test_backoff_is_capped_at_the_poll_interval(self):
        self.server.script("/sdk/v1/stream", *[(503, {})] * 6)
        client, _ = self.made(poll_interval=5, flush_interval=100000)
        client.ready()
        self.clock.run_until(lambda: self.server.count("GET", "/sdk/v1/stream") >= 7, step=0.5, what="7 tries")
        sleeps = [w for w in self.clock.waits if w != 100000]
        self.assertEqual(max(sleeps), 3.75)  # min(5, 8) * 0.75
        self.assertLessEqual(max(sleeps), 5)

    def test_a_clean_stream_end_is_not_an_error_and_reconnects_without_a_poll(self):
        client, errors = self.made(poll_interval=30)
        client.ready()
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="connected")
        polls = self.server.count("GET", "/sdk/v1/ruleset")
        self.server.drop_streams()
        wait_until(lambda: client.status().stream == StreamState.DISCONNECTED, what="disconnected")
        self.assertEqual(client.status().last_error, None)
        self.assertEqual(errors, [])
        wait_until(lambda: 0.75 in self.clock.waits, what="the short wait after a stream that was up")
        self.clock.run_until(lambda: client.status().stream == StreamState.CONNECTED, step=1, what="reconnect")
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="reconnected")
        self.assertEqual(self.server.count("GET", "/sdk/v1/ruleset"), polls)

    def test_changed_with_the_held_tag_is_a_sync_without_a_fetch(self):
        client, _ = self.made(poll_interval=30)
        client.ready()
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="connected")
        fetches = self.server.count("GET", "/sdk/v1/ruleset")
        self.clock.advance(5)
        self.server.publish('"v1"')
        wait_until(lambda: client.status().last_sync_at == 1_700_000_005_000, what="a tag-equal sync")
        self.assertEqual(self.server.count("GET", "/sdk/v1/ruleset"), fetches)

    def test_changed_with_a_new_tag_fetches_and_swaps(self):
        client, _ = self.made(poll_interval=30)
        client.ready()
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="connected")
        flags = self.server.ruleset
        flags["flags"][0]["fallthrough"] = {"variation": 0}
        self.server.set(flags, '"v2"')
        self.server.publish('"v2"')
        wait_until(lambda: client.status().etag == '"v2"', what="the new tag")
        self.assertIs(client.variation("checkout", CTX, True), False)

    def test_a_failed_fetch_after_changed_ends_the_stream(self):
        client, errors = self.made(poll_interval=30)
        client.ready()
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="connected")
        self.server.script("/sdk/v1/ruleset", (500, {}))
        self.server.publish('"v9"')
        wait_until(lambda: client.status().stream == StreamState.DISCONNECTED, what="the stream ended")
        self.assertEqual(client.status().last_error, "http_500")
        self.assertEqual(len(errors), 1)

    def test_keep_alive_comments_do_not_move_last_sync_at(self):
        client, _ = self.made(poll_interval=30)
        client.ready()
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="connected")
        before = client.status().last_sync_at
        self.clock.advance(20)
        self.server.write(": keep-alive\n\n")
        self.server.write("event: other\ndata: x\n\n")
        import time

        time.sleep(0.2)
        self.assertEqual(client.status().last_sync_at, before)

    def _hold_of(self, route, status_script):
        self.server.script(route, *status_script)

    def test_retry_after_above_300_is_held_for_300_on_the_ruleset(self):
        self.server.script("/sdk/v1/ruleset", (200, {}), (429, {"Retry-After": "1000"}))
        # the first scripted answer is a bare 200, which is not a ruleset: bad_ruleset, so the first load fails
        client, _ = self.made(stream=False, poll_interval=1)
        client.ready()
        self.clock.wait_started(2)
        self.clock.advance(1)
        wait_until(lambda: client.status().last_error == "http_429", what="the 429")
        wait_until(lambda: 300 in self.clock.waits, what="a 300 s wait")
        self.assertNotIn(1000, self.clock.waits)

    def test_retry_after_above_300_is_held_for_300_on_the_stream(self):
        self.server.script("/sdk/v1/stream", (429, {"Retry-After": "1000"}))
        client, _ = self.made(poll_interval=30)
        client.ready()
        wait_until(lambda: 300 in self.clock.waits, what="a 300 s wait")
        self.assertEqual(client.status().last_error, "http_429")

    def test_a_retry_after_that_is_not_whole_seconds_is_no_hold(self):
        for value in ("1.5", "-3", "Wed, 21 Oct 2026 07:28:00 GMT", "", "abc", "1e3"):
            with self.subTest(value=value):
                clock = FakeClock()
                server = FakeServer()
                self.addCleanup(server.stop)
                server.script("/sdk/v1/stream", (429, {"Retry-After": value}))
                client = toggleslash.create(
                    url=server.url,
                    sdk_key="k",
                    poll_interval=30,
                    request_timeout=2,
                    _seams=Seams(clock=clock, jitter=lambda: 0.5),
                )
                self.addCleanup(client.close)
                client.ready()
                wait_until(lambda waits=clock.waits: 1.5 in waits, what="the backoff")
                self.assertTrue(all(w <= 30 for w in clock.waits), clock.waits)
                self.assertEqual(client.status().last_error, "http_429")

    def test_a_later_shorter_retry_after_does_not_shorten_the_hold(self):
        from toggleslash._hold import Hold

        hold = Hold(self.clock)
        hold.extend(100)
        hold.extend(5)
        self.assertEqual(hold.remaining(), 100)
        self.clock.advance(60)
        hold.extend(5)  # 65 from now is not before 100 from the start
        self.assertEqual(hold.remaining(), 40)
        hold.extend(300)
        self.assertEqual(hold.remaining(), 300)

    def test_retry_after_parsing(self):
        from toggleslash._hold import parse_retry_after

        for value, expected in [
            ("0", 0),
            ("7", 7),
            (" 12 ", 12),
            ("300", 300),
            ("301", 300),
            ("99999999999999999999", 300),
        ]:
            self.assertEqual(parse_retry_after(value), expected, value)
        for value in [
            None,
            "",
            " ",
            "1.5",
            "-1",
            "+1",
            "1e3",
            "abc",
            "\u0661",
            "Wed, 21 Oct 2026 07:28:00 GMT",
        ]:
            self.assertIsNone(parse_retry_after(value), value)

    def test_the_first_load_is_not_held(self):
        self.server.script("/sdk/v1/ruleset", (429, {"Retry-After": "100"}))
        client, _ = self.made(stream=False, poll_interval=1)
        self.assertIs(client.ready(), False)  # it asked at once, and was refused
        self.assertEqual(client.status().last_error, "http_429")
        self.assertEqual(self.server.count("GET", "/sdk/v1/ruleset"), 1)
        wait_until(lambda: 100 in self.clock.waits, what="the hold")

    def test_close_while_waiting_returns_at_once(self):
        client, _ = self.made(stream=False, poll_interval=1000)
        client.ready()
        self.clock.wait_started(2)
        client.close()  # the loops wait on a clock nobody moves; close wakes them
        self.assertEqual(client.status().stream, StreamState.CLOSED)


if __name__ == "__main__":
    unittest.main()
