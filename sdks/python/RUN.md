# Python SDK: run report

## Prompt version

- Prompt: 26598eee3b61a0b44edc1c6d50c5d4e4ad08b813 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run and again before the phase 6 checks)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-12
- Input: language Python; runtime chosen: CPython; minimum version chosen: 3.11; package manager chosen: pip with setuptools, through `pyproject.toml` (nothing is published)
- Toolchain: `mise.toml` pins `python = "3.15.2"` (`python --version` prints `Python 3.15.2`) and `ruff = "0.17.4"` (`ruff --version` prints `ruff 0.17.4`). The harness side is Bun 1.4.2 (`bun --version`). The openssl CLI (OpenSSL 3.6.5) is used by the TLS tests only and is not pinned. The build file pins nothing else: there is no runtime dependency and no test library, because `unittest` ships with Python. The unit tests were also run on Python 3.11.18 (`mise exec python@3.11`), the declared minimum, and passed.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 192 passed of 192 (`bin/test`, `python -X dev -W error -m unittest`). Per file: `test_client` 43, `test_concurrency` 15, `test_evaluator` 19, `test_http` 31, `test_source` 6, `test_sse` 12, `test_sync` 28, `test_text_json` 14, `test_usage` 22, `test_vectors` 2. `test_vectors` is the vector test (one test with 1371 cases, as `subTest`s) and the pin test (17 cases). The test service has no tests of its own; the harness runs it.
- Repeat runs: `bin/test` passed 10 times in a row on the committed tree (each `Ran 192 tests in about 14.7s`, `OK`, `vectors: 1371 pins: 17`), then an eleventh time with 12 busy shell loops (`nproc` is 12) started before it and running until it ended (`Ran 192 tests in 17.005s`, `OK`). The tree changed twice after earlier ten-run series (a DESIGN.md edit and a `None`-fallback loop in `test_client`, then the stream and events hold-cap tests), and each time the ten started again from the first.
- Lint: `bin/lint` prints `All checks passed!` and `30 files already formatted`. `bun run format:check`, `bun scripts/check-dashes.ts` and `bun scripts/check-workflows.ts .github/workflows/*.yml` (8 workflows read, 0 failures) pass.

Headers the SDK sends, captured on a loopback listener, one request per route:

| Route | Headers on the wire |
| --- | --- |
| `GET /sdk/v1/ruleset` | `Host`, `Connection: close`, `Authorization` |
| `GET /sdk/v1/ruleset` (conditional) | `Host`, `Connection: close`, `Authorization`, `If-None-Match` |
| `GET /sdk/v1/stream` | `Host`, `Connection: close`, `Authorization`, `Accept: text/event-stream` |
| `POST /sdk/v1/events` | `Host`, `Connection: close`, `Authorization`, `Content-Type: application/json`, `Content-Length` |

The SDK writes its own request bytes (`_http.py`), so the client adds nothing it was not told to: no `User-Agent`, no `Accept-Encoding`. `Host` is the configured URL's host and port (the server needs it to route). `Connection: close` is the SDK's one-connection-per-request choice. `Content-Length` is the length of the usage body. None of these says anything about a user or a machine. `test_the_only_request_headers_the_sdk_names_are_the_allowed_ones` and `test_nothing_names_the_user_or_the_machine` search the source for this.

## Decisions

1. **HTTP layer: a small socket and `select` client over `http.client`.** `http.client` adds `Accept-Encoding`, its timeout is per socket operation, not per request (contract 7, 9: the whole request is bounded), and for an answer that ends with the connection it sets its socket to `None`, which takes a read timeout away. The custom client has a whole-request deadline and a wake socket so `close` interrupts a request at once. Settled by contract 12 (no header beyond the protocol's) and the contract's "request timeout includes the body". Documented in `_http.py` and DESIGN.md.
2. **Threads over asyncio.** The SDK starts two daemon threads, `toggleslash-sync` and `toggleslash-flush`; `variation` never waits on either (contract 5.2, 6.2, 6.6). Python has no single event loop a library may assume.
3. **A separate wake for the flush thread, closed only at the end of `close`.** The final flush must still be able to send after the sync thread has stopped, so the flush wake is not shared with the sync wake.
4. **Minimum Python 3.11.** `enum.StrEnum` and `datetime.UTC` are used, and both arrived in 3.11 (`requires-python = ">=3.11"` in `pyproject.toml`). The tests pass on 3.11.18 and on the pinned 3.15.2.
5. **TLS is tested through the openssl CLI and `SSL_CERT_FILE`.** The standard library cannot mint a certificate, and a test must not weaken verification. `test_a_trusted_certificate_works_over_https` (the CA is trusted through `SSL_CERT_FILE`), and three failure tests.
6. **`last_sync_at` is a timezone-aware UTC `datetime`.** The harness wants whole epoch milliseconds; `test-service/main.py` converts (`status_json`). Settled by contract 8 ("a point in time in the language's own type") and `sdk-test-service.md`, "Status".
7. **A stream that ends without the chunked terminator, and a TLS EOF, are `network` errors**, not `timeout` or a clean end. Contract 8 names `network` for any transport failure, and a clean end needs the terminator.
8. **Toolchain in CI: `jdx/mise-action` over `actions/setup-python`**, so Python and Ruff are pinned in one `mise.toml` like the other SDKs, and `bin/lint` and `bin/test` run with only what `mise.toml` pins (PROMPT.md, "Entry points"). The install is limited to `python ruff`, so the root `.tool-versions` is not installed.
9. **Lint and format: Ruff over flake8 plus black**, one tool for both, pinned in `mise.toml`. The selected rule sets are named in `pyproject.toml`, each ignore has a reason beside it. **Tests: `unittest` over pytest**, so the tests need nothing installed. **No type checker is pinned**, so the code and tests carry no `type: ignore`.
10. **Test seams are `Config` fields**: `now`, `monotonic`, `random` and `stream_idle`. No module global or environment variable is read by the SDK (contract 13, PROMPT.md phase 3). `Config` is the package's own, so this is "options on create, which may be internal to the package".
11. **A generic accessor with an `int` fallback keeps the number kind; `int_variation` is the integer accessor.** Python has no static type that separates `int` from `float` for the caller, so `variation(flag, ctx, 2)` served `2.5` is `2.5` (kind `number`, matching the harness `number` accessor), and `int_variation` applies contract 4's rule (a served `2.0` is `2`, a fraction or a value outside signed 64 bits is `TYPE_MISMATCH`). Settled by contract 4, "Typed languages": the integer rule is for "a generic method whose result has the fallback's static type (Java, C#)", which Python's does not have. `test_the_generic_number_accessor_has_no_integer_rule`, `test_the_integer_accessor`.
12. **A `None` fallback accepts any kind** on the generic method, and `int_variation(..., None)` given a fraction, a huge number, a string or a bool answers a `None` value and `TYPE_MISMATCH` (contract 4, the mismatch rule for a nullable type). `test_the_integer_accessor`.
13. **A hook that returns a future or a coroutine.** A future (anything with `add_done_callback`) that later fails goes to `on_error`; a coroutine is closed unawaited because the SDK runs no event loop. Contract 10 allows either; the cancelled-future case is not an error (`test_a_cancelled_future_is_not_an_error`).
14. **Test fix, not a guess (checklist, repeat runs).** While getting the first ten runs green, four of five early evaluator test failures were wrong test expectations, and one was a real bug (`to_float` in `_context.py` overflowed on a huge integer, now fixed). Tests that spin a reader while the SDK's loops need the GIL give up the interpreter each turn. These fixes are in the tree the ten runs were taken on.
15. **SSE: a lone CR ends a line.** WHATWG's rule, which the contract points at; the parser keeps a skip-LF flag across reads so CR then LF in two reads is one line end (`test_sse`).

## Guesses

None.

## Stuck points

None.

## Proposed prompt changes

None.

## Checklist

- [x] `bin/test` runs every vector but `buckets.json` and every pin, all pass, count equals the phase 2 command's: `test_vectors`, `test_bucket_pins`; `bin/test` prints `vectors: 1371 pins: 17`, the phase 2 command prints `1371 17`.
- [x] `bin/test` passes 10 times in a row, and an eleventh with the CPU loaded, counts in Results: ten runs of `Ran 192 tests`, `OK`; eleventh under 12 busy loops, `Ran 192 tests in 17.005s`, `OK`. Fixes made on the way are Decision 14.
- [x] The harness exits 0, only the 2 named vector skips and no scenario skipped: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (with `integer-accessor` advertised).
- [x] Both context shapes accepted: the harness `privacy.json` and the `kinds.json` vectors; `test_vectors`.
- [x] Results follow contract 4's order and the mismatch rule, with the SDK's own error codes: harness `evaluation.json`, `usage.json`; `test_the_kind_rule`.
- [x] With `integer-accessor`, a served fraction is `TYPE_MISMATCH` and a served `2.0` is `2`: harness `evaluation.json`; `test_the_integer_accessor`.
- [x] No failure of the server, the network or a context makes an operation raise: every harness scenario (the service answers 500 on a raise); `test_a_context_of_odd_python_values_does_not_raise`, `test_a_flag_key_that_is_not_a_string_fails_cleanly`.
- [x] Evaluating before the first load answers `NOT_READY` at once: harness `load.json`; `test_before_any_load_every_answer_is_the_fallback_not_ready`, `test_evaluation_before_the_first_load_does_not_wait_for_it`.
- [x] `ready` settles after the first attempt, bounded, callable again, false after `close` if unsettled: harness `load.json`, `close.json`; `test_ready_is_bounded_by_the_request_timeout`, `test_close_is_idempotent_and_ready_settles_false`.
- [x] An answer that is not a ruleset never replaces the one held: harness `status.json`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining: harness `load.json`, `status.json`; the header table in Results shows the conditional request.
- [x] The stream: events, SSE parsing, refresh on change, a failed refresh ends the stream: harness `stream.json`, `load.json`, `status.json`; `test_sse`.
- [x] The loop and the jittered backoff: harness `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; `test_sync`, `test_usage` (`HoldTests`).
- [x] Status fields, states and `lastError` codes: harness `status.json` and its shape check on every status read.
- [x] Usage counts, summing, batches of 500, keep or drop by answer: harness `usage.json`, `rate-limit.json`; `test_usage`.
- [x] Hook events and `onError`, a hook that raises, an `onError` that raises: harness `hooks.json`; `test_a_hook_that_raises_goes_to_on_error_and_changes_nothing`.
- [x] `close` in order, twice at once, during a hold, with a final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`; `test_close_sends_the_final_flush`, `test_the_final_flush_does_not_wait_out_a_hold_and_says_so`.
- [x] No context, context key or attribute in any request or `onError` message, no header beyond the protocol's: harness `privacy.json` and the after-every-scenario checks. Log and error search: `test_the_sdk_writes_no_log_line_and_prints_nothing` (AST search of `src/` for `logging`, `warnings`, `traceback`, `pprint`, `print` and `sys.stdout`/`sys.stderr`; the SDK builds its error values from fixed strings and codes).
- [x] Many threads at once, `close` racing `variation`, two `close` calls racing: `test_every_operation_from_many_threads_together`, `test_close_racing_evaluation`, `test_two_closes_race_and_the_final_flush_goes_once`. Python has no race detector; the stress tests use a `threading.Barrier`, every operation, `close` racing, and a `join(timeout)` bound on the wait. Each worker yields every turn, so the GIL does not starve the code under test.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `test_a_trusted_certificate_works_over_https`, `test_an_untrusted_certificate_is_a_network_error`, `test_a_certificate_for_another_host_is_a_network_error`, `test_plain_http_to_a_tls_port_fails_cleanly` (Decision 5).
- [x] `variation` returns within a bound while a ruleset request hangs and while a flush hangs: `test_evaluation_does_not_wait_for_a_hanging_ruleset_request`, `test_evaluation_does_not_wait_for_a_hanging_flush`.
- [x] A reader sees one ruleset or the other across a swap; `etag` and `flagVersion` from one ruleset: `test_the_etag_and_the_flag_version_always_come_from_one_ruleset`.
- [x] Callbacks that call back into the client do not deadlock, and `ready` from a callback on the SDK's own thread returns at once: `test_a_hook_may_call_back_into_the_client`, `test_a_hook_may_close_the_client`, `test_on_error_on_an_sdk_thread_may_call_ready_status_and_variation`, `test_on_error_on_an_sdk_thread_may_close_without_joining_itself`, `test_a_close_from_an_sdk_thread_while_the_caller_closes`, `test_callbacks_run_with_no_sdk_lock_held`. `ready` on an SDK thread returns without waiting (`_on_sdk_thread` in `_client.py`), and so does `close` (contract 6.4: `test_on_error_on_an_sdk_thread_may_close_without_joining_itself` times it against a final flush that hangs; the shutdown finishes on a `toggleslash-close` thread). Harness: the `6.4` scenarios in `close.json`.
- [x] A stream with no bytes for 60 s is dropped as `timeout` and a keep-alive restarts it, through a seam and a real loopback socket that goes quiet: `test_a_quiet_stream_is_given_up_on_by_the_watchdog`, `test_every_read_restarts_the_watchdog` (the seam is `Config.stream_idle`).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam (`Config.monotonic`): `test_the_hold_is_capped_at_300_seconds` (ruleset), `test_the_stream_routes_hold_is_capped_at_300_seconds`, `test_the_events_routes_hold_is_capped_at_300_seconds`; and `retry_after` in `test_http` (301 and a 10,000-digit value both give 300).
- [x] Counts either side of a UTC midnight go out as two days: `test_utc_midnight_makes_two_days`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call: `test_an_evaluator_that_raises_is_evaluation_failed_with_one_on_error`.
- [x] A hook's future that fails later goes to `onError` and changes neither the answer nor the count: `test_a_future_a_hook_returns_that_fails_later_goes_to_on_error`.
- [x] With `integer-accessor`, a number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor (Python integers are unbounded): harness `evaluation.json` (`huge`); `test_the_integer_accessor`.
- [x] A client never closed does not keep the process from exiting: both threads are daemon threads (started in `Client.__init__`); `test_a_client_that_is_never_closed_does_not_keep_the_process_alive` runs a child interpreter that creates a client and exits.
- [x] A typed accessor whose type admits null, given a null fallback and served another kind, answers `TYPE_MISMATCH`: `int_variation(..., None)` over a fraction, a huge number, a string and a bool, in `test_the_integer_accessor`.
- [x] A generic accessor with an integer fallback type: n/a for the integer rule on the generic method, which has no static fallback type in Python (Decision 11, `test_the_generic_number_accessor_has_no_integer_rule`); the integer accessor meets the rule (`test_the_integer_accessor`). A fallback of a class with no JSON kind gives the fallback and `TYPE_MISMATCH` (the `accessor shape` row of the DESIGN.md mapping table; the `b"bytes"` fallback assertion in `test_client.py`).
- [x] The hook event's `value` for the integer accessor is the `int`: `test_the_integer_hook_value_is_the_int`, and the mapping table row for `int_variation_detail`.
- [x] A context attribute of each numeric type matches as the JSON number of its value: `test_numbers_a_python_caller_hands_over` (a huge `int` that is not a safe integer, `nan` and infinities, `Decimal`, `Fraction`, `-0.0`) and `test_numbers_are_binary64` in `test_text_json`.
- [x] `DESIGN.md` passes the phase 1 row check and `README.md` links to the mapping table: DESIGN.md section 1 holds the table; README.md links to it.
- [x] The names in the mapping table keep the canonical meaning and defaults, and the closed sets keep their spelling: DESIGN.md section 1, `StrEnum` classes whose names and values are the contract's spelling.
- [x] The evaluator has no regular expression from the platform: `test_the_evaluator_imports_no_regular_expression_module` (AST search for `re`, `regex`, `fnmatch`, `glob`); patterns run on the hand-written Pike VM in `_pattern.py`.
- [x] The lint configuration names its rule set and each ignore carries a reason: `pyproject.toml` `[tool.ruff.lint]` `select` and `ignore`, a comment beside each ignored rule.
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none; DESIGN.md section 4 says "none" and why the standard library covers it. `test_no_dependency_beyond_the_standard_library` holds that.
- [x] No telemetry and no header that identifies a user or a machine: the header table in Results (captured on loopback, route by route); `test_the_only_request_headers_the_sdk_names_are_the_allowed_ones`, `test_nothing_names_the_user_or_the_machine`.
- [x] Every extra beyond the contract is one contract 13 allows: the context manager (`with`), the `Hooks` dataclass and a `dict` form, `int_variation` (the integer accessor), and `Config` seams; none weakens contract 5 or sends anything new.
- [x] Test seams are options on `create` or the package's own config, never a global or environment variable: Decision 10. `test_source` shows `os.environ` is never read.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and run with only what `mise.toml` pins: `ls -l sdks/python/bin` (mode 755), each starts `#!/bin/sh`, `set -eu`, `cd`, and goes through `mise exec --` when mise is present.
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method: its accessor table (`bool`, `string`, `number`, `json`, `any` to `variation_detail`; `int` to `int_variation_detail`).
- [x] `.github/workflows/sdk-python.yml` follows the skeleton, filters on its paths and pins every action by commit SHA: checkout, setup-bun and mise-action are each pinned by a 40-character SHA with the version in a comment; `check-workflows.ts` passes.
- [x] A `status` read racing a stream failure never shows that failure's `lastError` beside `stream: connected` (there is no race detector in Python): `test_status_never_shows_connected_with_an_error`, a many-reader stress test with a bound on the wait.
- [x] No em dash or en dash in any file written: `bun scripts/check-dashes.ts` passes.
- [x] `bun run format:check` passes: it prints `All matched files use the correct format.`
- [x] Only `sdks/python/` and the one workflow changed: the phase 6 scope check prints `scope ok` (run after the fold).
- [x] No package was published and no name reserved: nothing was uploaded and `pyproject.toml` is never built by a command in the run.
- [x] Commit subjects are Conventional Commits, none `feat`, with no `Co-Authored-By` or AI attribution: one commit, `chore(sdk-python): the Python server SDK regenerated cold from the authoring prompt`.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:` guard, triggers only `pull_request` and `push`, installs its own toolchain (`mise-action`), and `bun scripts/check-workflows.ts .github/workflows/*.yml` passes: `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of this list: this file.
