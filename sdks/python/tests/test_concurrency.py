from __future__ import annotations

import subprocess
import sys
import threading
import time
import unittest
from pathlib import Path
from typing import Any

import toggleslash
from support import KEY, FakeServer, flag, wait_until
from toggleslash import StreamState

CTX = {"key": "u1"}
SRC = str(Path(__file__).resolve().parent.parent / "src")


class ConcurrencyTests(unittest.TestCase):
    def setUp(self) -> None:
        self.server = FakeServer()

    def tearDown(self) -> None:
        self.server.close()

    def test_every_operation_races_with_close(self) -> None:
        client = toggleslash.create(
            self.server.url, KEY, poll_interval=0.01, flush_interval=0.01, request_timeout=1
        )
        client.ready()
        failures: list[BaseException] = []
        stop = threading.Event()
        barrier = threading.Barrier(13)

        def worker(n: int) -> None:
            barrier.wait()
            try:
                while not stop.is_set():
                    client.variation("checkout", CTX, False)
                    client.variation_detail("legacy", {"key": f"k{n}"}, True)
                    client.int_variation("checkout", CTX, 1)
                    client.status()
                    client.ready()
                    time.sleep(0.0005)
            except BaseException as error:
                failures.append(error)

        threads = [threading.Thread(target=worker, args=(i,), daemon=True) for i in range(12)]
        for thread in threads:
            thread.start()
        barrier.wait()
        time.sleep(0.3)
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 3)
        time.sleep(0.1)
        stop.set()
        for thread in threads:
            thread.join(5)
            self.assertFalse(thread.is_alive())
        self.assertEqual(failures, [])
        self.assertEqual(client.status().stream, StreamState.CLOSED)
        self.assertTrue(client.variation("checkout", CTX, False))

    def test_two_closes_race(self) -> None:
        for _ in range(10):
            client = toggleslash.create(
                self.server.url, KEY, poll_interval=0.01, flush_interval=0.01, request_timeout=1
            )
            client.ready()
            client.variation("checkout", CTX, False)
            barrier = threading.Barrier(8)
            done: list[float] = []

            def closer() -> None:
                barrier.wait()
                started = time.monotonic()
                client.close()
                done.append(time.monotonic() - started)

            threads = [threading.Thread(target=closer, daemon=True) for _ in range(8)]
            for thread in threads:
                thread.start()
            for thread in threads:
                thread.join(6)
                self.assertFalse(thread.is_alive())
            self.assertEqual(len(done), 8)
            self.assertLess(max(done), 3)
            self.assertEqual(client.status().stream, StreamState.CLOSED)

    def test_variation_returns_while_the_ruleset_request_hangs(self) -> None:
        self.server.on("ruleset", lambda r: r.hang(5))
        client = toggleslash.create(self.server.url, KEY, stream=False, request_timeout=2)
        try:
            worst = 0.0
            for _ in range(200):
                started = time.monotonic()
                client.variation("checkout", CTX, False)
                client.status()
                worst = max(worst, time.monotonic() - started)
            self.assertLess(worst, 0.25)
        finally:
            started = time.monotonic()
            client.close()
            self.assertLess(time.monotonic() - started, 4)

    def test_variation_returns_while_a_flush_hangs(self) -> None:
        self.server.on("events", lambda r: r.hang(5))
        client = toggleslash.create(
            self.server.url, KEY, stream=False, poll_interval=30, flush_interval=0.02, request_timeout=1.5
        )
        try:
            self.assertTrue(client.ready())
            client.variation("checkout", CTX, False)
            self.assertTrue(wait_until(lambda: self.server.count("events") >= 1))
            worst = 0.0
            for _ in range(300):
                started = time.monotonic()
                client.variation("checkout", CTX, False)
                worst = max(worst, time.monotonic() - started)
            self.assertLess(worst, 0.25)
        finally:
            started = time.monotonic()
            client.close()
            self.assertLess(time.monotonic() - started, 4)

    def test_a_hook_sees_the_etag_and_version_of_one_ruleset(self) -> None:
        self.server.bump(flag("f", [False, True], 1))
        seen: list[tuple[str | None, int | None]] = []
        lock = threading.Lock()

        def hook(event: toggleslash.EvaluationEvent) -> None:
            with lock:
                seen.append((event.etag, event.flag_version))

        client = toggleslash.create(
            self.server.url,
            KEY,
            stream=False,
            poll_interval=0.001,
            hooks=toggleslash.Hooks(after_evaluation=hook),
        )
        try:
            self.assertTrue(client.ready())
            stop = threading.Event()

            def publisher() -> None:
                while not stop.is_set():
                    self.server.bump(flag("f", [False, True], 1))
                    time.sleep(0.001)

            def reader() -> None:
                while not stop.is_set():
                    client.variation("f", CTX, False)
                    time.sleep(0.0005)

            threads = [threading.Thread(target=publisher, daemon=True)] + [
                threading.Thread(target=reader, daemon=True) for _ in range(6)
            ]
            for thread in threads:
                thread.start()
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                with lock:
                    if len({v for _, v in seen}) > 5:
                        break
                time.sleep(0.05)
            stop.set()
            for thread in threads:
                thread.join(5)
            with lock:
                versions = {v for _, v in seen}
            self.assertGreater(len(versions), 5, "the ruleset never changed under the readers")
            with lock:
                pairs = list(seen)
            for etag, version in pairs:
                if version is not None:
                    self.assertEqual(etag, f'"v{version}"')
        finally:
            client.close()

    def test_callbacks_may_use_the_client_without_deadlock(self) -> None:
        self.server.on("ruleset", lambda r: r.send(500))
        calls: list[Any] = []
        holder: dict[str, toggleslash.Client] = {}

        def on_error(error: BaseException) -> None:
            client = holder["c"]
            calls.append((client.ready(), client.status().last_error, client.variation("x", CTX, 5)))
            client.close()
            calls.append("closed")

        client = toggleslash.create(self.server.url, KEY, stream=False, on_error=on_error)
        holder["c"] = client
        self.assertTrue(wait_until(lambda: "closed" in calls))
        self.assertEqual(calls[0], (False, "http_500", 5))
        client.close()

    def test_a_hook_may_use_the_client(self) -> None:
        depth = [0]
        holder: dict[str, toggleslash.Client] = {}

        def hook(event: toggleslash.EvaluationEvent) -> None:
            if depth[0] == 0:
                depth[0] += 1
                holder["c"].variation("legacy", CTX, True)
                holder["c"].status()
                holder["c"].ready()

        client = toggleslash.create(
            self.server.url,
            KEY,
            stream=False,
            poll_interval=30,
            hooks=toggleslash.Hooks(after_evaluation=hook),
        )
        holder["c"] = client
        try:
            self.assertTrue(client.ready())
            self.assertTrue(client.variation("checkout", CTX, False))
        finally:
            client.close()

    def test_ready_is_fixed_and_false_after_close_if_unsettled(self) -> None:
        self.server.on("ruleset", lambda r: r.hang(5))
        client = toggleslash.create(self.server.url, KEY, stream=False, request_timeout=3)
        results: list[bool] = []
        waiter = threading.Thread(target=lambda: results.append(client.ready()), daemon=True)
        waiter.start()
        time.sleep(0.2)
        client.close()
        waiter.join(5)
        self.assertEqual(results, [False])
        self.assertFalse(client.ready())

    def test_ready_answer_is_fixed_once_settled(self) -> None:
        client = toggleslash.create(self.server.url, KEY, stream=False, poll_interval=0.02)
        try:
            self.assertTrue(client.ready())
            self.server.on("ruleset", lambda r: r.send(500))
            time.sleep(0.1)
            self.assertTrue(client.ready())
        finally:
            client.close()

    def test_daemon_threads_do_not_keep_the_process_alive(self) -> None:
        script = (
            "import sys, toggleslash\n"
            "c = toggleslash.create(sys.argv[1], 'k', flush_interval=0.05)\n"
            "c.variation('checkout', {'key': 'u'}, False)\n"
        )
        hang = FakeServer()
        hang.on("ruleset", lambda r: r.hang(30))
        try:
            for url in ("http://127.0.0.1:9", self.server.url, hang.url):
                started = time.monotonic()
                done = subprocess.run(
                    [sys.executable, "-X", "dev", "-W", "error", "-c", script, url],
                    env={"PYTHONPATH": SRC, "PYTHONDONTWRITEBYTECODE": "1"},
                    capture_output=True,
                    check=False,
                    timeout=20,
                )
                self.assertEqual(done.returncode, 0, done.stderr.decode())
                self.assertLess(time.monotonic() - started, 8, url)
        finally:
            hang.close()


if __name__ == "__main__":
    unittest.main()
