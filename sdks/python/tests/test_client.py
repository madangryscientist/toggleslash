"""The client's public behaviour: accessors, results, hooks, status, ``ready`` and ``close``."""

import itertools
import threading
import time
import unittest
from datetime import UTC, datetime, timedelta

from fakes import (
    CONTEXT,
    Recorded,
    ServerCase,
    flag,
    hang,
    ruleset,
    serve_ruleset,
    wait_until,
)
from toggleslash import (
    Detail,
    ErrorCode,
    EvaluationEvent,
    Hooks,
    ReasonKind,
    Status,
    StreamState,
    ToggleSlashError,
    create_client,
)
from toggleslash.client import Seams, _as_int64

RULESET = "/sdk/v1/ruleset"
INT64_MAX = 2**63 - 1
INT64_MIN = -(2**63)


def one(value, key="f"):
    return ruleset(flag(key, [None, value], serve=1))


class AccessorTests(ServerCase):
    def load(self, *flags) -> object:
        serve_ruleset(self.server, ruleset(*flags))
        client = self.make()
        self.assertTrue(client.ready())
        return client

    def test_each_accessor_returns_its_kind(self) -> None:
        client = self.load(
            flag("b", [False, True], serve=1),
            flag("s", ["a", "b"], serve=1),
            flag("n", [1, 2.5], serve=1),
            flag("i", [1, 7], serve=1),
            flag("j", [{}, {"a": [1]}], serve=1),
            flag("l", [[], [1, 2]], serve=1),
        )
        self.assertIs(client.bool_variation("b", CONTEXT, False), True)
        self.assertEqual(client.string_variation("s", CONTEXT, "z"), "b")
        self.assertEqual(client.float_variation("n", CONTEXT, 0.0), 2.5)
        self.assertEqual(client.int_variation("i", CONTEXT, 0), 7)
        self.assertEqual(client.json_variation("j", CONTEXT, {}), {"a": [1]})
        self.assertEqual(client.json_variation("l", CONTEXT, []), [1, 2])
        self.assertEqual(client.variation("s", CONTEXT, "z"), "b")
        self.assertEqual(client.variation("s", CONTEXT), "b")  # a null fallback accepts any kind

    def test_the_detail_carries_the_index_and_the_evaluators_reason(self) -> None:
        client = self.load(flag("s", ["a", "b"], serve=1))
        detail = client.string_variation_detail("s", CONTEXT, "z")
        self.assertIsInstance(detail, Detail)
        self.assertEqual((detail.value, detail.variation_index), ("b", 1))
        self.assertIs(detail.reason.kind, ReasonKind.FALLTHROUGH)

    def test_a_float_accessor_returns_a_float_for_a_whole_number(self) -> None:
        client = self.load(flag("n", [1, 3], serve=1))
        value = client.float_variation("n", CONTEXT, 0.5)
        self.assertEqual(value, 3.0)
        self.assertIsInstance(value, float)

    def test_a_kind_mismatch_gives_the_fallback_and_type_mismatch(self) -> None:
        client = self.load(flag("s", ["a", "b"], serve=1), flag("n", [1, 2], serve=1))
        for detail in (
            client.bool_variation_detail("s", CONTEXT, False),
            client.float_variation_detail("s", CONTEXT, 0.0),
            client.string_variation_detail("n", CONTEXT, "z"),
            client.json_variation_detail("n", CONTEXT, {}),
            client.variation_detail("n", CONTEXT, "x"),
        ):
            self.assertIsNone(detail.variation_index)
            self.assertIs(detail.reason.code, ErrorCode.TYPE_MISMATCH)
        self.assertEqual(client.bool_variation("s", CONTEXT, True), True)

    def test_a_bool_is_not_a_number(self) -> None:
        client = self.load(flag("b", [False, True], serve=1), flag("n", [0, 1], serve=1))
        self.assertIs(client.float_variation_detail("b", CONTEXT, 0.0).reason.code, ErrorCode.TYPE_MISMATCH)
        self.assertIs(client.int_variation_detail("b", CONTEXT, 0).reason.code, ErrorCode.TYPE_MISMATCH)
        self.assertIs(client.bool_variation_detail("n", CONTEXT, False).reason.code, ErrorCode.TYPE_MISMATCH)

    def test_array_and_object_are_different_kinds(self) -> None:
        client = self.load(flag("a", [[], [1]], serve=1), flag("o", [{}, {"k": 1}], serve=1))
        self.assertIs(client.json_variation_detail("a", CONTEXT, {}).reason.code, ErrorCode.TYPE_MISMATCH)
        self.assertIs(client.json_variation_detail("o", CONTEXT, []).reason.code, ErrorCode.TYPE_MISMATCH)
        self.assertIs(client.variation_detail("a", CONTEXT, {}).reason.code, ErrorCode.TYPE_MISMATCH)
        # a null fallback says nothing about the kind, so either is accepted
        self.assertEqual(client.json_variation("a", CONTEXT, None), [1])
        self.assertEqual(client.json_variation("o", CONTEXT, None), {"k": 1})

    def test_a_null_served_value_against_a_fallback_is_a_mismatch(self) -> None:
        client = self.load(flag("z", ["x", None], serve=1))
        self.assertIs(client.variation_detail("z", CONTEXT, "d").reason.code, ErrorCode.TYPE_MISMATCH)
        detail = client.variation_detail("z", CONTEXT, None)
        self.assertEqual((detail.value, detail.variation_index), (None, 1))

    def test_the_integer_accessor_follows_the_contract(self) -> None:
        # Numbers on the wire are binary64 (docs/evaluation.md), so 2**63 - 1 reads as 2.0**63, which is
        # out of range; the largest in-range double is 2**63 - 1024.
        top = 2**63 - 1024
        values = [2, 2.0, 1.5, top, INT64_MIN, 2**63, INT64_MIN - 2048, 1e30, -0.0]
        flags = [flag(f"i{n}", [0, v], serve=1) for n, v in enumerate(values)]
        client = self.load(*flags)
        got = [client.int_variation_detail(f"i{n}", CONTEXT, -1) for n in range(len(values))]
        expected = [2, 2, -1, top, INT64_MIN, -1, -1, -1, 0]
        self.assertEqual([d.value for d in got], expected)
        mismatches = [n for n, d in enumerate(got) if d.reason.code is ErrorCode.TYPE_MISMATCH]
        self.assertEqual(mismatches, [2, 5, 6, 7])
        self.assertIsInstance(got[1].value, int)
        self.assertIsNone(got[2].variation_index)

    def test_the_int64_bound_exactly(self) -> None:
        self.assertEqual(_as_int64(INT64_MAX), INT64_MAX)
        self.assertEqual(_as_int64(INT64_MIN), INT64_MIN)
        self.assertIsNone(_as_int64(INT64_MAX + 1))
        self.assertIsNone(_as_int64(INT64_MIN - 1))
        self.assertEqual(_as_int64(2.0), 2)
        self.assertIsNone(_as_int64(1.5))
        self.assertIsNone(_as_int64(True))
        self.assertIsNone(_as_int64(float("nan")))
        self.assertIsNone(_as_int64(float("inf")))

    def test_the_other_accessors_take_a_fractional_number(self) -> None:
        client = self.load(flag("n", [0, 1.5], serve=1))
        self.assertEqual(client.float_variation("n", CONTEXT, 0.0), 1.5)
        self.assertEqual(client.variation("n", CONTEXT, 0), 1.5)

    def test_the_evaluators_reason_is_unchanged_when_there_is_no_variation(self) -> None:
        off = flag("off", [True, False], on=False)
        off["offVariation"] = None
        client = self.load(off)
        detail = client.bool_variation_detail("off", CONTEXT, True)
        self.assertIs(detail.reason.kind, ReasonKind.OFF)
        self.assertIsNone(detail.variation_index)
        missing = client.bool_variation_detail("nope", CONTEXT, True)
        self.assertIs(missing.reason.code, ErrorCode.FLAG_NOT_FOUND)
        bad_context = client.bool_variation_detail("off", {"key": ""}, True)
        self.assertIs(bad_context.reason.code, ErrorCode.INVALID_CONTEXT)

    def test_not_ready_gives_the_fallback_without_waiting(self) -> None:
        self.server.route("GET", RULESET, hang)
        client = self.make(request_timeout=30)
        started = time.monotonic()
        detail = client.bool_variation_detail("f", CONTEXT, True)
        self.assertLess(time.monotonic() - started, 0.5)
        self.assertEqual((detail.value, detail.variation_index), (True, None))
        self.assertIs(detail.reason.code, ErrorCode.NOT_READY)
        self.assertIs(detail.reason.kind, ReasonKind.ERROR)

    def test_a_ruleset_of_the_wrong_shape_is_not_a_ruleset(self) -> None:
        self.server.route("GET", RULESET, lambda r, o: o.send(200, b'{"flags":[]}'))
        client = self.make()
        self.assertFalse(client.ready())
        self.assertIs(client.bool_variation_detail("f", CONTEXT, True).reason.code, ErrorCode.NOT_READY)

    def test_contexts_that_are_not_objects_do_not_raise(self) -> None:
        client = self.load(flag("b", [False, True], serve=1))
        for context in (None, 5, "s", [], object(), {"kind": 3, "key": "k"}, {"key": 7}):
            self.assertIs(client.bool_variation("b", context, False), False)

    def test_bad_options_fall_back_to_defaults_and_never_raise(self) -> None:
        serve_ruleset(self.server, ruleset(flag("b", [False, True], serve=1)))
        bad = [-1, 0, float("nan"), float("inf"), "x", None, True, [], timedelta(seconds=-1)]
        for value in bad:
            client = self.make(poll_interval=value, flush_interval=value, request_timeout=value)
            self.assertTrue(client.ready(), value)
        client = self.make(stream="no", hooks="nope", on_error=5)
        self.assertTrue(client.ready())
        self.assertTrue(client.bool_variation("b", CONTEXT, False))

    def test_a_timedelta_is_a_duration(self) -> None:
        serve_ruleset(self.server, one(True))
        client = self.make(poll_interval=timedelta(seconds=5), request_timeout=timedelta(seconds=5))
        self.assertTrue(client.ready())

    def test_create_with_nothing_does_not_raise(self) -> None:
        client = create_client()
        self.addCleanup(client.close)
        self.assertFalse(client.ready())
        self.assertEqual(client.variation("f", CONTEXT, 3), 3)

    def test_the_context_manager_closes(self) -> None:
        serve_ruleset(self.server, one(True))
        with create_client(url=self.server.url, sdk_key="k", stream=False) as client:
            self.assertTrue(client.ready())
        self.assertIs(client.status().stream, StreamState.CLOSED)


class StatusTests(ServerCase):
    def test_status_before_and_after_the_first_load(self) -> None:
        self.server.route("GET", RULESET, hang)
        client = self.make(stream=True, request_timeout=30)
        status = client.status()
        self.assertIsInstance(status, Status)
        self.assertEqual((status.etag, status.last_sync_at, status.last_error), (None, None, None))
        self.assertIs(status.stream, StreamState.CONNECTING)

    def test_stream_off_reads_off(self) -> None:
        serve_ruleset(self.server, one(True))
        client = self.make(stream=False)
        client.ready()
        self.assertIs(client.status().stream, StreamState.OFF)

    def test_last_sync_at_is_an_aware_utc_datetime(self) -> None:
        serve_ruleset(self.server, one(True))
        client = self.make()
        client.ready()
        synced = client.status().last_sync_at
        self.assertIsInstance(synced, datetime)
        self.assertEqual(synced.utcoffset(), timedelta(0))
        self.assertLess(abs((datetime.now(UTC) - synced).total_seconds()), 30)

    def test_status_never_waits_on_the_network(self) -> None:
        self.server.route("GET", RULESET, hang)
        client = self.make(request_timeout=30)
        started = time.monotonic()
        for _ in range(2000):
            client.status()
        self.assertLess(time.monotonic() - started, 1.0)


class HookTests(ServerCase):
    def load(self, hooks, **options):
        serve_ruleset(self.server, ruleset(flag("f", [False, True], version=7, serve=1)))
        client = self.make(hooks=hooks, **options)
        self.assertTrue(client.ready())
        return client

    def test_the_hook_sees_the_evaluation(self) -> None:
        events: list[EvaluationEvent] = []
        client = self.load(Hooks(after_evaluation=events.append))
        context = {"key": "u", "attributes": {"plan": "pro"}}
        client.bool_variation("f", context, False)
        (event,) = events
        self.assertEqual(
            (event.flag_key, event.value, event.variation_index, event.etag, event.flag_version),
            ("f", True, 1, '"v1"', 7),
        )
        self.assertIs(event.context, context)
        self.assertIs(event.reason.kind, ReasonKind.FALLTHROUGH)

    def test_the_hook_runs_on_the_callers_thread_and_before_the_call_returns(self) -> None:
        seen = []
        client = self.load(Hooks(after_evaluation=lambda e: seen.append(threading.current_thread())))
        client.bool_variation("f", CONTEXT, False)
        self.assertEqual(seen, [threading.current_thread()])

    def test_the_hook_runs_for_errors_with_no_flag_version(self) -> None:
        events = []
        client = self.load(Hooks(after_evaluation=events.append))
        client.bool_variation("nope", CONTEXT, False)
        self.assertIsNone(events[0].flag_version)
        self.assertIs(events[0].reason.code, ErrorCode.FLAG_NOT_FOUND)

    def test_the_hook_runs_before_the_first_load_too(self) -> None:
        events = []
        self.server.route("GET", RULESET, hang)
        client = self.make(hooks=Hooks(after_evaluation=events.append), request_timeout=30)
        client.bool_variation("f", CONTEXT, False)
        self.assertEqual((events[0].etag, events[0].reason.code), (None, ErrorCode.NOT_READY))

    def test_a_hook_that_raises_goes_to_on_error_and_changes_nothing(self) -> None:
        def boom(event) -> None:
            raise RuntimeError("hook failed")

        client = self.load(Hooks(after_evaluation=boom))
        self.assertIs(client.bool_variation("f", CONTEXT, False), True)
        self.assertEqual([str(e) for e in self.errors], ["hook failed"])
        self.assertEqual(client.status().last_error, None)

    def test_an_async_hook_is_closed_not_leaked_and_is_reported(self) -> None:
        async def late(event) -> None:
            return None

        client = self.load(Hooks(after_evaluation=late))
        self.assertIs(client.bool_variation("f", CONTEXT, False), True)  # -W error would fail on a leak
        self.assertEqual(len(self.errors), 1)
        self.assertIsInstance(self.errors[0], ToggleSlashError)

    def test_an_on_error_that_raises_is_ignored(self) -> None:
        def boom(event) -> None:
            raise RuntimeError("hook failed")

        def worse(error) -> None:
            raise ValueError("on_error failed")

        client = self.load(Hooks(after_evaluation=boom), on_error=worse)
        self.assertIs(client.bool_variation("f", CONTEXT, False), True)

    def test_the_hooks_etag_and_flag_version_are_always_a_pair(self) -> None:
        counter = itertools.count(1)

        def handle(request: Recorded, out) -> None:
            n = next(counter)
            out.send(200, ruleset(flag("f", [False, True], version=n, serve=1)), {"ETag": f'"e{n}"'})

        self.server.route("GET", RULESET, handle)
        pairs: set[tuple[str | None, int | None]] = set()
        lock = threading.Lock()

        def hook(event) -> None:
            with lock:
                pairs.add((event.etag, event.flag_version))

        client = self.make(hooks=Hooks(after_evaluation=hook), poll_interval=0.005)
        client.ready()
        deadline = time.monotonic() + 1.5
        while time.monotonic() < deadline:
            client.bool_variation("f", CONTEXT, False)
            time.sleep(0)
        self.assertGreater(len(pairs), 5, "the ruleset was not swapped while evaluating")
        for etag, version in pairs:
            if etag is not None:
                self.assertEqual(etag, f'"e{version}"')

    def test_a_reentrant_hook_and_on_error_call_back_into_the_client(self) -> None:
        calls = []

        def hook(event) -> None:
            calls.append(client.bool_variation("f", CONTEXT, False))  # no hook recursion guard needed:
            client.status()
            client.flush()

        def on_error(error) -> None:
            client.status()
            client.bool_variation("f", CONTEXT, False)
            client.ready()

        serve_ruleset(self.server, ruleset(flag("f", [False, True], serve=1)))
        client = self.make(hooks=Hooks(after_evaluation=None), on_error=on_error)
        client.ready()
        # a hook that evaluates would recurse forever, so it evaluates only once
        guard = {"in": False}

        def once(event) -> None:
            if guard["in"]:
                return
            guard["in"] = True
            try:
                hook(event)
            finally:
                guard["in"] = False

        client2 = self.make(hooks=Hooks(after_evaluation=once), on_error=on_error)
        client = client2
        client.ready()
        done = threading.Thread(target=lambda: client.bool_variation("f", CONTEXT, False), daemon=True)
        done.start()
        done.join(10)
        self.assertFalse(done.is_alive(), "a reentrant hook deadlocked")
        self.assertTrue(all(calls))

    def test_on_error_may_close_the_client_from_the_sync_thread(self) -> None:
        closed = threading.Event()
        holder = {}

        def on_error(error) -> None:
            holder["client"].close()
            closed.set()

        self.server.route("GET", RULESET, lambda r, o: o.send(500))
        holder["client"] = self.make(on_error=on_error)
        self.assertTrue(closed.wait(10), "close from inside on_error did not return")
        self.assertIs(holder["client"].status().stream, StreamState.CLOSED)

    def test_on_error_may_call_ready_on_the_sync_thread(self) -> None:
        answers = []
        done = threading.Event()

        def on_error(error) -> None:
            answers.append(holder["client"].ready())
            done.set()

        holder = {}
        self.server.route("GET", RULESET, lambda r, o: o.send(500))
        holder["client"] = self.make(on_error=on_error)
        self.assertTrue(done.wait(10), "ready() inside on_error on the sync thread waited for itself")
        self.assertEqual(answers[0], False)


class EvaluationFailureTests(ServerCase):
    def test_an_evaluator_that_raises_is_evaluation_failed_with_one_on_error_call(self) -> None:
        def broken(rules, key, context):
            raise RuntimeError("evaluator bug")

        serve_ruleset(self.server, one(True))
        client = self.make(seams=Seams(evaluate=broken))
        self.assertTrue(client.ready())
        events = []
        detail = client.bool_variation_detail("f", CONTEXT, True)
        self.assertEqual((detail.value, detail.variation_index), (True, None))
        self.assertIs(detail.reason.code, ErrorCode.EVALUATION_FAILED)
        self.assertEqual([str(e) for e in self.errors], ["evaluator bug"])
        self.assertEqual(events, [])
        client.flush()  # nothing was counted
        self.assertEqual(self.server.seen("POST", "/sdk/v1/events"), [])

    def test_the_hook_still_runs_after_an_evaluation_failure(self) -> None:
        events = []

        def broken(rules, key, context):
            raise RuntimeError("evaluator bug")

        serve_ruleset(self.server, one(True))
        client = self.make(seams=Seams(evaluate=broken), hooks=Hooks(after_evaluation=events.append))
        client.ready()
        client.bool_variation("f", CONTEXT, True)
        self.assertIs(events[0].reason.code, ErrorCode.EVALUATION_FAILED)
        self.assertEqual(len(self.errors), 1)

    def test_the_real_evaluator_does_not_raise_on_hostile_rulesets(self) -> None:
        hostile = ruleset(
            {"key": "a", "on": True, "variations": [1], "version": 1, "salt": "s"},
            {"key": "b", "on": "yes"},
            5,
            flag("c", [1], serve=0),
        )
        serve_ruleset(self.server, hostile)
        client = self.make()
        self.assertTrue(client.ready())
        for key in "abc":
            client.variation(key, CONTEXT, 0)
        self.assertEqual(self.errors, [])


class ReadyTests(ServerCase):
    def test_ready_is_fixed_after_the_first_attempt(self) -> None:
        state = {"ok": False}

        def handle(request, out) -> None:
            if state["ok"]:
                out.send(200, one(True), {"ETag": '"x"'})
            else:
                out.send(500)

        self.server.route("GET", RULESET, handle)
        client = self.make(fake_clock=True, poll_interval=1)
        self.assertFalse(client.ready())
        state["ok"] = True
        self.clock.advance(2)
        wait_until(lambda: client.status().etag == '"x"', what="the later load")
        self.assertFalse(client.ready(), "ready changed its answer after the first attempt")
        self.assertTrue(client.bool_variation("f", CONTEXT, False))

    def test_ready_is_true_after_a_good_first_load_even_when_later_polls_fail(self) -> None:
        serve_ruleset(self.server, one(True))
        client = self.make()
        self.assertTrue(client.ready())
        self.assertTrue(client.ready())

    def test_close_settles_an_unsettled_ready_as_false(self) -> None:
        self.server.route("GET", RULESET, hang)
        client = self.make(request_timeout=30)
        answers = []
        waiter = threading.Thread(target=lambda: answers.append(client.ready()), daemon=True)
        waiter.start()
        wait_until(lambda: len(self.server.seen("GET", RULESET)) == 1, what="the request")
        client.close()
        waiter.join(5)
        self.assertEqual(answers, [False])
        self.assertFalse(client.ready())

    def test_many_threads_may_wait_on_ready(self) -> None:
        serve_ruleset(self.server, one(True))
        client = self.make()
        answers = []
        threads = [threading.Thread(target=lambda: answers.append(client.ready())) for _ in range(20)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(10)
        self.assertEqual(answers, [True] * 20)


class ConcurrencyTests(ServerCase):
    """No race detector exists for Python, so these run many threads under ``-X dev -W error`` and join
    every one with a timeout; each worker yields on every turn so the interpreter really interleaves."""

    THREADS = 16

    def swapping_client(self, **options):
        counter = itertools.count(1)

        def handle(request: Recorded, out) -> None:
            n = next(counter)
            out.send(
                200,
                ruleset(flag("f", [False, True], version=n, serve=n % 2)),
                {"ETag": f'"e{n}"'},
            )

        self.server.route("GET", RULESET, handle)
        self.server.route("POST", "/sdk/v1/events", lambda r, o: o.send(204))
        client = self.make(poll_interval=0.003, flush_interval=0.01, **options)
        self.assertTrue(client.ready())
        return client

    def run_workers(self, work, count=THREADS):
        barrier = threading.Barrier(count)
        failures: list[BaseException] = []

        def runner(n: int) -> None:
            try:
                barrier.wait(10)
                work(n)
            except BaseException as error:
                failures.append(error)

        threads = [threading.Thread(target=runner, args=(n,), daemon=True) for n in range(count)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(30)
        self.assertFalse([t for t in threads if t.is_alive()], "a worker did not finish")
        self.assertEqual(failures, [])

    def test_many_threads_evaluate_while_the_ruleset_swaps_and_usage_flushes(self) -> None:
        client = self.swapping_client()
        results: list[bool] = []

        def work(n: int) -> None:
            for i in range(1500):
                detail = client.bool_variation_detail("f", {"key": f"u{n}-{i}"}, False)
                assert detail.reason.kind is ReasonKind.FALLTHROUGH, detail
                assert detail.variation_index in (0, 1)
                client.status()
                time.sleep(0)
            results.append(True)

        self.run_workers(work)
        self.assertEqual(len(results), self.THREADS)
        self.assertEqual(self.errors, [])

    def test_close_races_variation(self) -> None:
        client = self.swapping_client()
        stop = threading.Event()
        seen = []

        def work(n: int) -> None:
            if n == 0:
                time.sleep(0.05)
                client.close()
                stop.set()
                return
            while not stop.is_set():
                detail = client.bool_variation_detail("f", CONTEXT, False)
                seen.append(detail.reason.kind)
                time.sleep(0)
            for _ in range(100):  # and after close it still answers
                assert client.bool_variation_detail("f", CONTEXT, False).variation_index in (0, 1)

        self.run_workers(work)
        self.assertNotIn(ReasonKind.ERROR, seen)
        self.assertIs(client.status().stream, StreamState.CLOSED)

    def test_two_closes_race_and_both_return_after_one_shutdown(self) -> None:
        client = self.swapping_client()
        client.bool_variation("f", CONTEXT, False)
        returned = []

        def work(n: int) -> None:
            client.close()
            returned.append(n)

        self.run_workers(work, count=2)
        self.assertEqual(sorted(returned), [0, 1])
        client.close()
        self.assertEqual(self.errors, [])

    def test_many_closes_race(self) -> None:
        client = self.swapping_client()
        self.run_workers(lambda n: client.close(), count=12)
        self.assertIs(client.status().stream, StreamState.CLOSED)

    def test_variation_returns_at_once_while_a_ruleset_request_hangs(self) -> None:
        first = {"done": False}

        def handle(request: Recorded, out) -> None:
            if not first["done"]:
                first["done"] = True
                out.send(200, one(True), {"ETag": '"a"'})
            else:
                out.hold_until_released()

        self.server.route("GET", RULESET, handle)
        client = self.make(poll_interval=0.01, request_timeout=30)
        self.assertTrue(client.ready())
        wait_until(lambda: len(self.server.seen("GET", RULESET)) >= 2, what="the hung poll")
        started = time.monotonic()
        for _ in range(2000):
            client.bool_variation("f", CONTEXT, False)
            client.status()
        self.assertLess(time.monotonic() - started, 2.0)
        closer = threading.Thread(target=client.close, daemon=True)
        closer.start()
        closer.join(5)
        self.assertFalse(closer.is_alive(), "close waited on a hung ruleset request")

    def test_a_flush_hang_does_not_block_other_threads_and_close_is_bounded(self) -> None:
        serve_ruleset(self.server, one(True))
        self.server.route("POST", "/sdk/v1/events", hang)
        client = self.make(flush_interval=0.05, request_timeout=1)
        client.ready()
        client.bool_variation("f", CONTEXT, False)
        wait_until(lambda: len(self.server.seen("POST", "/sdk/v1/events")) >= 1, what="a hung flush")
        started = time.monotonic()
        for _ in range(2000):
            client.bool_variation("f", CONTEXT, False)
        self.assertLess(time.monotonic() - started, 2.0)
        # an in-flight usage request is bounded by request_timeout, and so is the final flush
        started = time.monotonic()
        client.close()
        self.assertLess(time.monotonic() - started, 2 * 1 + 3)


if __name__ == "__main__":
    unittest.main()
