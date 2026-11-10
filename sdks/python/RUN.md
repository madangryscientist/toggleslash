# Python SDK: run report

## Prompt version

- Prompt: 180a02dfe6b363203e19f8b9d19a24cbbfb74927
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), run as FS-113 "SDK sweep run 3: Python, cold"
- Date: 2026-11-10 (started), 2026-11-11 (finished)
- Input: language Python; runtime chosen: CPython; minimum version chosen: 3.11; package manager chosen: none for
  the checks (pip with the `setuptools` backend only to install from a checkout)
- Toolchain: `mise.toml` pins python 3.15.2 (`python --version` prints `Python 3.15.2`) and ruff 0.17.4
  (`ruff --version` prints `ruff 0.17.4`); bun 1.4.2 (`bun --version`) for the harness and the repository checks.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2
  count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 68 passed of 68 (`Ran 68 tests`, about 35 s). 10 runs in a row passed, and one more with 12 busy
  loops running (one per core): 68 of 68 each time, 33.6 s to 35.3 s.

The first conformance run had 72 failures, all from one fault of mine: the test service sent `lastSyncAt` as an
ISO string and the protocol asks for whole epoch milliseconds or `null`. The protocol says so; I read it too fast.

## Decisions

1. **Lint and format: `ruff` (check and format) over pylint, flake8 plus black, or a type checker.** One pinned tool
   for both jobs; `docs/sdk-authoring/language-notes/python.md` says no type checker is required. Rule set in
   `pyproject.toml`: E, F, W, I, B, UP, SIM, RUF, PL, C4, PIE, T20, S, with every ignored rule commented.
2. **Test framework: `unittest` over pytest.** Nothing to install, so `bin/test` needs only the pinned Python.
3. **Concurrency: threads (one sync thread, one flush thread, both daemon) over `asyncio`.** The contract's operations
   are synchronous calls from any thread, and daemon threads give contract 6.6 for free.
4. **HTTP: `http.client` over `urllib` or a third-party client.** The SDK needs the socket to shut it down at a
   deadline and on `close`, to read a stream with `read1`, and to know exactly which headers go out. No runtime dependency.
5. **Deadlines: a `threading.Timer` that calls `shutdown` on the socket, with the SDK keeping its own socket reference.**
   `http.client` sets `conn.sock` to `None` after `getresponse()` for an answer that ends the connection, so a timeout
   set through the connection afterwards goes nowhere (the language notes say the same). A fired timer means `timeout`.
6. **Minimum Python 3.11** (`enum.StrEnum`, `datetime.UTC`); the checks run on 3.15.2.
7. **Build: `setuptools` backend in `pyproject.toml`, no build step in the checks.** `pip install ./sdks/python`
   works from a checkout (checked into a `--target` directory, which I then removed). The distribution and import
   name are both `toggleslash`; the prompt does not say, and nothing is registered under it.
8. **Seams are underscore keyword arguments of `create`:** `_transport`, `_clock`, `_jitter_source`, `_stream_watchdog`,
   `_evaluator`. No global and no environment variable is read (a source search for `environ` and `getenv` in `src` finds nothing).
9. **Order of work: the test service and the conformance run before the native tests.** It gave fast feedback on the
   client; the native tests then covered what the harness cannot reach.
10. **Native test server: a bespoke `FakeServer` on `http.server`** (`tests/support.py`, loopback, optional TLS) with
    per-route handler overrides, over mocking the transport, so the real socket code is exercised.
11. **Workflow toolchain step: `jdx/mise-action` limited to `python ruff`, over `actions/setup-python`.** One source of
    the versions (`mise.toml`), and ruff is not in the Python setup action.
12. **The unsent-usage error on close has the SDK-built code `usage_unsent`** and the contract's text
    ("closed with N usage counts unsent: the server asked to wait", N the number of pending entries).
13. **`ready()` waits at most `request_timeout + 1 s`** (a second of grace for work the SDK does not control, such as a
    name lookup), then settles to false.
14. **A stream answer with `Content-Length: 0` counts as a body-less answer** and is treated as an empty stream that ended.
15. **A hook that raises, or an `on_error` that raises:** the first goes to `on_error`, the second is dropped; both are
    caught around the call with no SDK lock held.

## Guesses

1. **Is "cold" compatible with `language-notes/python.md`?** The brief says to build from the prompt, and phase 0 step 9
   says to read the language notes, but `python.md` is a summary of what three earlier Python runs found (it names
   FS-95, FS-109 and FS-111). Where: FS-113 brief and `PROMPT.md` phase 0 step 9. Assumed: reading it is allowed
   because the prompt says to, and it is the prompt's own document. Its advice (the `http.client` `sock` quirk,
   the big-integer rule, `SIM105` and `S110`, the `.gitignore` list) shaped the code, so this run is not a blank-slate
   test of the prompt without the notes.
2. **May a run read a sibling SDK's `bin/lint`?** `PROMPT.md` phase 5 step 4 says a sibling workflow may be read for
   the `jdx/mise-action` inputs, and says nothing about other sibling files. While writing `bin/lint` I listed
   `sdks/ruby/bin` and read the first lines of `sdks/ruby/bin/lint`. I did not copy it (mine is a ruff check, a format
   check and a compile pass), but it informed the shape of the header comment and the `mise` fallback. Where:
   `PROMPT.md`, "Repository rules" and phase 5. Assumed: a one-off peek is harmless, disclosed here.

I searched the contract, the protocol page, the checklist and the language notes for the other points I first
thought were guesses (the enum spelling: `sdk-contract.md` section 2; the type of `lastSyncAt`: section 8; `ready`
from the SDK's own thread: 6.4). Each is answered, so none is listed.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md` phase 0 step 9, after "language notes", add: "A run that is meant to test the prompt
   without them says so in its brief, and then skips this step."
2. For guess 2, in `PROMPT.md` "Repository rules", add: "Do not read another SDK's files under `sdks/`, except the
   workflow named in phase 5 step 4."
3. For the first-run failure in Results (no guess behind it), in `PROMPT.md` phase 4, after "Write the service", add: "Read the status
   shape in `sdk-test-service.md` before the first run: `lastSyncAt` is whole epoch milliseconds or `null`."
4. For the native tests (no guess behind it), in phase 3, add one sentence to the test list: "A fake server for the native tests that binds
   loopback and records each request's headers serves the header audit, the Retry-After cap and the watchdog
   tests; write it once, first." (This run wrote `tests/support.py` after the service and reused it in every test file.)

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin: `tests/test_vectors.py`; it prints
  `vectors: 1371 pins: 17`, equal to the phase 2 count command's `1371 17`.
- [x] `bin/test` 10 times in a row and once under load: run 1 to 10 each `Ran 68 tests ... OK` (33.6 s to 35.3 s),
  and with 12 busy loops (`nproc` is 12): `Ran 68 tests in 34.7s`, OK, `vectors: 1371 pins: 17`.
- [x] The harness exits 0 with only the 2 vector skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369
  passed, 0 failed, 2 skipped`. The service advertises `integer-accessor`, so no scenario skips.

### What the harness checks

- [x] Both context shapes: the harness (`privacy.json`, `kinds.json` vectors through the service) passes.
- [x] Results follow the order of contract 4: the harness (`evaluation.json`, `usage.json`) passes; native
  `MismatchTests` (kinds, null fallback, non-JSON fallback).
- [x] Integer accessor fraction and `2.0`: harness `evaluation.json`; native `MismatchTests.test_integer_accessor`.
- [x] Nothing raises at a caller: every harness scenario (the service answers 500 on anything raised); native
  `BasicTests.test_an_unusable_url_is_a_network_error_never_a_raise`.
- [x] `NOT_READY` before the first load: harness `load.json`; native `BasicTests.test_not_ready_before_first_load`.
- [x] `ready` settles, bounded, repeatable, false after `close` if unsettled: harness `load.json`, `close.json`;
  native `ConcurrencyTests.test_ready_is_fixed_and_false_after_close_if_unsettled` and `test_ready_answer_is_fixed_once_settled`.
- [x] A non-ruleset never replaces one: harness `status.json`; native `BasicTests.test_a_bad_ruleset_replaces_nothing`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`; native
  `test_if_none_match_only_with_a_ruleset_and_a_tag`, `test_trailing_slashes_are_removed`.
- [x] The stream: harness `stream.json`; native `BasicTests.test_a_changed_ruleset_arrives_through_the_stream`,
  `tests/test_sse.py` (13 parser tests).
- [x] The loop and the jittered backoff: harness `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; native `RetryAfterTests`.
- [x] Status fields, states and `lastError` codes: harness `status.json` with its shape check.
- [x] Usage counts, summing, batches of 500, keep or drop: harness `usage.json`, `rate-limit.json`; native
  `UsageTests.test_batches_of_at_most_500_and_the_kept_and_dropped_statuses` and
  `test_kept_statuses_are_resent_and_dropped_statuses_are_not`.
- [x] Hook events and `onError`, a hook that raises, an `onError` that raises: harness `hooks.json`; native `HookTests`.
- [x] `close` in order, twice at once, during a hold, final flush: harness `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`; native `ConcurrencyTests.test_two_closes_race`,
  `RetryAfterTests.test_cap_on_the_events_route_and_the_loss_report`.
- [x] No context, key or attribute in any request or `onError` message, no header beyond the protocol's: harness
  `privacy.json` and the after-scenario checks; native `BasicTests.test_request_headers_are_the_allowed_ones` sends a
  context with `SENTINEL` values and asserts they appear in no path, header or body. The SDK writes no logs: a
  search `grep -n "logging\|print(\|warnings\|traceback" -r src` finds nothing. Every error value the SDK builds is a
  constant string or a type name (`_client.py:252`, `_usage.py:130-138`, the sync errors, `usage_unsent`).

### Native tests

- [x] Many threads, every operation, `close` racing, two `close` calls racing: `tests/test_concurrency.py`
  `test_every_operation_races_with_close` (12 workers, a `Barrier`, `join(timeout)`, a bound on `close`) and
  `test_two_closes_race` (8 closers, 10 rounds). Python has no race detector: the stress tests under
  `python -X dev -W error` stand in. Each worker sleeps 0.5 ms every turn, so the GIL is handed on.
- [x] SHOULD: an `https` URL reaches the TLS path: `tests/test_http.py` `TlsTests.test_https_with_a_local_certificate`
  makes a certificate with the `openssl` CLI, serves it from `FakeServer(tls=...)` and trusts it through
  `Transport(ssl_context)`; the strict default client fails with `network`. The test skips when `openssl` is missing.
- [x] `variation` within a bound while a ruleset request hangs and while a flush hangs:
  `test_variation_returns_while_the_ruleset_request_hangs` and `test_variation_returns_while_a_flush_hangs`
  (200 and 300 calls, worst under 0.25 s).
- [x] A reader sees one ruleset: `test_a_hook_sees_the_etag_and_version_of_one_ruleset` (a publisher bumps the
  version while six readers evaluate and a 1 ms poll swaps; every hook event's `etag` is `"v<flag_version>"`, and more
  than 5 distinct versions were seen).
- [x] Callbacks that call back into the client: `test_callbacks_may_use_the_client_without_deadlock` (`on_error` calls
  `ready`, `status`, `variation` and `close` on the SDK's own thread) and `test_a_hook_may_use_the_client`.
- [x] The 60 s watchdog through a real loopback socket: `tests/test_http.py`
  `test_the_stream_watchdog_drops_a_quiet_stream_and_a_keep_alive_restarts_it` (`_stream_watchdog=0.4`; keep-alives
  hold the stream open past one watchdog length, then the quiet stream is dropped as `timeout`). Also
  `test_a_body_that_trickles_is_bounded_as_a_whole`.
- [x] A `Retry-After` above 300 is held for 300 s on all three routes: `RetryAfterTests.test_cap_on_the_ruleset_route`,
  `test_cap_on_the_stream_route`, `test_cap_on_the_events_route_and_the_loss_report`; parsing in
  `HelperTests.test_retry_after`.
- [x] Counts either side of UTC midnight: `UsageTests.test_counts_either_side_of_utc_midnight_go_out_as_two_days` (a
  `SystemClock` subclass with a settable `now`), and `test_a_non_utc_clock_is_converted`.
- [x] An evaluation that raises: `HookTests.test_evaluation_failed_is_reported_once_and_still_hooked` (an `_evaluator`
  that raises: fallback, `EVALUATION_FAILED`, one `on_error`, the hook called).
- [n/a] Hook futures: no futures; a hook is a plain function.
- [x] A number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `MismatchTests.test_the_int64_range`
  (`2^63-1024` served, `2^63` and `-2^63-2048` refused) and `Int64ThroughClient.test_range`; harness `huge`.
- [x] A client never closed does not keep the process alive: `ConcurrencyTests.test_daemon_threads_do_not_keep_the_process_alive`
  runs a subprocess that creates a client and exits, against a dead port, a healthy fake server and a hanging one.

### Code and documents

- [x] `DESIGN.md` has the mapping table (section 1) and the phase 1 rows; `README.md` links to it.
- [x] Names keep their meaning, defaults and spelling: `DESIGN.md` section 1, `StrEnum` for reasons, codes and states.
- [x] No platform regular expression in the evaluator: `grep -rn "import re\b\|from re \|import regex\|fnmatch" src`
  finds nothing; the matcher is `_pattern.py`.
- [x] The lint configuration names its rule set and each ignored rule carries a reason: `pyproject.toml`.
- [x] Every runtime dependency has a line: `DESIGN.md` section 4 (none).
- [x] No telemetry; headers captured on a loopback listener (`FakeServer`, requests to the ruleset, stream and events routes):
  - ruleset GET: `Accept`, `Accept-Encoding`, `Authorization`, `Host` (plus `If-None-Match` once a tag is held)
  - stream GET: `Accept`, `Accept-Encoding`, `Authorization`, `Host`
  - events POST: `Accept-Encoding`, `Authorization`, `Content-Length`, `Content-Type`, `Host`

  The SDK sets `Authorization`, `Accept`, `If-None-Match` and `Content-Type`. `http.client` adds `Host` (the server
  being called), `Accept-Encoding: identity` (a constant) and `Content-Length` (the body size); none identifies a user or
  a machine, and it sends no `User-Agent`.
- [x] Every extra is one contract 13 allows: the context manager (`__enter__`, `__exit__`), `int_variation` and
  `int_variation_detail` (the integer accessor), underscore seams.
- [x] Seams are `create` options (underscore-prefixed), never a global or an environment variable.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK, and use only `mise.toml`'s tools.
- [x] `test-service/README.md` maps every accessor to an SDK method.
- [x] `.github/workflows/sdk-python.yml` follows the skeleton, filters on its paths and pins every action by commit
  SHA (`gh api repos/<owner>/<repo>/commits/<tag> --jq .sha` agreed with `ci.yml` for checkout and setup-bun and
  with the sibling workflows for `jdx/mise-action` v5.2.7).

### Repository

- [x] `bun scripts/check-dashes.ts` passes.
- [x] `bun run format:check` passes (`mise.toml`, `pyproject.toml` and the workflow went through `bunx oxfmt`).
- [x] The scope check prints `scope ok`.
- [x] No package was published and no name reserved.
- [x] Commit subject `chore(sdk-python): the Python server SDK regenerated cold from the authoring prompt`; no
  `Co-Authored-By` and no AI attribution.
- [x] This file follows `RUN-TEMPLATE.md`.
