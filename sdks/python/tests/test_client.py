import json
import threading
import unittest

import toggleslash
from support import CTX, FakeServer, bool_flag, wait_until
from toggleslash import ErrorCode, Hooks, ReasonKind, Seams, StreamState


def make(server, **options):
    errors = []
    options.setdefault("stream", False)
    options.setdefault("request_timeout", 2)
    client = toggleslash.create(url=server.url, sdk_key="k", on_error=errors.append, **options)
    return client, errors


class ClientTest(unittest.TestCase):
    def setUp(self):
        self.server = FakeServer()
        self.addCleanup(self.server.stop)

    def made(self, **options):
        client, errors = make(self.server, **options)
        self.addCleanup(client.close)
        return client, errors

    def test_not_ready_before_the_first_load_and_after_it_the_value(self):
        self.server.hang.add("/sdk/v1/ruleset")
        client, _ = self.made(request_timeout=0.3)
        detail = client.variation_detail("checkout", CTX, False)
        self.assertEqual(detail.reason.kind, ReasonKind.ERROR)
        self.assertEqual(detail.reason.code, ErrorCode.NOT_READY)
        self.assertIs(detail.value, False)

    def test_ready_true_serves_the_flag(self):
        client, errors = self.made()
        self.assertIs(client.ready(), True)
        detail = client.variation_detail("checkout", CTX, False)
        self.assertEqual((detail.value, detail.variation_index), (True, 1))
        self.assertEqual(detail.reason.kind, ReasonKind.FALLTHROUGH)
        self.assertEqual(errors, [])

    def test_ready_is_false_when_the_server_is_down_and_stays_false(self):
        self.server.script("/sdk/v1/ruleset", (500, {}))
        client, errors = self.made()
        self.assertIs(client.ready(), False)
        self.assertEqual(client.status().last_error, "http_500")
        self.assertEqual(len(errors), 1)
        self.assertIs(client.ready(), False)

    def test_ready_is_bounded_by_the_request_timeout(self):
        import time

        self.server.hang.add("/sdk/v1/ruleset")
        client, _ = self.made(request_timeout=0.4)
        start = time.monotonic()
        self.assertIs(client.ready(), False)
        self.assertLess(time.monotonic() - start, 3.0)
        self.assertEqual(client.status().last_error, "timeout")

    def test_the_second_fetch_is_conditional(self):
        client, _ = self.made(poll_interval=0.05)
        client.ready()
        wait_until(lambda: self.server.count("GET", "/sdk/v1/ruleset") >= 3, what="polls")
        first, second = self.server.requests_to("GET", "/sdk/v1/ruleset")[:2]
        self.assertNotIn("if-none-match", first["headers"])
        self.assertEqual(second["headers"]["if-none-match"], '"v1"')
        self.assertEqual(client.status().etag, '"v1"')

    def test_a_ruleset_that_is_not_one_is_bad_ruleset_and_keeps_nothing(self):
        self.server.raw_body = b'{"flags": {}, "segments": []}'
        client, errors = self.made()
        self.assertIs(client.ready(), False)
        self.assertEqual(client.status().last_error, "bad_ruleset")
        self.assertEqual(client.status().etag, None)
        self.assertEqual(len(errors), 1)

    def test_a_bad_ruleset_after_a_good_one_leaves_the_good_one(self):
        client, _ = self.made(poll_interval=0.05)
        client.ready()
        self.server.raw_body = b"not json"
        self.server.set(self.server.ruleset, '"v2"')
        wait_until(lambda: client.status().last_error == "bad_ruleset", what="bad_ruleset")
        self.assertEqual(client.status().etag, '"v1"')
        self.assertIs(client.variation("checkout", CTX, False), True)

    def test_type_mismatch_falls_back_and_is_not_counted(self):
        client, _ = self.made()
        client.ready()
        detail = client.variation_detail("checkout", CTX, "text")
        self.assertEqual(detail.reason.code, ErrorCode.TYPE_MISMATCH)
        self.assertEqual(detail.value, "text")
        self.assertIsNone(detail.variation_index)
        client.close()
        self.assertEqual(self.server.count("POST", "/sdk/v1/events"), 0)

    def test_a_null_fallback_takes_any_kind_and_a_null_value_is_a_mismatch_for_others(self):
        self.server.set({"flags": [self._flag("n", [None, 1])], "segments": []}, '"v1"')
        client, _ = self.made()
        client.ready()
        none_fallback = client.variation_detail("n", CTX, None)
        self.assertEqual((none_fallback.value, none_fallback.variation_index), (None, 0))
        mismatch = client.variation_detail("n", CTX, "text")
        self.assertEqual(mismatch.reason.code, ErrorCode.TYPE_MISMATCH)

    def _flag(self, key, variations, serve=0):
        flag = bool_flag(key, True)
        flag["variations"] = variations
        flag["fallthrough"] = {"variation": serve}
        return flag

    def test_json_accessor_compares_kinds(self):
        self.server.set(
            {
                "flags": [self._flag("a", [[1, 2]]), self._flag("o", [{"x": 1}]), self._flag("s", ["v"])],
                "segments": [],
            },
            '"v1"',
        )
        client, _ = self.made()
        client.ready()
        self.assertEqual(client.json_variation("a", CTX, []), [1, 2])
        self.assertEqual(client.json_variation("a", CTX, {"k": 1}), {"k": 1})  # an array against an object
        self.assertEqual(client.json_variation_detail("a", CTX, {}).reason.code, ErrorCode.TYPE_MISMATCH)
        self.assertEqual(client.json_variation("o", CTX, {}), {"x": 1})
        self.assertEqual(client.json_variation("a", CTX, None), [1, 2])
        self.assertEqual(client.json_variation_detail("s", CTX, None).reason.code, ErrorCode.TYPE_MISMATCH)

    def test_a_served_container_is_a_copy(self):
        self.server.set({"flags": [self._flag("a", [{"x": [1]}])], "segments": []}, '"v1"')
        client, _ = self.made()
        client.ready()
        value = client.variation("a", CTX, {})
        value["x"].append(2)
        self.assertEqual(client.variation("a", CTX, {}), {"x": [1]})

    def test_integer_accessor_bounds(self):
        raw = (
            '{"segments": [], "flags": ['
            + ",".join(
                json.dumps(self._flag(key, [None], 0)).replace("[null]", literal)
                for key, literal in [
                    ("big", "[9223372036854775807]"),
                    ("two", "[2.0]"),
                    ("frac", "[2.5]"),
                    ("min", "[-9223372036854775808]"),
                    ("huge", "[1e30]"),
                    ("ok", "[9007199254740992]"),
                    ("bool", "[true]"),
                ]
            )
            + "]}"
        )
        self.server.raw_body = raw.encode()
        client, _ = self.made()
        client.ready()
        code = lambda key: client.int_variation_detail(key, CTX, -1).reason.code  # noqa: E731
        self.assertEqual(code("big"), ErrorCode.TYPE_MISMATCH)  # 2**63-1 is 2**63 as binary64
        self.assertEqual(client.int_variation("two", CTX, -1), 2)
        self.assertIsInstance(client.int_variation("two", CTX, -1), int)
        self.assertEqual(code("frac"), ErrorCode.TYPE_MISMATCH)
        self.assertEqual(client.int_variation("min", CTX, -1), -(2**63))
        self.assertEqual(code("huge"), ErrorCode.TYPE_MISMATCH)
        self.assertEqual(client.int_variation("ok", CTX, -1), 2**53)
        self.assertEqual(code("bool"), ErrorCode.TYPE_MISMATCH)

    def test_typed_accessors(self):
        client, _ = self.made()
        client.ready()
        self.assertIs(client.bool_variation("checkout", CTX, False), True)
        self.assertEqual(client.string_variation("checkout", CTX, "d"), "d")
        self.assertEqual(client.number_variation("checkout", CTX, 3), 3)
        self.assertEqual(client.number_variation_detail("checkout", CTX, 3).reason.code, ErrorCode.TYPE_MISMATCH)

    def test_unknown_flag_is_the_evaluators_error_not_a_mismatch(self):
        client, _ = self.made()
        client.ready()
        detail = client.variation_detail("nope", CTX, 5)
        self.assertEqual(detail.reason.code, ErrorCode.FLAG_NOT_FOUND)
        self.assertEqual(detail.value, 5)

    def test_invalid_context_is_an_error_result(self):
        client, _ = self.made()
        client.ready()
        self.assertEqual(client.variation_detail("checkout", 42, False).reason.code, ErrorCode.INVALID_CONTEXT)

    def test_an_evaluator_that_raises_is_evaluation_failed_and_one_on_error(self):
        def boom(*_args):
            raise RuntimeError("boom")

        client, errors = self.made(_seams=Seams(evaluator=boom))
        client.ready()
        detail = client.variation_detail("checkout", CTX, False)
        self.assertEqual(detail.reason.code, ErrorCode.EVALUATION_FAILED)
        self.assertIs(detail.value, False)
        self.assertEqual(len(errors), 1)
        self.assertEqual(str(errors[0]), "boom")

    def test_hook_gets_every_evaluation_with_the_etag_and_flag_version(self):
        events = []
        client, _ = self.made(hooks=Hooks(after_evaluation=events.append))
        client.variation("checkout", CTX, False)  # not ready yet or ready: either way an event
        client.ready()
        client.variation("checkout", CTX, False)
        client.variation("nope", CTX, 1)
        last = events[-2]
        self.assertEqual((last.flag_key, last.value, last.variation_index), ("checkout", True, 1))
        self.assertEqual((last.etag, last.flag_version), ('"v1"', 7))
        self.assertEqual(last.context, CTX)
        missing = events[-1]
        self.assertEqual((missing.value, missing.variation_index, missing.flag_version), (1, None, None))
        self.assertEqual(missing.reason.code, ErrorCode.FLAG_NOT_FOUND)

    def test_a_hook_that_raises_goes_to_on_error_and_the_answer_is_the_same(self):
        def hook(_event):
            raise ValueError("hook")

        client, errors = self.made(hooks=Hooks(after_evaluation=hook))
        client.ready()
        self.assertIs(client.variation("checkout", CTX, False), True)
        self.assertEqual([str(e) for e in errors], ["hook"])

    def test_a_hook_that_is_not_callable_is_ignored(self):
        client, errors = self.made(hooks=Hooks(after_evaluation="no"))
        client.ready()
        self.assertIs(client.variation("checkout", CTX, False), True)
        self.assertEqual(errors, [])

    def test_a_hook_and_on_error_may_call_back_into_the_client(self):
        seen = []
        holder = {}

        def hook(event):
            seen.append(holder["client"].variation("legacy", CTX, True))
            seen.append(holder["client"].status().etag)
            raise ValueError("after calling back")

        def on_error(error):
            seen.append(holder["client"].status().stream)
            raise RuntimeError("on_error raises too")

        client = toggleslash.create(
            url=self.server.url,
            sdk_key="k",
            stream=False,
            hooks=Hooks(after_evaluation=hook),
            on_error=on_error,
        )
        holder["client"] = client
        self.addCleanup(client.close)
        client.ready()
        self.assertIs(client.variation("checkout", CTX, False), True)
        self.assertIn(False, seen)
        self.assertIn('"v1"', seen)

    def test_close_is_idempotent_and_status_is_closed_and_evaluation_still_answers(self):
        client, _ = self.made()
        client.ready()
        client.close()
        client.close()
        self.assertEqual(client.status().stream, StreamState.CLOSED)
        self.assertIs(client.variation("checkout", CTX, False), True)

    def test_nothing_is_counted_after_close_begins(self):
        client, _ = self.made()
        client.ready()
        client.close()
        client.variation("checkout", CTX, False)
        self.assertEqual(self.server.count("POST", "/sdk/v1/events"), 0)

    def test_a_context_manager_closes(self):
        with toggleslash.create(url=self.server.url, sdk_key="k", stream=False) as client:
            client.ready()
        self.assertEqual(client.status().stream, StreamState.CLOSED)

    def test_a_trailing_slash_is_removed_from_the_url(self):
        client = toggleslash.create(url=self.server.url + "//", sdk_key="k", stream=False)
        self.addCleanup(client.close)
        self.assertIs(client.ready(), True)

    def test_a_bad_url_is_a_network_error_not_a_raise(self):
        errors = []
        client = toggleslash.create(url="not a url", sdk_key="k", stream=False, on_error=errors.append)
        self.addCleanup(client.close)
        self.assertIs(client.ready(), False)
        self.assertEqual(client.status().last_error, "network")
        self.assertEqual(len(errors), 1)

    def test_odd_option_values_are_defaults(self):
        client = toggleslash.create(
            url=self.server.url,
            sdk_key="k",
            stream=False,
            poll_interval=-1,
            flush_interval=float("nan"),
            request_timeout=True,
            on_error="no",
        )
        self.addCleanup(client.close)
        self.assertIs(client.ready(), True)

    def test_ready_called_from_a_callback_returns_at_once(self):
        results = []
        done = threading.Event()

        def on_error(_error):
            results.append(holder["client"].ready())
            done.set()

        self.server.script("/sdk/v1/ruleset", (500, {}))
        holder = {}
        client = toggleslash.create(
            url=self.server.url, sdk_key="k", stream=False, on_error=on_error, request_timeout=5
        )
        holder["client"] = client
        self.addCleanup(client.close)
        self.assertTrue(done.wait(5))
        self.assertEqual(results, [False])

    def test_requests_carry_only_the_allowed_headers(self):
        client, _ = self.made(poll_interval=0.05, flush_interval=0.05)
        client.ready()
        client.variation("checkout", {"key": "SENTINEL-USER-9f3a"}, False)
        wait_until(lambda: self.server.count("POST", "/sdk/v1/events") >= 1, what="a usage post")
        allowed = {
            "authorization",
            "accept",
            "if-none-match",
            "content-type",
            # the HTTP library's own:
            "host",
            "accept-encoding",
            "content-length",
        }
        with self.server.lock:
            requests = list(self.server.requests)
        for request in requests:
            self.assertLessEqual(set(request["headers"]), allowed)
            wire = json.dumps([request["path"], request["headers"]]) + request["body"].decode()
            self.assertNotIn("SENTINEL-USER", wire)
        self.assertNotIn("SENTINEL-USER", repr(client.status()))


if __name__ == "__main__":
    unittest.main()
