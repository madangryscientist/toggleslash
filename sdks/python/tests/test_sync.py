"""Loading, polling, the stream, Retry-After and close, against the loopback fake server."""

from __future__ import annotations

import time
import unittest
from typing import Any

from support import (
    FakeServer,
    chunk,
    event,
    flag_json,
    make_client,
    reply,
    ruleset_json,
    stalled,
    stream,
    wait_for,
)
from toggleslash import StreamState
from toggleslash._client import Client
from toggleslash._http import Hold

RULESET = "/sdk/v1/ruleset"
STREAM = "/sdk/v1/stream"


class SyncTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.server = FakeServer()
        self.errors: list[Exception] = []
        self.addCleanup(self.server.stop)

    def client(self, **changes: Any) -> Client:
        changes.setdefault("on_error", self.errors.append)
        client = make_client(self.server, **changes)
        self.addCleanup(client.close)
        return client

    def codes(self) -> list[str | None]:
        return [getattr(e, "code", None) for e in self.errors]


class PollingTests(SyncTestCase):
    def test_polling_picks_up_a_new_ruleset_and_sends_the_etag_back(self) -> None:
        self.server.on(
            "GET",
            RULESET,
            reply(200, ruleset_json(flag_json("f", fallthrough=0)), ETag='"v1"'),
            reply(200, ruleset_json(flag_json("f", fallthrough=1)), ETag='"v2"'),
            reply(304),
        )
        client = self.client()
        self.assertTrue(client.ready())
        self.assertTrue(wait_for(lambda: client.status().etag == '"v2"'))
        self.assertIs(client.variation("f", {"key": "u"}, None), True)
        self.assertTrue(wait_for(lambda: len(self.server.seen("GET", RULESET)) >= 3))
        sent = [r.headers.get("if-none-match") for r in self.server.seen("GET", RULESET)]
        self.assertEqual(sent[:3], [None, '"v1"', '"v2"'])

    def test_the_poll_interval_is_the_gap_when_the_stream_is_off(self) -> None:
        self.server.on("GET", RULESET, reply(200, ruleset_json(flag_json()), ETag='"a"'))
        self.client(poll_interval=0.2)
        time.sleep(0.9)
        count = len(self.server.seen("GET", RULESET))
        self.assertTrue(3 <= count <= 6, count)

    def test_a_dropped_request_is_retried_at_the_next_poll(self) -> None:
        self.server.on(
            "GET",
            RULESET,
            lambda _r, c: c.close(),
            reply(200, ruleset_json(flag_json()), ETag='"a"'),
        )
        client = self.client()
        self.assertFalse(client.ready())
        self.assertTrue(wait_for(lambda: client.status().etag == '"a"'))
        self.assertIsNone(client.status().last_error)
        self.assertEqual(self.codes()[:1], ["network"])


class RetryAfterTests(SyncTestCase):
    def hold_after(self, status: int, header: str | None) -> float:
        server = FakeServer()
        self.addCleanup(server.stop)
        headers = {} if header is None else {"Retry_After": header}
        server.on("GET", RULESET, reply(status, **headers))
        client = make_client(server, poll_interval=3600)
        self.addCleanup(client.close)
        client.ready()
        return client._hold.remaining()

    def test_only_a_429_with_whole_seconds_counts(self) -> None:
        self.assertAlmostEqual(self.hold_after(429, "5"), 5, delta=0.5)
        self.assertAlmostEqual(self.hold_after(429, " 7 "), 7, delta=0.5)
        for header in (
            "abc",
            "-1",
            "1.5",
            "",
            "+5",
            "5s",
            "Wed, 21 Oct 2026 07:28:00 GMT",
            "\u0665",
        ):
            with self.subTest(header=header):
                self.assertEqual(self.hold_after(429, header), 0)
        for status in (500, 503, 408, 404):
            with self.subTest(status=status):
                self.assertEqual(self.hold_after(status, "5"), 0)

    def test_the_hold_is_capped_at_300_seconds(self) -> None:
        self.assertAlmostEqual(self.hold_after(429, "1000"), 300, delta=1)
        self.assertAlmostEqual(self.hold_after(429, "9" * 5000), 300, delta=1)

    def test_a_429_on_the_first_load_is_not_waited_on_but_still_holds(self) -> None:
        self.server.on("GET", RULESET, reply(429, Retry_After="60"))
        client = self.client(poll_interval=0.05)
        started = time.monotonic()
        self.assertFalse(client.ready())
        self.assertLess(time.monotonic() - started, 1.0)
        self.assertEqual(client.status().last_error, "http_429")
        time.sleep(0.4)
        self.assertEqual(len(self.server.seen("GET", RULESET)), 1)

    def test_a_429_with_no_retry_after_does_not_hold(self) -> None:
        self.server.on("GET", RULESET, reply(429))
        self.client(poll_interval=0.05)
        self.assertTrue(wait_for(lambda: len(self.server.seen("GET", RULESET)) >= 3))

    def test_the_hold_only_lengthens(self) -> None:
        now = [0.0]
        hold = Hold(lambda: now[0])
        hold.raise_to(10)
        hold.raise_to(3)
        self.assertEqual(hold.remaining(), 10)
        now[0] = 4
        self.assertEqual(hold.remaining(), 6)
        hold.raise_to(20)
        self.assertEqual(hold.remaining(), 20)
        now[0] = 100
        self.assertEqual(hold.remaining(), 0)

    def test_the_stream_routes_hold_is_capped_at_300_seconds(self) -> None:
        self.server.on("GET", RULESET, reply(200, ruleset_json(flag_json()), ETag='"a"'))
        self.server.on("GET", STREAM, reply(429, Retry_After="1000"))
        client = self.client(stream=True, poll_interval=0.05)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().last_error == "http_429"))
        self.assertAlmostEqual(client._hold.remaining(), 300, delta=2)

    def test_a_429_on_the_stream_route_holds_the_ruleset_route_too(self) -> None:
        self.server.on("GET", RULESET, reply(200, ruleset_json(flag_json()), ETag='"a"'))
        self.server.on("GET", STREAM, reply(429, Retry_After="60"))
        client = self.client(stream=True, poll_interval=0.05)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().last_error == "http_429"))
        self.assertGreater(client._hold.remaining(), 55)
        before = len(self.server.seen("GET", RULESET))
        time.sleep(0.4)
        self.assertEqual(len(self.server.seen("GET", RULESET)), before)
        self.assertEqual(len(self.server.seen("GET", STREAM)), 1)


class StreamTests(SyncTestCase):
    def serve_ruleset(self, *answers: Any) -> None:
        self.server.on("GET", RULESET, *answers)

    def test_connected_sets_the_state_and_clears_the_error(self) -> None:
        self.serve_ruleset(reply(500), reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, stream(event("connected", '"v1"')))
        client = self.client(stream=True, poll_interval=0.05)
        self.assertFalse(client.ready())
        self.assertTrue(wait_for(lambda: client.status().stream == StreamState.CONNECTED))
        status = client.status()
        self.assertIsNone(status.last_error)
        self.assertEqual(status.etag, '"v1"')
        request = self.server.seen("GET", STREAM)[0]
        self.assertEqual(request.headers["accept"], "text/event-stream")
        self.assertEqual(request.headers["authorization"], "Bearer test-key")

    def test_a_stream_starts_in_connecting_and_a_stream_off_client_reads_off(self) -> None:
        self.serve_ruleset(stalled())
        self.assertEqual(
            self.client(stream=True, request_timeout=5).status().stream, StreamState.CONNECTING
        )
        self.assertEqual(
            self.client(stream=False, request_timeout=5).status().stream, StreamState.OFF
        )

    def test_a_changed_event_with_a_new_tag_fetches_and_swaps(self) -> None:
        self.serve_ruleset(
            reply(200, ruleset_json(flag_json("f", fallthrough=0)), ETag='"v1"'),
            reply(200, ruleset_json(flag_json("f", fallthrough=1)), ETag='"v2"'),
        )
        self.server.on(
            "GET", STREAM, stream(event("connected", '"v1"'), event("changed", '"v2"'), gap=0.1)
        )
        client = self.client(stream=True, poll_interval=3600)
        self.assertTrue(client.ready())
        self.assertTrue(wait_for(lambda: client.status().etag == '"v2"'))
        self.assertIs(client.variation("f", {"key": "u"}, None), True)
        self.assertEqual(client.status().stream, StreamState.CONNECTED)

    def test_a_tag_equal_to_the_one_held_moves_last_sync_and_a_keep_alive_does_not(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        pieces = [event("connected", '"v1"')]
        self.server.on("GET", STREAM, self.paced(pieces))
        client = self.client(stream=True, poll_interval=3600)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().stream == StreamState.CONNECTED))
        first = client.status().last_sync_at
        self.connection.send(chunk(": keep-alive\n\n"))
        time.sleep(0.3)
        self.assertEqual(client.status().last_sync_at, first)
        self.connection.send(event("changed", '"v1"'))
        self.assertTrue(wait_for(lambda: client.status().last_sync_at != first))
        self.assertEqual(len(self.server.seen("GET", RULESET)), 1)

    def paced(self, pieces: list[bytes]) -> Any:
        from support import STREAM_HEAD

        def handle(_request: Any, connection: Any) -> None:
            self.connection = connection
            connection.send(STREAM_HEAD)
            for piece in pieces:
                connection.send(piece)
            connection.wait_closed(20)
            connection.close()

        return handle

    def test_a_failed_fetch_asked_for_by_changed_ends_the_stream(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'), reply(500))
        self.server.on("GET", STREAM, stream(event("connected", '"v1"'), event("changed", '"v2"')))
        client = self.client(stream=True, poll_interval=3600, request_timeout=2)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().last_error == "http_500"))
        self.assertTrue(wait_for(lambda: client.status().stream == StreamState.DISCONNECTED))
        self.assertEqual(client.status().etag, '"v1"')

    def test_a_non_200_open_is_http_status_and_disconnected(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, reply(503))
        client = self.client(stream=True, poll_interval=3600)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().last_error == "http_503"))
        self.assertEqual(client.status().stream, StreamState.DISCONNECTED)

    def test_the_failed_stream_falls_back_to_polling(self) -> None:
        self.serve_ruleset(
            reply(200, ruleset_json(flag_json("f", fallthrough=0)), ETag='"v1"'),
            reply(200, ruleset_json(flag_json("f", fallthrough=1)), ETag='"v2"'),
        )
        self.server.on("GET", STREAM, reply(500))
        client = self.client(stream=True, poll_interval=0.1, random=lambda: 0.0)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().etag == '"v2"', 6))
        self.assertGreaterEqual(len(self.server.seen("GET", STREAM)), 1)

    def test_a_clean_end_leaves_the_last_error_alone(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, stream(then="end"))
        client = self.client(stream=True, poll_interval=3600)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().stream == StreamState.DISCONNECTED))
        self.assertIsNone(client.status().last_error)
        self.assertEqual(self.errors, [])

    def test_an_abrupt_end_mid_chunk_is_network(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, stream(b"20\r\nevent: conn", then="cut"))
        client = self.client(stream=True, poll_interval=3600)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().last_error == "network"))
        self.assertEqual(client.status().stream, StreamState.DISCONNECTED)

    def test_opening_is_bounded_by_the_request_timeout(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, stalled())
        client = self.client(stream=True, poll_interval=3600, request_timeout=0.3)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().last_error == "timeout"))
        self.assertEqual(client.status().stream, StreamState.DISCONNECTED)

    def test_a_quiet_stream_is_given_up_on_by_the_watchdog(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, stream(event("connected", '"v1"')))
        client = self.client(stream=True, poll_interval=3600, stream_idle=0.4)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().stream == StreamState.CONNECTED))
        started = time.monotonic()
        self.assertTrue(wait_for(lambda: client.status().last_error == "timeout"))
        self.assertTrue(0.25 <= time.monotonic() - started <= 2.0)
        self.assertEqual(client.status().stream, StreamState.DISCONNECTED)

    def test_every_read_restarts_the_watchdog(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        ticks = [event("connected", '"v1"')] + [chunk(": ka\n\n")] * 12
        self.server.on("GET", STREAM, stream(*ticks, gap=0.1))
        client = self.client(stream=True, poll_interval=3600, stream_idle=0.4)
        client.ready()
        end = time.monotonic() + 1.1
        while time.monotonic() < end:
            self.assertIsNone(client.status().last_error)
            time.sleep(0.05)
        self.assertEqual(client.status().stream, StreamState.CONNECTED)

    def test_events_with_other_names_are_ignored(self) -> None:
        self.serve_ruleset(reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, stream(event("surprise", "x"), event("connected", '"v1"')))
        client = self.client(stream=True, poll_interval=3600)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().stream == StreamState.CONNECTED))
        self.assertEqual(self.errors, [])


class CloseTests(SyncTestCase):
    def test_close_ends_a_stream_at_once_and_reads_closed(self) -> None:
        self.server.on("GET", RULESET, reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, stream(event("connected", '"v1"')))
        client = self.client(stream=True, poll_interval=3600)
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().stream == StreamState.CONNECTED))
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 1.0)
        self.assertEqual(client.status().stream, StreamState.CLOSED)
        self.assertEqual(self.errors, [])

    def test_after_close_the_held_ruleset_answers_and_nothing_is_requested(self) -> None:
        self.server.on("GET", RULESET, reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        client = self.client(poll_interval=0.05)
        client.ready()
        client.close()
        seen = len(self.server.requests)
        time.sleep(0.4)
        self.assertEqual(len(self.server.requests), seen)
        self.assertIs(client.variation("f", {"key": "u"}, False), True)
        self.assertEqual(client.status().stream, StreamState.CLOSED)
        self.assertEqual(client.status().etag, '"v1"')

    def test_close_is_idempotent_and_ready_settles_false(self) -> None:
        self.server.on("GET", RULESET, stalled())
        client = self.client(request_timeout=5)
        started = time.monotonic()
        client.close()
        client.close()
        self.assertLess(time.monotonic() - started, 2.0)
        self.assertFalse(client.ready())

    def test_a_failure_caused_by_close_is_not_recorded(self) -> None:
        self.server.on("GET", RULESET, reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on("GET", STREAM, stalled())
        client = self.client(stream=True, poll_interval=3600, request_timeout=5)
        client.ready()
        time.sleep(0.2)
        client.close()
        self.assertIsNone(client.status().last_error)
        self.assertEqual(self.errors, [])

    def test_close_is_bounded_while_a_ruleset_request_hangs(self) -> None:
        self.server.on("GET", RULESET, stalled())
        client = self.client(request_timeout=0.5)
        time.sleep(0.1)
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 1.0)


if __name__ == "__main__":
    unittest.main()
