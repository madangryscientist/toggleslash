# Python SDK: run report

## Prompt version

- Prompt: 5540f5ef34d5d420eeafd1e2eee30e4704b0dac6
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-10
- Input: language Python; runtime chosen: CPython; minimum version chosen: Python 3.11; package manager chosen:
  none needed from a checkout, `pip` with a `setuptools` backend if installed
- Toolchain: Python 3.15.2 (`python --version` prints `Python 3.15.2`) and ruff 0.17.4 (`ruff --version` prints
  `ruff 0.17.4`), both pinned in `mise.toml`. The run was cold: the earlier Python SDK was removed first and
  nothing of it was read.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the
  phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 85 passed of 85 (`Ran 85 tests`, under `python -X dev -W error`), which include the 2 vector tests

`bin/test` passed 10 times in a row (`Ran 85 tests` each, 49 to 51 s a run) and once with the CPU loaded (12 busy
loops, one per core): `Ran 85 tests in 50.448s`, OK.

## Decisions

1. Lint and format: ruff (`ruff check` plus `ruff format --check`) over flake8 plus black plus isort, because one
   pinned binary does all three. The rule set is wide (`E F I B UP SIM RUF PL C4 PIE T20 S`) with the
   complexity and magic-number rules off, line length 120. No type checker, since the prompt does not ask for one.
2. Test framework: `unittest` over `pytest`, because it needs nothing installed and the language notes name it.
3. Minimum runtime Python 3.11 (`StrEnum`, `slots=True` dataclasses, `TimeoutError` as the one timeout name); the
   development pin is 3.15.2, the newest stable at the run date.
4. Package manager: none is needed to use it from a checkout. `pyproject.toml` has a `setuptools` backend and
   `pip install sdks/python` into a virtual environment was checked (import from `site-packages`, no dependency).
5. HTTP: the standard library's `http.client` over a socket the SDK opens itself, instead of `urllib` (no stream
   control) or a third-party client (no dependencies). Owning the socket gives a timeout that covers the whole
   request (a `threading.Timer` guard that shuts the socket down), an abort for `close`, and a kept reference to the
   socket, which is what the `http.client` `sock` quirk in the language notes needs.
6. Request timeouts run on real time, not on the fake clock. The fake clock covers the sync loop's sleeps, the
   flush interval, `Retry-After` holds and the UTC day. A timeout test uses a server that hangs.
7. The test stream is a close-delimited response (no `Content-Length`, no chunking), the shape that makes
   `http.client` drop its socket, so the watchdog test on a real loopback socket exercises the quirk.
8. `ready()` is bounded by `request_timeout + 1 s`: if the first attempt has not settled by then it settles `False`
   and stays fixed (first writer wins). Called from an SDK thread (a hook, `on_error`) it returns the settled value
   or `False` at once, never waiting.
9. A served dict or list is deep-copied before it is returned, so a caller cannot change the held ruleset.
10. Test seams: a private keyword `_seams=Seams(clock, jitter, watchdog, evaluator, ssl_context)` on `create`. The
    `ssl_context` is there so a test can trust a local certificate. No global, no environment variable.
11. No `re` module anywhere in `src` (every parser is hand written), so the evaluator cannot depend on Python's
    regular expressions.
12. Workflow toolchain step: `jdx/mise-action` limited to `python ruff` over `actions/setup-python` plus a ruff
    install, because the `bin/` scripts call `mise exec`, so CI runs exactly the tools `mise.toml` pins.
13. A hook is a plain function; one that returns an awaitable is not awaited (the future line is `n/a`).
14. The reason kinds, error codes and stream states are `StrEnum`s with the contract's spelling, so
    `ReasonKind.RULE_MATCH == "RULE_MATCH"`.

## Guesses

1. The contract says the URL is "not parsed beyond removing trailing /", yet a socket client needs the scheme, host,
   port and path prefix. Where: `docs/sdk-contract.md`, 7.1. Assumed: the URL is split with `urllib.parse.urlsplit`
   for those parts only; a URL it cannot use makes every request fail as a network error (`last_error` `network`)
   and never raises at the caller (`test_a_bad_url_is_a_network_error_not_a_raise`).
2. The TypeScript reference SDK honours `Retry-After` on any non-200 ruleset answer, while the contract names 429
   only. Where: `packages/sdk/src/` against `docs/sdk-contract.md`, 7.4. Assumed: the contract wins; only a 429
   with a valid `Retry-After` starts a hold (`test_a_retry_after_that_is_not_whole_seconds_is_no_hold`, and the
   harness's `rate-limit.json`).
3. `PROMPT.md` phase 5 says to copy the action pins from `ci.yml`, but `ci.yml` has no `jdx/mise-action`. Where:
   `PROMPT.md`, phase 5, item 4. Assumed: the pin comes from the sibling workflow `sdk-ruby.yml` and was checked with
   `gh api repos/jdx/mise-action/commits/v5.2.7 --jq .sha`, which printed the same SHA.
4. Which ruff rules count as "no warnings allowed" is not said. Where: `PROMPT.md`, phase 5, item 3. Assumed: the
   rule set in Decisions 1, with a short ignore list in `pyproject.toml` for rules that count branches or magic
   numbers and for `assert` and `subprocess` in tests.
5. Whether `ready()` called from inside a hook may wait is not said beyond "does not deadlock". Where:
   `docs/sdk-contract.md`, 6.4 and 5.3. Assumed: it returns the settled value, or `False` if unsettled, without
   waiting (`test_ready_called_from_a_callback_returns_at_once`).
6. The harness's phase 6 asks for a 10 times in a row run, but a full `bin/test` takes about 50 s here, so the
   whole check is 9 minutes. Where: `CHECKLIST.md`, "The two executable checks". Assumed: that is intended.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md` phase 3, after "Create the client", add: "The URL is split into scheme, host, port
   and path prefix when the HTTP client needs them; the contract's 'not parsed' means it is not validated."
2. For guess 2, in `PROMPT.md` phase 3, add: "Where the reference SDK and the contract disagree, the contract
   wins; say so in `RUN.md` as a guess."
3. For guess 3, in `PROMPT.md` phase 5 item 4, after "as `ci.yml` does", add: "(an action `ci.yml` does not use,
   such as `jdx/mise-action`, is copied from a sibling `sdk-<lang>.yml` and checked with `gh api`)".
4. For guess 4, in `CHECKLIST.md` under "Code and documents", add: "`bin/lint` enables a rule set named in `DESIGN.md`
   and ignores only rules named there, with a reason."
5. For guess 5, in `CHECKLIST.md`, native tests, on the callback line, add: "and `ready` called from inside the
   callback returns without waiting".
6. In `language-notes/python.md`, add: "A test server whose stream response has no `Content-Length` and is not chunked
   reproduces the `sock` is `None` quirk without a special case. `ruff` accepts `contextlib.suppress` where a
   swallowed error is intended (rules `SIM105` and `S110`)."

## Checklist

- [x] `bin/test` runs every vector but `buckets.json` and every pin, counts equal the phase 2 command's:
  `tests/test_vectors.py` (`test_every_vector`, `test_every_pin`); `bin/test` prints `vectors: 1371 pins: 17` and the
  count command prints `1371 17`.
- [x] `bin/test` passes 10 times in a row and once under CPU load: 10 of 10 plain runs and 1 loaded run (12 busy loops), all `Ran 85 tests ... OK`.
- [x] The harness exits 0, only the 2 named vectors skipped: `bun run conformance -- --service
  sdks/python/bin/test-service` printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed,
  2 skipped` and exit 0, twice. With `integer-accessor` advertised, no scenario was skipped.
- [x] Both context shapes accepted (contract 3): the harness, `privacy.json`, `kinds.json` vectors.
- [x] Result order and the mismatch rule: the harness, `evaluation.json`, `usage.json`; native
  `test_type_mismatch_falls_back_and_is_not_counted`, `test_a_null_fallback_takes_any_kind_and_a_null_value_is_a_mismatch_for_others`,
  `test_json_accessor_compares_kinds`.
- [x] With `integer-accessor`, a fraction is `TYPE_MISMATCH` and `2.0` is `2`: the harness, `evaluation.json`;
  native `test_integer_accessor_bounds`.
- [x] No failure makes an operation raise: the harness, every scenario (the service answers 500 on a raise).
- [x] Before the first load, `NOT_READY` at once: the harness, `load.json`; native
  `test_not_ready_before_the_first_load_and_after_it_the_value`.
- [x] `ready` settles after the first attempt, bounded, false after `close` if unsettled: the harness, `load.json`,
  `close.json`; native `test_ready_is_bounded_by_the_request_timeout`,
  `test_a_read_returns_at_once_while_the_ruleset_request_hangs`.
- [x] A non-ruleset answer never replaces the ruleset held: the harness, `status.json`; native
  `test_a_bad_ruleset_after_a_good_one_leaves_the_good_one`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining: the harness, `load.json`,
  `status.json`; native `test_the_second_fetch_is_conditional`, `test_a_trailing_slash_is_removed_from_the_url`.
- [x] The stream: events, SSE parsing, refresh on change, a failed refresh ends the stream: the harness,
  `stream.json`; native `tests/test_sse.py` (13 tests) and `test_a_failed_fetch_after_changed_ends_the_stream`.
- [x] The loop and the jittered backoff: the harness, `backoff.json`, `stream.json`; native
  `test_stream_failure_backs_off_with_jitter_then_polls`, `test_backoff_is_capped_at_the_poll_interval`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`; native `tests/test_sync.py`
  `test_retry_after_*` and `tests/test_usage.py` `test_a_429_with_a_retry_after_holds_the_next_flushes`.
- [x] Status fields, states and `lastError` codes: the harness, `status.json`, and the shape check on each status read.
- [x] Usage counts: what counts, summing, batches of 500, keep or drop by answer: the harness, `usage.json`,
  `rate-limit.json`; native `tests/test_usage.py`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: the harness, `hooks.json`; native
  `test_hook_gets_every_evaluation_with_the_etag_and_flag_version`,
  `test_a_hook_that_raises_goes_to_on_error_and_the_answer_is_the_same`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`,
  `rate-limit.json`, `usage.json`, `status.json`; native `test_close_does_not_wait_out_a_hold_and_reports_the_unsent_counts`.
- [x] No context, key or attribute in a request or `onError`, no header beyond the protocol's: the harness,
  `privacy.json`; native `test_requests_carry_only_the_allowed_headers` and
  `test_the_body_holds_counts_and_nothing_about_a_user`. Source search: `grep -rnE "logging|print\(|warnings"
  sdks/python/src` finds nothing, so no log line exists to carry one.
- [x] Many threads, `close` racing `variation`, two `close` calls racing: `tests/test_robustness.py`
  `test_close_racing_readers_and_a_second_close_finishes` (8 reader threads and 2 closers on a
  `threading.Barrier`, joined with a timeout). Python has no race detector; the tests run under `-X dev -W error`
  and the stress test above stands in, with `Thread.join(timeout)` so a deadlock fails.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `test_https_with_a_self_signed_certificate`
  (an `openssl` certificate under `$TMPDIR`, a trusting and a distrusting client; skips if `openssl` is absent).
- [x] `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs:
  `test_a_read_returns_at_once_while_the_ruleset_request_hangs`,
  `test_reads_stay_fast_and_close_is_bounded_while_usage_requests_hang`,
  `test_close_is_bounded_while_the_stream_and_ruleset_hang`.
- [x] A reader sees one ruleset or the other across a swap, `etag` paired with `flagVersion`:
  `test_the_etag_in_a_hook_event_always_matches_the_flag_version` (4 readers while the ruleset alternates).
- [x] A hook and an `onError` that call back into the client do not deadlock:
  `test_a_hook_and_on_error_may_call_back_into_the_client`, `test_ready_called_from_a_callback_returns_at_once`.
- [x] A stream with no bytes for the watchdog is dropped and a keep-alive restarts it, on a real loopback socket,
  the length set by a seam: `test_a_silent_stream_is_dropped_after_the_watchdog_and_reopened`,
  `test_keep_alive_comments_hold_the_watchdog_off`.
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through the fake clock:
  `test_retry_after_above_300_is_held_for_300_on_the_ruleset`, `..._on_the_stream` (`tests/test_sync.py`),
  `test_a_retry_after_above_300_is_held_for_300_on_the_events_route` (`tests/test_usage.py`).
- [x] Counts either side of a UTC midnight go out as two days:
  `test_counts_either_side_of_utc_midnight_go_out_as_two_days`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`:
  `test_an_evaluator_that_raises_is_evaluation_failed_and_one_on_error`.
- [x] Hooks returning a future: n/a: a Python hook is a plain function, and an awaitable it returns is not awaited
  (Decision 13).
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: the harness `huge`
  scenario; native `test_integer_accessor_bounds` (`2**63` and `-2**63 - 1`).
- [x] A client never closed does not keep the process alive: `test_an_unclosed_client_does_not_keep_the_process_alive`
  (a `python -I` subprocess that creates a client, waits for ready and exits without `close`).
- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` links the mapping table: `README.md`, "The API"
  (a short table, and the link to `DESIGN.md` section 1).
- [x] Names keep the canonical meaning and defaults, enums keep their spelling: `DESIGN.md` section 1,
  `src/toggleslash/_types.py` (`StrEnum`s with the contract's values).
- [x] The evaluator has no platform regular expression: `grep -rnE "import re\b|from re\b" sdks/python/src` finds
  nothing.
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none (`DESIGN.md` section 4,
  `pyproject.toml` `dependencies = []`).
- [x] No telemetry, headers only `Authorization`, `Accept`, `If-None-Match` and `Content-Type` from the SDK: the
  source sets exactly those (`src/toggleslash/_sync.py`, `_usage.py`). One request of each route captured on a
  loopback listener (`tests/support.py` `FakeServer`) showed the headers the client added on its own: `Host` (the
  server's own loopback address, which the caller chose), `Accept-Encoding: identity` (set by `http.client`,
  carries no identity, and asks for an uncompressed body) and `Content-Length` (the size of the usage body). No
  `User-Agent`, no `Connection` is sent.
  `test_requests_carry_only_the_allowed_headers` holds this.
- [x] Every extra beyond the contract is one contract 13 allows: the context manager, the `timedelta` durations and
  the `Seams` option; none weakens contract 5 or sends anything new.
- [x] Seams are options on `create`, never a global or an environment variable: `src/toggleslash/_seams.py`;
  `test_no_seam_is_a_global_or_an_environment_variable` searches `src` for `os.environ` and `getenv`.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK and run with the pinned tools:
  `ls -l sdks/python/bin` shows mode 755 on all three; each starts with `cd "$(dirname "$0")/.."` and calls `mise
  exec`.
- [x] `test-service/README.md` maps every accessor: its table lists `bool`, `string`, `number`, `int`, `json`
  and `any`.
- [x] The workflow follows the skeleton, filters on its paths, and pins each action by SHA:
  `.github/workflows/sdk-python.yml`.
- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes: `All matched files use the correct format.`
- [x] Only `$L/` and the one workflow changed: the phase 6 scope check printed `scope ok`.
- [x] No package published and no name reserved: nothing was uploaded; `pip install` was run on a local copy only.
- [x] Commit subject is a Conventional Commit (`chore(sdk-python): ...`), not `feat`, with no `Co-Authored-By` or
  AI attribution.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: every section above, every checklist line with its evidence.
