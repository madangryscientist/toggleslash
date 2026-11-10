import json
import shutil
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from pathlib import Path

import toggleslash
from support import CTX, FakeServer, bool_flag, join_all, wait_until
from toggleslash import Hooks, ReasonKind, Seams, StreamState

SRC = Path(__file__).resolve().parents[1] / "src"
RULESET = "/sdk/v1/ruleset"
EVENTS = "/sdk/v1/events"
STREAM = "/sdk/v1/stream"


def ruleset_of(version):
    return {"flags": [bool_flag("checkout", True, version)], "segments": []}


class RobustnessTest(unittest.TestCase):
    def setUp(self):
        self.server = FakeServer()
        self.addCleanup(self.server.stop)

    def made(self, **options):
        errors = []
        options.setdefault("stream", False)
        options.setdefault("request_timeout", 1)
        client = toggleslash.create(
            url=options.pop("url", self.server.url), sdk_key="k", on_error=errors.append, **options
        )
        self.addCleanup(client.close)
        return client, errors

    def test_close_racing_readers_and_a_second_close_finishes(self):
        client, _ = self.made(flush_interval=0.01)
        self.assertTrue(client.ready())
        barrier = threading.Barrier(10)
        outcomes = []

        def read():
            barrier.wait(5)
            for _ in range(300):
                outcomes.append(client.variation("checkout", CTX, False))

        def close():
            barrier.wait(5)
            client.close()

        threads = [threading.Thread(target=read, name=f"reader-{i}") for i in range(8)]
        threads += [threading.Thread(target=close, name=f"closer-{i}") for i in range(2)]
        for thread in threads:
            thread.start()
        join_all(threads, 15)
        self.assertEqual(len(outcomes), 2400)
        self.assertEqual(client.status().stream, StreamState.CLOSED)

    def test_a_read_returns_at_once_while_the_ruleset_request_hangs(self):
        self.server.hang.add(RULESET)
        client, _ = self.made(request_timeout=2)
        started = time.monotonic()
        detail = client.variation_detail("checkout", CTX, False)
        self.assertLess(time.monotonic() - started, 0.5)
        self.assertEqual(detail.reason.kind, ReasonKind.ERROR)
        self.assertFalse(client.ready())  # bounded: it settles false instead of waiting for ever

    def test_reads_stay_fast_and_close_is_bounded_while_usage_requests_hang(self):
        self.server.hang.add(EVENTS)
        client, _ = self.made(request_timeout=1, flush_interval=0.05)
        self.assertTrue(client.ready())
        slowest = 0.0
        for _ in range(40):
            started = time.monotonic()
            client.variation("checkout", CTX, False)
            slowest = max(slowest, time.monotonic() - started)
            time.sleep(0.02)
        self.assertLess(slowest, 0.25)
        wait_until(lambda: self.server.count("POST", EVENTS) >= 1, what="a hung post")
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 2.5)  # about twice the request timeout, no more

    def test_close_is_bounded_while_the_stream_and_ruleset_hang(self):
        client, _ = self.made(request_timeout=1, stream=True, poll_interval=30)
        self.assertTrue(client.ready())
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="a connected stream")
        self.server.hang.update({RULESET, STREAM})
        self.server.drop_streams()
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 2.5)

    def test_the_etag_in_a_hook_event_always_matches_the_flag_version(self):
        self.server.set(ruleset_of(1), '"a"')
        events = []
        client, _ = self.made(stream=True, poll_interval=30, hooks=Hooks(after_evaluation=events.append))
        self.assertTrue(client.ready())
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="a connected stream")
        stop = threading.Event()

        def read():
            while not stop.is_set():
                client.variation("checkout", CTX, False)

        readers = [threading.Thread(target=read, name=f"reader-{i}") for i in range(4)]
        for thread in readers:
            thread.start()
        for round_ in range(20):
            tag, version = ('"b"', 2) if round_ % 2 == 0 else ('"a"', 1)
            self.server.set(ruleset_of(version), tag)
            self.server.publish(tag)
            time.sleep(0.05)
        stop.set()
        join_all(readers, 10)
        seen = {(e.etag, e.flag_version) for e in events}
        self.assertEqual(seen, {('"a"', 1), ('"b"', 2)})

    def test_https_with_a_self_signed_certificate(self):
        openssl = shutil.which("openssl")
        if openssl is None:
            self.skipTest("no openssl command to make a certificate with")
        with tempfile.TemporaryDirectory() as directory:
            cert, key = f"{directory}/cert.pem", f"{directory}/key.pem"
            subprocess.run(
                [
                    openssl,
                    *("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", cert),
                    *("-days", "2", "-subj", "/CN=127.0.0.1", "-addext", "subjectAltName=IP:127.0.0.1"),
                ],
                check=True,
                capture_output=True,
            )
            server_side = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            server_side.load_cert_chain(cert, key)
            trusting = ssl.create_default_context(cafile=cert)
            tls = FakeServer(tls=server_side)
            self.addCleanup(tls.stop)
            client, _ = self.made(url=tls.url, _seams=Seams(ssl_context=trusting), stream=True)
            self.assertTrue(client.ready())
            wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="a stream over tls")
            self.assertTrue(client.variation("checkout", CTX, False))
            # a client that does not trust the certificate does not become ready, and says why
            distrustful, errors = self.made(url=tls.url, request_timeout=1)
            self.assertFalse(distrustful.ready())
            self.assertTrue(errors)

    def test_a_silent_stream_is_dropped_after_the_watchdog_and_reopened(self):
        client, _ = self.made(stream=True, poll_interval=1, _seams=Seams(watchdog=0.4, jitter=lambda: 0.0))
        self.assertTrue(client.ready())
        wait_until(lambda: self.server.count("GET", STREAM) >= 2, 10, what="a second stream request")

    def test_keep_alive_comments_hold_the_watchdog_off(self):
        client, _ = self.made(stream=True, poll_interval=1, _seams=Seams(watchdog=0.5))
        self.assertTrue(client.ready())
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="a connected stream")
        for _ in range(12):  # 1.8 s of comments, well past the 0.5 s watchdog
            self.server.write(": keep-alive\n\n")
            time.sleep(0.15)
        self.assertEqual(self.server.count("GET", STREAM), 1)
        self.assertEqual(client.status().stream, StreamState.CONNECTED)

    def test_an_unclosed_client_does_not_keep_the_process_alive(self):
        code = (
            "import sys\n"
            f"sys.path.insert(0, {str(SRC)!r})\n"
            "import toggleslash\n"
            f"c = toggleslash.create(url={self.server.url!r}, sdk_key='k')\n"
            "assert c.ready()\n"
            "print('ready', flush=True)\n"
        )
        result = subprocess.run(
            [sys.executable, "-I", "-c", code], capture_output=True, text=True, timeout=20, check=False
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("ready", result.stdout)

    def test_no_seam_is_a_global_or_an_environment_variable(self):
        for path in SRC.rglob("*.py"):
            text = path.read_text()
            self.assertNotIn("os.environ", text, path.name)
            self.assertNotIn("getenv", text, path.name)

    def test_the_stream_response_the_server_closes_is_not_an_error_for_the_caller(self):
        client, errors = self.made(stream=True, poll_interval=1, _seams=Seams(jitter=lambda: 0.0))
        self.assertTrue(client.ready())
        wait_until(lambda: client.status().stream == StreamState.CONNECTED, what="a connected stream")
        self.server.drop_streams()
        self.assertTrue(client.variation("checkout", CTX, False))
        wait_until(lambda: self.server.count("GET", STREAM) >= 2, 10, what="a reconnect")
        self.assertEqual(errors, [])
        json.dumps(client.status().last_error)


if __name__ == "__main__":
    unittest.main()
