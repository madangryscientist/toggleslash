import threading
import time
import unittest
from typing import Any

from support import FakeReply, FakeTime, LocalServer, ScriptedTransport, flag, hang, ruleset_body, until
from toggleslash import StreamState
from toggleslash._http import HttpTransport, TransportError
from toggleslash._seams import Seams
from toggleslash._sync import Sync
from toggleslash._types import NETWORK

BODY = ruleset_body(flag("f", True))
CONNECTED = b'event: connected\ndata: "e1"\n\n'


def ruleset(etag: str = '"e1"', body: bytes = BODY):
    return lambda *_: FakeReply(200, headers={"ETag": etag}, body=body)


def status(code: int, retry_after: str | None = None):
    headers = {"Retry-After": retry_after} if retry_after is not None else None
    return lambda *_: FakeReply(code, headers=headers)


def stream(*chunks: bytes):
    return lambda *_: FakeReply(200, chunks=list(chunks))


class Rig:
    """A Sync run on its own thread against a scripted transport and a clock that does not sleep."""

    def __init__(
        self, *, stop_after: int | None = 1, on_stream: bool = True, jitter: float = 1.0, **routes: Any
    ) -> None:
        self.time = FakeTime()
        self.time.stop_after = stop_after
        self.transport = ScriptedTransport(**routes)
        self.errors: list[BaseException] = []
        self.stop = threading.Event()
        seams = Seams(monotonic=self.time.monotonic, wall=self.time.wall, wait=self.waiter, jitter=lambda: jitter)
        self.sync = Sync(
            self.transport,
            "sdk-key",
            stream=on_stream,
            poll_interval=30.0,
            request_timeout=5.0,
            report=self.errors.append,
            stop=self.stop,
            seams=seams,
        )

    def waiter(self, seconds: float) -> bool:
        stopped = self.time.wait(seconds)
        if stopped:
            self.stop.set()
        return stopped

    def run(self) -> "Rig":
        self.sync.start()
        self.sync._thread.join(10)
        assert not self.sync._thread.is_alive(), "the sync loop did not stop"
        return self


class FirstLoadTests(unittest.TestCase):
    def test_the_first_load_is_not_held_and_sends_only_the_allowed_headers(self) -> None:
        rig = Rig(on_stream=False, ruleset=ruleset()).run()
        request = rig.transport.requests[0]
        self.assertEqual((request["method"], request["route"]), ("GET", "/sdk/v1/ruleset"))
        self.assertEqual(request["headers"], {"Authorization": "Bearer sdk-key"})
        self.assertTrue(rig.sync.ready())
        self.assertEqual(rig.sync.status().etag, '"e1"')

    def test_a_poll_sends_if_none_match_and_a_304_keeps_the_ruleset(self) -> None:
        rig = Rig(on_stream=False, stop_after=2, ruleset=[ruleset(), status(304)]).run()
        self.assertEqual(rig.transport.requests[1]["headers"]["If-None-Match"], '"e1"')
        held = rig.sync.held
        self.assertIsNotNone(held)
        self.assertEqual(rig.sync.status().last_error, None)
        self.assertEqual(rig.time.waits, [30.0, 30.0])

    def test_a_failed_first_load_is_not_ready_and_is_recorded(self) -> None:
        rig = Rig(on_stream=False, ruleset=status(503)).run()
        self.assertFalse(rig.sync.ready())
        self.assertEqual(rig.sync.status().last_error, "http_503")
        self.assertEqual(len(rig.errors), 1)

    def test_a_bad_body_leaves_the_last_good_ruleset(self) -> None:
        rig = Rig(on_stream=False, stop_after=2, ruleset=[ruleset(), ruleset('"e2"', b"[1]")]).run()
        self.assertEqual(rig.sync.status().etag, '"e1"')
        self.assertEqual(rig.sync.status().last_error, "bad_ruleset")

    def test_a_network_failure_is_recorded_as_network(self) -> None:
        def broken(*_: Any):
            raise TransportError(NETWORK, "no route")

        rig = Rig(on_stream=False, ruleset=broken).run()
        self.assertEqual(rig.sync.status().last_error, "network")


class LoopTests(unittest.TestCase):
    def test_a_stream_that_never_connected_backs_off_and_polls_once(self) -> None:
        rig = Rig(stop_after=5, ruleset=ruleset(), stream=status(503)).run()
        self.assertEqual(rig.time.waits[:4], [2.0, 4.0, 8.0, 16.0])
        self.assertGreaterEqual(rig.transport.count("ruleset"), 3)

    def test_the_delay_is_the_backoff_ceiling_times_half_to_one_by_jitter(self) -> None:
        low = Rig(stop_after=4, jitter=0.0, ruleset=ruleset(), stream=status(503)).run()
        high = Rig(stop_after=4, jitter=1.0, ruleset=ruleset(), stream=status(503)).run()
        self.assertEqual(low.time.waits[0], 1.0)
        self.assertEqual(high.time.waits[0], 2.0)
        self.assertEqual(low.time.waits[0] * 2, high.time.waits[0])

    def test_the_backoff_is_capped_by_the_poll_interval(self) -> None:
        rig = Rig(stop_after=14, ruleset=ruleset(), stream=status(503)).run()
        self.assertEqual(max(rig.time.waits), 30.0)

    def test_a_stream_that_connected_and_dropped_does_not_poll(self) -> None:
        rig = Rig(stop_after=1, ruleset=ruleset(), stream=stream(CONNECTED)).run()
        self.assertEqual(rig.transport.count("ruleset"), 1)
        self.assertEqual(rig.time.waits, [1.0])
        self.assertEqual(rig.sync.status().stream, StreamState.CLOSED if rig.sync._closed else StreamState.DISCONNECTED)

    def test_connected_marks_the_stream_and_clears_the_error(self) -> None:
        rig = Rig(stop_after=1, ruleset=[ruleset()], stream=stream(CONNECTED)).run()
        self.assertIsNone(rig.sync.status().last_error)

    def test_a_changed_event_with_a_new_tag_fetches_and_the_same_tag_does_not(self) -> None:
        rig = Rig(
            stop_after=1,
            ruleset=[ruleset('"e1"'), ruleset('"e2"')],
            stream=stream(CONNECTED + b'event: changed\ndata: "e1"\n\n' + b'event: changed\ndata: "e2"\n\n'),
        ).run()
        self.assertEqual(rig.transport.count("ruleset"), 2)
        self.assertEqual(rig.sync.status().etag, '"e2"')

    def test_a_failed_fetch_for_a_changed_event_ends_the_stream(self) -> None:
        rig = Rig(
            stop_after=1,
            ruleset=[ruleset('"e1"'), status(500)],
            stream=stream(CONNECTED + b'event: changed\ndata: "e2"\n\n' + b'event: changed\ndata: "e3"\n\n'),
        ).run()
        self.assertEqual(rig.transport.count("ruleset"), 2)
        self.assertEqual(rig.sync.status().last_error, "http_500")

    def test_the_stream_request_carries_accept_and_authorization_only(self) -> None:
        rig = Rig(stop_after=1, ruleset=ruleset(), stream=stream(CONNECTED)).run()
        request = next(r for r in rig.transport.requests if r["route"].endswith("stream"))
        self.assertEqual(request["headers"], {"Authorization": "Bearer sdk-key", "Accept": "text/event-stream"})

    def test_unknown_events_are_ignored(self) -> None:
        rig = Rig(stop_after=1, ruleset=ruleset(), stream=stream(b"event: other\ndata: x\n\n" + CONNECTED)).run()
        self.assertEqual(rig.transport.count("ruleset"), 1)


class RetryAfterTests(unittest.TestCase):
    def test_a_ruleset_429_holds_the_next_poll(self) -> None:
        rig = Rig(on_stream=False, ruleset=[ruleset(), status(429, "100")]).run()
        self.assertEqual(rig.time.waits, [30.0])
        rig = Rig(on_stream=False, stop_after=2, ruleset=[status(429, "100"), ruleset()]).run()
        self.assertEqual(rig.time.waits[0], 100.0)

    def test_the_ruleset_hold_is_capped_at_300(self) -> None:
        rig = Rig(on_stream=False, ruleset=status(429, "999999")).run()
        self.assertEqual(rig.time.waits, [300.0])

    def test_the_stream_hold_is_capped_at_300(self) -> None:
        rig = Rig(ruleset=ruleset(), stream=status(429, "86400")).run()
        self.assertEqual(rig.time.waits, [300.0])

    def test_a_stream_429_holds_the_ruleset_route_too(self) -> None:
        rig = Rig(stop_after=2, ruleset=ruleset(), stream=[status(429, "200"), status(503)]).run()
        self.assertEqual(rig.time.waits[0], 200.0)
        self.assertEqual(rig.time.waits[1], 4.0)
        self.assertEqual(rig.transport.count("ruleset"), 2)

    def test_a_retry_after_that_is_not_whole_digits_is_ignored(self) -> None:
        for value in ("1.5", "-5", "soon", "Wed, 21 Oct 2026 07:28:00 GMT"):
            with self.subTest(value=value):
                rig = Rig(on_stream=False, ruleset=status(429, value)).run()
                self.assertEqual(rig.time.waits, [30.0])

    def test_only_a_429_holds(self) -> None:
        rig = Rig(on_stream=False, ruleset=status(503, "100")).run()
        self.assertEqual(rig.time.waits, [30.0])


class CloseTests(unittest.TestCase):
    def test_begin_close_abandons_a_hung_request_and_reports_nothing(self) -> None:
        def hanging(method: str, headers: Any, body: Any, aborter: Any):
            hang(aborter)
            return FakeReply()

        errors: list[BaseException] = []
        stop = threading.Event()
        sync = Sync(
            ScriptedTransport(ruleset=hanging),
            "k",
            stream=False,
            poll_interval=30.0,
            request_timeout=60.0,
            report=errors.append,
            stop=stop,
            seams=Seams(),
        )
        sync.start()
        time.sleep(0.05)
        began = time.monotonic()
        sync.begin_close()
        stop.set()
        sync.join()
        self.assertLess(time.monotonic() - began, 2.0)
        self.assertEqual(errors, [])
        self.assertFalse(sync.ready())
        self.assertEqual(sync.status().stream, StreamState.CLOSED)


class WatchdogTests(unittest.TestCase):
    """The 60 s watchdog through the `watchdog` seam, on a real socket."""

    def handler(self, pings: int, gap: float):
        def handle(request: Any) -> None:
            if request.path.endswith("/ruleset"):
                body = BODY
                request.send_response(200)
                request.send_header("ETag", '"e1"')
                request.send_header("Content-Length", str(len(body)))
                request.end_headers()
                request.wfile.write(body)
                return
            request.send_response(200)
            request.send_header("Content-Type", "text/event-stream")
            request.send_header("Connection", "close")
            request.end_headers()
            request.close_connection = True
            request.wfile.write(CONNECTED)
            request.wfile.flush()
            for _ in range(pings):
                time.sleep(gap)
                request.wfile.write(b": ping\n\n")
                request.wfile.flush()
            time.sleep(30)

        return handle

    def run_sync(self, server: LocalServer, watchdog: float) -> tuple[Sync, list[BaseException], threading.Event]:
        errors: list[BaseException] = []
        stop = threading.Event()
        sync = Sync(
            HttpTransport(server.url),
            "k",
            stream=True,
            poll_interval=30.0,
            request_timeout=5.0,
            report=errors.append,
            stop=stop,
            seams=Seams(watchdog=watchdog),
        )
        sync.start()
        return sync, errors, stop

    def test_a_quiet_stream_is_dropped_as_a_timeout(self) -> None:
        with LocalServer(self.handler(0, 0)) as server:
            sync, errors, stop = self.run_sync(server, 0.3)
            try:
                until(
                    lambda: any(getattr(error, "code", None) == "timeout" for error in errors),
                    5,
                    "the watchdog never fired",
                )
            finally:
                sync.begin_close()
                stop.set()
                sync.join()

    def test_keep_alive_comments_restart_the_watchdog(self) -> None:
        with LocalServer(self.handler(8, 0.1)) as server:
            sync, _errors, stop = self.run_sync(server, 0.4)
            try:
                until(lambda: sync.status().stream == StreamState.CONNECTED, 5, "never connected")
                time.sleep(0.75)  # past the watchdog, kept alive by the pings
                self.assertEqual(sync.status().stream, StreamState.CONNECTED)
                self.assertIsNone(sync.status().last_error)
            finally:
                sync.begin_close()
                stop.set()
                sync.join()

    def test_the_watchdog_seam_reaches_the_reader(self) -> None:
        reply = FakeReply(200, chunks=[CONNECTED])
        rig = Rig(stop_after=1, ruleset=ruleset(), stream=lambda *_: reply)
        rig.sync._watchdog = 7.5
        rig.run()
        self.assertEqual(reply.watchdogs[0], 7.5)
        self.assertEqual(Seams().watchdog, 60.0)


if __name__ == "__main__":
    unittest.main()
