import json
import os
import subprocess
import sys
import threading
import time
import unittest
from pathlib import Path
from typing import Any

from support import FakeReply, LocalServer, ScriptedTransport, flag, hang, ruleset_body
from toggleslash import ErrorCode, Hooks, ReasonKind, StreamState
from toggleslash._client import Client
from toggleslash._seams import Seams

ROOT = Path(__file__).resolve().parent.parent
CONTEXT = {"key": "u1"}


def serving(*flags: dict[str, Any], etag: str = '"e1"'):
    return lambda *_: FakeReply(200, headers={"ETag": etag}, body=ruleset_body(*flags))


def accepted(*_: Any):
    return FakeReply(202)


def make(*flags: dict[str, Any], events: Any = accepted, **options: Any) -> tuple[Client, ScriptedTransport]:
    transport = ScriptedTransport(ruleset=serving(*flags), events=events)
    seams = options.pop("seams", None) or Seams()
    seams.transport = transport
    options.setdefault("stream", False)
    client = Client("http://example.invalid", "sdk-secret", _seams=seams, **options)
    return client, transport


class EvaluationTests(unittest.TestCase):
    def test_before_the_first_load_every_call_is_the_fallback_with_not_ready(self) -> None:
        transport = ScriptedTransport(ruleset=lambda m, h, b, aborter: (hang(aborter), FakeReply())[1])
        client = Client("http://x", "k", stream=False, request_timeout=60, _seams=Seams(transport=transport))
        try:
            began = time.monotonic()
            detail = client.variation_detail("f", CONTEXT, "fb")
            self.assertLess(time.monotonic() - began, 0.5)
            self.assertEqual((detail.value, detail.variation_index), ("fb", None))
            self.assertEqual((detail.reason.kind, detail.reason.code), (ReasonKind.ERROR, ErrorCode.NOT_READY))
        finally:
            began = time.monotonic()
            client.close()
            self.assertLess(time.monotonic() - began, 2.0, "close abandons the request in flight")

    def test_a_loaded_flag_serves_its_value(self) -> None:
        client, _ = make(flag("f", "blue"))
        self.addCleanup(client.close)
        self.assertTrue(client.ready())
        self.assertEqual(client.variation("f", CONTEXT, "none"), "blue")
        detail = client.variation_detail("f", CONTEXT, "none")
        self.assertEqual((detail.variation_index, detail.reason.kind), (1, ReasonKind.FALLTHROUGH))

    def test_a_missing_flag_is_flag_not_found_with_the_fallback(self) -> None:
        client, _ = make(flag("f", True))
        self.addCleanup(client.close)
        client.ready()
        detail = client.variation_detail("nope", CONTEXT, 5)
        found = (detail.value, detail.variation_index, detail.reason.code)
        self.assertEqual(found, (5, None, ErrorCode.FLAG_NOT_FOUND))

    def test_an_invalid_context_is_invalid_context(self) -> None:
        client, _ = make(flag("f", True))
        self.addCleanup(client.close)
        client.ready()
        for bad in (None, 5, "u", {}, {"key": ""}, {"key": 3}):
            with self.subTest(context=bad):
                detail = client.variation_detail("f", bad, False)  # type: ignore[arg-type]
                self.assertEqual(detail.reason.code, ErrorCode.INVALID_CONTEXT)

    def test_typed_accessors(self) -> None:
        client, _ = make(
            flag("b", True), flag("s", "x"), flag("i", 7), flag("f", 1.5), flag("j", {"a": [1]}), flag("a", [1, 2])
        )
        self.addCleanup(client.close)
        client.ready()
        self.assertIs(client.bool_variation("b", CONTEXT, False), True)
        self.assertEqual(client.string_variation("s", CONTEXT, ""), "x")
        self.assertEqual(client.int_variation("i", CONTEXT, 0), 7)
        self.assertEqual(client.float_variation("f", CONTEXT, 0.0), 1.5)
        self.assertEqual(client.float_variation("i", CONTEXT, 0.0), 7.0)
        self.assertEqual(client.json_variation("j", CONTEXT, {}), {"a": [1]})
        self.assertEqual(client.json_variation("a", CONTEXT, []), [1, 2])

    def test_a_value_of_another_kind_is_type_mismatch_with_the_fallback(self) -> None:
        client, _ = make(flag("b", True), flag("s", "x"), flag("i", 7), flag("a", [1]), flag("o", {}), flag("n", None))
        self.addCleanup(client.close)
        client.ready()
        cases = [
            (client.bool_variation_detail("s", CONTEXT, False), False),
            (client.string_variation_detail("b", CONTEXT, "fb"), "fb"),
            (client.int_variation_detail("s", CONTEXT, 9), 9),
            (client.float_variation_detail("b", CONTEXT, 9.5), 9.5),
            (client.json_variation_detail("i", CONTEXT, {}), {}),
            (client.variation_detail("b", CONTEXT, "fb"), "fb"),
            (client.variation_detail("a", CONTEXT, {}), {}),
            (client.variation_detail("o", CONTEXT, []), []),
            (client.json_variation_detail("a", CONTEXT, {}), {}),
            (client.variation_detail("n", CONTEXT, "fb"), "fb"),
        ]
        for detail, fallback in cases:
            self.assertEqual(detail.reason.code, ErrorCode.TYPE_MISMATCH, detail)
            self.assertEqual((detail.value, detail.variation_index), (fallback, None))

    def test_the_generic_method_with_a_null_fallback_accepts_any_kind(self) -> None:
        client, _ = make(flag("s", "x"), flag("a", [1]), flag("z", None))
        self.addCleanup(client.close)
        client.ready()
        self.assertEqual(client.variation("s", CONTEXT, None), "x")
        self.assertEqual(client.variation("a", CONTEXT, None), [1])
        self.assertEqual(client.variation("z", CONTEXT, "fb"), "fb", "null is a kind of its own, so it mismatches")
        self.assertEqual(client.variation_detail("z", CONTEXT, None).variation_index, 1)

    def test_the_int_accessor_and_its_range(self) -> None:
        big = 2**62  # numbers are binary64: 2**63 - 1 is not representable, and reads as 2**63
        client, _ = make(
            flag("two", 2.0),
            flag("frac", 2.5),
            flag("max", big),
            flag("over", 2**63),
            flag("huge", 1e20),
            flag("min", -(2**63)),
            flag("under", -(2**64)),
        )
        self.addCleanup(client.close)
        client.ready()
        value = client.int_variation("two", CONTEXT, 0)
        self.assertEqual(value, 2)
        self.assertIsInstance(value, int)
        self.assertEqual(client.int_variation("max", CONTEXT, 0), big)
        self.assertEqual(client.int_variation("min", CONTEXT, 0), -(2**63))
        for key in ("frac", "over", "huge", "under"):
            detail = client.int_variation_detail(key, CONTEXT, -1)
            self.assertEqual((detail.value, detail.reason.code), (-1, ErrorCode.TYPE_MISMATCH), key)
        self.assertEqual(client.float_variation("over", CONTEXT, 0.0), float(2**63))

    def test_a_served_list_is_a_copy(self) -> None:
        client, _ = make(flag("a", [1, {"x": [2]}]))
        self.addCleanup(client.close)
        client.ready()
        first = client.json_variation("a", CONTEXT, [])
        first[1]["x"].append(3)
        first.append(9)
        self.assertEqual(client.json_variation("a", CONTEXT, []), [1, {"x": [2]}])

    def test_a_deeply_nested_value_is_served_without_recursion(self) -> None:
        deep: Any = []
        for _ in range(3000):
            deep = [deep]
        client, _ = make(flag("d", deep))
        self.addCleanup(client.close)
        # json.loads of a body this deep may itself be refused; either way nothing raises
        client.ready()
        client.variation_detail("d", CONTEXT, None)


class EventsAndHooksTests(unittest.TestCase):
    def test_the_hook_sees_the_etag_and_flag_version_of_the_ruleset_that_answered(self) -> None:
        events = []
        client, _ = make(flag("f", True, version=7), hooks=Hooks(after_evaluation=events.append))
        self.addCleanup(client.close)
        client.ready()
        client.variation("f", CONTEXT, False)
        client.variation("missing", CONTEXT, False)
        self.assertEqual((events[0].etag, events[0].flag_version, events[0].variation_index), ('"e1"', 7, 1))
        self.assertEqual((events[1].flag_version, events[1].etag), (None, '"e1"'))
        self.assertEqual(events[1].reason.code, ErrorCode.FLAG_NOT_FOUND)

    def test_before_the_load_the_hook_has_no_etag(self) -> None:
        events = []
        transport = ScriptedTransport(ruleset=lambda m, h, b, aborter: (hang(aborter), FakeReply())[1])
        client = Client(
            "http://x",
            "k",
            stream=False,
            request_timeout=60,
            hooks=Hooks(after_evaluation=events.append),
            _seams=Seams(transport=transport),
        )
        self.addCleanup(client.close)
        client.variation("f", CONTEXT, 1)
        self.assertEqual((events[0].etag, events[0].flag_version), (None, None))
        self.assertEqual(events[0].reason.code, ErrorCode.NOT_READY)

    def test_a_raising_hook_and_a_raising_on_error_change_nothing(self) -> None:
        errors = []

        def hook(_: Any) -> None:
            raise RuntimeError("hook")

        def on_error(error: Exception) -> None:
            errors.append(error)
            raise RuntimeError("on_error")

        client, _ = make(flag("f", True), hooks=Hooks(after_evaluation=hook), on_error=on_error)
        self.addCleanup(client.close)
        client.ready()
        self.assertIs(client.bool_variation("f", CONTEXT, False), True)
        self.assertEqual([str(e) for e in errors], ["hook"])

    def test_evaluation_failed_is_the_fallback_one_on_error_and_not_counted(self) -> None:
        errors = []
        events = []

        def broken(*_: Any) -> Any:
            raise ZeroDivisionError("boom")

        client, transport = make(
            flag("f", True),
            seams=Seams(evaluator=broken),
            on_error=errors.append,
            hooks=Hooks(after_evaluation=events.append),
        )
        client.ready()
        detail = client.variation_detail("f", CONTEXT, "fb")
        self.assertEqual((detail.value, detail.variation_index), ("fb", None))
        self.assertEqual((detail.reason.kind, detail.reason.code), (ReasonKind.ERROR, ErrorCode.EVALUATION_FAILED))
        self.assertEqual(len(errors), 1)
        self.assertEqual(len(events), 1)
        client.close()
        self.assertEqual(transport.count("events"), 0)

    def test_usage_is_counted_once_per_served_value_and_flushed_on_close(self) -> None:
        client, transport = make(flag("f", True))
        client.ready()
        for _ in range(4):
            client.variation("f", CONTEXT, False)
        client.variation("f", CONTEXT, "wrong kind")
        client.variation("missing", CONTEXT, False)
        client.close()
        sent = [json.loads(r["body"]) for r in transport.requests if r["route"].endswith("events")]
        entries = [e for body in sent for e in body["events"]]
        self.assertEqual([(e["flag"], e["variationIndex"], e["count"]) for e in entries], [("f", 1, 4)])

    def test_status_after_the_first_load_and_after_close(self) -> None:
        client, _ = make(flag("f", True))
        client.ready()
        status = client.status()
        self.assertEqual((status.etag, status.stream, status.last_error), ('"e1"', StreamState.OFF, None))
        self.assertIsNotNone(status.last_sync_at)
        client.close()
        self.assertEqual(client.status().stream, StreamState.CLOSED)

    def test_every_request_header_is_one_the_protocol_allows(self) -> None:
        client, transport = make(flag("f", True))
        client.ready()
        client.variation("f", CONTEXT, False)
        client.close()
        allowed = {"Authorization", "Accept", "If-None-Match", "Content-Type"}
        for request in transport.requests:
            self.assertLessEqual(set(request["headers"]), allowed)
            self.assertEqual(request["headers"]["Authorization"], "Bearer sdk-secret")
            self.assertNotIn(b"u1", request["body"] or b"")

    def test_options_that_are_not_usable_fall_back_to_the_defaults(self) -> None:
        for value in (0, -1, float("nan"), float("inf"), True, "5", None):
            client, _ = make(flag("f", True), poll_interval=value, flush_interval=value, request_timeout=value)
            client.close()

    def test_error_text_names_no_url_and_no_key(self) -> None:
        errors = []
        client = Client(
            "http://127.0.0.1:1/secret-path", "sdk-secret-key", stream=False, request_timeout=2, on_error=errors.append
        )
        client.ready()
        client.close()
        self.assertTrue(errors)
        for error in errors:
            self.assertNotIn("sdk-secret-key", str(error))
            self.assertNotIn("secret-path", str(error))
            self.assertNotIn("127.0.0.1", str(error))

    def test_a_bad_url_never_raises(self) -> None:
        for url in ("", "not a url", "ftp://x", None, 5):
            client = Client(url, "k", stream=False, request_timeout=1)  # type: ignore[arg-type]
            self.assertFalse(client.ready())
            self.assertEqual(client.variation("f", CONTEXT, 1), 1)
            client.close()


class ReentrancyTests(unittest.TestCase):
    def test_a_hook_may_call_status_variation_and_close(self) -> None:
        seen: list[Any] = []
        holder: dict[str, Client] = {}
        inside = threading.local()

        def hook(event: Any) -> None:
            if getattr(inside, "on", False):
                return
            inside.on = True
            try:
                client = holder["c"]
                seen.append(client.status().etag)
                seen.append(client.variation("f", CONTEXT, False))
                client.close()
                seen.append("closed")
            finally:
                inside.on = False

        client, _ = make(flag("f", True), hooks=Hooks(after_evaluation=hook))
        holder["c"] = client
        client.ready()
        done = threading.Thread(target=lambda: client.variation("f", CONTEXT, False), daemon=True)
        done.start()
        done.join(10)
        self.assertFalse(done.is_alive(), "a hook that closes the client must not deadlock")
        self.assertEqual(seen, ['"e1"', True, "closed"])

    def test_on_error_may_call_close(self) -> None:
        holder: dict[str, Client] = {}
        built = threading.Event()
        called = threading.Event()

        def on_error(_: Exception) -> None:
            built.wait(10)
            holder["c"].close()
            called.set()

        transport = ScriptedTransport(ruleset=lambda *_: FakeReply(503))
        client = Client("http://x", "k", stream=False, on_error=on_error, _seams=Seams(transport=transport))
        holder["c"] = client
        built.set()
        self.assertTrue(called.wait(10), "on_error never ran")
        client.close()
        self.assertEqual(client.status().stream, StreamState.CLOSED)

    def test_on_error_from_the_final_flush_may_call_close(self) -> None:
        holder: dict[str, Client] = {}

        def on_error(_: Exception) -> None:
            holder["c"].close()
            holder["c"].status()

        client, _ = make(flag("f", True), events=lambda *_: FakeReply(503), on_error=on_error)
        holder["c"] = client
        client.ready()
        client.variation("f", CONTEXT, False)
        done = threading.Thread(target=client.close, daemon=True)
        done.start()
        done.join(10)
        self.assertFalse(done.is_alive())


class ConcurrencyTests(unittest.TestCase):
    def test_close_racing_variation_and_two_closes_racing(self) -> None:
        for _ in range(20):
            self.race_close_against_readers()

    def race_close_against_readers(self) -> None:
        client, _ = make(flag("f", True))
        client.ready()
        failures: list[BaseException] = []
        start = threading.Barrier(10)

        def reader() -> None:
            start.wait()
            try:
                for _ in range(200):
                    client.variation("f", CONTEXT, False)
                    client.status()
            except BaseException as error:
                failures.append(error)

        def closer() -> None:
            start.wait()
            try:
                client.close()
            except BaseException as error:
                failures.append(error)

        threads = [threading.Thread(target=reader) for _ in range(7)] + [
            threading.Thread(target=closer) for _ in range(2)
        ]
        for thread in threads:
            thread.start()
        start.wait()
        for thread in threads:
            thread.join(10)
            self.assertFalse(thread.is_alive())
        self.assertEqual(failures, [])
        self.assertEqual(client.status().stream, StreamState.CLOSED)

    def test_the_etag_and_the_flag_version_always_belong_together_while_the_ruleset_swaps(self) -> None:
        counter = {"n": 0}
        lock = threading.Lock()

        def next_ruleset(*_: Any) -> FakeReply:
            with lock:
                counter["n"] += 1
                n = counter["n"]
            return FakeReply(200, headers={"ETag": f'"e{n}"'}, body=ruleset_body(flag("f", True, version=n)))

        events: list[Any] = []
        transport = ScriptedTransport(ruleset=next_ruleset, events=accepted)
        client = Client(
            "http://x",
            "k",
            stream=False,
            poll_interval=0.001,
            hooks=Hooks(after_evaluation=events.append),
            _seams=Seams(transport=transport),
        )
        client.ready()
        stop = time.monotonic() + 0.6
        torn: list[Any] = []

        def reader() -> None:
            while time.monotonic() < stop:
                client.variation("f", CONTEXT, False)

        threads = [threading.Thread(target=reader) for _ in range(4)]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()
        client.close()
        self.assertGreater(counter["n"], 5, "the ruleset never swapped under the readers")
        for event in events:
            if event.flag_version != int(event.etag.strip('"e')):
                torn.append(event)
        self.assertEqual(torn, [])
        self.assertGreater(len(events), 100)

    def test_variation_does_not_wait_for_a_hung_flush_and_close_is_bounded_by_the_request_timeout(self) -> None:
        def hung_events(m: str, h: Any, b: Any, aborter: Any) -> FakeReply:
            hang(aborter)
            return FakeReply()

        client, _ = make(flag("f", True), events=hung_events, request_timeout=0.4, flush_interval=0.05)
        client.ready()
        client.variation("f", CONTEXT, False)
        time.sleep(0.2)  # the flush thread is now inside the hung request
        began = time.monotonic()
        for _ in range(500):
            client.variation("f", CONTEXT, False)
        self.assertLess(time.monotonic() - began, 0.5)
        began = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - began, 3.0)


class ProcessTests(unittest.TestCase):
    def test_a_client_never_closed_does_not_keep_the_process_alive(self) -> None:
        def handle(request: Any) -> None:
            if request.path.endswith("/ruleset"):
                body = ruleset_body(flag("f", True))
                request.send_response(200)
                request.send_header("ETag", '"e1"')
                request.send_header("Content-Length", str(len(body)))
                request.end_headers()
                request.wfile.write(body)
                return
            request.send_response(200)
            request.send_header("Connection", "close")
            request.end_headers()
            request.wfile.write(b'event: connected\ndata: "e1"\n\n')
            request.wfile.flush()
            time.sleep(20)

        with LocalServer(handle) as server:
            code = (
                "import toggleslash\n"
                f"c = toggleslash.create_client({server.url!r}, 'k')\n"
                "assert c.ready()\n"
                "print(c.variation('f', {'key': 'u'}, False))\n"
            )
            env = dict(os.environ, PYTHONPATH=str(ROOT / "src"))
            began = time.monotonic()
            result = subprocess.run(
                [sys.executable, "-c", code], env=env, capture_output=True, text=True, timeout=30, cwd=ROOT, check=False
            )
            self.assertLess(time.monotonic() - began, 10)
            self.assertEqual((result.returncode, result.stdout.strip()), (0, "True"), result.stderr)


if __name__ == "__main__":
    unittest.main()
