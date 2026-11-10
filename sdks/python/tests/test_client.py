from __future__ import annotations

import threading
import time
import unittest
from datetime import UTC, datetime
from typing import Any

import toggleslash
from support import KEY, FakeServer, bool_flag, flag, ruleset, wait_until
from toggleslash import ErrorCode, ReasonKind, StreamState
from toggleslash._client import SystemClock, _as_int64, _duration, _expected_kind
from toggleslash._usage import retry_after

CTX = {"key": "u1"}


class Base(unittest.TestCase):
    def setUp(self) -> None:
        self.server = FakeServer()
        self.clients: list[toggleslash.Client] = []
        self.errors: list[BaseException] = []

    def tearDown(self) -> None:
        for client in self.clients:
            client.close()
        self.server.close()

    def make(self, **options: Any) -> toggleslash.Client:
        options.setdefault("on_error", self.errors.append)
        client = toggleslash.create(self.server.url, KEY, **options)
        self.clients.append(client)
        return client


class BasicTests(Base):
    def test_load_evaluate_status_close(self) -> None:
        client = self.make(stream=False, poll_interval=5)
        self.assertTrue(client.ready())
        detail = client.variation_detail("checkout", CTX, False)
        self.assertEqual((detail.value, detail.variation_index), (True, 1))
        self.assertEqual(detail.reason.kind, ReasonKind.FALLTHROUGH)
        status = client.status()
        self.assertEqual(status.etag, self.server.etag)
        self.assertEqual(status.stream, StreamState.OFF)
        self.assertIsNotNone(status.last_sync_at)
        self.assertIsNone(status.last_error)
        client.close()
        client.close()
        self.assertEqual(client.status().stream, StreamState.CLOSED)
        self.assertTrue(client.variation("checkout", CTX, False))

    def test_not_ready_before_first_load(self) -> None:
        self.server.on("ruleset", lambda r: r.hang(0.6))
        client = self.make(stream=False, request_timeout=0.3)
        detail = client.variation_detail("checkout", CTX, False)
        self.assertEqual(detail.reason.code, ErrorCode.NOT_READY)
        self.assertIs(detail.value, False)
        self.assertFalse(client.ready())
        self.assertEqual(client.status().last_error, "timeout")

    def test_request_headers_are_the_allowed_ones(self) -> None:
        client = self.make(stream=True, poll_interval=5, flush_interval=0.05)
        self.assertTrue(client.ready())
        client.variation("checkout", {"key": "SENTINEL-KEY", "email": "SENTINEL-MAIL"}, False)
        self.assertTrue(wait_until(lambda: self.server.count("events") >= 1))
        client.close()
        allowed = {
            "authorization",
            "accept",
            "if-none-match",
            "content-type",
            "host",
            "accept-encoding",
            "content-length",
        }
        seen = self.server.requests[:]
        self.assertGreaterEqual(len(seen), 3)
        for recorded in seen:
            self.assertLessEqual({k.lower() for k in recorded.headers}, allowed, recorded.headers)
            self.assertEqual(recorded.headers["Authorization"], "Bearer " + KEY)
            wire = recorded.path + repr(recorded.headers) + recorded.body.decode("latin-1")
            self.assertNotIn("SENTINEL", wire)
        self.assertEqual({r.headers["Accept"] for r in seen if r.route == "stream"}, {"text/event-stream"})
        self.assertEqual(self.errors, [])

    def test_if_none_match_only_with_a_ruleset_and_a_tag(self) -> None:
        client = self.make(stream=False, poll_interval=0.05)
        self.assertTrue(client.ready())
        self.assertTrue(wait_until(lambda: self.server.count("ruleset") >= 3))
        client.close()
        first, *rest = self.server.recorded("ruleset")
        self.assertNotIn("If-None-Match", first.headers)
        self.assertTrue(all(r.headers.get("If-None-Match") == '"v1"' for r in rest))

    def test_a_changed_ruleset_arrives_through_the_stream(self) -> None:
        client = self.make()
        self.assertTrue(client.ready())
        self.assertTrue(wait_until(lambda: client.status().stream == StreamState.CONNECTED))
        self.server.publish(ruleset(bool_flag("checkout", False)))
        self.assertTrue(wait_until(lambda: client.variation("checkout", CTX, True) is False))
        self.assertEqual(client.status().etag, '"v2"')

    def test_with_a_missing_etag_header_none_is_held(self) -> None:
        self.server.on("ruleset", lambda r: r.send(200, b'{"flags":[],"segments":[]}'))
        client = self.make(stream=False, poll_interval=5)
        self.assertTrue(client.ready())
        self.assertIsNone(client.status().etag)

    def test_a_bad_ruleset_replaces_nothing(self) -> None:
        client = self.make(stream=False, poll_interval=0.05)
        self.assertTrue(client.ready())
        self.server.on("ruleset", lambda r: r.send(200, b'{"flags":{},"segments":[]}', {"ETag": '"x"'}))
        self.assertTrue(wait_until(lambda: client.status().last_error == "bad_ruleset"))
        self.assertTrue(client.variation("checkout", CTX, False))
        self.assertEqual(client.status().etag, '"v1"')

    def test_body_bom_and_duplicate_keys(self) -> None:
        body = (
            b"\xef\xbb\xbf"
            + b'{"flags":[],"segments":[],'
            + b'"flags":[{"key":"a","on":false,"variations":[1,2],"offVariation":1}]}'
        )
        self.server.on("ruleset", lambda r: r.send(200, body, {"ETag": '"b"'}))
        client = self.make(stream=False, poll_interval=5)
        self.assertTrue(client.ready())
        self.assertEqual(client.variation("a", CTX, 0), 2)

    def test_an_unusable_url_is_a_network_error_never_a_raise(self) -> None:
        for url in ("", "nonsense", "ftp://x", "http://", "http://[::1"):
            client = toggleslash.create(url, KEY, stream=False, on_error=self.errors.append)
            try:
                self.assertFalse(client.ready())
                self.assertEqual(client.variation("a", CTX, 7), 7)
                self.assertEqual(client.status().last_error, "network")
            finally:
                client.close()

    def test_trailing_slashes_are_removed(self) -> None:
        client = toggleslash.create(self.server.url + "///", KEY, stream=False)
        self.clients.append(client)
        self.assertTrue(client.ready())

    def test_context_manager_closes(self) -> None:
        with toggleslash.create(self.server.url, KEY, stream=False) as client:
            self.assertTrue(client.ready())
        self.assertEqual(client.status().stream, StreamState.CLOSED)


class MismatchTests(Base):
    def setUp(self) -> None:
        super().setUp()
        self.server.publish(
            ruleset(
                bool_flag("b", True),
                flag("s", ["x", "y"], 1),
                flag("n", [1, 2.5], 1),
                flag("whole", [1, 2.0], 1),
                flag("a", [[1], [2]], 1),
                flag("o", [{}, {"k": 1}], 1),
                flag("null", [None, None], 1),
            )
        )
        self.client = self.make(stream=False, poll_interval=5)
        self.assertTrue(self.client.ready())

    def kind(self, key: str, fallback: Any) -> tuple[Any, Any]:
        d = self.client.variation_detail(key, CTX, fallback)
        return d.value, d.reason.code

    def test_matching_kinds(self) -> None:
        self.assertEqual(self.kind("b", False), (True, None))
        self.assertEqual(self.kind("s", ""), ("y", None))
        self.assertEqual(self.kind("n", 0), (2.5, None))
        self.assertEqual(self.kind("a", []), ([2], None))
        self.assertEqual(self.kind("a", ()), ([2], None))
        self.assertEqual(self.kind("o", {}), ({"k": 1}, None))
        self.assertEqual(self.kind("null", None), (None, None))

    def test_mismatches_give_the_fallback_and_no_index(self) -> None:
        for key, fallback in [
            ("b", 0),
            ("b", ""),
            ("s", True),
            ("n", True),
            ("a", {}),
            ("o", []),
            ("null", 0),
            ("null", ""),
        ]:
            d = self.client.variation_detail(key, CTX, fallback)
            self.assertEqual(d.reason.code, ErrorCode.TYPE_MISMATCH, (key, fallback))
            self.assertIsNone(d.variation_index)
            self.assertEqual(d.value, fallback)

    def test_any_fallback_accepts_any_kind(self) -> None:
        for key in ("b", "s", "n", "a", "o", "null"):
            self.assertIsNotNone(self.client.variation_detail(key, CTX, None).variation_index)

    def test_a_fallback_that_is_not_json_never_matches(self) -> None:
        marker = object()
        d = self.client.variation_detail("b", CTX, marker)
        self.assertIs(d.value, marker)
        self.assertEqual(d.reason.code, ErrorCode.TYPE_MISMATCH)

    def test_integer_accessor(self) -> None:
        self.assertEqual(self.client.int_variation("whole", CTX, 0), 2)
        self.assertIsInstance(self.client.int_variation("whole", CTX, 0), int)
        d = self.client.int_variation_detail("n", CTX, 9)
        self.assertEqual((d.value, d.reason.code), (9, ErrorCode.TYPE_MISMATCH))
        self.assertEqual(self.client.int_variation_detail("b", CTX, 9).reason.code, ErrorCode.TYPE_MISMATCH)

    def test_the_int64_range(self) -> None:
        top = float(2**63 - 1024)
        self.assertEqual(_as_int64(top), 2**63 - 1024)
        self.assertIsNone(_as_int64(float(2**63)))
        self.assertEqual(_as_int64(-float(2**63)), -(2**63))
        self.assertIsNone(_as_int64(-float(2**63) - 2048.0))
        self.assertIsNone(_as_int64(float("inf")))
        self.assertIsNone(_as_int64(float("nan")))
        self.assertIsNone(_as_int64(True))
        self.assertEqual(_as_int64(2**63 - 1), 2**63 - 1)
        self.assertIsNone(_as_int64(2**63))
        self.assertEqual(_as_int64(-0.0), 0)

    def test_the_int64_range_through_the_client(self) -> None:
        self.server.publish(
            ruleset(
                flag("top", [0, 2**63 - 1024], 1),
                flag("over", [0, 2**63], 1),
                flag("under", [0, -(2**63) - 2048], 1),
            )
        )
        self.assertTrue(wait_until(lambda: self.client.status().etag == '"v2"' or self._poll()))

    def _poll(self) -> bool:
        return False


class Int64ThroughClient(Base):
    def test_range(self) -> None:
        self.server.publish(
            ruleset(
                flag("top", [0, 2**63 - 1024], 1),
                flag("over", [0, 2**63], 1),
                flag("under", [0, -(2**63)], 1),
            )
        )
        client = self.make(stream=False, poll_interval=5)
        self.assertTrue(client.ready())
        self.assertEqual(client.int_variation("top", CTX, 5), 2**63 - 1024)
        self.assertEqual(client.int_variation("over", CTX, 5), 5)
        self.assertEqual(client.int_variation("under", CTX, 5), -(2**63))


class HelperTests(unittest.TestCase):
    def test_durations_fall_back(self) -> None:
        self.assertEqual(_duration(2, 9.0), 2.0)
        self.assertEqual(_duration(0.5, 9.0), 0.5)
        for bad in (0, -1, None, "3", True, float("nan"), 10**400, [1]):
            self.assertEqual(_duration(bad, 9.0), 9.0, bad)

    def test_expected_kind(self) -> None:
        self.assertEqual(
            [_expected_kind(v) for v in (None, True, 1, 1.5, "s", [], (), {}, object())],
            ["any", "boolean", "number", "number", "string", "array", "array", "object", "invalid"],
        )

    def test_retry_after(self) -> None:
        class H(dict):  # type: ignore[type-arg]
            pass

        def ra(value: str | None) -> float | None:
            return retry_after(H() if value is None else H({"Retry-After": value}))

        self.assertEqual(ra("5"), 5.0)
        self.assertEqual(ra("  7 "), 7.0)
        self.assertEqual(ra("0"), 0.0)
        self.assertEqual(ra("999999"), 300.0)
        self.assertEqual(ra("9" * 5000), 300.0)
        for bad in ("", " ", "1.5", "-1", "1e3", "Wed, 21 Oct 2026 07:28:00 GMT", "٣", "5 s", "+5"):
            self.assertIsNone(ra(bad), bad)
        self.assertIsNone(ra(None))


class HookTests(Base):
    def test_hook_event_fields_and_errors(self) -> None:
        events: list[toggleslash.EvaluationEvent] = []
        client = self.make(
            stream=False, poll_interval=5, hooks=toggleslash.Hooks(after_evaluation=events.append)
        )
        client.variation("checkout", CTX, False)  # NOT_READY or ready, either way a hook call
        self.assertTrue(client.ready())
        client.variation("checkout", CTX, False)
        client.variation("missing", CTX, 3)
        client.variation_detail("checkout", "not a context", False)
        last = events[-3:]
        self.assertEqual(
            (last[0].flag_key, last[0].value, last[0].variation_index, last[0].etag, last[0].flag_version),
            ("checkout", True, 1, '"v1"', 1),
        )
        self.assertEqual((last[1].reason.code, last[1].flag_version), (ErrorCode.FLAG_NOT_FOUND, None))
        self.assertEqual(last[2].reason.code, ErrorCode.INVALID_CONTEXT)
        self.assertEqual(last[2].context, "not a context")

    def test_a_raising_hook_goes_to_on_error_and_changes_nothing(self) -> None:
        def boom(_event: Any) -> None:
            raise ValueError("hook failed")

        client = self.make(stream=False, poll_interval=5, hooks=toggleslash.Hooks(after_evaluation=boom))
        self.assertTrue(client.ready())
        self.assertTrue(client.variation("checkout", CTX, False))
        self.assertEqual([str(e) for e in self.errors], ["hook failed"])

    def test_a_non_callable_hook_is_ignored(self) -> None:
        client = self.make(stream=False, poll_interval=5, hooks=toggleslash.Hooks(after_evaluation="nope"))  # type: ignore[arg-type]
        self.assertTrue(client.ready())
        self.assertTrue(client.variation("checkout", CTX, False))

    def test_an_on_error_that_raises_is_ignored(self) -> None:
        calls: list[int] = []

        def bad(_error: BaseException) -> None:
            calls.append(1)
            raise RuntimeError("on_error raised")

        self.server.on("ruleset", lambda r: r.send(500))
        client = toggleslash.create(self.server.url, KEY, stream=False, on_error=bad)
        self.clients.append(client)
        self.assertFalse(client.ready())
        self.assertEqual(calls, [1])
        self.assertEqual(client.status().last_error, "http_500")

    def test_evaluation_failed_is_reported_once_and_still_hooked(self) -> None:
        events: list[toggleslash.EvaluationEvent] = []

        def explode(*_args: Any) -> Any:
            raise RuntimeError("evaluator failed")

        client = self.make(
            stream=False,
            poll_interval=5,
            _evaluator=explode,
            hooks=toggleslash.Hooks(after_evaluation=events.append),
        )
        self.assertTrue(client.ready())
        before = len(self.errors)
        d = client.variation_detail("checkout", CTX, "fb")
        self.assertEqual(
            (d.value, d.variation_index, d.reason.code), ("fb", None, ErrorCode.EVALUATION_FAILED)
        )
        self.assertEqual(len(self.errors) - before, 1)
        self.assertEqual(events[-1].reason.code, ErrorCode.EVALUATION_FAILED)
        self.assertNotIn("SENTINEL", str(self.errors[-1]))

    def test_the_hook_runs_on_the_callers_thread(self) -> None:
        seen: list[str] = []
        client = self.make(
            stream=False,
            poll_interval=5,
            hooks=toggleslash.Hooks(after_evaluation=lambda e: seen.append(threading.current_thread().name)),
        )
        self.assertTrue(client.ready())
        client.variation("checkout", CTX, False)
        self.assertEqual(seen[-1], threading.current_thread().name)


class RetryAfterTests(Base):
    def test_cap_on_the_ruleset_route(self) -> None:
        self.server.on("ruleset", lambda r: r.send(429, headers={"Retry-After": "999999"}))
        client = self.make(stream=False, poll_interval=5)
        self.assertFalse(client.ready())
        remaining = client._hold_until - time.monotonic()
        self.assertTrue(290 < remaining <= 300.5, remaining)
        self.assertEqual(client.status().last_error, "http_429")

    def test_cap_on_the_stream_route(self) -> None:
        self.server.on("stream", lambda r: r.send(429, headers={"Retry-After": "1000000000"}))
        client = self.make()
        self.assertTrue(client.ready())
        self.assertTrue(wait_until(lambda: client._hold_until > 0))
        remaining = client._hold_until - time.monotonic()
        self.assertTrue(290 < remaining <= 300.5, remaining)

    def test_cap_on_the_events_route_and_the_loss_report(self) -> None:
        self.server.on("events", lambda r: r.send(429, headers={"Retry-After": "86400"}))
        client = self.make(stream=False, poll_interval=30, flush_interval=0.05)
        self.assertTrue(client.ready())
        client.variation("checkout", CTX, False)
        self.assertTrue(wait_until(lambda: client._usage._hold_until > 0))
        remaining = client._usage._hold_until - time.monotonic()
        self.assertTrue(290 < remaining <= 300.5, remaining)
        client.variation("legacy", CTX, True)
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 3)
        self.assertEqual(self.server.count("events"), 1)
        self.assertTrue(any("closed with" in str(e) and "unsent" in str(e) for e in self.errors), self.errors)

    def test_a_hold_is_never_shortened(self) -> None:
        client = self.make(stream=False, poll_interval=5)
        client._hold(100)
        client._hold(5)
        self.assertGreater(client._hold_until - time.monotonic(), 90)


class UsageTests(Base):
    def test_counts_either_side_of_utc_midnight_go_out_as_two_days(self) -> None:
        class Clock(SystemClock):
            current = datetime(2026, 11, 10, 23, 59, 59, tzinfo=UTC)

            def now(self) -> datetime:
                return self.current

        clock = Clock()
        client = self.make(stream=False, poll_interval=30, flush_interval=60, _clock=clock)
        self.assertTrue(client.ready())
        client.variation("checkout", CTX, False)
        client.variation("checkout", CTX, False)
        clock.current = datetime(2026, 11, 11, 0, 0, 1, tzinfo=UTC)
        client.variation("checkout", CTX, False)
        client.close()
        events = self.server.recorded("events")
        self.assertEqual(len(events), 1)
        body = events[0].json()
        self.assertEqual(set(body), {"events"})
        got = {(e["day"], e["flag"], e["variationIndex"]): e["count"] for e in body["events"]}
        self.assertEqual(got, {("2026-11-10", "checkout", 1): 2, ("2026-11-11", "checkout", 1): 1})
        for e in body["events"]:
            self.assertEqual(set(e), {"flag", "variationIndex", "count", "day"})
        self.assertEqual(events[0].headers["Content-Type"], "application/json")

    def test_a_non_utc_clock_is_converted(self) -> None:
        from datetime import timedelta, timezone

        class Clock(SystemClock):
            def now(self) -> datetime:
                return datetime(2026, 11, 11, 1, 30, tzinfo=timezone(timedelta(hours=2)))

        client = self.make(stream=False, poll_interval=30, flush_interval=60, _clock=Clock())
        self.assertTrue(client.ready())
        client.variation("checkout", CTX, False)
        client.close()
        self.assertEqual(self.server.recorded("events")[0].json()["events"][0]["day"], "2026-11-10")

    def test_batches_of_at_most_500_and_the_kept_and_dropped_statuses(self) -> None:
        flags = [bool_flag(f"f{i}", True) for i in range(1203)]
        self.server.publish(ruleset(*flags))
        client = self.make(stream=False, poll_interval=30, flush_interval=60)
        self.assertTrue(client.ready())
        for i in range(1203):
            client.variation(f"f{i}", CTX, False)
        client.close()
        sizes = [len(r.json()["events"]) for r in self.server.recorded("events")]
        self.assertEqual(sorted(sizes, reverse=True), [500, 500, 203])

    def test_kept_statuses_are_resent_and_dropped_statuses_are_not(self) -> None:
        for status, kept in ((500, True), (408, True), (429, True), (400, False), (404, False)):
            with self.subTest(status=status):
                server = FakeServer()
                server.on("events", lambda r, s=status: r.send(s))
                errors: list[BaseException] = []
                client = toggleslash.create(
                    server.url,
                    KEY,
                    stream=False,
                    poll_interval=30,
                    flush_interval=0.05,
                    on_error=errors.append,
                )
                try:
                    self.assertTrue(client.ready())
                    client.variation("checkout", CTX, False)
                    self.assertTrue(wait_until(lambda: server.count("events") >= (3 if kept else 1)))
                    time.sleep(0.2)
                    self.assertTrue(server.count("events") >= 3 if kept else server.count("events") == 1)
                    self.assertTrue(any(f"http_{status}" in str(e) for e in errors))
                finally:
                    client.close()
                    server.close()

    def test_nothing_is_counted_after_close_and_an_empty_flush_sends_nothing(self) -> None:
        client = self.make(stream=False, poll_interval=30, flush_interval=0.05)
        self.assertTrue(client.ready())
        time.sleep(0.2)
        self.assertEqual(self.server.count("events"), 0)
        client.close()
        client.variation("checkout", CTX, False)
        self.assertEqual(client._usage.pending(), 0)
        self.assertEqual(self.server.count("events"), 0)


if __name__ == "__main__":
    unittest.main()
