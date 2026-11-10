# Ruby SDK: run report

## Prompt version

- Prompt: d8735fba478d7da8b9f2a2ff554bf34a42f0ec3d
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-10
- Input: language Ruby; runtime chosen: MRI (CRuby), developed on 4.0.9; minimum version chosen: 3.3; package manager chosen: RubyGems (`toggleslash.gemspec`, nothing published)
- Toolchain: ruby 4.0.9 (`ruby 4.0.9 (2026-10-23 revision 3e93b23afc) +PRISM [x86_64-linux]`), pinned in `mise.toml`; rubocop 1.92.2 (`1.92.2 (using Parser 3.3.13.0, Prism 2.1.0, rubocop-ast 1.51.0, analyzing as Ruby 3.3, running on ruby 4.0.9)`), pinned in `mise.toml` as `gem:rubocop`; minitest from Ruby's bundled gems (no pin needed); bun 1.4.2 for the harness and the checks. Versions were looked up with `mise ls-remote ruby`, `mise ls-remote gem:rubocop` and the ruby-lang.org release page on the run date, not from memory.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 65 passed of 65 (52 in `client_test.rb`, `stream_test.rb`, `http_test.rb`, `sse_test.rb` and `json_test.rb`, 13 from `vectors_test.rb`: one test per vector file but `buckets.json` (11), the bucket pins and the fixtures check)

## Decisions

1. Runtime: MRI over JRuby or TruffleRuby, because it is the mainstream runtime and the only one with a GVL whose behaviour the concurrency design can state. Minimum 3.3, the oldest release still in security maintenance (3.2 is end of life), which has `Data.define` and `Hash#except`.
2. Package manager and layout: RubyGems with a gemspec over Bundler-only, because the gemspec is the language's package description. It sets no homepage and no registry, and `lib/` is put on the load path with `-I` or a Gemfile `path:` line.
3. Test framework: Minitest over RSpec, because it ships with Ruby and adds no dependency.
4. Lint and format: RuboCop over StandardRB, because it is one pinned tool for both, and its rules are in `.rubocop.yml` (target 3.3, new cops enabled, line length 120, metrics relaxed, single quotes). `bin/lint` also runs `ruby -wc` on every file. The tests fail on any Ruby warning (`Warning.warn` is patched in `test/test_helper.rb`), Ruby's analogue of `-W error`.
5. No runtime dependency. The standard library's `json` raises on a lone surrogate (`JSON.parse('"\ud800"')` fails on json 2.18.0), which the contract's context handling must accept, so the SDK has its own JSON parser and generator (`json.rb`). `Net::HTTP` cannot bound the whole response with one deadline, cancel a blocked read from another thread or enforce a stream idle limit at the socket, so the SDK has its own small HTTP/1.1 client over `socket` and `openssl` (`http.rb`), and its own SSE parser (`sse.rb`). Each is a line in `DESIGN.md`.
6. Concurrency: the SDK state is one frozen `State` swapped under a `Mutex`, so a reader sees one ruleset or the other; two daemon threads (sync, usage flush). A reader never takes a lock for I/O.
7. Public API shapes: keyword arguments on `create`, durations as `Numeric` seconds, closed sets (reason kinds, error codes, stream states, `lastError` codes) as Symbols with the contract's spelling, snake_case method names (`bool_variation_detail`).
8. Seams are keyword options on `create`: `clock:` (wall time and the Retry-After hold; request deadlines use the real monotonic clock), `random:`, `evaluator:` and `stream_idle_timeout:`. They are not documented as public API and nothing reads a global or an environment variable.
9. Advertised `integer-accessor`, because Ruby has a separate `Integer` type. `int_variation` takes the signed 64-bit rule explicitly, since Ruby's integers are unbounded (`2.0` becomes `2`; a fraction or a number outside 64 bits is `TYPE_MISMATCH`).
10. Generic `variation` takes its kind from the fallback, and a `nil` fallback accepts any served kind. The json accessor accepts an array or an object that matches the kind of its fallback (a `nil` fallback accepts either).
11. Hooks and `on_error` are plain callables, so the future line of the checklist does not apply. A hook that is not callable is ignored.
12. `close` is leader and follower under a mutex and a condition variable, in the contract's order: stop counting, close the store, cancel both loops, join the threads, settle `ready`, the final flush, release the pipes. A usage flush that is running is waited for (contract 11 step 3) and is bounded by `request_timeout`; a `close` from the leader's thread or an SDK thread returns at once. The skeptical review found that an earlier version abandoned the running flush and could deadlock on a `close` from `on_error`; both were changed.
13. Test service: a hand-written `TCPServer` on `127.0.0.1` port 0, one thread per connection, over the same dependency-free approach as the SDK.
14. Toolchain entry points: the `bin` scripts `cd` to the SDK and run through `mise exec`, with a plain `ruby` fallback when mise is absent. `bin/lint` runs the RuboCop gem script under the mise-pinned ruby (see stuck point 1, which is not a blocker).
15. The workflow follows `sdk-python.yml` line for line, with the toolchain step limited to `ruby gem:rubocop` so the root `.tool-versions` is not installed.

## Guesses

1. There is no `language-notes/ruby.md`, so every Ruby choice above was a decision with no note to check it against. Where: `language-notes/README.md`. Assumed: the notes are optional, and `DESIGN.md` carries the reasons.
2. What a typed json accessor accepts was not stated. The first version required the served value to match the fallback exactly, and the harness sent a `{}` fallback with a served `[1]`. Where: `sdk-test-service.md`, "Accessors", and `sdk-contract.md`, section 4, "Typed languages". Assumed: array and object are one container kind for the json accessor, and a `nil` fallback accepts either.
3. What `close` does when called from `onError` or a hook on the closing thread or on one of the SDK's own threads, which would wait for a shutdown that is waiting for it. Where: `sdk-contract.md`, section 11 ("safe to call twice (or concurrently)") and 6.4. Assumed: it returns at once.
4. Where a native test for "two `close` calls racing" belongs, and whether it may be the same test as the many-thread stress. Where: `CHECKLIST.md`, "Native tests". Assumed: one test (`test_operations_called_from_many_threads_with_close_racing`) with workers that call `close` and four racing closers, bounded by `Timeout`.
5. How a busy test thread is allowed to behave under the GVL. Twelve threads spinning without `Thread.pass` starved the close leader for more than 5 s. Where: `CHECKLIST.md`, "Native tests", stress test. Assumed: a test thread passes the GVL each turn, which is what a real caller that does any I/O does.
6. Whether CI can be run before the PR. The workflow could not be run locally (no GitHub Actions runner). Where: `PROMPT.md`, phase 5. Assumed: it is correct because it differs from `sdk-python.yml` only in the name, the paths, the concurrency group and the toolchain step; the first CI run is the proof.
7. The headers an own HTTP client adds. The checklist lists `Host`, `Content-Length` and `Connection` as allowed, and this client writes exactly those. Where: `CHECKLIST.md`, "Code and documents". Assumed: the capture below is the evidence.

## Stuck points

None. Nothing needed outside help and no contract, harness, scenario or vector was wrong. Three tool problems were worked around and are in the proposals below:

1. `mise exec -- rubocop` fails with `cannot load such file -- erb`: the system ruby (3.4.10) comes before the mise one on `PATH` for the rubocop shim. `bin/lint` instead runs the gem's script under the pinned ruby (`GEM_HOME="$(mise where gem:rubocop)/libexec" mise exec -- ruby "$(mise where gem:rubocop)/libexec/bin/rubocop"`).
2. `rubocop -A` (autocorrect) broke the code in four places: `.freeze` on counters that are mutated, and `.zero?` on a comparison that can return `nil` (`compare(a, b) == 0` in `eval/clause.rb`, kept with an inline disable that says why).
3. Ruby gotchas the vectors found, all fixed with tests: `"".split('.', -1)` is `[]` (a semver of `""`), `Float("1e999")` can warn under `-w`, and a regexp scan over invalid UTF-8 can raise (so the evaluator scans bytes).

## Proposed prompt changes

1. For guess 1, add `language-notes/ruby.md` with: MRI and the GVL (a thread-safety design, not a speed one); no race detector, so the stress test is the substitute; `-w` plus `Warning.warn` patched in the test helper is the warnings-as-errors; Minitest and RuboCop with one pinned `.rubocop.yml`; `Data.define` for events (3.2 and later); unbounded `Integer`, so state the 64-bit rule; symbols for closed sets.
2. For stuck point 1, in `language-notes/ruby.md`: "`mise exec -- rubocop` can pick the system ruby; run the gem script under the pinned ruby with `GEM_HOME` set to `$(mise where gem:rubocop)/libexec`. In CI the `install_args: ruby gem:rubocop` form of the mise action works."
3. For stuck point 2, in `language-notes/ruby.md`: "Do not run `rubocop -A` over the tree: `Style/RedundantFreeze` and `Style/NumericPredicate` autocorrect mutable counters and nil-able comparisons into bugs. Fix by hand and disable a cop inline with the reason."
4. For stuck point 3, in `language-notes/ruby.md`: "`''.split('.', -1)` is `[]`; `Float('1e999')` can warn under `-w`; a regexp on invalid UTF-8 can raise: scan bytes (`/n` regexes) when parsing untrusted text."
5. For guess 2, in `sdk-test-service.md`, "Accessors", after the json row, add: "A language with a separate array and object type treats them as one kind for this accessor; the harness sends a `{}` fallback for a served `[1]`."
6. For guess 5, in `CHECKLIST.md`, "Native tests", stress test line, add: "In a runtime with a global lock, a worker yields each turn (`Thread.pass`), or the test starves the code it is checking."
7. For guess 4, in `CHECKLIST.md`, the stress test line, add: "One test may cover the many-thread race and the two-close race, so long as it names both."
8. For guess 7, in `PROMPT.md`, phase 3, "HTTP", after "Do not add a header the protocol does not name", add: "An own HTTP client writes `Host`, `Connection` and `Content-Length` itself; capture one request on a loopback listener and paste it in `RUN.md`."

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json`, read from the repository, and every pin; the count equals the phase 2 command's: `bin/test` prints `vectors: 1371 pins: 17`; `bun -e '...'` prints `1371 17`. `test/vectors_test.rb` (`test_fixtures_exist` fails when no files or no vectors were read, `test_bucket_pins` checks `uint32` and `bucket`).
- [x] `bin/test` passes 10 times in a row, and once under CPU load: 10 of 10 runs printed `65 runs, ... 0 failures, 0 errors, 0 skips` with `vectors: 1371 pins: 17` (assertions 29068 to 73761 per run, because the stress tests loop until a state is reached). Under load (12 busy loops, one per core, `nproc` = 12): `65 runs, 30057 assertions, 0 failures, 0 errors, 0 skips`, `vectors: 1371 pins: 17`.
- [x] The harness exits 0 with only the two named vector skips: `bun run conformance -- --service sdks/ruby/bin/test-service` printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, exit 0.

### What the harness checks

- [x] Both context shapes: the harness (`privacy.json`, the `kinds.json` vectors), exit 0.
- [x] Result order, mismatch rule, own error codes: the harness (`evaluation.json`, `usage.json`).
- [x] With `integer-accessor`, a served fraction is `TYPE_MISMATCH` and `2.0` is `2`: the harness (`evaluation.json`); also `test_the_integer_accessor_follows_the_signed_64_bit_rule`.
- [x] No failure makes an operation raise: the harness, every scenario (the service answers 500 on any raise); `test_a_url_that_cannot_be_reached_never_raises`.
- [x] `NOT_READY` before the first load, at once: the harness (`load.json`); `test_evaluating_before_the_first_load_is_not_ready_at_once`.
- [x] `ready` settles after the first attempt, bounded, may be called again, false after `close`: the harness (`load.json`, `close.json`).
- [x] An answer that is not a ruleset never replaces the one held: the harness (`status.json`).
- [x] The ruleset request, `If-None-Match`, the exact ETag, URL joining: the harness (`load.json`, `status.json`); `test_a_second_poll_sends_if_none_match_and_a_304_is_a_sync`; `test_target_joins_a_base_path_and_refuses_what_is_not_http`.
- [x] The stream, SSE parsing, a failed refresh ends the stream: the harness (`stream.json`, `load.json`, `status.json`); `sse_test.rb`, `stream_test.rb`.
- [x] The loop and the jittered backoff: the harness (`backoff.json`, `stream.json`).
- [x] `429` and `Retry-After` on every route: the harness (`rate-limit.json`); `test_retry_after_counts_on_a_429_only_in_whole_seconds_capped_at_300`.
- [x] Status fields, states and `lastError` codes: the harness (`status.json`, and the shape check on every read).
- [x] Usage counts: the harness (`usage.json`, `rate-limit.json`); `test_a_batch_holds_at_most_500_entries_and_usage_carries_no_context`.
- [x] Hook events and `onError`, a hook that raises, an `onError` that raises: the harness (`hooks.json`); `test_an_on_error_that_raises_is_ignored`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness (`close.json`, `rate-limit.json`, `usage.json`, `status.json`); `test_nothing_is_counted_after_close_begins_and_close_flushes`, `test_close_waits_for_a_hung_usage_flush_for_at_most_the_request_timeout`, `test_a_close_from_on_error_on_the_sdk_threads_and_the_closing_thread_does_not_deadlock`.
- [x] No context, key or attribute in any request or `onError` message, no extra header: the harness (`privacy.json` and the checks after every scenario). Source search for logs: `grep -rnE '\b(puts|warn|print|p |pp|logger|Logger|\$stderr|\$stdout|STDERR|STDOUT|abort)\b' lib` finds nothing, so the SDK writes no log line. Error values are built from the status, route, a fixed phrase or the Ruby exception of a network failure (`sync.rb` `StatusError` builds `GET <route> answered <status>`; `usage.rb` builds `POST /sdk/v1/events answered <status>`); no code path interpolates a context, a key or an attribute into text. The evaluator's failure is reported as the exception the evaluator raised, and `test_an_evaluation_that_raises_gives_the_fallback_and_one_on_error` checks that the one `onError` message is the evaluator's own text.

### Native tests

- [x] Every operation from many threads, `close` racing `variation`, two closes racing: `test_operations_called_from_many_threads_with_close_racing` (`client_test.rb`): 12 workers call `variation`, `int_variation_detail`, `status`, `ready` and one of them `close`, four extra threads call `close` under `Timeout.timeout(5)`, every join is bounded, and no error is collected. Ruby has no race detector (the GVL makes the interpreter state safe and the SDK's own state is one frozen object under a mutex), so this stress test, with its bound on every wait, stands in.
- [x] `variation` and `variationDetail` return while a ruleset request hangs and while a usage flush hangs: `test_variation_returns_while_a_ruleset_request_hangs`, `test_variation_returns_while_a_usage_flush_hangs`.
- [x] A reader sees one ruleset or the other across a swap: `test_a_hook_sees_an_etag_and_flag_version_of_the_same_ruleset_across_swaps`.
- [x] A hook and an `onError` that call back into the client do not deadlock: `test_a_hook_and_on_error_that_call_back_into_the_client_do_not_deadlock` (with a guard so the hook does not re-enter itself).
- [x] A stream with no bytes for 60 s is dropped as `timeout`, a keep-alive restarts it, through a seam on a real loopback socket: `test_a_stream_that_goes_quiet_is_dropped_as_a_timeout_through_the_idle_seam`, `test_a_keep_alive_restarts_the_idle_watchdog` (`stream_test.rb`), which use `stream_idle_timeout:` and a real `TCPServer` that goes quiet.
- [x] A `Retry-After` above 300 is held for 300 s on each route: `test_a_retry_after_above_300_is_held_for_300_seconds_on_the_ruleset_route`, `..._on_the_stream_route`, `test_the_events_route_has_its_own_hold_capped_at_300`, through the fake clock.
- [x] Counts either side of a UTC midnight go out as two days: `test_counts_either_side_of_a_utc_midnight_go_out_as_two_days`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`: `test_an_evaluation_that_raises_gives_the_fallback_and_one_on_error` (the `evaluator:` seam).
- [x] Where hooks may return a future: n/a: no futures (a hook is a plain callable; `test_a_hook_that_is_not_callable_is_ignored` covers the shape).
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `test_the_integer_accessor_follows_the_signed_64_bit_rule` and the harness's `huge` scenario.
- [x] A client never closed does not keep the process from exiting: `test_a_client_that_is_never_closed_does_not_keep_the_process_alive` (runs a child ruby that creates a client and ends), because Ruby ends every other thread when the main thread ends, so the two background threads cannot hold the process; the test starts a child ruby that creates a streaming client, never closes it, and expects a successful exit well inside 8 s.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` links the mapping table: `DESIGN.md#mapping-table-contract-section-2`, linked from `README.md`.
- [x] Names keep their canonical meaning and defaults, and reason kinds, error codes, stream states and `lastError` codes keep their spelling: the mapping table; `README.md`, "The API"; the harness checks every spelling through the service.
- [x] The evaluator has no platform regex: `grep -nE 'Regexp|=~|\.match|\.scan|\.gsub|\.sub\(|/[^/ ]+/[imxn]*[ ).,]' lib/toggleslash/eval/*.rb lib/toggleslash/eval.rb` finds only `String#split` and `String#index` with string arguments (`semver.rb`, `text.rb`), which are not regular expressions. The pattern engine is `eval/pattern_parser.rb` and `eval/pattern.rb` (a Pike VM). The platform's regexes are used only outside `eval/`: `config.rb` (trailing slash), `http.rb` (status line, digits, chunk size), `json.rb` (tokens).
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none, and `DESIGN.md`, "Dependencies", says why the standard library's `json` and `Net::HTTP` do not do.
- [x] No telemetry, no header that identifies a user or machine: the only headers the code writes are `Authorization` (`sync.rb`, `usage.rb`), `Accept` (`sync.rb` stream), `If-None-Match` (`sync.rb`), `Content-Type` (`usage.rb`), plus `Host`, `Connection` and `Content-Length` written by `http.rb` lines 80 to 86. One request captured on a loopback listener (the sdk key replaced by `<key>`, the port the one the OS gave): `GET /sdk/v1/ruleset HTTP/1.1`, `Host: 127.0.0.1:44275`, `Connection: close`, `Authorization: Bearer <key>`; and `POST /sdk/v1/events HTTP/1.1`, `Host: 127.0.0.1:44275`, `Connection: close`, `Authorization: Bearer <key>`, `Content-Type: application/json`, `Content-Length: 73`. The client adds `Host` (the server's own address, which the caller gave: it names the server, not a user or machine), `Connection: close` (one request per connection, a fixed word) and `Content-Length` (the byte count of the body). It adds no `User-Agent` and no `Accept-Encoding`. `test_the_headers_the_client_adds_are_the_protocol_ones_and_nothing_that_names_a_user` holds this in `http_test.rb`.
- [x] Every extra beyond the contract is one contract 13 allows: the typed accessors, the `Detail` and `EvaluationEvent` types and the seams. None sends anything new.
- [x] Seams are options on `create`, never a global or an environment variable: `grep -rn ENV lib` finds nothing, and the seams are `clock:`, `random:`, `evaluator:`, `stream_idle_timeout:`.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and use only the `mise.toml` toolchain: `ls -l sdks/ruby/bin`, each is `#!/bin/sh` with `set -eu` and `cd "$(dirname "$0")/.."`.
- [x] `test-service/README.md` maps every accessor to an SDK method: the table has bool, string, number, int, json and any.
- [x] The workflow follows the skeleton, filters on its paths and pins every action by SHA: `.github/workflows/sdk-ruby.yml` differs from `sdk-python.yml` only in its name, paths, concurrency group, job name and toolchain step (checked with `diff`); the three actions are pinned to the same SHAs as `ci.yml`.

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exit 0.
- [x] `bun run format:check` passes: `All matched files use the correct format.`
- [x] Scope: the phase 6 scope check printed only `scope ok`.
- [x] No package was published and no name reserved: the gemspec has no registry and no push was made; no `gem build` was run.
- [x] Commit subject is a Conventional Commit, not `feat`, with no `Co-Authored-By` and no AI attribution: `chore(sdk-ruby): a Ruby server SDK built from the authoring prompt`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: the section grep printed nothing.
