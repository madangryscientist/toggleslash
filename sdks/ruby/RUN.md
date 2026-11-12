# Ruby SDK: run report

## Prompt version

- Prompt: 26598eee3b61a0b44edc1c6d50c5d4e4ad08b813 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run and unchanged after the phase 6 `git fetch origin`)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-12
- Input: language Ruby; runtime chosen: MRI (CRuby); minimum version chosen: 3.3 (proved on 3.3.13); package manager chosen: RubyGems with a gemspec and no Bundler dependency
- Toolchain: `mise.toml` pins `ruby = "4.0.9"` and `"gem:rubocop" = "1.92.2"`. `ruby -v` prints `ruby 4.0.9 (2026-10-23 revision 3e93b23afc) +PRISM [x86_64-linux]`; `rubocop -V` prints `1.92.2 (using Parser 3.3.13.0, Prism 2.1.0, rubocop-ast 1.51.0, analyzing as Ruby 3.3, running on ruby 4.0.9)`. Minitest is the one that ships with Ruby 4.0.9 (a bundled gem), so the build file pins nothing for tests. The gemspec has no runtime or development dependency. Bun 1.4.2 runs the harness.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command gives the same two numbers)
- Conformance: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 110 passed of 110, 500 assertions (`bin/test`: `110 runs, 500 assertions, 0 failures, 0 errors, 0 skips`). By file: client 21, sync 21, http 16, usage 10, sse 10, json 10, concurrency 5, text 6, pattern 6, tls 3, vectors 2 (the vectors test is one test over 1371 vectors, the pins test one test over 17 pins). The test service has no tests of its own; the harness runs it.
- Repeat runs: `bin/test` ten times in a row, each `110 runs, 500 assertions, 0 failures, 0 errors, 0 skips`, then the eleventh with 12 busy shell loops (`nproc` is 12) running at normal priority from before the command until after it: `110 runs, 500 assertions, 0 failures`. The staged tree hash was `f24fb466bf4dd7decfee5e82c0e2045898d70670` before and after the eleven runs. Ruby 3.3.13 (the minimum) also passes: 110 runs, 0 failures, and the harness passes with the service on it.
- Lint: `bin/lint` prints `35 files inspected, no offenses detected` and compiles every file with `ruby -w` with no warning.

### Request headers the client sends, one request per route

Captured on a loopback listener with a script outside the tree (it starts the SDK against the test suite's fake server and prints each distinct route's header map, key redacted).

| Route | Headers on the wire |
| --- | --- |
| `GET /sdk/v1/ruleset` | `Host`, `Connection: close`, `Accept: application/json`, `Authorization` |
| `GET /sdk/v1/ruleset` (conditional) | the row above plus `If-None-Match` |
| `GET /sdk/v1/stream` | `Host`, `Connection: close`, `Accept: text/event-stream`, `Authorization` |
| `POST /sdk/v1/events` | `Host`, `Connection: close`, `Content-Length`, `Content-Type: application/json`, `Authorization` |

`Authorization`, `Accept`, `If-None-Match` and `Content-Type` are the protocol's four. The SDK writes its own requests over `socket`, so no HTTP library adds anything: `Host` (the URL's host and port, which the server already knows), `Connection: close` (one request per connection), and `Content-Length` (the byte length of the usage body) are the only others. There is no `User-Agent` and no `Accept-Encoding`. None identifies a user or a machine. `test_only_the_allowed_headers_are_sent` in `test/usage_test.rb` holds the set.

## Decisions

1. Toolchain through mise: every tool is called as `mise exec -- <tool>` because a bare `ruby` outside the SDK directory resolves to another version. RuboCop is a mise `gem:` tool whose own gem directory has to be on `GEM_HOME`, so `bin/lint` runs `GEM_HOME=$(mise where gem:rubocop)/libexec GEM_PATH=... mise exec -- ruby $GEM_HOME/bin/rubocop` (plain `rubocop` when mise is absent). Over a bare `mise exec -- rubocop`, which picked a different Ruby here and failed on the gem path.
2. Lint rule set: RuboCop's default cops with `NewCops: enable` over `lib test test-service toggleslash.gemspec`, plus a `ruby -w` compile of every file. `.rubocop.yml` names the set at its top and each cop turned off or loosened carries its reason beside it: the Metrics cops (a protocol client and a pattern VM are as long as their specification), `Style/Documentation` excluded for tests, `Naming/MethodParameterName` minimum 1 (`cp`, `lo`, `hi`), `Naming/PredicateMethod` off, `Style/ComparableBetween` off, `Style/OneClassPerFile` off, `Naming/VariableNumber` for method names, `Metrics/BlockNesting` 4, line length 120. Double quotes over single because the whole tree uses them. Shell scripts are not passed to RuboCop (it parses them as Ruby). Only RuboCop's safe autocorrect (`-a`) was used, never `-A`.
3. Format check: RuboCop's layout cops are the format check, so one tool does both, over a second formatter dependency.
4. No runtime dependency (DESIGN.md section 4): `socket`, `openssl`, `uri` and `thread` ship with Ruby. The JSON reader is the SDK's own (DESIGN.md section 5) because the standard `json` gem cannot hold a lone surrogate in a `String`, reads a literal such as `1e999` only with a warning, and does not skip a leading byte order mark or replace invalid bytes as the WHATWG decoder does (DESIGN.md section 5).
5. Numbers: a served or context number is read as binary64 through `Rational` and `Rational#to_f`, which is warning-free where `Float("1e999")` warns. A context attribute of any Integer width, Float, Rational or BigDecimal matches as the JSON number of its value (`test_a_context_attribute_of_each_numeric_type_matches_as_its_value`); `Complex` is not a JSON number and does not match.
6. Aborting a request in flight uses a self-pipe in `IO.select`, because closing an IO from another thread does not wake a thread blocked in `IO.select` in MRI (`test_abort_wakes_a_waiting_request`, `test_close_while_a_stream_is_open_returns_at_once`).
7. Usage and error callbacks are called after the flush lock is released, so a callback that calls back into the client cannot deadlock (`test_callbacks_that_call_back_into_the_client_do_not_deadlock`).
8. `Content-Length: 0` on the stream route is `http_200` (not a connected stream), and a clean empty 200 stream leaves `lastError` alone (`test_a_stream_answering_200_with_content_length_zero_is_http_200`, `test_a_stream_that_ends_cleanly_with_no_bytes_leaves_last_error_alone`). A 304 with no ruleset held is `http_304` (`test_a_304_with_no_ruleset_held_is_http_304`).
9. The generic `variation` is dynamically typed: an Integer fallback has the JSON kind number, so a served `2.0` comes back as `2.0`. The integer rules (`2.0` is `2`, a fraction or an out-of-range value is `TYPE_MISMATCH`) belong to `int_variation` and `int_variation_detail`, which the test service maps for the `int` accessor and `integer-accessor` advertises. The harness's `number` accessor expects the served number unchanged, which settles it. Tests: `test_the_generic_accessor_with_an_integer_fallback_returns_the_served_number`, `test_the_integer_accessor`.
10. The integer accessor with a `nil` fallback accepts any served number and answers `TYPE_MISMATCH` for any other kind, because the accessor's own kind is number (`test_the_integer_accessor_with_a_nil_fallback_accepts_a_number_only`). The generic accessor with a `nil` fallback accepts any kind (`test_a_null_fallback_accepts_any_kind`).
11. Durations (`poll_interval`, `flush_interval`, `request_timeout`) that are not positive finite numbers fall back to the default, and a very large one is clamped to one year so `Time` arithmetic cannot overflow (`test_durations_fall_back_to_the_default`).
12. No size cap on the 200 ruleset body, a stream line or an event, since the contract names none; only the nesting depth is bounded (1000, above the contract's 512 minimum; deeper is a parse error, and a `SystemStackError` is caught too, `test_nesting_limit`).
13. Seams are keywords on the private `Client.new` (`transport`, `clock`, `jitter`, `stream_watchdog`, `evaluator`; `lib/toggleslash/client.rb:25`), not on `Toggleslash.create`, which takes the public options only. No global and no environment variable is read by the SDK.
14. The gemspec license is `Elastic-2.0`, the repository's source-available licence; nothing is published and no gem name is reserved.
15. Workflow toolchain: `jdx/mise-action` (pinned by SHA) with `working_directory: sdks/ruby` and `install_args: ruby gem:rubocop`, so CI uses the versions `mise.toml` pins. The job is `runs-on: ubuntu-latest` with no `if:` guard, triggers `pull_request` and `push` only.
16. The test service is a few lines over `TCPServer` because the SDK has no runtime dependency and neither does its service. It rescues nothing the SDK raises: a raise answers 500, so the harness fails the scenario.
17. Threads and exit: the SDK's threads are daemon-like in effect (Ruby exits when the main thread ends and does not wait for them), and set `report_on_exception = false` so nothing is printed to stderr. A subprocess test holds that a never-closed client does not keep the process alive.
18. Race detector: Ruby has none. The stand-in is `test/concurrency_test.rb` (every operation from many threads, `close` racing `variation` and a second `close`, `ready` from many threads, `close` during a hung ruleset request and during an open stream, each with a bound on the wait). The many-thread test runs 16 threads of 300 operations each (variation, status, int_variation, variation_detail) under a 30 s `Timeout`, with fast poll and flush loops running beside them.
19. Optional `bigdecimal` is required inside a `begin ... rescue LoadError` in the numeric context test, because it is a bundled gem from Ruby 3.4 and absent from a bare 3.3 install.
20. Retry-After is digits only (any other form is ignored) and capped at 300 s; one hold is shared by the ruleset and the stream, and the events route has its own (contract 7.4, 9; `test_a_retry_after_that_is_not_whole_seconds_is_ignored`, `test_a_429_on_the_stream_holds_the_ruleset_too`).

## Guesses

None. Every point the prompt and the documents left open on purpose is a Decision above with the test or file that settles it.

## Stuck points

None.

## Proposed prompt changes

These are suggestions for `language-notes/ruby.md` (there is no Ruby note yet), each tied to a Decision above, not a guess:

1. For decision 1, in `language-notes/ruby.md`, add: "Call every tool as `mise exec -- <tool>`. RuboCop installed as `gem:rubocop` needs its own gem directory: `GH=\"$(mise where gem:rubocop)/libexec\"; D=$(mise exec -- ruby -e 'print Gem.default_dir'); GEM_HOME=$GH GEM_PATH=\"$GH:$D\" mise exec -- ruby \"$GH/bin/rubocop\" ...`. Use `-a` (safe), never `-A`. Do not pass shell scripts to RuboCop."
2. For decision 2, in `language-notes/ruby.md`, add: "`Minitest/MultipleAssertions` does not exist in RuboCop 1.92.2; do not name it. Set `Style/StringLiterals` and `Style/StringLiteralsInInterpolation` to one style, or the default cops fight the tree. Turn the Metrics cops off with a reason: they measure the specification."
3. For decision 6, in `language-notes/ruby.md`, add: "In MRI, closing an IO from another thread does not wake `IO.select` or a blocked read. Abort a request with a self-pipe the select waits on. Set `report_on_exception = false` on every SDK thread."
4. For decision 5, in `language-notes/ruby.md`, add: "`Float(\"1e999\")` prints a warning; read numbers through `Rational` and `Rational#to_f`. `\"\".split` is `[]`, not `[\"\"]`."
5. For decision 18, in `language-notes/ruby.md`, add: "A test that patches `Warning` must make the warning raise, or a warning in the SDK passes unnoticed; `bin/lint` also compiles every file with `ruby -w`."

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equal to the phase 2 command: `test/vectors_test.rb` (`test_vectors`, `test_pins`) reads `fixtures/evaluation/*.json` at test time; prints `vectors: 1371 pins: 17`, the same as the count command.
- [x] `bin/test` ten times in a row and an eleventh under CPU load: ten runs of `110 runs, 500 assertions, 0 failures, 0 errors, 0 skips`; the eleventh with 12 busy loops (`nproc`) started before and kept running until it ended: `110 runs, 500 assertions, 0 failures`. The staged tree hash was identical before and after (`f24fb466bf4dd7decfee5e82c0e2045898d70670`). No test needed fixing for load in the final tree.
- [x] The harness exits 0 with only the 2 named vector skips: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`; `integer-accessor` is advertised, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes accepted: the harness (`privacy.json`, `kinds.json` vectors through the service); a non-finite number is a present attribute of the wrong type (`test/vectors_test.rb`).
- [x] Result order and the mismatch rule with the SDK's error codes: harness `evaluation.json`, `usage.json`; `test_type_mismatch_gives_the_fallback`, `test_a_null_fallback_accepts_any_kind`, `test_a_class_with_no_json_kind_never_matches`.
- [x] `integer-accessor`: a fraction is `TYPE_MISMATCH` and `2.0` is `2`: harness `evaluation.json`; `test_the_integer_accessor`.
- [x] No failure makes an operation raise: harness, every scenario (the service answers 500 on a raise); `test_an_evaluator_that_raises_a_deep_stack_error_is_contained`, `test_on_error_that_raises_is_swallowed`.
- [x] Evaluating before the first load is `NOT_READY` at once: harness `load.json`; `test_not_ready_before_a_load_answers_the_fallback`.
- [x] `ready` settles after the first attempt, bounded, callable again, false after `close` if unsettled: harness `load.json`, `close.json`; `test_ready_called_from_many_threads_agrees`.
- [x] An answer that is not a ruleset never replaces the one held: harness `status.json`; `test_a_bad_ruleset_keeps_the_one_held`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`; `test_the_ruleset_request_headers_and_the_conditional_request`, `test_an_etag_is_kept_exactly_as_received`, `test_a_path_prefix_in_the_url_is_kept`.
- [x] The stream: harness `stream.json`, `load.json`, `status.json`; `test/sse_test.rb`, `test_a_changed_event_with_a_new_tag_fetches_the_ruleset`.
- [x] The loop and jittered backoff: harness `backoff.json`, `stream.json`; the `jitter` seam is `0.5 + rand * 0.5` by default.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; `test_a_429_on_the_stream_holds_the_ruleset_too`, `test_a_retry_after_above_300_holds_usage_for_300_and_close_reports_unsent_counts`.
- [x] Status fields, states and `lastError` codes: harness `status.json` and the shape check on every status read; `test_error_statuses_network_timeout_and_bad_bodies_set_last_error`.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`; `test_counts_are_summed_per_flag_variation_and_day_and_sent_on_close`, `test_more_than_500_entries_go_in_requests_of_at_most_500`, `test_5xx_408_and_429_keep_the_counts_and_other_4xx_drop_them`.
- [x] Hook events and `onError`, a hook that raises, an `onError` that raises: harness `hooks.json`; `test_a_hook_that_raises_goes_to_on_error_and_the_answer_stands`, `test_on_error_that_raises_is_swallowed`.
- [x] `close` in order, twice at once, during a hold, with a final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`; `test_close_is_safe_twice_and_concurrent_and_the_client_answers_after`, `test_close_during_a_usage_hold_does_not_wait_and_tells_on_error`.
- [x] No context, key or attribute in any request or `onError` message, no extra header: harness `privacy.json` and the after-scenario checks; `test_nothing_in_a_request_or_an_error_carries_the_context_or_the_key_beyond_authorization`. Source search for logging: `grep -rnE "puts|warn |logger|Logger|\$stderr|\$stdout" lib` finds nothing; the SDK writes no log line, and every error value is built from a fixed code and message with no context data (`lib/toggleslash/types.rb`, `lib/toggleslash/event.rb`).

### Native tests

- [x] Every operation from many threads, `close` racing `variation`, two `close` calls racing: `test_every_operation_from_many_threads`, `test_close_racing_variation_and_a_second_close` in `test/concurrency_test.rb` (Ruby has no race detector; decision 18 names this stress test as the stand-in, with a bound on the wait). Also `test_close_is_safe_twice_and_concurrent_and_the_client_answers_after`.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `test/tls_test.rb` (`test_a_ruleset_loads_over_tls_with_a_trusted_certificate`, `test_an_untrusted_certificate_is_refused_as_a_network_error`, `test_a_certificate_for_another_host_is_refused`).
- [x] `variation` and `variation_detail` return within a bound while a ruleset request hangs and while a usage flush hangs: `test_a_request_that_never_answers_is_a_timeout_and_variation_stays_fast`, `test_a_usage_request_that_hangs_is_bounded_and_variation_stays_fast`, `test_close_while_the_ruleset_request_hangs_returns_within_twice_the_timeout`.
- [x] A reader sees one ruleset or the other, and a hook's `etag` and `flagVersion` belong together: `test_a_ruleset_swap_is_seen_whole_by_a_reader` in `test/sync_test.rb`.
- [x] Callbacks that call back into the client do not deadlock, and `ready` from the SDK's own thread returns at once: `test_callbacks_that_call_back_into_the_client_do_not_deadlock`, `test_ready_and_close_from_the_sdks_own_thread_return_at_once`. The order that rules the unsettled case out: `ready` settles the first attempt before any callback runs on the SDK thread, and a call from the SDK's own thread returns at once without waiting.
- [x] 60 s stream watchdog through a seam on a real loopback socket: `test_a_stream_that_goes_quiet_for_the_watchdog_ends_and_reconnects`, `test_a_keep_alive_restarts_the_watchdog` (`stream_watchdog` seam), and `test_a_stream_idle_longer_than_the_bound_times_out` in `test/http_test.rb`.
- [x] A `Retry-After` above 300 held for 300 s on the ruleset, stream and events routes through a seam (the `clock` seam, `TestClock` offsets of 299 and 301): `test_a_429_retry_after_above_300_is_held_for_300_on_the_ruleset`, `test_a_429_on_the_stream_holds_the_ruleset_too`, `test_a_retry_after_above_300_holds_usage_for_300_and_close_reports_unsent_counts`.
- [x] Counts either side of a UTC midnight go out as two days: `test_counts_are_summed_per_flag_variation_and_day_and_sent_on_close` (23:59:59 and 00:00:01 on a `TestClock`).
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`: `test_an_evaluator_that_raises_gives_the_fallback_and_one_on_error`.
- [x] n/a: no futures (a hook is a plain callable).
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `test_the_integer_accessor` (the `over`, `under` and `float_over` flags; `max` 9223372036854774784 and `min` are accepted); harness `huge` scenario.
- [x] A client never closed does not keep the process from exiting: `test_a_client_never_closed_does_not_keep_the_process_alive` runs a subprocess that creates a client, calls `ready` and exits. Ruby does not wait for threads when the main thread ends.
- [x] A typed accessor whose type admits null, given a null fallback and served another kind: `test_the_integer_accessor_with_a_nil_fallback_accepts_a_number_only` (the integer accessor, a served string gives `TYPE_MISMATCH`); the generic accessor accepts any kind with a nil fallback by the mismatch rule (`test_a_null_fallback_accepts_any_kind`).
- [x] A generic accessor with an integer fallback: Ruby's `variation` is dynamically typed, so an Integer fallback has the JSON kind number and a served `2.0` comes back as served (decision 9); `int_variation` gives `2` and `TYPE_MISMATCH` for a fraction or an out-of-range value (`test_the_integer_accessor`). A fallback of a class with no JSON kind gives the fallback and `TYPE_MISMATCH`: `test_a_class_with_no_json_kind_never_matches`.
- [x] The hook event's `value` for the integer accessor is an Integer: `test_a_hook_sees_the_value_for_the_integer_accessor_as_an_integer`.
- [x] A context attribute of each numeric type matches as its value: `test_a_context_attribute_of_each_numeric_type_matches_as_its_value` (Integer, Float, Rational, BigDecimal; `Complex` does not match) and `test_a_fractional_numeric_attribute_keeps_its_value`.

### Code and documents

- [x] `DESIGN.md` has the mapping table (section 1) and `README.md` links to it.
- [x] Mapping names, reason kinds, error codes, stream states and `lastError` codes keep their spelling: `lib/toggleslash/types.rb`; the harness checks the strings.
- [x] No platform regular expression in the evaluator: `grep -rniE "\bRegexp\b|=~|\.match\??\(|\.scan\(|\.gsub|\.sub\(" lib/toggleslash/eval` finds only a comment in `lib/toggleslash/eval/pattern.rb:6` that says none is used. The pattern engine is the SDK's own VM, tested in `test/pattern_test.rb` (`test_time_is_bounded_whatever_the_pattern`).
- [x] The lint configuration names its rule set and each ignored rule carries a reason: `.rubocop.yml` (decision 2).
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none, and section 4 says why.
- [x] No telemetry and no identifying header: the request-header table in Results lists what is on the wire, each with why it does not identify a user or a machine; the source sets only `Authorization`, `Accept`, `If-None-Match` and `Content-Type` (`lib/toggleslash/client.rb:29`, `:391`, `:393`, `:471`, `lib/toggleslash/usage.rb:133`); `test_only_the_allowed_headers_are_sent`.
- [x] Every extra beyond the contract is one contract 13 allows: `int_variation` and `int_variation_detail` (typed accessor), `Client#status` fields, the private seams. None weakens contract 5 or sends anything new.
- [x] Test seams are not global and not an environment variable: private `Client.new` keywords (decision 13); `Toggleslash.create` exposes the public options only.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and run with only the `mise.toml` toolchain: `ls -l bin` shows mode 755 on each; each starts `cd "$(dirname "$0")/.."`.
- [x] `test-service/README.md` maps every accessor: `bool`, `string`, `number`, `json`, `any` to `variation_detail`, `int` to `int_variation_detail`.
- [x] `.github/workflows/sdk-ruby.yml` follows the skeleton, filters on its paths and pins every action by commit SHA: `bun scripts/check-workflows.ts .github/workflows/*.yml` passes.
- [x] A `status` read racing a stream failure never shows that failure's `lastError` beside `stream: connected`: `test_status_never_shows_a_stream_failure_beside_connected` (Ruby has no race detector; a reader thread reads `status` every 0.2 ms for 1.5 s while the stream connects, fails and reconnects, and counts any read with `stream: connected` beside a `lastError`).

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes.
- [x] Only `sdks/ruby/` and the one workflow file changed: the phase 6 scope check prints `scope ok`.
- [x] No package published and no name reserved: the gemspec is built by no step and the workflow publishes nothing.
- [x] Commit subject is a Conventional Commit (`chore(sdk-ruby): the Ruby server SDK regenerated cold from the authoring prompt`), not `feat`, with no `Co-Authored-By` or AI attribution.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:` guard, triggers `pull_request` and `push` only, installs its own toolchain (mise action) and `bun scripts/check-workflows.ts .github/workflows/*.yml` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of the list: this file, checked by the phase 6 section loop.
