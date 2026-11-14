"""Concurrency: there is no race detector in Python, so these start many threads together on a Barrier and
join them with a timeout, and check the invariants the contract states."""

from __future__ import annotations

import subprocess
import sys
import threading
import time
import unittest
from pathlib import Path
from typing import Any

from support import (
    FakeServer,
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

RULESET = "/sdk/v1/ruleset"
STREAM = "/sdk/v1/stream"
EVENTS = "/sdk/v1/events"
CTX = {"key": "u"}
HERE = Path(__file__).resolve().parent


def run_threads(count: int, body: Any, timeout: float = 15.0) -> list[BaseException]:
    """Runs `body(i)` on `count` threads released together; returns what they raised. Fails if one hangs."""
    barrier = threading.Barrier(count)
    failures: list[BaseException] = []

    def target(i: int) -> None:
        try:
            barrier.wait(5)
            body(i)
        except BaseException as exc:
            failures.append(exc)

    threads = [threading.Thread(target=target, args=(i,), daemon=True) for i in range(count)]
    for thread in threads:
        thread.start()
    end = time.monotonic() + timeout
    for thread in threads:
        thread.join(max(end - time.monotonic(), 0.0))
    stuck = [t for t in threads if t.is_alive()]
    if stuck:
        raise AssertionError(f"{len(stuck)} threads did not finish within {timeout}s")
    return failures


class ConcurrencyTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.server = FakeServer()
        self.errors: list[Exception] = []
        self.addCleanup(self.server.stop)

    def client(self, *flags: dict[str, Any], **changes: Any) -> Client:
        if flags or not self.server._routes.get(("GET", RULESET)):
            self.server.on(
                "GET", RULESET, reply(200, ruleset_json(*(flags or (flag_json(),))), ETag='"v1"')
            )
        changes.setdefault("on_error", self.errors.append)
        client = make_client(self.server, **changes)
        self.addCleanup(client.close)
        return client


class RaceTests(ConcurrencyTestCase):
    def test_close_racing_evaluation(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client()
        self.assertTrue(client.ready())
        seen: list[Any] = []

        def body(i: int) -> None:
            if i == 0:
                time.sleep(0.01)
                client.close()
                return
            for _ in range(2000):
                seen.append(client.variation("f", CTX, "fb"))
                client.status()

        self.assertEqual(run_threads(8, body), [])
        self.assertTrue(set(map(repr, seen)) <= {"True", "'fb'"})
        self.assertEqual(client.status().stream, StreamState.CLOSED)

    def test_two_closes_race_and_the_final_flush_goes_once(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client()
        client.ready()
        for _ in range(5):
            client.variation("f", CTX, False)
        started = time.monotonic()
        self.assertEqual(run_threads(16, lambda _i: client.close()), [])
        self.assertLess(time.monotonic() - started, 3.0)
        posts = self.server.seen("POST", EVENTS)
        self.assertEqual(len(posts), 1)
        self.assertEqual(posts[0].json()["events"][0]["count"], 5)

    def test_status_never_shows_connected_with_an_error(self) -> None:
        self.server.on("GET", RULESET, reply(200, ruleset_json(flag_json()), ETag='"v1"'))
        self.server.on(
            "GET", STREAM, stream(event("connected", '"v1"'), event("changed", '"v1"'), then="cut")
        )
        client = self.client(stream=True, poll_interval=0.02, random=lambda: 0.0)
        client.ready()
        bad: list[Any] = []
        stop = time.monotonic() + 1.5

        def body(_i: int) -> None:
            while time.monotonic() < stop:
                status = client.status()
                if status.stream == StreamState.CONNECTED and status.last_error is not None:
                    bad.append(status)
                time.sleep(0.0002)  # six spinning readers would starve the I/O threads of the GIL

        self.assertEqual(run_threads(6, body), [])
        self.assertEqual(bad, [])
        self.assertGreater(len(self.server.seen("GET", STREAM)), 1)

    def test_every_operation_from_many_threads_together(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        events: list[Any] = []
        client = self.client(
            flag_json("f", [1, 2, 3], fallthrough=2),
            after_evaluation=events.append,
            flush_interval=0.05,
            poll_interval=0.02,
        )

        def body(i: int) -> None:
            for n in range(300):
                match (i + n) % 5:
                    case 0:
                        client.variation("f", {"key": f"u{n}"}, 0)
                    case 1:
                        client.variation_detail("f", {"key": f"u{n}"}, 0)
                    case 2:
                        client.int_variation("f", {"key": f"u{n}"}, 0)
                    case 3:
                        client.status()
                    case _:
                        client.ready()

        self.assertEqual(run_threads(12, body), [])
        client.close()
        total = sum(
            e["count"] for r in self.server.seen("POST", EVENTS) for e in r.json()["events"]
        )
        served = sum(1 for e in events if e.variation_index is not None)
        self.assertEqual(total, served)
        self.assertEqual(self.errors, [])

    def test_the_etag_and_the_flag_version_always_come_from_one_ruleset(self) -> None:
        a = reply(200, ruleset_json(flag_json("f", version=1)), ETag='"a"')
        b = reply(200, ruleset_json(flag_json("f", version=2)), ETag='"b"')
        self.server.on("GET", RULESET, *([a, b] * 200))
        events: list[Any] = []
        client = self.client(poll_interval=0.001, after_evaluation=events.append)
        client.ready()
        stop = time.monotonic() + 1.0

        def body(_i: int) -> None:
            while time.monotonic() < stop:
                client.variation("f", CTX, False)

        self.assertEqual(run_threads(4, body), [])
        pairs = {(e.etag, e.flag_version) for e in events}
        self.assertTrue(pairs <= {('"a"', 1), ('"b"', 2)}, pairs)
        self.assertEqual(len(pairs), 2)


class BlockingTests(ConcurrencyTestCase):
    def latency(self, client: Client) -> float:
        worst = 0.0
        for _ in range(50):
            started = time.monotonic()
            client.variation("f", CTX, False)
            client.variation_detail("f", CTX, False)
            client.status()
            worst = max(worst, time.monotonic() - started)
        return worst

    def test_evaluation_does_not_wait_for_a_hanging_ruleset_request(self) -> None:
        self.server.on(
            "GET", RULESET, reply(200, ruleset_json(flag_json()), ETag='"v1"'), stalled()
        )
        client = self.client(poll_interval=0.01, request_timeout=5)
        client.ready()
        time.sleep(0.2)
        self.assertLess(self.latency(client), 0.1)

    def test_evaluation_does_not_wait_for_a_hanging_flush(self) -> None:
        self.server.on("POST", EVENTS, stalled())
        client = self.client(request_timeout=0.5)
        client.ready()
        client.variation("f", CTX, False)
        flusher = threading.Thread(target=client._usage.flush, daemon=True)
        flusher.start()
        self.assertTrue(wait_for(lambda: len(self.server.seen("POST", EVENTS)) == 1))
        self.assertLess(self.latency(client), 0.1)
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 2.5)
        flusher.join(8)

    def test_evaluation_before_the_first_load_does_not_wait_for_it(self) -> None:
        self.server.on("GET", RULESET, stalled())
        client = self.client(request_timeout=5)
        started = time.monotonic()
        client.variation("f", CTX, False)
        self.assertLess(time.monotonic() - started, 0.1)


class ReentrancyTests(ConcurrencyTestCase):
    def test_a_hook_may_call_back_into_the_client(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        seen: list[Any] = []

        def hook(_event: Any) -> None:
            seen.append((client.status().etag, client.variation("f", CTX, "inner")))

        client = self.client(after_evaluation=hook)
        client.ready()
        client.variation("f", CTX, False)
        self.assertEqual(seen[0], ('"v1"', "inner"))

    def test_a_hook_may_close_the_client(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        client = self.client(after_evaluation=lambda _e: client.close())
        client.ready()
        done = threading.Thread(target=client.variation, args=("f", CTX, False), daemon=True)
        done.start()
        done.join(5)
        self.assertFalse(done.is_alive())
        self.assertEqual(client.status().stream, StreamState.CLOSED)

    def test_on_error_on_an_sdk_thread_may_call_ready_status_and_variation(self) -> None:
        self.server.on("GET", RULESET, reply(503))
        calls: list[Any] = []

        def on_error(_error: Exception) -> None:
            calls.append(
                (client.ready(), client.status().last_error, client.variation("f", CTX, 9))
            )

        client = self.client(on_error=on_error)
        self.assertFalse(client.ready())
        self.assertTrue(wait_for(lambda: calls))
        self.assertEqual(calls[0], (False, "http_503", 9))

    def test_on_error_on_an_sdk_thread_may_close_without_joining_itself(self) -> None:
        took: list[float] = []

        def on_error(_error: Exception) -> None:
            if took:
                return
            started = time.monotonic()
            client.close()
            took.append(time.monotonic() - started)

        client = self.client(on_error=on_error, request_timeout=5.0, flush_interval=3600.0)
        self.assertTrue(client.ready())
        client.variation("f", CTX, False)  # a count is pending, so the final flush goes out
        self.server.on("POST", EVENTS, stalled(30))
        self.server.on("GET", RULESET, reply(500))
        self.assertTrue(wait_for(lambda: took))
        # Contract 6.4: it returns without waiting for the final flush, which hangs for request_timeout.
        self.assertLess(took[0], 1.0)
        self.assertTrue(wait_for(lambda: client.status().stream == StreamState.CLOSED))
        self.assertTrue(wait_for(lambda: self.server.seen("POST", EVENTS)))

    def test_a_close_from_an_sdk_thread_while_the_caller_closes(self) -> None:
        self.server.on("GET", RULESET, reply(503))
        entered = threading.Event()
        release = threading.Event()

        def on_error(_error: Exception) -> None:
            entered.set()
            release.wait(5)
            client.close()

        client = self.client(on_error=on_error, request_timeout=0.5)
        self.assertTrue(entered.wait(5))
        started = time.monotonic()
        threading.Timer(0.1, release.set).start()
        client.close()
        self.assertLess(time.monotonic() - started, 4.0)

    def test_callbacks_run_with_no_sdk_lock_held(self) -> None:
        self.server.on("POST", EVENTS, reply(202))
        results: list[Any] = []

        def hook(_event: Any) -> None:
            thread = threading.Thread(target=lambda: results.append(client.status()), daemon=True)
            thread.start()
            thread.join(2)
            results.append(thread.is_alive())

        client = self.client(after_evaluation=hook)
        client.ready()
        client.variation("f", CTX, False)
        self.assertIs(results[-1], False)


class ProcessTests(unittest.TestCase):
    def test_a_client_that_is_never_closed_does_not_keep_the_process_alive(self) -> None:
        script = (
            "import sys\n"
            "sys.path[:0] = [sys.argv[1], sys.argv[2]]\n"
            "from support import FakeServer, reply, ruleset_json, flag_json, stream, event\n"
            "import toggleslash\n"
            "server = FakeServer()\n"
            "server.on('GET', '/sdk/v1/ruleset', reply(200, ruleset_json(flag_json()), ETag='\"v1\"'))\n"
            "server.on('GET', '/sdk/v1/stream', stream(event('connected', '\"v1\"')))\n"
            "client = toggleslash.create(url=server.url, sdk_key='k', stream=True)\n"
            "assert client.ready()\n"
            "client.variation('f', {'key': 'u'}, False)\n"
            "print('done')\n"
        )
        src = str(HERE.parent / "src")
        started = time.monotonic()
        done = subprocess.run(
            [sys.executable, "-X", "dev", "-W", "error", "-c", script, str(HERE), src],
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertIn("done", done.stdout)
        self.assertLess(time.monotonic() - started, 10.0)


if __name__ == "__main__":
    unittest.main()
