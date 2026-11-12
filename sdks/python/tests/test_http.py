"""The HTTP layer: bodies in the three framings, deadlines, the wake socket, header checks and TLS."""

from __future__ import annotations

import contextlib
import os
import shutil
import socket
import ssl
import subprocess
import tempfile
import threading
import time
import unittest
from pathlib import Path

from support import Connection, FakeServer, Request, chunk, reply, stalled
from toggleslash._http import (
    Aborted,
    Hold,
    NetworkError,
    RequestTimeout,
    Transport,
    Wake,
    retry_after,
)


def raw(data: bytes, *, pause: float = 0.0, close: bool = True):
    def handle(_request: Request, connection: Connection) -> None:
        for start in range(0, len(data), 7 if pause else len(data) or 1):
            connection.send(data[start : start + 7] if pause else data)
            if pause:
                time.sleep(pause)
        if close:
            connection.close()

    return handle


class TransportTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.server = FakeServer()
        self.wake = Wake()
        self.transport = Transport(self.server.url, self.wake)
        self.addCleanup(self.server.stop)
        self.addCleanup(self.wake.close)

    def get(self, path: str = "/x", timeout: float = 2.0, **headers: str):
        return self.transport.request("GET", path, headers, None, timeout)


class FramingTests(TransportTestCase):
    def test_content_length(self) -> None:
        self.server.on("GET", "/x", reply(200, "hello", X_Thing="a"))
        response = self.get()
        self.assertEqual(
            (response.status, response.body, response.headers["x-thing"]), (200, b"hello", "a")
        )

    def test_chunked_in_odd_pieces(self) -> None:
        body = (
            b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"
            + chunk(b"hel")
            + chunk(b"lo")
            + b"0\r\n\r\n"
        )
        self.server.on("GET", "/x", raw(body, pause=0.001))
        self.assertEqual(self.get().body, b"hello")

    def test_chunk_extensions_and_trailers(self) -> None:
        body = (
            b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"
            b"5;ext=1\r\nhello\r\n0\r\nTrailer: x\r\n\r\n"
        )
        self.server.on("GET", "/x", raw(body))
        self.assertEqual(self.get().body, b"hello")

    def test_read_until_close(self) -> None:
        self.server.on("GET", "/x", raw(b"HTTP/1.1 200 OK\r\n\r\nuntil the end"))
        self.assertEqual(self.get().body, b"until the end")

    def test_no_body_for_204_and_304(self) -> None:
        for status in (204, 304):
            with self.subTest(status=status):
                self.server.on(
                    "GET",
                    "/x",
                    raw(f"HTTP/1.1 {status} X\r\nContent-Length: 5\r\n\r\n".encode(), close=False),
                )
                self.assertEqual(self.get().body, b"")

    def test_1xx_heads_are_skipped(self) -> None:
        self.server.on(
            "GET",
            "/x",
            raw(b"HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"),
        )
        response = self.get()
        self.assertEqual((response.status, response.body), (200, b"ok"))

    def test_a_short_body_is_a_network_error(self) -> None:
        self.server.on("GET", "/x", raw(b"HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nabc"))
        with self.assertRaises(NetworkError):
            self.get()

    def test_a_truncated_chunk_is_a_network_error(self) -> None:
        self.server.on(
            "GET", "/x", raw(b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhe")
        )
        with self.assertRaises(NetworkError):
            self.get()

    def test_garbage_is_a_network_error(self) -> None:
        for data in (
            b"",
            b"nonsense\r\n\r\n",
            b"HTTP/1.1 abc X\r\n\r\n",
            b"HTTP/2 200 OK\r\n\r\n",
            b"\x00" * 100,
        ):
            with self.subTest(data=data):
                self.server.on("GET", "/x", raw(data))
                with self.assertRaises(NetworkError):
                    self.get()

    def test_a_chunk_size_that_is_not_hex_is_a_network_error(self) -> None:
        self.server.on(
            "GET",
            "/x",
            raw(b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\nab\r\n0\r\n\r\n"),
        )
        with self.assertRaises(NetworkError):
            self.get()

    def test_a_head_that_never_ends_is_bounded(self) -> None:
        self.server.on(
            "GET", "/x", raw(b"HTTP/1.1 200 OK\r\n" + b"X: " + b"a" * 200_000, close=False)
        )
        with self.assertRaises((NetworkError, RequestTimeout)):
            self.get(timeout=1.0)


class RequestTests(TransportTestCase):
    def test_the_request_line_headers_and_body_are_exactly_these(self) -> None:
        self.server.on("POST", "/p", reply(200))
        self.transport.request(
            "POST",
            "/p",
            {"Authorization": "Bearer k", "Content-Type": "application/json"},
            b'{"a":1}',
            2,
        )
        request = self.server.seen("POST", "/p")[0]
        names = sorted(n.lower() for n, _ in request.raw_headers)
        self.assertEqual(
            names, ["authorization", "connection", "content-length", "content-type", "host"]
        )
        self.assertEqual(request.headers["content-length"], "7")
        self.assertEqual(request.headers["connection"], "close")
        self.assertEqual(request.headers["host"], f"127.0.0.1:{self.server.port}")
        self.assertEqual(request.body, b'{"a":1}')

    def test_a_large_body_goes_out_whole(self) -> None:
        self.server.on("POST", "/p", reply(200))
        body = b"x" * 3_000_000
        self.transport.request("POST", "/p", {}, body, 10)
        self.assertEqual(self.server.seen("POST", "/p")[0].body, body)

    def test_a_header_value_with_control_characters_is_refused_before_sending(self) -> None:
        for value in ("a\r\nX-Evil: 1", "a\nb", "a\x00b", "é", "a\rb"):
            with self.subTest(value=value), self.assertRaises(NetworkError):
                self.get(Authorization=value)
        self.assertEqual(self.server.requests, [])

    def test_a_path_prefix_is_kept(self) -> None:
        transport = Transport(self.server.url + "/base//", self.wake)
        self.server.on("GET", "/base/x", reply(200, "ok"))
        self.assertEqual(transport.request("GET", "/x", {}, None, 2).body, b"ok")

    def test_bad_urls(self) -> None:
        for url in (
            "",
            "ftp://h",
            "http://",
            "http://h:0",
            "http://h:70000",
            "http://[::1",
            "//h",
            "h:80",
        ):
            with self.subTest(url=url), self.assertRaises(NetworkError):
                Transport(url, self.wake).request("GET", "/x", {}, None, 1)

    def test_connection_refused_is_a_network_error(self) -> None:
        port = self.server.port
        self.server.stop()
        with self.assertRaises(NetworkError):
            Transport(f"http://127.0.0.1:{port}", self.wake).request("GET", "/x", {}, None, 2)

    def test_a_name_that_does_not_resolve_is_a_network_error_within_the_deadline(self) -> None:
        started = time.monotonic()
        with self.assertRaises((NetworkError, RequestTimeout)):
            Transport("http://nonexistent.invalid", self.wake).request("GET", "/x", {}, None, 3)
        self.assertLess(time.monotonic() - started, 4.0)

    def test_ipv6_loopback(self) -> None:
        try:
            listener = socket.socket(socket.AF_INET6)
            listener.bind(("::1", 0))
        except OSError:
            self.skipTest("no IPv6 loopback on this machine")
        listener.listen(1)
        self.addCleanup(listener.close)
        port = listener.getsockname()[1]
        seen: list[bytes] = []

        def serve() -> None:
            conn, _ = listener.accept()
            seen.append(conn.recv(4096))
            conn.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
            conn.close()

        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        response = Transport(f"http://[::1]:{port}", self.wake).request("GET", "/x", {}, None, 2)
        thread.join(2)
        self.assertEqual(response.body, b"ok")
        self.assertIn(f"Host: [::1]:{port}".encode(), seen[0])


class DeadlineTests(TransportTestCase):
    def test_a_silent_server_times_out(self) -> None:
        self.server.on("GET", "/x", stalled())
        started = time.monotonic()
        with self.assertRaises(RequestTimeout):
            self.get(timeout=0.3)
        self.assertTrue(0.25 <= time.monotonic() - started <= 1.0)

    def test_a_slow_drip_is_bounded_by_the_whole_request_deadline(self) -> None:
        def drip(_request: Request, connection: Connection) -> None:
            connection.send(b"HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n")
            for _ in range(200):
                connection.send(b"x")
                time.sleep(0.02)
            connection.close()

        self.server.on("GET", "/x", drip)
        started = time.monotonic()
        with self.assertRaises(RequestTimeout):
            self.get(timeout=0.4)
        self.assertLess(time.monotonic() - started, 1.0)

    def test_the_wake_aborts_a_request_at_once(self) -> None:
        self.server.on("GET", "/x", stalled())
        threading.Timer(0.2, self.wake.set).start()
        started = time.monotonic()
        with self.assertRaises(Aborted):
            self.get(timeout=10)
        self.assertLess(time.monotonic() - started, 1.0)

    def test_a_wake_set_before_a_request_aborts_it_without_connecting(self) -> None:
        self.wake.set()
        with self.assertRaises(Aborted):
            self.get()
        self.assertEqual(self.server.requests, [])

    def test_a_server_that_stops_reading_a_big_body_is_bounded(self) -> None:
        listener = socket.socket()
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        self.addCleanup(listener.close)
        held: list[socket.socket] = []
        threading.Thread(target=lambda: held.append(listener.accept()[0]), daemon=True).start()
        started = time.monotonic()
        with self.assertRaises((RequestTimeout, NetworkError)):
            Transport(f"http://127.0.0.1:{listener.getsockname()[1]}", self.wake).request(
                "POST", "/x", {}, b"x" * 200_000_000, 0.5
            )
        self.assertLess(time.monotonic() - started, 2.0)
        for sock in held:
            sock.close()


class StreamReadTests(TransportTestCase):
    def test_chunks_arrive_as_they_are_sent(self) -> None:
        from support import STREAM_HEAD, event

        def handle(_request: Request, connection: Connection) -> None:
            connection.send(STREAM_HEAD + event("connected", "a"))
            time.sleep(0.3)
            connection.send(event("changed", "b"))
            connection.send(b"0\r\n\r\n")
            connection.close()

        self.server.on("GET", "/s", handle)
        opened = self.transport.open_stream("/s", {}, 2)
        self.addCleanup(opened.close)
        started = time.monotonic()
        first = next(opened.chunks(lambda: time.monotonic() + 2))
        self.assertLess(time.monotonic() - started, 0.25)
        self.assertIn(b"connected", first)


class RetryAfterParsing(unittest.TestCase):
    def test_values(self) -> None:
        self.assertEqual(retry_after(429, "5"), 5)
        self.assertEqual(retry_after(429, " 5\t"), 5)
        self.assertEqual(retry_after(429, "0"), 0)
        self.assertEqual(retry_after(429, "301"), 300)
        self.assertEqual(retry_after(429, "9" * 10_000), 300)
        for bad in (None, "", " ", "-1", "+1", "1.0", "1e3", "x", "٣", "5 5"):
            self.assertIsNone(retry_after(429, bad), bad)
        for status in (200, 408, 500, 503):
            self.assertIsNone(retry_after(status, "5"))

    def test_a_hold_counts_down_on_its_clock(self) -> None:
        now = [0.0]
        hold = Hold(lambda: now[0])
        self.assertEqual(hold.remaining(), 0)
        hold.raise_to(5)
        now[0] = 2
        self.assertEqual(hold.remaining(), 3)


@unittest.skipUnless(shutil.which("openssl"), "the openssl command line tool is not installed")
class TlsTests(unittest.TestCase):
    """An https server with a certificate made for the test, trusted through SSL_CERT_FILE."""

    def setUp(self) -> None:
        self.dir = Path(tempfile.mkdtemp(prefix="toggleslash-tls-"))
        self.addCleanup(shutil.rmtree, self.dir, True)
        self.wake = Wake()
        self.addCleanup(self.wake.close)

    def certificate(self, name: str, san: str = "IP:127.0.0.1") -> tuple[Path, Path]:
        cert, key = self.dir / f"{name}.pem", self.dir / f"{name}.key"
        subprocess.run(
            [
                "openssl",
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-keyout",
                str(key),
                "-out",
                str(cert),
                "-days",
                "1",
                "-subj",
                "/CN=localhost",
                "-addext",
                f"subjectAltName={san}",
            ],
            check=True,
            capture_output=True,
        )
        return cert, key

    def serve(self, cert: Path, key: Path) -> int:
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(cert, key)
        listener = socket.socket()
        listener.bind(("127.0.0.1", 0))
        listener.listen(4)
        self.addCleanup(listener.close)

        def run() -> None:
            while True:
                try:
                    conn, _ = listener.accept()
                except OSError:
                    return
                with (
                    contextlib.suppress(OSError, ssl.SSLError),
                    context.wrap_socket(conn, server_side=True) as tls,
                ):
                    tls.recv(65536)
                    tls.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 6\r\n\r\nsecure")

        threading.Thread(target=run, daemon=True).start()
        return listener.getsockname()[1]

    def with_trust(self, cert: Path):
        previous = os.environ.get("SSL_CERT_FILE")
        os.environ["SSL_CERT_FILE"] = str(cert)

        def restore() -> None:
            if previous is None:
                os.environ.pop("SSL_CERT_FILE", None)
            else:
                os.environ["SSL_CERT_FILE"] = previous

        self.addCleanup(restore)

    def test_a_trusted_certificate_works_over_https(self) -> None:
        cert, key = self.certificate("good")
        port = self.serve(cert, key)
        self.with_trust(cert)
        response = Transport(f"https://127.0.0.1:{port}", self.wake).request(
            "GET", "/x", {}, None, 5
        )
        self.assertEqual(response.body, b"secure")

    def test_an_untrusted_certificate_is_a_network_error(self) -> None:
        cert, key = self.certificate("untrusted")
        other, _ = self.certificate("other")
        port = self.serve(cert, key)
        self.with_trust(other)
        with self.assertRaises(NetworkError):
            Transport(f"https://127.0.0.1:{port}", self.wake).request("GET", "/x", {}, None, 5)

    def test_a_certificate_for_another_host_is_a_network_error(self) -> None:
        cert, key = self.certificate("wronghost", san="DNS:example.test")
        port = self.serve(cert, key)
        self.with_trust(cert)
        with self.assertRaises(NetworkError):
            Transport(f"https://127.0.0.1:{port}", self.wake).request("GET", "/x", {}, None, 5)

    def test_plain_http_to_a_tls_port_fails_cleanly(self) -> None:
        cert, key = self.certificate("good2")
        port = self.serve(cert, key)
        with self.assertRaises((NetworkError, RequestTimeout)):
            Transport(f"http://127.0.0.1:{port}", self.wake).request("GET", "/x", {}, None, 1)


if __name__ == "__main__":
    unittest.main()
