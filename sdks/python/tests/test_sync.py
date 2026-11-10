"""The ruleset, the stream, the loop and its backoff, the holds, and ``close`` against all of them."""

import threading
import unittest

from fakes import (
    CONTEXT,
    FLUSH,
    SDK_KEY,
    ServerCase,
    flag,
    hang,
    ruleset,
    serve_ruleset,
    sse,
    stream_route,
    wait_until,
)
from toggleslash import StreamState, ToggleSlashError
from toggleslash.client import Seams

RULESET = "/sdk/v1/ruleset"
STREAM = "/sdk/v1/stream"
BODY = ruleset(flag("f", [False, True], serve=1))


def drive(case: ServerCase, steps: int, per_step: float = 100.0) -> None:
    """Move the fake clock on until the loop has asked for ``steps`` waits (or give up)."""
    for _ in range(2000):
        if len(case.clock.waited()) >= steps:
            return
        case.clock.advance(per_step)
        wait_until(lambda: True, 0.01)
        threading.Event().wait(0.003)


class RulesetTests(ServerCase):
    def test_first_load_sends_the_key_and_no_condition(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make()
        self.assertTrue(client.ready())
        first = self.server.seen("GET", RULESET)[0]
        self.assertEqual(first.headers["authorization"], f"Bearer {SDK_KEY}")
        self.assertNotIn("if-none-match", first.headers)
        self.assertEqual(client.status().etag, '"v1"')
        self.assertIsNone(client.status().last_error)

    def test_a_later_poll_is_conditional_and_a_304_is_a_sync(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make(fake_clock=True, poll_interval=30)
        self.assertTrue(client.ready())
        before = client.status().last_sync_at
        self.clock.advance(31)
        wait_until(lambda: len(self.server.seen("GET", RULESET)) >= 2, what="the second poll")
        wait_until(lambda: client.status().last_sync_at != before, what="the 304 to count")
        second = self.server.seen("GET", RULESET)[1]
        self.assertEqual(second.headers["if-none-match"], '"v1"')
        self.assertEqual(client.status().etag, '"v1"')

    def test_a_body_that_is_not_json_is_bad_ruleset_and_the_last_good_one_stays(self) -> None:
        state = {"body": BODY}

        def handle(request, out) -> None:
            out.send(200, state["body"], {"ETag": '"x"'})

        self.server.route("GET", RULESET, handle)
        client = self.make(fake_clock=True, poll_interval=30)
        self.assertTrue(client.ready())
        for bad in (b"not json", b"[]", b'{"flags":[]}', b'{"flags":{},"segments":[]}', b"null"):
            state["body"] = bad
            seen = len(self.server.seen("GET", RULESET))
            self.clock.advance(31)
            wait_until(lambda n=seen: len(self.server.seen("GET", RULESET)) > n, what="a poll")
            wait_until(lambda: client.status().last_error == "bad_ruleset", what="bad_ruleset")
            self.assertTrue(client.bool_variation("f", CONTEXT, False), bad)
            state["body"] = BODY
            seen = len(self.server.seen("GET", RULESET))
            self.clock.advance(31)
            wait_until(lambda n=seen: len(self.server.seen("GET", RULESET)) > n, what="a poll")
            wait_until(lambda: client.status().last_error is None, what="the error to clear")

    def test_an_http_error_is_recorded_and_told_to_on_error(self) -> None:
        self.server.route("GET", RULESET, lambda r, o: o.send(503))
        client = self.make()
        self.assertFalse(client.ready())
        self.assertEqual(client.status().last_error, "http_503")
        self.assertIsInstance(self.errors[0], ToggleSlashError)
        self.assertEqual(self.errors[0].code, "http_503")
        self.assertNotIn(SDK_KEY, str(self.errors[0]))
        self.assertNotIn("127.0.0.1", str(self.errors[0]))

    def test_a_hung_ruleset_request_times_out_and_readiness_is_bounded(self) -> None:
        self.server.route("GET", RULESET, hang)
        client = self.make(request_timeout=0.4)
        started = threading.Event()
        started.wait(0)
        self.assertFalse(client.ready())
        self.assertEqual(client.status().last_error, "timeout")

    def test_a_slow_body_is_cut_by_the_whole_request_timeout(self) -> None:
        def drip(request, out) -> None:
            out._h.send_response(200)
            out._h.send_header("Content-Length", "100000")
            out._h.end_headers()
            for _ in range(200):
                if not out.write(b"x" * 10) or out.release.wait(0.1):
                    return

        self.server.route("GET", RULESET, drip)
        client = self.make(request_timeout=0.5)
        self.assertFalse(client.ready())
        self.assertEqual(client.status().last_error, "timeout")

    def test_a_refused_connection_is_network(self) -> None:
        client = self.make(url="http://127.0.0.1:1")
        self.assertFalse(client.ready())
        self.assertEqual(client.status().last_error, "network")

    def test_a_url_that_is_not_a_url_does_not_raise(self) -> None:
        for url in ("", "nonsense", "ftp://x", "http://", "http://[::1", None, 5):
            client = self.make(url=url)
            self.assertFalse(client.ready(), url)
            self.assertEqual(client.status().last_error, "network")
            self.assertEqual(client.bool_variation("f", CONTEXT, True), True)

    def test_request_headers_are_only_the_ones_the_protocol_needs(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make()
        client.ready()
        allowed = {"host", "accept-encoding", "authorization", "if-none-match", "accept"}
        for request in self.server.requests:
            self.assertLessEqual(set(request.headers), allowed, request.headers)
            self.assertEqual(request.headers.get("accept-encoding"), "identity")

    def test_a_trailing_slash_on_the_url_is_dropped(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make(url=self.server.url + "/")
        self.assertTrue(client.ready())

    def test_a_url_with_a_path_prefix_keeps_it(self) -> None:
        self.server.route("GET", "/prefix" + RULESET, lambda r, o: o.send(200, BODY))
        client = self.make(url=self.server.url + "/prefix")
        self.assertTrue(client.ready())


class LoopTests(ServerCase):
    def delays(self, count: int) -> list[float]:
        drive(self, count)
        return self.clock.waited()[:count]

    def test_polling_waits_exactly_poll_interval(self) -> None:
        serve_ruleset(self.server, BODY)
        self.make(fake_clock=True, poll_interval=7)
        self.assertEqual(self.delays(3), [7.0, 7.0, 7.0])

    def test_the_backoff_doubles_from_one_second_with_jitter_and_stops_at_poll_interval(
        self,
    ) -> None:
        self.server.route("GET", RULESET, lambda r, o: o.send(500))
        self.server.route("GET", STREAM, lambda r, o: o.send(500))
        self.make(
            stream=True,
            poll_interval=10,
            flush_interval=FLUSH,
            seams=Seams(clock=self.clock, random=lambda: 0.0),
        )
        waits = [w for w in self.delays(40) if w != 0]
        self.assertEqual(waits[:5], [1.0, 2.0, 4.0, 5.0, 5.0])
        full = Seams(clock=self.clock, random=lambda: 0.999999)
        self.assertLess(full.random(), 1.0)

    def test_jitter_is_between_half_and_whole(self) -> None:
        self.server.route("GET", RULESET, lambda r, o: o.send(500))
        self.server.route("GET", STREAM, lambda r, o: o.send(500))
        self.make(
            stream=True,
            poll_interval=10,
            flush_interval=FLUSH,
            seams=Seams(clock=self.clock, random=lambda: 0.999999),
        )
        waits = [w for w in self.delays(6) if w != 0]
        self.assertAlmostEqual(waits[0], 2.0 * 0.9999995, places=3)
        self.assertTrue(all(0.5 * c <= w < c for w, c in zip(waits, [2.0, 4.0, 8.0, 10.0, 10.0])))

    def test_a_429_retry_after_above_300_holds_the_ruleset_for_300(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make(fake_clock=True, poll_interval=30)
        self.assertTrue(client.ready())
        self.server.route("GET", RULESET, lambda r, o: o.send(429, b"", {"Retry-After": "99999"}))
        self.clock.advance(31)
        wait_until(lambda: client.status().last_error == "http_429", what="the 429")
        self.assertIn(300.0, self.clock.waited())
        self.assertNotIn(99999.0, self.clock.waited())

    def test_retry_after_that_is_not_whole_decimal_seconds_holds_nothing(self) -> None:
        self.server.route("GET", RULESET, lambda r, o: o.send(429, b"", {"Retry-After": "1e3"}))
        self.make(fake_clock=True, poll_interval=30)
        drive(self, 2)
        self.assertTrue(all(w <= 30 for w in self.clock.waited()))
        for value in ("-5", "abc", "", "1.5", "Wed, 21 Oct 2026 07:28:00 GMT"):
            self.server.route("GET", RULESET, lambda r, o, v=value: o.send(429, b"", {"Retry-After": v}))
        drive(self, 5)
        self.assertTrue(all(w <= 30 for w in self.clock.waited()))

    def test_a_429_on_the_stream_route_holds_for_300(self) -> None:
        serve_ruleset(self.server, BODY)
        self.server.route("GET", STREAM, lambda r, o: o.send(429, b"", {"Retry-After": "5000"}))
        self.make(fake_clock=True, stream=True, poll_interval=30)
        wait_until(lambda: 300.0 in self.clock.waited(), what="a 300 s wait")

    def test_a_429_on_the_ruleset_route_holds_the_stream_too(self) -> None:
        calls = {"n": 0}

        def handle(request, out) -> None:
            calls["n"] += 1
            if calls["n"] == 2:
                out.send(429, b"", {"Retry-After": "120"})
            else:
                out.send(200, BODY, {"ETag": '"v1"'})

        self.server.route("GET", RULESET, handle)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"'), sse("changed", '"v2"')]))
        self.make(fake_clock=True, stream=True, poll_interval=300)
        wait_until(lambda: 120.0 in self.clock.waited(), 10, "the 120 s hold")
        opened = len(self.server.seen("GET", STREAM))
        self.clock.advance(60)
        threading.Event().wait(0.15)
        self.assertEqual(len(self.server.seen("GET", STREAM)), opened, "the stream reopened in the hold")
        self.clock.advance(61)
        wait_until(lambda: len(self.server.seen("GET", STREAM)) > opened, what="the stream after the hold")

    def test_the_first_load_is_not_held(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make(fake_clock=True)
        self.assertTrue(client.ready())


class StreamTests(ServerCase):
    def test_connected_marks_the_stream_and_clears_the_error(self) -> None:
        serve_ruleset(self.server, BODY)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"')]))
        client = self.make(fake_clock=True, stream=True)
        wait_until(lambda: client.status().stream is StreamState.CONNECTED, what="connected")
        stream_request = self.server.seen("GET", STREAM)[0]
        self.assertEqual(stream_request.headers["accept"], "text/event-stream")
        self.assertEqual(stream_request.headers["authorization"], f"Bearer {SDK_KEY}")

    def test_changed_with_the_held_tag_fetches_nothing(self) -> None:
        serve_ruleset(self.server, BODY)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"'), sse("changed", '"v1"')]))
        client = self.make(fake_clock=True, stream=True)
        wait_until(lambda: client.status().stream is StreamState.CONNECTED, what="connected")
        threading.Event().wait(0.1)
        self.assertEqual(len(self.server.seen("GET", RULESET)), 1)

    def test_changed_with_a_new_tag_fetches_and_swaps(self) -> None:
        versions = {"n": 0}

        def handle(request, out) -> None:
            versions["n"] += 1
            tag = f'"v{versions["n"]}"'
            out.send(200, ruleset(flag("f", [False, True], serve=int(versions["n"] > 1))), {"ETag": tag})

        self.server.route("GET", RULESET, handle)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"'), sse("changed", '"v2"')]))
        client = self.make(fake_clock=True, stream=True)
        wait_until(lambda: client.status().etag == '"v2"', what="the new tag")
        self.assertTrue(client.bool_variation("f", CONTEXT, False))

    def test_unknown_events_and_comments_are_ignored(self) -> None:
        serve_ruleset(self.server, BODY)
        self.server.route(
            "GET",
            STREAM,
            stream_route([b": hi\n\n", sse("ping", "1"), sse("connected", '"v1"'), sse("other", "2")]),
        )
        client = self.make(fake_clock=True, stream=True)
        wait_until(lambda: client.status().stream is StreamState.CONNECTED, what="connected")
        self.assertEqual(len(self.server.seen("GET", RULESET)), 1)

    def test_a_stream_that_ends_cleanly_leaves_last_error_alone_and_reconnects(self) -> None:
        serve_ruleset(self.server, BODY)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"')], hold=False))
        client = self.make(fake_clock=True, stream=True)
        wait_until(lambda: len(self.clock.waited()) >= 1, what="the wait after the stream")
        self.assertIsNone(client.status().last_error)
        self.assertIs(client.status().stream, StreamState.DISCONNECTED)
        self.clock.advance(5)
        wait_until(lambda: len(self.server.seen("GET", STREAM)) >= 2, what="a reconnect")

    def test_a_failed_refresh_ends_the_stream(self) -> None:
        calls = {"n": 0}

        def handle(request, out) -> None:
            calls["n"] += 1
            if calls["n"] == 1:
                out.send(200, BODY, {"ETag": '"v1"'})
            else:
                out.send(500)

        self.server.route("GET", RULESET, handle)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"'), sse("changed", '"v2"')]))
        client = self.make(fake_clock=True, stream=True)
        wait_until(lambda: client.status().last_error == "http_500", what="http_500")
        wait_until(lambda: client.status().stream is StreamState.DISCONNECTED, what="disconnected")
        self.assertTrue(client.bool_variation("f", CONTEXT, False))

    def test_a_connected_event_clears_the_error(self) -> None:
        calls = {"n": 0}

        def handle(request, out) -> None:
            calls["n"] += 1
            out.send(200 if calls["n"] == 1 else 500, BODY, {"ETag": '"v1"'})

        self.server.route("GET", RULESET, handle)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"')], hold=False))
        client = self.make(fake_clock=True, stream=True)
        wait_until(lambda: len(self.clock.waited()) >= 1, what="the first wait")
        self.assertIsNone(client.status().last_error)

    def test_a_stream_quiet_for_the_watchdog_period_is_dropped_as_timeout(self) -> None:
        serve_ruleset(self.server, BODY)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"')]))
        client = self.make(stream=True, seams=Seams(clock=self.clock, stream_idle_timeout=0.4))
        wait_until(lambda: client.status().last_error == "timeout", 10, "the watchdog")
        self.assertIs(client.status().stream, StreamState.DISCONNECTED)

    def test_any_bytes_restart_the_watchdog(self) -> None:
        serve_ruleset(self.server, BODY)

        def chatty(request, out) -> None:
            out.stream_open()
            out.write(sse("connected", '"v1"'))
            for _ in range(12):
                if out.release.wait(0.15) or not out.write(b": keep-alive\n\n"):
                    return

        self.server.route("GET", STREAM, chatty)
        client = self.make(stream=True, seams=Seams(clock=self.clock, stream_idle_timeout=0.5))
        wait_until(lambda: client.status().stream is StreamState.CONNECTED, what="connected")
        threading.Event().wait(1.2)  # more than twice the watchdog, with bytes arriving throughout
        self.assertIs(client.status().stream, StreamState.CONNECTED)
        self.assertIsNone(client.status().last_error)

    def test_the_stream_open_is_bounded_by_request_timeout(self) -> None:
        serve_ruleset(self.server, BODY)
        self.server.route("GET", STREAM, hang)
        client = self.make(stream=True, request_timeout=0.4)
        wait_until(lambda: client.status().last_error == "timeout", 10, "the open to time out")

    def test_no_context_key_or_attribute_is_in_any_request(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make()
        client.ready()
        client.bool_variation("f", {"key": "secret-user-key", "attributes": {"email": "a@b.c"}}, False)
        client.flush()
        for request in self.server.requests:
            blob = repr((request.path, request.headers, request.body))
            self.assertNotIn("secret-user-key", blob)
            self.assertNotIn("a@b.c", blob)


class CloseAgainstSyncTests(ServerCase):
    def test_close_abandons_a_hung_ruleset_request_quietly(self) -> None:
        self.server.route("GET", RULESET, hang)
        client = self.make(request_timeout=30)
        wait_until(lambda: len(self.server.seen("GET", RULESET)) == 1, what="the request")
        done = threading.Thread(target=client.close, daemon=True)
        done.start()
        done.join(5)
        self.assertFalse(done.is_alive(), "close hung on an in-flight request")
        self.assertFalse(client.ready())
        self.assertEqual(self.errors, [])
        self.assertIsNone(client.status().last_error)

    def test_close_ends_an_open_stream_quietly(self) -> None:
        serve_ruleset(self.server, BODY)
        self.server.route("GET", STREAM, stream_route([sse("connected", '"v1"')]))
        client = self.make(stream=True, request_timeout=30)
        wait_until(lambda: client.status().stream is StreamState.CONNECTED, what="connected")
        done = threading.Thread(target=client.close, daemon=True)
        done.start()
        done.join(5)
        self.assertFalse(done.is_alive())
        self.assertIs(client.status().stream, StreamState.CLOSED)
        self.assertEqual(self.errors, [])
        self.assertIsNone(client.status().last_error)

    def test_nothing_reaches_the_server_after_close(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make(fake_clock=True, poll_interval=1)
        self.assertTrue(client.ready())
        client.close()
        seen = len(self.server.requests)
        self.clock.advance(100)
        threading.Event().wait(0.2)
        self.assertEqual(len(self.server.requests), seen)
        self.assertTrue(client.bool_variation("f", CONTEXT, False))  # still answers from what it held

    def test_the_sync_thread_is_a_daemon_and_ends_on_close(self) -> None:
        serve_ruleset(self.server, BODY)
        client = self.make()
        client.ready()
        names = {t.name: t for t in threading.enumerate()}
        self.assertTrue(names["toggleslash-sync"].daemon)
        self.assertTrue(names["toggleslash-flush"].daemon)
        client.close()
        wait_until(
            lambda: (
                "toggleslash-sync" not in {t.name for t in threading.enumerate() if t.is_alive()}
                or all(t is not names["toggleslash-sync"] for t in threading.enumerate())
            ),
            what="the sync thread to end",
        )


if __name__ == "__main__":
    unittest.main()
