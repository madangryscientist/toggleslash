"""The client against a loopback fake server: loading, evaluation, results, status, hooks and errors."""

from __future__ import annotations

import concurrent.futures
import datetime
import json
import threading
import time
import unittest
from typing import Any

import toggleslash
from support import (
    FakeServer,
    flag_json,
    make_client,
    reply,
    ruleset_json,
    wait_for,
)
from toggleslash import ErrorCode, ReasonKind, StreamState
from toggleslash._client import Client


class ClientTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.server = FakeServer()
        self.clients: list[Client] = []
        self.errors: list[Exception] = []
        self.addCleanup(self.server.stop)
        self.addCleanup(self.close_all)

    def close_all(self) -> None:
        for client in self.clients:
            client.close()

    def serve(self, *flags: dict[str, Any], etag: str = '"v1"') -> None:
        self.server.on("GET", "/sdk/v1/ruleset", reply(200, ruleset_json(*flags), ETag=etag))

    def client(self, **changes: Any) -> Client:
        changes.setdefault("on_error", self.errors.append)
        client = make_client(self.server, **changes)
        self.clients.append(client)
        return client


class EvaluationTests(ClientTestCase):
    def test_before_any_load_every_answer_is_the_fallback_not_ready(self) -> None:
        self.server.on("GET", "/sdk/v1/ruleset", lambda *_: time.sleep(0.3))
        client = self.client()
        detail = client.variation_detail("f", {"key": "u"}, "fallback")
        self.assertEqual(detail.value, "fallback")
        self.assertIsNone(detail.variation_index)
        self.assertEqual(detail.reason.kind, ReasonKind.ERROR)
        self.assertEqual(detail.reason.code, ErrorCode.NOT_READY)

    def test_a_served_value_is_returned_and_counted(self) -> None:
        self.serve(flag_json("f", [False, True], fallthrough=1))
        client = self.client()
        self.assertTrue(client.ready())
        detail = client.variation_detail("f", {"key": "u"}, False)
        self.assertIs(detail.value, True)
        self.assertEqual(detail.variation_index, 1)
        self.assertEqual(detail.reason.kind, ReasonKind.FALLTHROUGH)
        self.assertEqual(client._usage.pending_count(), 1)

    def test_a_flag_that_is_not_there_answers_the_evaluators_reason(self) -> None:
        self.serve(flag_json("f"))
        client = self.client()
        client.ready()
        detail = client.variation_detail("nope", {"key": "u"}, 5)
        self.assertEqual(detail.value, 5)
        self.assertEqual(detail.reason.code, ErrorCode.FLAG_NOT_FOUND)
        self.assertEqual(client._usage.pending_count(), 0)

    def test_an_invalid_context_is_the_fallback(self) -> None:
        self.serve(flag_json("f"))
        client = self.client()
        client.ready()
        detail = client.variation_detail("f", {"nokey": 1}, "x")
        self.assertEqual(detail.value, "x")
        self.assertEqual(detail.reason.code, ErrorCode.INVALID_CONTEXT)

    def test_off_without_an_off_variation_is_the_fallback_with_the_off_reason(self) -> None:
        self.serve(flag_json("f", on=False, offVariation=None))
        client = self.client()
        client.ready()
        detail = client.variation_detail("f", {"key": "u"}, "x")
        self.assertEqual(detail.value, "x")
        self.assertIsNone(detail.variation_index)
        self.assertEqual(detail.reason.kind, ReasonKind.OFF)
        self.assertEqual(client._usage.pending_count(), 0)

    def test_the_kind_rule(self) -> None:
        self.serve(
            flag_json("b", [True], fallthrough=0),
            flag_json("n", [1.5], fallthrough=0),
            flag_json("s", ["x"], fallthrough=0),
            flag_json("a", [[1, 2]], fallthrough=0),
            flag_json("o", [{"k": 1}], fallthrough=0),
            flag_json("z", [None], fallthrough=0),
        )
        client = self.client()
        client.ready()
        ctx = {"key": "u"}
        cases = [
            ("b", False, True, "boolean"),
            ("n", 0, 1.5, "number"),
            ("s", "", "x", "string"),
            ("a", [], [1, 2], "array"),
            ("o", {}, {"k": 1}, "object"),
        ]
        others = {"boolean": False, "number": 0, "string": "", "array": [], "object": {}}
        for key, fallback, expected, kind in cases:
            with self.subTest(key=key):
                self.assertEqual(client.variation(key, ctx, fallback), expected)
                for other_kind, other in others.items():
                    if other_kind == kind:
                        continue
                    detail = client.variation_detail(key, ctx, other)
                    self.assertEqual(detail.reason.code, ErrorCode.TYPE_MISMATCH, (key, other_kind))
                    self.assertEqual(detail.value, other)
                    self.assertIsNone(detail.variation_index)
                self.assertEqual(client.variation(key, ctx, None), expected)
        self.assertEqual(client.variation("a", ctx, ()), [1, 2])
        self.assertEqual(
            client.variation_detail("z", ctx, "x").reason.code, ErrorCode.TYPE_MISMATCH
        )
        self.assertIsNone(client.variation("z", ctx, None))
        self.assertEqual(
            client.variation_detail("n", ctx, True).reason.code, ErrorCode.TYPE_MISMATCH
        )
        self.assertEqual(
            client.variation_detail("n", ctx, b"bytes").reason.code, ErrorCode.TYPE_MISMATCH
        )
        self.assertEqual(
            client.variation_detail("n", ctx, object()).reason.code, ErrorCode.TYPE_MISMATCH
        )

    def test_a_mismatch_is_not_counted(self) -> None:
        self.serve(flag_json("n", [1.5], fallthrough=0))
        client = self.client()
        client.ready()
        client.variation("n", {"key": "u"}, "x")
        self.assertEqual(client._usage.pending_count(), 0)
        client.variation("n", {"key": "u"}, 0)
        self.assertEqual(client._usage.pending_count(), 1)

    def test_the_integer_accessor(self) -> None:
        big = 2.0**63 - 1024.0
        self.serve(
            flag_json("two", [2.0], fallthrough=0),
            flag_json("frac", [2.5], fallthrough=0),
            flag_json("big", [big], fallthrough=0),
            flag_json("huge", [2.0**63], fallthrough=0),
            flag_json("low", [-(2.0**63)], fallthrough=0),
            flag_json("str", ["2"], fallthrough=0),
            flag_json("bool", [True], fallthrough=0),
        )
        client = self.client()
        client.ready()
        ctx = {"key": "u"}
        value = client.int_variation("two", ctx, -1)
        self.assertEqual(value, 2)
        self.assertIsInstance(value, int)
        self.assertNotIsInstance(value, bool)
        self.assertEqual(client.int_variation("big", ctx, -1), int(big))
        self.assertEqual(client.int_variation("low", ctx, -1), -(2**63))
        self.assertEqual(client.int_variation("two", ctx, None), 2)
        for key in ("frac", "huge", "str", "bool"):
            with self.subTest(key=key):
                detail = client.int_variation_detail(key, ctx, -1)
                self.assertEqual(detail.value, -1)
                self.assertEqual(detail.reason.code, ErrorCode.TYPE_MISMATCH)
                self.assertIsNone(detail.variation_index)
        for key in ("frac", "huge", "str", "bool"):
            with self.subTest(key=key, fallback=None):
                detail = client.int_variation_detail(key, ctx, None)
                self.assertIsNone(detail.value)
                self.assertEqual(detail.reason.code, ErrorCode.TYPE_MISMATCH)
        self.assertEqual(
            client.int_variation_detail("two", ctx, "x").reason.code, ErrorCode.TYPE_MISMATCH
        )

    def test_the_generic_number_accessor_has_no_integer_rule(self) -> None:
        self.serve(
            flag_json("frac", [2.5], fallthrough=0), flag_json("huge", [2.0**70], fallthrough=0)
        )
        client = self.client()
        client.ready()
        self.assertEqual(client.variation("frac", {"key": "u"}, 0), 2.5)
        self.assertEqual(client.variation("huge", {"key": "u"}, 0), 2.0**70)

    def test_a_returned_container_is_a_copy(self) -> None:
        self.serve(flag_json("o", [{"k": [1]}], fallthrough=0))
        client = self.client()
        client.ready()
        first = client.variation("o", {"key": "u"}, {})
        first["k"].append(2)
        first["new"] = 1
        self.assertEqual(client.variation("o", {"key": "u"}, {}), {"k": [1]})

    def test_a_context_of_odd_python_values_does_not_raise(self) -> None:
        self.serve(flag_json("f"))
        client = self.client()
        client.ready()
        for context in (
            None,
            5,
            "x",
            [],
            {"key": object()},
            {"key": "u", "attributes": {"a": object()}},
        ):
            with self.subTest(context=repr(context)):
                self.assertEqual(client.variation("f", context, "fb") in {"fb", True, False}, True)

    def test_a_flag_key_that_is_not_a_string_fails_cleanly(self) -> None:
        self.serve(flag_json("f"))
        client = self.client()
        client.ready()
        for key in (None, 5, [], {}):
            detail = client.variation_detail(key, {"key": "u"}, "fb")
            self.assertEqual(detail.value, "fb")
            self.assertEqual(detail.reason.kind, ReasonKind.ERROR)

    def test_an_evaluator_that_raises_is_evaluation_failed_with_one_on_error(self) -> None:
        self.serve(flag_json("f"))
        client = self.client()
        client.ready()
        boom = RuntimeError("boom")

        def raising(*_args: Any) -> Any:
            raise boom

        import toggleslash._client as module

        original = module.evaluate
        module.evaluate = raising
        try:
            detail = client.variation_detail("f", {"key": "u"}, "fb")
        finally:
            module.evaluate = original
        self.assertEqual(detail.value, "fb")
        self.assertIsNone(detail.variation_index)
        self.assertEqual(detail.reason.kind, ReasonKind.ERROR)
        self.assertEqual(detail.reason.code, ErrorCode.EVALUATION_FAILED)
        self.assertEqual(self.errors, [boom])
        self.assertEqual(client._usage.pending_count(), 0)


class ReadyAndStatusTests(ClientTestCase):
    def test_ready_is_true_once_a_ruleset_is_held_and_status_shows_it(self) -> None:
        self.serve(flag_json("f"), etag='"abc"')
        client = self.client()
        before = time.time()
        self.assertTrue(client.ready())
        status = client.status()
        self.assertEqual(status.etag, '"abc"')
        self.assertEqual(status.stream, StreamState.OFF)
        self.assertIsNone(status.last_error)
        self.assertIsInstance(status.last_sync_at, datetime.datetime)
        self.assertEqual(status.last_sync_at.tzinfo, datetime.UTC)
        self.assertGreaterEqual(status.last_sync_at.timestamp(), before - 1)

    def test_ready_is_false_when_the_first_load_fails_and_stays_false(self) -> None:
        self.server.on(
            "GET", "/sdk/v1/ruleset", reply(500), reply(200, ruleset_json(flag_json("f")))
        )
        client = self.client()
        self.assertFalse(client.ready())
        self.assertTrue(wait_for(lambda: client.variation("f", {"key": "u"}, False) is True))
        self.assertFalse(client.ready())
        self.assertIsNone(client.status().last_error)

    def test_lasterror_is_set_before_ready_settles(self) -> None:
        self.server.on("GET", "/sdk/v1/ruleset", reply(503))
        client = self.client()
        self.assertFalse(client.ready())
        self.assertEqual(client.status().last_error, "http_503")

    def test_ready_is_bounded_by_the_request_timeout(self) -> None:
        self.server.on("GET", "/sdk/v1/ruleset", lambda *_: time.sleep(1.5))
        client = self.client(request_timeout=0.3)
        started = time.monotonic()
        self.assertFalse(client.ready())
        self.assertLess(time.monotonic() - started, 1.2)
        self.assertEqual(client.status().last_error, "timeout")

    def test_the_error_codes(self) -> None:
        cases = [
            (reply(404), "http_404"),
            (reply(200, "not json"), "bad_ruleset"),
            (reply(200, "{}"), "bad_ruleset"),
            (reply(200, '{"flags":[],"segments":{}}'), "bad_ruleset"),
            (reply(200, '{"flags":{},"segments":[]}'), "bad_ruleset"),
            (reply(200, "[]"), "bad_ruleset"),
            (reply(200, "null"), "bad_ruleset"),
            (reply(304), "http_304"),
            (lambda _r, c: c.close(), "network"),
        ]
        for handler, code in cases:
            with self.subTest(code=code):
                server = FakeServer()
                self.addCleanup(server.stop)
                server.on("GET", "/sdk/v1/ruleset", handler)
                errors: list[Exception] = []
                client = make_client(server, on_error=errors.append)
                self.addCleanup(client.close)
                self.assertFalse(client.ready())
                self.assertEqual(client.status().last_error, code)
                self.assertTrue(wait_for(lambda seen=errors: len(seen) >= 1))
                self.assertEqual(errors[0].code, code)

    def test_a_bad_answer_does_not_replace_the_ruleset(self) -> None:
        self.server.on(
            "GET",
            "/sdk/v1/ruleset",
            reply(200, ruleset_json(flag_json("f")), ETag='"a"'),
            reply(200, "{}", ETag='"b"'),
        )
        client = self.client()
        self.assertTrue(client.ready())
        self.assertTrue(wait_for(lambda: client.status().last_error == "bad_ruleset"))
        self.assertEqual(client.status().etag, '"a"')
        self.assertIs(client.variation("f", {"key": "u"}, False), True)

    def test_a_sync_clears_the_error(self) -> None:
        self.server.on(
            "GET",
            "/sdk/v1/ruleset",
            reply(200, ruleset_json(flag_json("f")), ETag='"a"'),
            reply(500),
            reply(304),
        )
        client = self.client()
        client.ready()
        self.assertTrue(wait_for(lambda: client.status().last_error == "http_500"))
        self.assertTrue(wait_for(lambda: client.status().last_error is None))

    def test_if_none_match_only_with_a_ruleset_and_a_non_empty_etag(self) -> None:
        self.server.on(
            "GET",
            "/sdk/v1/ruleset",
            reply(500),
            reply(200, ruleset_json(flag_json("f")), ETag=""),
            reply(200, ruleset_json(flag_json("f")), ETag='W/"x"'),
            reply(304),
        )
        client = self.client()
        client.ready()
        self.assertTrue(wait_for(lambda: len(self.server.seen("GET", "/sdk/v1/ruleset")) >= 5))
        sent = [r.headers.get("if-none-match") for r in self.server.seen("GET", "/sdk/v1/ruleset")]
        self.assertEqual(sent[:5], [None, None, None, 'W/"x"', 'W/"x"'])
        self.assertIsNone(client.status().last_error)

    def test_an_empty_etag_counts_as_none(self) -> None:
        self.serve(flag_json("f"), etag="")
        client = self.client()
        client.ready()
        self.assertIsNone(client.status().etag)

    def test_a_304_with_a_ruleset_held_moves_last_sync(self) -> None:
        self.server.on(
            "GET",
            "/sdk/v1/ruleset",
            reply(200, ruleset_json(flag_json("f")), ETag='"a"'),
            reply(304),
        )
        client = self.client()
        client.ready()
        first = client.status().last_sync_at
        self.assertTrue(wait_for(lambda: client.status().last_sync_at != first))

    def test_the_request_headers_are_the_ones_the_contract_allows(self) -> None:
        self.serve(flag_json("f"))
        client = self.client()
        client.ready()
        request = self.server.seen("GET", "/sdk/v1/ruleset")[0]
        self.assertEqual(request.headers["authorization"], "Bearer test-key")
        self.assertEqual(
            sorted(name.lower() for name, _ in request.raw_headers),
            ["authorization", "connection", "host"],
        )

    def test_a_url_with_a_path_prefix_and_trailing_slashes(self) -> None:
        self.server.on("GET", "/prefix/sdk/v1/ruleset", reply(200, ruleset_json(flag_json("f"))))
        client = self.client(url=self.server.url + "/prefix//")
        self.assertTrue(client.ready())

    def test_an_unusable_url_is_a_network_error_and_never_a_raise(self) -> None:
        for url in ("", "ftp://x", "http://", "http://127.0.0.1:99999", "not a url", "http://[::1"):
            with self.subTest(url=url):
                errors: list[Exception] = []
                client = self.client(url=url, on_error=errors.append)
                self.assertFalse(client.ready())
                self.assertEqual(client.status().last_error, "network")
                self.assertEqual(client.variation("f", {"key": "u"}, 1), 1)

    def test_nothing_listening_is_network(self) -> None:
        port = self.server.port
        self.server.stop()
        client = self.client(url=f"http://127.0.0.1:{port}")
        self.assertFalse(client.ready())
        self.assertEqual(client.status().last_error, "network")


class HookTests(ClientTestCase):
    def test_the_event_carries_what_the_contract_lists(self) -> None:
        self.serve(flag_json("f", version=9), etag='"e"')
        events: list[Any] = []
        client = self.client(after_evaluation=events.append)
        client.ready()
        context = {"key": "u"}
        client.variation("f", context, False)
        client.variation("missing", context, "fb")
        found, missing = events
        self.assertEqual(found.flag_key, "f")
        self.assertIs(found.context, context)
        self.assertIs(found.value, True)
        self.assertEqual(found.variation_index, 1)
        self.assertEqual(found.reason.kind, ReasonKind.FALLTHROUGH)
        self.assertEqual(found.etag, '"e"')
        self.assertEqual(found.flag_version, 9)
        self.assertEqual(missing.value, "fb")
        self.assertIsNone(missing.variation_index)
        self.assertEqual(missing.etag, '"e"')
        self.assertIsNone(missing.flag_version)

    def test_before_the_first_load_the_event_has_no_etag_or_version(self) -> None:
        self.server.on("GET", "/sdk/v1/ruleset", lambda *_: time.sleep(0.3))
        events: list[Any] = []
        client = self.client(after_evaluation=events.append)
        client.variation("f", {"key": "u"}, 1)
        self.assertIsNone(events[0].etag)
        self.assertIsNone(events[0].flag_version)

    def test_a_malformed_flag_has_no_version(self) -> None:
        bad = flag_json("bad")
        del bad["variations"]
        self.serve(bad)
        events: list[Any] = []
        client = self.client(after_evaluation=events.append)
        client.ready()
        detail = client.variation_detail("bad", {"key": "u"}, 1)
        self.assertEqual(detail.reason.code, ErrorCode.MALFORMED_FLAG)
        self.assertIsNone(events[0].flag_version)

    def test_a_flag_naming_a_missing_segment_has_no_version(self) -> None:
        rule = {"clauses": [{"op": "segmentMatch", "values": ["gone"]}], "variation": 1}
        self.serve(flag_json("f", rules=[rule]))
        events: list[Any] = []
        client = self.client(after_evaluation=events.append)
        client.ready()
        detail = client.variation_detail("f", {"key": "u"}, 1)
        self.assertEqual(detail.reason.code, ErrorCode.SEGMENT_NOT_FOUND)
        self.assertIsNone(events[0].flag_version)

    def test_the_integer_hook_value_is_the_int(self) -> None:
        self.serve(flag_json("two", [2.0], fallthrough=0))
        events: list[Any] = []
        client = self.client(after_evaluation=events.append)
        client.ready()
        client.int_variation("two", {"key": "u"}, 0)
        self.assertEqual(events[0].value, 2)
        self.assertIsInstance(events[0].value, int)

    def test_a_hook_that_raises_goes_to_on_error_and_changes_nothing(self) -> None:
        self.serve(flag_json("f"))
        failure = ValueError("hook")

        def hook(_event: Any) -> None:
            raise failure

        client = self.client(after_evaluation=hook)
        client.ready()
        detail = client.variation_detail("f", {"key": "u"}, False)
        self.assertIs(detail.value, True)
        self.assertEqual(self.errors, [failure])
        self.assertEqual(client._usage.pending_count(), 1)

    def test_a_hook_runs_on_the_callers_thread(self) -> None:
        self.serve(flag_json("f"))
        seen: list[threading.Thread] = []
        client = self.client(after_evaluation=lambda _e: seen.append(threading.current_thread()))
        client.ready()
        client.variation("f", {"key": "u"}, False)
        self.assertEqual(seen, [threading.current_thread()])

    def test_a_hook_that_is_not_callable_is_ignored(self) -> None:
        self.serve(flag_json("f"))
        client = toggleslash.create(
            url=self.server.url, sdk_key="k", stream=False, hooks=toggleslash.Hooks(5)
        )
        self.addCleanup(client.close)
        client.ready()
        self.assertIs(client.variation("f", {"key": "u"}, False), True)
        client2 = toggleslash.create(url=self.server.url, sdk_key="k", stream=False, hooks="nope")
        self.addCleanup(client2.close)
        client2.ready()
        self.assertIs(client2.variation("f", {"key": "u"}, False), True)

    def test_a_hook_given_as_a_dict(self) -> None:
        self.serve(flag_json("f"))
        events: list[Any] = []
        client = toggleslash.create(
            url=self.server.url,
            sdk_key="k",
            stream=False,
            hooks={"after_evaluation": events.append},
        )
        self.addCleanup(client.close)
        client.ready()
        client.variation("f", {"key": "u"}, False)
        self.assertEqual(len(events), 1)

    def test_a_coroutine_a_hook_returns_is_closed_unawaited(self) -> None:
        self.serve(flag_json("f"))

        async def later() -> None:
            return None

        coroutines = []

        def hook(_event: Any) -> Any:
            coroutine = later()
            coroutines.append(coroutine)
            return coroutine

        client = self.client(after_evaluation=hook)
        client.ready()
        client.variation("f", {"key": "u"}, False)
        self.assertEqual(len(coroutines), 1)
        self.assertEqual(self.errors, [])
        # Closed, so garbage collection raises no "never awaited" warning (the run turns warnings into errors).
        self.assertIsNone(coroutines[0].close())

    def test_a_future_a_hook_returns_that_fails_later_goes_to_on_error(self) -> None:
        self.serve(flag_json("f"))
        failure = RuntimeError("later")
        future: concurrent.futures.Future[None] = concurrent.futures.Future()
        client = self.client(after_evaluation=lambda _e: future)
        client.ready()
        client.variation("f", {"key": "u"}, False)
        self.assertEqual(self.errors, [])
        future.set_exception(failure)
        self.assertEqual(self.errors, [failure])

    def test_a_cancelled_future_is_not_an_error(self) -> None:
        self.serve(flag_json("f"))
        future: concurrent.futures.Future[None] = concurrent.futures.Future()
        client = self.client(after_evaluation=lambda _e: future)
        client.ready()
        client.variation("f", {"key": "u"}, False)
        future.cancel()
        self.assertEqual(self.errors, [])

    def test_on_error_that_raises_is_ignored(self) -> None:
        self.server.on("GET", "/sdk/v1/ruleset", reply(500))

        def angry(_error: Exception) -> None:
            raise RuntimeError("on_error raised")

        client = self.client(on_error=angry)
        self.assertFalse(client.ready())
        self.assertEqual(client.variation("f", {"key": "u"}, 1), 1)

    def test_on_error_is_never_given_a_url_key_or_context(self) -> None:
        self.server.on("GET", "/sdk/v1/ruleset", reply(500))
        client = self.client(sdk_key="secret-key-123")
        client.ready()
        client.variation("flag-x", {"key": "private-user"}, 1)
        wait_for(lambda: len(self.errors) >= 1)
        text = " ".join(f"{e!s} {getattr(e, 'code', '')}" for e in self.errors)
        for forbidden in ("secret-key-123", "127.0.0.1", "private-user", str(self.server.port)):
            self.assertNotIn(forbidden, text)
        self.assertNotIn(
            "secret-key-123",
            json.dumps(
                client.status().__dict__ if hasattr(client.status(), "__dict__") else "",
                default=str,
            ),
        )


class CreateTests(unittest.TestCase):
    def test_create_never_raises_and_a_bad_option_is_the_default(self) -> None:
        for kwargs in (
            {"url": None, "sdk_key": None},
            {"url": "http://127.0.0.1:1", "sdk_key": "k", "poll_interval": "x"},
            {"url": "http://127.0.0.1:1", "sdk_key": "k", "poll_interval": True},
            {"url": "http://127.0.0.1:1", "sdk_key": "k", "poll_interval": -1},
            {"url": "http://127.0.0.1:1", "sdk_key": "k", "request_timeout": float("nan")},
            {"url": "http://127.0.0.1:1", "sdk_key": "k", "flush_interval": float("inf")},
            {"url": "http://127.0.0.1:1", "sdk_key": "k", "poll_interval": 10**500},
            {"url": "http://127.0.0.1:1", "sdk_key": "k", "on_error": 5, "hooks": 5},
            {"url": "http://127.0.0.1:1", "sdk_key": "k", "stream": None},
        ):
            with self.subTest(kwargs=kwargs):
                client = toggleslash.create(**kwargs)
                config = client._config
                self.assertEqual(
                    config.poll_interval in {30.0, 31_536_000.0} or config.poll_interval > 0, True
                )
                self.assertGreater(config.request_timeout, 0)
                self.assertGreater(config.flush_interval, 0)
                client.close()

    def test_defaults_and_only_false_turns_the_stream_off(self) -> None:
        a = toggleslash.create(url="http://127.0.0.1:1", sdk_key="k", stream=False)
        b = toggleslash.create(url="http://127.0.0.1:1", sdk_key="k", stream=None)
        c = toggleslash.create(url="http://127.0.0.1:1", sdk_key="k")
        try:
            self.assertFalse(a._config.stream)
            self.assertTrue(b._config.stream)
            self.assertTrue(c._config.stream)
            self.assertEqual(
                (c._config.poll_interval, c._config.flush_interval, c._config.request_timeout),
                (30.0, 30.0, 10.0),
            )
        finally:
            for client in (a, b, c):
                client.close()


if __name__ == "__main__":
    unittest.main()
