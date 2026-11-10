# Python SDK: run report

## Prompt version

- Prompt: 647f849fbf6cc799bc337fd3d9fdaff4d22650c4
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), run as a T3 Code thread for ticket FS-95
- Date: 2026-11-10
- Input: language Python; runtime chosen: CPython; minimum version chosen: 3.11; package manager chosen: pip with setuptools (src layout, `pyproject.toml`)
- Toolchain: `mise.toml` pins `python = "3.15.2"` (prints `Python 3.15.2`) and `ruff = "0.17.4"` (prints `ruff 0.17.4`). Nothing else: the tests are `unittest` and the SDK has no dependency.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (run twice on the final tree, exit 0 both times)
- Capabilities advertised: `integer-accessor`
- Native tests: 81 passed of 81 (79 unit and concurrency tests, plus the 2 vector tests that run the 1371 vectors and 17 pins)

## Decisions

1. Lint and format: `ruff` (`ruff check` and `ruff format --check`) over flake8 plus black, because one pinned tool does both and mise installs it. `pyproject.toml` selects a broad rule set and lists ten ignores, each with a reason in the file.
2. Tests: `unittest` over pytest, so the SDK and `bin/test` need no dependency beyond the interpreter.
3. Build step: there is no compile step, so `bin/test` runs `python -m compileall -f` first (a syntax error fails there) and then the tests under `python -X dev -W error`.
4. No type checker (mypy or pyright): a second pinned tool with its own opinions. The code is annotated and ships `py.typed`.
5. Minimum Python 3.11, because it is the oldest in security support and has `enum.StrEnum`. CI runs only the pinned 3.15.2; no 3.11 runner was available, so the minimum is by construction, not by test.
6. HTTP: `http.client`, one connection per request, no pooling and no `urllib`. It gives the socket to shut down on `close` and a per-read timeout for the stream watchdog.
7. Patterns: a Pike VM written in the evaluator (`_pattern.py`), parsed and emitted iteratively so a deep pattern cannot hit the recursion limit, with an `lru_cache` of compiled patterns. No `re` anywhere.
8. Concurrency: threads (two daemon threads, one for sync and one for usage), a snapshot object swapped by one attribute write, and five small locks named in `DESIGN.md`. The reader path takes no lock.
9. The workflow installs the toolchain with `jdx/mise-action` (v5.2.7, pinned by SHA) limited to `python ruff` with `working_directory: sdks/python`, over `actions/setup-python`. The versions then come from the one file `mise.toml`, and the `bin` scripts already call `mise exec`.
10. `bin/*` run from any directory: each does `cd` to the SDK and calls tools through `mise exec --` when mise exists, plain otherwise.
11. Test seams: a private `_seams` keyword on `Client` taking a `Seams` dataclass (transport, clocks, wait, jitter, watchdog, evaluator). `create_client` does not accept it and nothing is read from the environment.
12. `Client.flush()` is an extra (contract 13 allows an explicit flush). It sends the counts now, under the same flush lock, with the same keep or drop rules and the same hold.
13. `last_sync_at` is a timezone-aware UTC `datetime`, and durations accept a number of seconds or a `timedelta`.
14. A sync failure that `close` caused is neither recorded nor reported (`_sync.py`, `_failed`).
15. The test service turns `lastSyncAt` into epoch milliseconds with integer arithmetic, so a float cannot round a microsecond up.
16. The surrogate model: the evaluator works on UTF-16 code units (`u16`) where the spec counts them and on code points where the spec counts those (`code_points`), because a Python `str` holds lone surrogates and neither view is the default.

## Guesses

1. Where the SDK should find its toolchain when a script is run from outside its directory. Where: `PROMPT.md`, "Entry points" (and `CHECKLIST.md`, "`bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK, and run with only the toolchain `$L/mise.toml` pins"). `mise` on this machine resolves `python` through a shim that fails in a directory with no active tool, and the repository root `.tool-versions` is a different file. Assumed: `cd` into the SDK, then `mise exec --` for every tool call, falling back to bare tools when mise is missing.
2. The integer range rule for Python. Where: `PROMPT.md`, phase 3, "Capabilities": "The range rule applies only where the integer type is bounded; Python and Ruby integers are not, so it has nothing to check there." Against that, `fixtures/sdk/evaluation.json` has a scenario with flag `huge` (`"variations":[1e20, 3]`), accessor `int`, fallback 7, and expects `value` 7, `variationIndex` null, `TYPE_MISMATCH`. Python's `int` can hold 1e20, so by the prompt's words the scenario would fail. Assumed: the integer accessor is bounded to signed 64 bits (`int_variation`), as every other typed-language SDK is. The harness passes with it.
3. The same rule has a second edge. Where: `docs/sdk-contract.md`, section 4, "Typed languages" ("outside the integer type's range"), and `docs/evaluation.md` on numbers. A served `9223372036854775807` is parsed as a binary64 number (JSON numbers are binary64 in the spec), so it reads as `2^63` and is `TYPE_MISMATCH` on the integer accessor. Assumed: that is correct, because the spec defines a JSON number as binary64 and the SDK must agree with the server's evaluator. `_json.py` converts any integer literal beyond plus or minus 2^53 to a float for the same reason.
4. How the native test for the 60 s stream watchdog should be written. Where: `CHECKLIST.md`, "Native tests": "tested through a seam, not by waiting". The harness cannot reach it and `PROMPT.md` does not say a seam must reach the real socket read. A first version of this SDK passed the harness and a seam-only test while the watchdog never fired: `http.client` sets `connection.sock` to `None` after `getresponse()` for an answer that ends with the connection, so the reader's `settimeout` call went nowhere. Assumed: the test must run a real socket against a local server (`WatchdogTests`, with a short watchdog value through the seam). That test caught the bug.
5. Whether a stream that was up and then dropped polls. Where: `docs/sdk-contract.md`, 7.3. The text is clear on a stream that never connected (poll once) and on one that connected (do not), and "failures is cumulative until a stream connects" is easy to read the other way. Assumed: cumulative, reset only when a stream connects (`LoopTests.test_the_delay_is_the_backoff_ceiling_times_half_to_one_by_jitter`).
6. The value of `ready` after a failed first attempt followed by a later success. Where: `docs/sdk-contract.md`, 5.3 ("gives the same answer"). Assumed: it is fixed when the first attempt settles, so a later load does not turn a `False` into `True` (the harness failed on the first run until `ready` stopped re-reading the live state).
7. The number `N` in the final-flush message. Where: `docs/sdk-contract.md`, 9 and 11 ("closed with N usage counts unsent: the server asked to wait"). Not stated whether N is entries or the sum of counts. Assumed: the number of entries (distinct flag, variation and day), the size of the pending map; the harness does not check the number.
8. Whether a typed JSON accessor treats an array and an object as one kind. Where: `docs/sdk-contract.md`, section 4 ("the mismatch rule"). Assumed: `json_variation` accepts either and returns it; the generic method with a non-null fallback keeps array and object distinct (the harness failed on the first run until it did).
9. The kind of `Retry-After` value that counts. Where: `docs/sdk-contract.md`, 7.4 ("whole digits"). Assumed: ASCII digits only after trimming spaces, so `1.5`, `-1`, `+3` and HTTP dates are ignored, and 429 only.
10. What `close` does with a request stuck in DNS or in the TCP connect. Where: `docs/sdk-contract.md`, 11 and 6. There is no socket to shut down yet, so `close` waits for the connect timeout (`request_timeout`). Assumed that meets "bounded" and recorded in `DESIGN.md`.
11. The "Python has no race detector" gap. Where: `CHECKLIST.md`, "Native tests", first line: "under the race detector where the language has one". Assumed: `python -X dev -W error`, a `Barrier(10)` stress of 7 readers and 2 closers over 20 rounds, and the lock order written down in `DESIGN.md`.

## Stuck points

None. Guess 2 is the nearest thing: the prompt and a harness scenario disagree, but the harness is the stricter side and the SDK meets it.

## Proposed prompt changes

1. For guess 2, in `PROMPT.md` phase 3, "Capabilities", replace "The range rule applies only where the integer type is bounded; Python and Ruby integers are not, so it has nothing to check there." with: "The range rule applies to every SDK that advertises `integer-accessor`, including Python and Ruby, whose integers are unbounded: bound the accessor to signed 64 bits, because the `evaluation.json` scenario with flag `huge` (1e20) expects `TYPE_MISMATCH` on it." In `CHECKLIST.md`, "Native tests", change the bounded-integer line the same way and drop "Not applicable where integers are unbounded (Python, Ruby)".
2. For guess 1, in `PROMPT.md`, "Entry points", after "`cd` to the SDK", add: "and call every tool through the toolchain manager (`mise exec -- <tool>`) rather than from `PATH`: a shim for a tool that is not active in the current directory fails, and the root `.tool-versions` is a different set."
3. For guess 3, in `language-notes/python.md` (new), add: "`json` hands you Python ints of any size. Convert an integer literal beyond plus or minus 2^53 (or longer than about 18 characters) to a float when parsing, so a served `2**63-1` reads as the binary64 `2**63`, and keep Python's integer-digit limit away from a literal of thousands of digits."
4. For guess 4, in `CHECKLIST.md`, "Native tests", the watchdog line: after "tested through a seam, not by waiting", add: "the seam sets the watchdog's length, and the test must read a real socket (a loopback server that goes quiet), because a seam-only test can pass while the timeout is never applied to the socket (`http.client` drops `connection.sock` after `getresponse()` in Python)."
5. For guesses 5 and 6, in `docs/sdk-contract.md` (a contract change, so the maintainer's call), 7.3 and 5.3: add one sentence each, "`failures` is cumulative until a stream connects" and "`ready` answers the same after it has settled, even when a later attempt loads a ruleset".
6. For guess 7, in `docs/sdk-contract.md`, 11: say whether "N" is the number of entries or the sum of the counts. The harness does not check it.
7. For guess 8, in `docs/sdk-contract.md`, section 4: say whether a JSON accessor treats an array and an object as one kind.
8. For guess 11, in `language-notes/python.md`: "There is no race detector. Run the tests under `python -X dev -W error` and stress close against readers with a `threading.Barrier`; `Thread.join(timeout)` in the test turns a deadlock into a failure."
9. In `PROMPT.md`, phase 6, say that `git add -A` in the scope check also stages `__pycache__` unless the SDK has a `.gitignore`, so add the `.gitignore` in phase 5 (a Python-only trap, for `language-notes/python.md`).

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin: `bin/test` prints `vectors: 1371` and `pins: 17`; the phase 2 count command prints `1371 17`. Tests: `tests/test_vectors.py` (`test_vectors`, `test_bucket_pins`), which fail if they read no files.
- [x] The harness exits 0 with only the 2 named skips: `bun run conformance -- --service sdks/python/bin/test-service` printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, exit 0, twice.

### What the harness checks

- [x] Both context shapes: the harness (`privacy.json`, the `kinds.json` vectors through the service) passed.
- [x] Results follow contract 4 and the mismatch rule: harness `evaluation.json`, `usage.json` passed; native `EvaluationTests.test_a_value_of_another_kind_is_type_mismatch_with_the_fallback`.
- [x] With `integer-accessor`, a served fraction is `TYPE_MISMATCH` and `2.0` is `2`: harness `evaluation.json` passed; native `EvaluationTests.test_the_int_accessor_and_its_range`.
- [x] No failure makes an operation raise: every scenario passed (the service answers 500 on anything raised); native `EventsAndHooksTests.test_a_bad_url_never_raises`.
- [x] Evaluating before the first load answers `NOT_READY` at once: harness `load.json` passed; native `EvaluationTests.test_before_the_first_load_every_call_is_the_fallback_with_not_ready`.
- [x] `ready` settles, is bounded, may be called again, false after `close` if unsettled: harness `load.json`, `close.json` passed; native `FirstLoadTests.test_a_failed_first_load_is_not_ready_and_is_recorded`.
- [x] A non-ruleset answer never replaces the one held: harness `status.json` passed; native `FirstLoadTests.test_a_bad_body_leaves_the_last_good_ruleset`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining: harness `load.json`, `status.json` passed; native `FirstLoadTests.test_a_poll_sends_if_none_match_and_a_304_keeps_the_ruleset`.
- [x] The stream: harness `stream.json`, `load.json`, `status.json` passed; native `SseTests` (12 tests) and `LoopTests.test_a_failed_fetch_for_a_changed_event_ends_the_stream`.
- [x] The loop and the jittered backoff: harness `backoff.json`, `stream.json` passed; native `LoopTests.test_the_delay_is_the_backoff_ceiling_times_half_to_one_by_jitter`, `test_the_backoff_is_capped_by_the_poll_interval`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json` passed; native `RetryAfterTests` (sync, 6 tests), `HoldTests` (usage), `RetryAfterTests` (sse, 3 tests).
- [x] Status fields, states and `lastError` codes: harness `status.json` passed (it checks the shape on every read); native `EventsAndHooksTests.test_status_after_the_first_load_and_after_close`.
- [x] Usage counts: harness `usage.json`, `rate-limit.json` passed; native `UsageTests` (9 tests).
- [x] Hook events and `onError`, a hook that raises, an `onError` that raises: harness `hooks.json` passed; native `EventsAndHooksTests.test_a_raising_hook_and_a_raising_on_error_change_nothing`.
- [x] `close` in order, twice at once, during a hold, with a final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json` passed; native `HoldTests.test_the_final_flush_does_not_wait_and_says_what_it_lost`.
- [x] No context, key or attribute in any request or `onError` message, no extra header: harness `privacy.json` and the after-scenario checks passed; native `EventsAndHooksTests.test_error_text_names_no_url_and_no_key`, `test_every_request_header_is_one_the_protocol_allows`. Source search for logging: `grep -rniE "logging|print\(|stderr|warnings" sdks/python/src` finds nothing; the SDK writes no log line, and an error value is built from a fixed message and a route (`_sync.py` `_Failure`, `_http.py` `TransportError`).

### Native tests

- [x] Every operation from many threads, `close` racing `variation`, two `close` calls racing: `ConcurrencyTests.test_close_racing_variation_and_two_closes_racing` (7 readers, 2 closers, `Barrier(10)`, 20 rounds, joins with a timeout so a deadlock fails). No race detector exists for Python; `bin/test` runs under `-X dev -W error` instead (guess 11).
- [x] `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs: `EvaluationTests.test_before_the_first_load_every_call_is_the_fallback_with_not_ready` (ruleset request hung, under 0.5 s, `close` under 2 s) and `ConcurrencyTests.test_variation_does_not_wait_for_a_hung_flush_and_close_is_bounded_by_the_request_timeout`.
- [x] One ruleset or the other across a swap: `ConcurrencyTests.test_the_etag_and_the_flag_version_always_belong_together_while_the_ruleset_swaps`; `EventsAndHooksTests.test_the_hook_sees_the_etag_and_flag_version_of_the_ruleset_that_answered`.
- [x] A hook and an `onError` that call back into the client do not deadlock: `ReentrancyTests` (`test_a_hook_may_call_status_variation_and_close`, `test_on_error_may_call_close`, `test_on_error_from_the_final_flush_may_call_close`).
- [x] A stream with no bytes for 60 s is dropped as `timeout`, a keep-alive restarts it, through a seam: `WatchdogTests.test_a_quiet_stream_is_dropped_as_a_timeout`, `test_keep_alive_comments_restart_the_watchdog`, `test_the_watchdog_seam_reaches_the_reader` (a real loopback socket, a short watchdog value through `Seams`).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam: `RetryAfterTests.test_the_ruleset_hold_is_capped_at_300`, `test_the_stream_hold_is_capped_at_300`, `HoldTests.test_the_hold_is_capped_at_300_seconds` (a fake clock).
- [x] Counts either side of a UTC midnight go out as two days: `UsageTests.test_the_day_is_the_utc_day_and_splits_at_midnight`, `test_a_clock_in_another_zone_still_counts_the_utc_day`.
- [x] An evaluation that raises inside the SDK gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`: `EventsAndHooksTests.test_evaluation_failed_is_the_fallback_one_on_error_and_not_counted` (through `Seams.evaluator`).
- [x] Hooks that return a future: not applicable. A Python hook is a plain synchronous callable, so there is no future that can fail later; a hook that raises is `test_a_raising_hook_and_a_raising_on_error_change_nothing`.
- [x] Bounded integer type, a served number out of range is `TYPE_MISMATCH` on the integer accessor: Python's `int` is unbounded, but the accessor is bounded to signed 64 bits (guess 2): `EvaluationTests.test_the_int_accessor_and_its_range`, and the harness `huge` scenario.
- [x] A client never closed does not keep the process from exiting: `ProcessTests.test_a_client_never_closed_does_not_keep_the_process_alive` (a subprocess; both threads are daemons).

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` links to the mapping table: `README.md`, "The API", links `DESIGN.md#the-mapping-table`.
- [x] Names, meanings, defaults, reason kinds, error codes, stream states and `lastError` codes: `DESIGN.md` mapping table; `_types.py` (`ReasonKind`, `ErrorCode`, `StreamState` as `StrEnum`, and the `NETWORK`, `TIMEOUT`, `BAD_RULESET` codes); the harness shape check on status passed.
- [x] The evaluator has no platform regular expression: `grep -rnE "^\s*(import|from) +re\b|import regex|fnmatch" sdks/python/src` finds nothing.
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none. `DESIGN.md`, "Dependencies": "Runtime: none." The imports in `src` are all standard library (`codecs`, `dataclasses`, `datetime`, `enum`, `functools`, `hashlib`, `http.client`, `json`, `math`, `random`, `socket`, `threading`, `time`, `urllib.parse`, `collections.abc`, `typing`).
- [x] No telemetry and no identifying header: the only header literals in `src` are `Authorization` and `Content-Type` (`_usage.py:37`), `Authorization` and `If-None-Match` (`_sync.py:209, 212`) and `Authorization` and `Accept` (`_sync.py:252`). `http.client.request` itself adds `Host`, `Content-Length` (with a body) and `Accept-Encoding: identity`; none names a user or a machine, and the SDK sets no `User-Agent` (the harness's after-scenario check on every request passed).
- [x] Every extra is one contract 13 allows: `Client.flush()`, the context manager, `timedelta` durations and the typed accessors (`DESIGN.md`, the mapping table). None changes contract 5 or sends anything new.
- [x] Test seams are options, not a global or an environment variable: `Seams` passed as the private `_seams` argument of `Client` (`_client.py`, `_seams.py`); `grep -rnE "os\.environ|getenv" sdks/python/src` finds nothing.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and use only the `mise.toml` toolchain: `ls -l sdks/python/bin` shows `-rwxr-xr-x` on all three; each starts with `cd "$(dirname "$0")/.."`; `bin/lint` printed `ruff 0.17.4`, `All checks passed!`, `30 files already formatted`.
- [x] `test-service/README.md` maps every accessor: `sdks/python/test-service/README.md` (bool, string, number, int, json, any).
- [x] The workflow follows the skeleton, filters on its paths, and pins every action by commit SHA: `.github/workflows/sdk-python.yml` (checkout `3d3c42e5...` v7.0.1 and setup-bun `0c5077e5...` v2.2.0 copied from `ci.yml`; `jdx/mise-action@b44d12e7...` v5.2.7 resolved with `gh api repos/jdx/mise-action/commits/v5.2.7 --jq .sha`).

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exits 0; `grep -nP "\x{2014}|\x{2013}" -r sdks/python .github/workflows/sdk-python.yml` finds nothing.
- [x] `bun run format:check` passes: "All matched files use the correct format." over 351 files.
- [x] Only `sdks/python/` and the workflow changed: the phase 6 scope check printed only `scope ok`.
- [x] No package was published and no name reserved: nothing was uploaded, tagged or registered; `README.md` says the SDK is not published. A scratch `pip install` ran in a throwaway virtual environment under `$TMPDIR` to prove `pyproject.toml` builds, and was deleted.
- [x] Commit subject is `chore(sdk-python): a Python server SDK built from the authoring prompt` (Conventional Commits, not `feat`), with no `Co-Authored-By` and no AI attribution line in the commit or the pull request.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: all seven sections are present, and each line of this list has evidence.
