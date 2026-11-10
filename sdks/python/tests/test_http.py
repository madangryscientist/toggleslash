from __future__ import annotations

import shutil
import subprocess
import tempfile
import threading
import time
import unittest
from pathlib import Path

import toggleslash
from support import KEY, FakeServer, wait_until
from toggleslash import StreamState
from toggleslash._http import Aborted, Aborts, Endpoint, NetworkFailure, RequestTimeout, Transport


class TransportTests(unittest.TestCase):
    def setUp(self) -> None:
        self.server = FakeServer()
        self.endpoint = Endpoint(self.server.url)

    def tearDown(self) -> None:
        self.server.close()

    def test_a_body_that_trickles_is_bounded_as_a_whole(self) -> None:
        def trickle(request: object) -> None:
            r = request  # type: ignore[assignment]
            r.handler.send_response(200)
            r.handler.send_header("Content-Length", "100")
            r.handler.end_headers()
            for _ in range(100):
                r.write(b"x")
                if r.server.stopping.wait(0.05):
                    return

        self.server.on("ruleset", trickle)
        started = time.monotonic()
        with self.assertRaises(RequestTimeout):
            Transport().fetch(self.endpoint, "GET", "/sdk/v1/ruleset", {}, None, 0.4, None)
        self.assertLess(time.monotonic() - started, 1.5)

    def test_a_body_without_length_that_never_ends_is_a_timeout_not_a_clean_end(self) -> None:
        def endless(request: object) -> None:
            r = request  # type: ignore[assignment]
            r.open_stream()
            r.write(b"partial")
            r.hang(10)

        self.server.on("ruleset", endless)
        with self.assertRaises(RequestTimeout):
            Transport().fetch(self.endpoint, "GET", "/sdk/v1/ruleset", {}, None, 0.3, None)

    def test_connection_refused_is_a_network_failure(self) -> None:
        with self.assertRaises(NetworkFailure):
            Transport().fetch(Endpoint("http://127.0.0.1:9"), "GET", "/", {}, None, 1, None)

    def test_abort_all_ends_a_request_in_flight(self) -> None:
        self.server.on("ruleset", lambda r: r.hang(10))
        aborts = Aborts()
        result: list[BaseException] = []

        def go() -> None:
            try:
                Transport().fetch(self.endpoint, "GET", "/sdk/v1/ruleset", {}, None, 8, aborts)
            except BaseException as error:
                result.append(error)

        thread = threading.Thread(target=go, daemon=True)
        thread.start()
        self.assertTrue(wait_until(lambda: self.server.count("ruleset") == 1))
        started = time.monotonic()
        aborts.abort_all()
        thread.join(3)
        self.assertFalse(thread.is_alive())
        self.assertLess(time.monotonic() - started, 2)
        self.assertEqual([type(e) for e in result], [Aborted])

    def test_the_stream_watchdog_drops_a_quiet_stream_and_a_keep_alive_restarts_it(self) -> None:
        quiet_after = 0.9

        def stream(request: object) -> None:
            r = request  # type: ignore[assignment]
            r.open_stream()
            r.write(b'event: connected\ndata: "v1"\n\n')
            end = time.monotonic() + quiet_after
            while time.monotonic() < end:
                r.write(b": keep-alive\n\n")
                if r.server.stopping.wait(0.1):
                    return
            r.hang(10)

        self.server.on("stream", stream)
        errors: list[BaseException] = []
        client = toggleslash.create(
            self.server.url,
            KEY,
            poll_interval=30,
            request_timeout=3,
            on_error=errors.append,
            _stream_watchdog=0.4,
        )
        try:
            self.assertTrue(client.ready())
            self.assertTrue(wait_until(lambda: client.status().stream == StreamState.CONNECTED))
            time.sleep(0.7)  # past one watchdog length: only the keep-alives hold it open
            self.assertEqual(client.status().stream, StreamState.CONNECTED)
            self.assertEqual(errors, [])
            self.assertTrue(wait_until(lambda: any("timeout" in str(e) for e in errors), 5))
            self.assertTrue(
                wait_until(
                    lambda: client.status().stream in (StreamState.DISCONNECTED, StreamState.CONNECTING), 3
                )
            )
        finally:
            client.close()


class TlsTests(unittest.TestCase):
    def test_https_with_a_local_certificate(self) -> None:
        import ssl

        openssl = shutil.which("openssl")
        if openssl is None:
            self.skipTest("no openssl on this machine to make a certificate")
        with tempfile.TemporaryDirectory() as directory:
            cert, key = str(Path(directory) / "c.pem"), str(Path(directory) / "k.pem")
            made = subprocess.run(
                [
                    openssl,
                    "req",
                    "-x509",
                    "-newkey",
                    "rsa:2048",
                    "-nodes",
                    "-keyout",
                    key,
                    "-out",
                    cert,
                    "-days",
                    "1",
                    "-subj",
                    "/CN=127.0.0.1",
                    "-addext",
                    "subjectAltName=IP:127.0.0.1",
                ],
                capture_output=True,
                check=False,
            )
            if made.returncode != 0:
                self.skipTest("openssl could not make a certificate")
            server = FakeServer(tls=(cert, key))
            try:
                trusting = ssl.create_default_context(cafile=cert)
                client = toggleslash.create(
                    server.url, KEY, stream=False, poll_interval=30, _transport=Transport(trusting)
                )
                try:
                    self.assertTrue(client.ready())
                    self.assertTrue(client.variation("checkout", {"key": "u"}, False))
                finally:
                    client.close()
                strict = toggleslash.create(server.url, KEY, stream=False, poll_interval=30)
                try:
                    self.assertFalse(strict.ready())
                    self.assertEqual(strict.status().last_error, "network")
                finally:
                    strict.close()
            finally:
                server.close()


if __name__ == "__main__":
    unittest.main()
