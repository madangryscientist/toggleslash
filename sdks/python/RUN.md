# Python SDK: run report

## Prompt version

- Prompt: 7dc76f10af412e2f2faacbe5f8336038c2b99b89 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-10
- Input: language Python; runtime chosen: CPython; minimum version chosen: 3.11; package manager chosen: pip with a
  setuptools build backend (the SDK has no runtime dependency, so no resolver matters)
- Toolchain: `mise.toml` pins `python = "3.15.2"` and `ruff = "0.17.4"`. `mise exec -- python --version` prints
  `Python 3.15.2`; `mise exec -- ruff --version` prints `ruff 0.17.4`. The system `python` is 3.14, so the pin is
  not an accident of the host. No type checker, no pytest, no other tool.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2
  count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
  (`bun run conformance -- --service sdks/python/bin/test-service`, exit 0, run twice)
- Capabilities advertised: `integer-accessor`
- Native tests: 117 passed of 117 (`Ran 117 tests`, which includes the vector and pin tests)
- Stability: `bin/test` passed 10 times in a row (117 tests each, 54.4 s to 56.5 s), then once more with 12 busy loops running (one per core, `TOGGLESLASH_TEST_LOAD=1`): 117 tests in 57.7 s, all passed

## Decisions

1. Test runner: `unittest` over pytest. The prompt does not name one, the standard library has one, and the SDK and
   its tests then need nothing installed.
2. Lint and format: ruff only (check and format), pinned in `mise.toml`. No type checker: the prompt names none
   (guess 2), and annotations are kept accurate by hand and by ruff's rules.
3. Build backend: setuptools in `pyproject.toml`, over hatchling or flit, because it is the stock one and `pip install ./sdks/python`
   then needs no network for a build tool beyond what pip fetches itself.
4. Minimum version 3.11 (`enum.StrEnum` and `datetime.UTC` exist from there; `requires-python = ">=3.11"`), pin 3.15.2 for the toolchain.
5. Provisioning in CI: `jdx/mise-action` (pinned by SHA) over `actions/setup-python`, so CI and a local run use the one
   `mise.toml` and the one pin.
6. ruff `line-length = 110` over the default 88, because the contract's names and the test fixtures read worse wrapped.
7. No runtime dependency. HTTP is `http.client`, JSON is `json`, SHA-256 is `hashlib`, threads are `threading`.
8. A hand-written SSE parser over a library: it is about 60 lines, is tested byte at a time (`tests/test_sse.py`), and
   a library would decode on its own schedule instead of the contract's.
9. Both accessor shapes: one generic `variation` / `variation_detail` over the fallback's JSON kind, and typed
   `bool_`, `string_`, `float_`, `int_`, `json_` accessors (contract 13 allows both).
10. `create_client(url="", sdk_key="")` defaults to empty strings instead of raising on a missing required argument,
    because contract 5.1 says no operation raises. A missing key then shows up as a status error, not an exception.
11. The usage flush has its own `GuardSet`, which `close` never aborts. An in-flight usage request is not abandoned
    (contract 6: `close` sends the final flush), so a hung events route costs `close` about twice `request_timeout`
    (the in-flight request, then the final flush). Both are bounded.
12. DNS resolution has no timeout in `socket.create_connection`. A helper thread resolves, and the caller abandons it at
    the deadline (a daemon thread that finishes later and is dropped), so `request_timeout` bounds the whole request.
13. `ready()` called from an SDK callback thread (an `on_error` on the sync thread) returns the held state at once
    instead of waiting for itself (guess 9).
14. The hook also runs for `NOT_READY` and error results, with `flag_version` `None` (guess 11).

## Guesses

1. `sdks/python/bin/*` is named in PROMPT.md "Entry points" as an example to follow, and a cold run cannot read it
   because the old files are removed. Where: PROMPT.md, "Entry points". Assumed: the table of three scripts and the
   rules beside it are the whole spec; wrote them from that.
2. No Python type checker is named, and the checklist asks for none. Where: PROMPT.md, phase 5 and language-notes/python.md.
   Assumed: ruff alone is the lint, and a type checker is an extra a run may skip. Recorded as decision 2.
3. "`bin/test` builds" means little for an interpreted language. Where: PROMPT.md, "Entry points". Assumed: byte-compile
   everything with `compileall` (the SDK, the tests and the test service) so a syntax error anywhere fails, then run
   the tests under `-X dev -W error`.
4. pytest or unittest is unspecified. Where: PROMPT.md, phase 2 and 3. Assumed: unittest (decision 1).
5. The `ob-spinoff` skill describes omnibus gates (nx, three skeptical review legs run by a script this repo does not have)
   and the brief says to run it end to end. Where: the brief against the skill. Assumed: only the steps that map to this
   repo apply (State line, rebase, push, PR, handoff); the skeptical review legs are not run, and the PR says so.
6. The Python language note cites an older prompt (`647f849`). Where: language-notes/python.md, line 3. Assumed: its
   facts still hold, which this run's tests confirmed (big integer literals, `-X dev -W error`, no race detector).
7. How `close` is bounded when DNS hangs, and when a usage request is in flight. Where: contract 6 and 11, which say
   `close` returns after the final flush but give no bound for a flush that hangs. Assumed: about twice
   `request_timeout` in the worst case (decision 11), and a test asserts it.
8. The generic `variation` is kind-only: it checks the fallback's JSON kind (null accepts any), and a `bool` is a
   boolean, never a number, although Python's `bool` is an `int`. Where: contract 4, "Typed languages" speaks of typed
   accessors only. Assumed: as stated.
9. `ready()` called from an SDK callback thread. Where: contract 5.3 and 6.4 are silent on a callback that waits for
   the first load on the very thread that does the load. Assumed: it must not deadlock, so it returns the held state at once.
10. `json_variation` accepts an array or an object and treats a null fallback as accepting any kind. Where: contract 4
    names no `json` accessor. Assumed: as stated, matching the harness's `json` accessor in `docs/sdk-test-service.md`.
11. Whether the `afterEvaluation` hook runs for `NOT_READY` results. Where: contract 10 says the hook runs "after every
    evaluation". Assumed: yes, including before the first load, with `etag` and `flag_version` `None`.
12. Wire numbers are binary64, so the exact literal `2**63-1` reads as `2.0**63`, which is out of signed 64-bit range.
    Where: contract 4, "Typed languages", and the `huge` scenario. Assumed: the largest in-range value is `2**63 - 1024`;
    a test checks that value and `_as_int64` directly at the exact bounds. The first draft of the test used `2**63 - 1`
    and failed, which is how this surfaced.
13. TLS is untested: the harness and the fake servers are plain HTTP on loopback. Where: contract 7.1 allows `https`.
    Assumed: `http.client.HTTPSConnection` with the default context is right; no test reaches it.
14. The `mise-action` inputs (`working_directory`, `install_args`) are not in the workflow skeleton. Where: PROMPT.md,
    phase 5. Assumed: I looked at the sibling `.github/workflows/sdk-ruby.yml` for the input names, which is outside
    "cold", and used `working_directory: sdks/python` and `install_args: python ruff`. Nothing else was copied.
15. `lastSyncAt` in the test service is epoch milliseconds. Where: `docs/sdk-test-service.md`. Assumed: read from the
    protocol's own words; the harness's shape check on every status read confirms it.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md` "Entry points", replace "`sdks/python/bin/*` and `sdks/go/bin/*` are examples" with
   "the three scripts in the table above are the whole spec; there is no example to read, and in a cold run the
   old ones are removed".
2. For guess 2, in `language-notes/python.md`, add: "No type checker is required. ruff check and ruff format are the
   lint, both pinned in `mise.toml`."
3. For guess 3, in `PROMPT.md` "Entry points", after "`bin/test` builds every time", add: "In an interpreted language
   that is a byte-compile of every source, test and test-service file, so a syntax error in a file no test imports
   still fails."
4. For guess 4, in `language-notes/python.md`, add: "unittest and pytest both fit; unittest needs nothing installed,
   and `python -m unittest discover -s tests -t tests` finds `test_*.py`."
5. For guess 5, in the `ob-spinoff` skill (outside `docs/`, so for its owner), add a section "In a repo that is not
   omnibus": the State line, rebase, push, PR and handoff apply; the nx gates and the skeptical review legs do not.
6. For guess 6, in `language-notes/python.md` line 3, update the cited prompt hash and date when the note is next
   touched.
7. For guess 7, in `docs/sdk-contract.md` section 11, add a SHOULD: "`close` returns within a bound of a small
   multiple of `requestTimeout`, even when a request hangs", so every SDK states the same bound.
8. For guess 9, in `docs/sdk-contract.md` section 6.4, add: "`ready` called from a callback the SDK runs on its own
   thread MUST NOT wait for the load that thread performs."
9. For guess 12, in `docs/sdk-contract.md` section 4 "Typed languages", add: "A number on the wire is a binary64
   double, so the largest integer a test can serve and still read as in range is `2^63 - 1024`; `2^63 - 1` as a literal
   reads as `2^63`." and in `language-notes/python.md`, a pointer to it.
10. For guess 14, in `PROMPT.md` phase 5 (the workflow skeleton), include the toolchain step's inputs (for `jdx/mise-action`:
    `working_directory` and `install_args`), so a cold run does not look at a sibling's workflow.
11. For guess 11, in `docs/sdk-contract.md` section 10, add: "The hook also runs for `NOT_READY` and for error results;
    `etag` and `flagVersion` are then null."
12. For guess 13, in `CHECKLIST.md` "Native tests", add a SHOULD line: "A `https` URL reaches the TLS path (a test with a
    local certificate), or `RUN.md` says it is untested." so the gap is a stated one.

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, from the repository at test time, count equal to
  the phase 2 count command: `tests/test_vectors.py` (`test_every_vector`, `test_every_bucket_pin`) reads
  `../../fixtures/evaluation` through `tests/support.py` (`FIXTURES`), fails on zero files or zero vectors, and prints
  `vectors: 1371 pins: 17`. The count command (`bun -e ...` from PROMPT.md phase 2) prints `1371 17`.
- [x] `bin/test` passes 10 times in a row and once under load: 10 of 10 passes at 117 tests each, then 1 of 1 under 12 busy loops (one per core, `TOGGLESLASH_TEST_LOAD=1`) at 117 tests; no failure, no flake.
- [x] The harness exits 0 with only the two named skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369
  passed, 0 failed, 2 skipped`; capability `integer-accessor` is advertised, so none of the integer scenarios is skipped.

### What the harness checks

- [x] Both context shapes: the harness (`privacy.json`, the `kinds.json` vectors through the service); natively
  `test_contexts_that_are_not_objects_do_not_raise` and every `kinds.json` vector in `test_every_vector`.
- [x] Result order and the mismatch rule with the SDK's own codes: the harness (`evaluation.json`, `usage.json`);
  natively `AccessorTests` (`test_a_kind_mismatch_gives_the_fallback_and_type_mismatch`,
  `test_a_null_served_value_against_a_fallback_is_a_mismatch`, `test_not_ready_gives_the_fallback_without_waiting`).
- [x] With `integer-accessor`, a fraction is `TYPE_MISMATCH` and `2.0` is `2`: the harness (`evaluation.json`);
  `test_the_integer_accessor_follows_the_contract`.
- [x] No failure makes an operation raise: the harness (every scenario; the service answers 500 on anything raised);
  natively `test_bad_options_fall_back_to_defaults_and_never_raise`, `test_create_with_nothing_does_not_raise`,
  `test_a_url_that_is_not_a_url_does_not_raise`.
- [x] `NOT_READY` at once before the first load: the harness (`load.json`); `test_not_ready_gives_the_fallback_without_waiting`.
- [x] `ready` settles after the first attempt, bounded, repeatable, false after `close` if unsettled: the harness
  (`load.json`, `close.json`); `ReadyTests` (`test_ready_is_fixed_after_the_first_attempt`,
  `test_close_settles_an_unsettled_ready_as_false`, `test_many_threads_may_wait_on_ready`).
- [x] An answer that is not a ruleset never replaces the held one: the harness (`status.json`);
  `test_a_body_that_is_not_json_is_bad_ruleset_and_the_last_good_one_stays`, `test_a_ruleset_of_the_wrong_shape_is_not_a_ruleset`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining: the harness (`load.json`, `status.json`);
  `test_first_load_sends_the_key_and_no_condition`, `test_a_later_poll_is_conditional_and_a_304_is_a_sync`,
  `test_a_trailing_slash_on_the_url_is_dropped`, `test_a_url_with_a_path_prefix_keeps_it`.
- [x] The stream: the harness (`stream.json`, `load.json`, `status.json`); `StreamTests` and `tests/test_sse.py`
  (14 parser tests, including byte at a time and a split multibyte character), `test_a_failed_refresh_ends_the_stream`.
- [x] The loop and the jittered backoff: the harness (`backoff.json`, `stream.json`);
  `test_the_backoff_doubles_from_one_second_with_jitter_and_stops_at_poll_interval`, `test_jitter_is_between_half_and_whole`,
  `test_polling_waits_exactly_poll_interval` (through an injected clock).
- [x] `429` and `Retry-After` on every route: the harness (`rate-limit.json`); `test_a_429_on_the_ruleset_route_holds_the_stream_too`,
  `test_a_429_on_the_stream_route_holds_for_300`, `test_a_429_on_events_does_not_hold_the_ruleset`,
  `test_retry_after_that_is_not_whole_decimal_seconds_holds_nothing`.
- [x] Status fields, states and `lastError` codes: the harness (`status.json` and the shape check on every status read);
  `StatusTests` (4 tests), `test_connected_marks_the_stream_and_clears_the_error`.
- [x] Usage counts: the harness (`usage.json`, `rate-limit.json`); `UsageTests` (`test_counts_are_summed_per_flag_variation_and_day`,
  `test_more_than_500_entries_go_out_500_at_a_time`, `test_a_server_error_keeps_the_counts_and_stops_the_flush`,
  `test_a_refused_batch_is_dropped_and_the_flush_goes_on`, `test_408_and_network_failures_keep_the_counts`).
- [x] Hook events and `onError`, a raising hook, a raising `onError`: the harness (`hooks.json`);
  `test_a_hook_that_raises_goes_to_on_error_and_changes_nothing`, `test_an_on_error_that_raises_is_ignored`,
  `test_the_hook_sees_the_evaluation`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness (`close.json`, `rate-limit.json`,
  `usage.json`, `status.json`); `test_close_sends_the_last_counts`, `test_close_in_a_hold_loses_the_counts_and_says_so_with_the_entry_count`,
  `test_two_closes_race_and_both_return_after_one_shutdown`.
- [x] No context, key or attribute in any request or `onError` message, and no extra header: the harness (`privacy.json`
  and the after-every-scenario checks); natively `test_no_context_key_or_attribute_is_in_any_request`,
  `test_request_headers_are_only_the_ones_the_protocol_needs`,
  `test_the_request_has_exactly_the_four_fields_and_the_headers_it_needs`. Source search, from the repo root:
  `grep -rnE '\b(print|logging|logger|warnings\.warn|traceback)\b' sdks/python/src` finds nothing, so the SDK writes no log
  line; the only error values are `ToggleSlashError(code, ...)` built from a code and a status number, never from a
  context. Request headers: `grep -rn 'Authorization\|If-None-Match\|Accept\|Content-Type' sdks/python/src` finds exactly
  `Authorization` (`sync.py`, `usage.py`), `If-None-Match` (`sync.py`), `Accept: text/event-stream` (`sync.py`) and
  `Content-Type: application/json` (`usage.py`).

### Native tests

- [x] Every operation from many threads, `close` racing `variation`, two `close` calls racing, no error or deadlock; no
  race detector in Python, so the stress tests stand in: `ConcurrencyTests` in `tests/test_client.py`
  (`test_many_threads_evaluate_while_the_ruleset_swaps_and_usage_flushes`, `test_close_races_variation`,
  `test_two_closes_race_and_both_return_after_one_shutdown`, `test_many_closes_race`): a `Barrier`, 16 threads, every
  operation, `time.sleep(0)` each turn (the GIL), `join(timeout)` as the bound. `bin/test` runs `python -X dev -W error`,
  which fails on an unraisable exception, an unclosed resource or an exception in a thread.
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a flush hangs:
  `test_variation_returns_at_once_while_a_ruleset_request_hangs`,
  `test_a_flush_hang_does_not_block_other_threads_and_close_is_bounded`, `test_a_hung_usage_flush_does_not_slow_evaluation_and_is_bounded`.
- [x] A reader sees one ruleset or the other across a swap: `test_the_hooks_etag_and_flag_version_are_always_a_pair`
  (the pairing under concurrent swaps), `test_many_threads_evaluate_while_the_ruleset_swaps_and_usage_flushes`.
- [x] A hook and an `onError` that call back into the client do not deadlock:
  `test_a_reentrant_hook_and_on_error_call_back_into_the_client`, `test_on_error_may_close_the_client_from_the_sync_thread`,
  `test_on_error_may_call_ready_on_the_sync_thread`.
- [x] A stream quiet for the watchdog period is dropped as `timeout`, and a keep-alive restarts it, through a seam and a real
  loopback socket: `test_a_stream_quiet_for_the_watchdog_period_is_dropped_as_timeout` (`Seams(stream_idle_timeout=0.4)`
  against a real loopback server that goes quiet) and `test_any_bytes_restart_the_watchdog` (keep-alive comments for more
  than twice the watchdog; the stream stays connected).
- [x] A `Retry-After` above 300 is held for 300 s on all three routes, through an injected clock:
  `test_a_429_retry_after_above_300_holds_the_ruleset_for_300` (the clock records a wait of 300.0, never 99999.0),
  `test_a_429_on_the_stream_route_holds_for_300`, `test_a_429_retry_after_holds_the_flushes_and_is_capped_at_300`.
- [x] Counts either side of a UTC midnight go out as two days: `test_a_utc_midnight_splits_one_flag_into_two_days`.
- [x] An evaluation that raises inside the SDK gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call:
  `test_an_evaluator_that_raises_is_evaluation_failed_with_one_on_error_call` (through `Seams(evaluate=broken)`),
  `test_the_hook_still_runs_after_an_evaluation_failure`, and `test_the_real_evaluator_does_not_raise_on_hostile_rulesets`.
- [x] A hook that returns a future that fails later goes to `onError`, changing neither answer nor count: a Python hook can be
  an `async def`; `test_an_async_hook_is_closed_not_leaked_and_is_reported` (the coroutine is closed, so there is no
  "never awaited" warning under `-W error`, and `on_error` is told once).
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor (Python ints are unbounded):
  `test_the_integer_accessor_follows_the_contract` (at `2**63 - 1024`, the largest in-range binary64, and a double beyond
  it), `test_the_int64_bound_exactly` (`_as_int64` at both exact bounds); the harness's `huge` scenario.
- [x] A client that is never closed does not keep the process from exiting: both background threads are daemons
  (`test_the_sync_thread_is_a_daemon_and_ends_on_close`), and `tests/test_exit.py` runs a child process that never closes
  its client, with the stream on and off, and checks it exits 0 within 30 s.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check (re-run at phase 6: it printed nothing and exited 0); `README.md`
  links the full mapping table (`DESIGN.md#the-mapping-table`) and shows a short one.
- [x] Names, defaults, reason kinds, error codes, stream states and `lastError` codes keep their spelling: `ReasonKind` and
  `ErrorCode` in `evaluator/result.py` and `StreamState` in `types.py` hold the contract's strings, and the test service sends them as they are (the
  harness's shape check on every status read and its reason comparison pass).
- [x] No platform regular expression in the evaluator: `grep -rnE 'import re\b|from re\b|re\.(compile|match|search|fullmatch|sub)' sdks/python/src/toggleslash/evaluator`
  finds nothing. `re` appears only in `http.py` (two patterns: whole decimal seconds for `Retry-After`, and splitting the URL),
  which is not the evaluator.
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none (`DESIGN.md` "Dependencies": "Runtime: none").
- [x] No telemetry, and no header beyond `Authorization`, `Accept`, `If-None-Match`, `Content-Type`. Added by `http.client`,
  captured on a loopback listener (the key redacted):

  ```
  GET /sdk/v1/ruleset   accept-encoding: identity, authorization: Bearer <key>, host: 127.0.0.1:<port>
  GET /sdk/v1/stream    accept: text/event-stream, accept-encoding: identity, authorization, host
  GET /sdk/v1/ruleset   accept-encoding: identity, authorization, host, if-none-match: "v1"
  POST /sdk/v1/events   accept-encoding: identity, authorization, content-length: 73, content-type: application/json, host
  ```

  `Host` is the server the caller named, so it names the server and not a user or machine. `Accept-Encoding: identity` is a
  constant that `http.client` adds and that asks for no compression. `Content-Length` is the body's size. There is no
  `User-Agent` and no `Connection` header.
- [x] Every extra is one contract 13 allows: typed accessors and a generic one, `Client.flush()` (contract 13 names it),
  the context manager (`with create_client(...)`, which only calls `close`), and the `Seams` options (a clock, a jitter source, an
  evaluator, a stream idle timeout). None weakens contract 5 or sends anything new.
- [x] Test seams are options on `Client` (`Seams`, passed as the package-internal `_seams` argument; not exported), never a global or an
  environment variable the SDK reads. `TOGGLESLASH_TEST_LOAD` is read by the tests (`tests/support.py`), not by the SDK.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable (`ls -l sdks/python/bin`: `-rwxr-xr-x`), start with
  `#!/bin/sh` and `set -eu`, `cd "$(dirname "$0")/.."`, and use only the tools `mise.toml` pins (`mise exec --` when mise exists,
  the plain `python` and `ruff` otherwise); `bin/test-service` ends with `exec`.
- [x] `test-service/README.md` maps every accessor (`bool`, `string`, `number`, `int`, `json`, `any`) to an SDK method in its
  "Accessor mapping" table.
- [x] `.github/workflows/sdk-python.yml` follows the skeleton, filters on `sdks/python/**` and the shared inputs, and pins every
  action by commit SHA (`actions/checkout`, `oven-sh/setup-bun`, `jdx/mise-action`, each with `@<40 hex> # vX.Y.Z`).

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` passes (after `git add -A`, because it reads tracked files).
- [x] `bun run format:check` passes (oxfmt checks TOML, YAML and JSON, so `pyproject.toml`, `mise.toml` and the workflow are covered; fixed with `bunx oxfmt <file>` where it complained).
- [x] Only `sdks/python/` and the one workflow file changed: the phase 6 scope check prints `scope ok`.
- [x] No package was published and no name reserved: `README.md` names no registry, and nothing was uploaded.
- [x] Commit subject is a Conventional Commit, `chore(sdk-python): ...`, not `feat`, with no `Co-Authored-By` and no AI attribution.
- [x] This file follows `RUN-TEMPLATE.md`: the phase 6 section check prints nothing.
