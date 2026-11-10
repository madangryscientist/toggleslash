# C# SDK: run report

## Prompt version

- Prompt: e3b07b5311284e08d3f4ecec6f10903250730c65
- Model: claude-sonnet-5-5 (Sonnet 5.5), run as a spun-off builder thread
- Date: 2026-11-10
- Input: language C#; runtime chosen: .NET (CoreCLR); minimum version chosen: net10.0; package manager chosen:
  NuGet through the `dotnet` CLI
- Toolchain: .NET SDK 10.0.403, pinned in `mise.toml` (`dotnet = "10.0.403"`) and in `global.json`
  (`rollForward: disable`); `mise exec -- dotnet --version` prints `10.0.403`; mise 2026.11.0 linux-x64.
  Test packages: xunit 2.9.3, xunit.runner.visualstudio 3.1.5, Microsoft.NET.Test.Sdk 18.11.0,
  Microsoft.Extensions.TimeProvider.Testing (tests only). Runtime dependencies of the SDK: none.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test`, which prints both from
  `artifacts/vector-counts.txt`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
  (`bun run conformance -- --service sdks/csharp/bin/test-service`, exit 0)
- Capabilities advertised: `integer-accessor`
- Native tests: 34 passed of 34 (`dotnet test`, the vector test counted as one)

## Decisions

1. Runtime: .NET (CoreCLR) over Mono and .NET Framework, because it is the mainstream runtime and the
   only one with `TimeProvider`, `Lock` and `SocketsHttpHandler` as the SDK uses them.
2. Minimum version net10.0 over net8.0 and net9.0: both reach end of support on 2026-11-10, the day of the run,
   so only .NET 10 (LTS to 2028-11-14) is certainly in support.
3. Toolchain manager: mise, because the prompt names `mise.toml`. SDK 10.0.403 pinned (the newest 10.0 SDK),
   with a `global.json` of the same number and `rollForward: disable` so the repository root's `10.0.402` is
   never picked in this directory.
4. Package manager and build tool: NuGet with `dotnet` and one `.slnx` solution, over Paket or a Makefile;
   central package management is not used because three projects do not need it.
5. Lint and format: `dotnet format --verify-no-changes` plus the SDK's Roslyn analyzers
   (`AnalysisLevel` latest, `AnalysisMode` recommended) as build errors, over StyleCop.Analyzers and CSharpier:
   nothing extra to install, only what `mise.toml` pins.
6. Test framework: xunit 2.9.3 over xunit v3, NUnit and MSTest, because it is what the repository's own C# tests
   use and nothing here needs v3's runner model.
7. Fake time: `Microsoft.Extensions.TimeProvider.Testing` (`FakeTimeProvider`), the standard fake-time facility,
   plus internal seams on `ClientOptions` (`Handler`, `Time`, `Jitter`, `StreamIdleTimeout`, `Evaluate`).
8. Dependencies: none at runtime. HTTP, JSON and SHA-256 come from the base class library. The regular
   expression matcher is a Pike VM written for the spec, not `System.Text.RegularExpressions`.
9. Test service: `Microsoft.NET.Sdk.Web` with Kestrel from the shared framework (no package), in
   `test-service/`, over a hand-written `HttpListener` or `TcpListener` server.
10. CI action for the toolchain: `jdx/mise-action` over `actions/setup-dotnet`, so CI and a developer use the
    same pin from `mise.toml`. Actions are pinned by commit SHA (checkout v7.0.1, setup-bun v2.2.0,
    mise-action v5.2.7), with Bun 1.4.2.
11. Integer accessor: `IntVariation` returns `long`. A served fraction or a number outside signed 64 bits is
    `TYPE_MISMATCH`, and `2.0` counts as `2`; `integer-accessor` is advertised.
12. Build output: `UseArtifactsOutput` puts everything under `sdks/csharp/artifacts/` (ignored), so `bin/` holds
    only the three entry-point scripts.
13. Race detector: .NET has none. `ConcurrencyTests.Every_operation_from_many_threads_with_close_racing_does_not_raise_or_hang`
    stands in (see the checklist).
14. Hooks and `onError` are plain `Action` delegates, not functions returning a task, so the "future" line of the
    checklist is `n/a`.
15. Timer tests step the fake clock only when the SDK is waiting on it: `Clock`, a `FakeTimeProvider` that counts
    its armed timers (`tests/ToggleSlash.Sdk.Tests/Support.cs`), and `Wait.Advance`, which waits for the poll or
    hold loop and the flush loop to be armed before and after every step. Over a fixed real pause between steps,
    which was the first version and flaked (guess 11).
16. Skeptical review: not run. The `ob-skeptical-review` skill belongs to the omnibus spinoff procedure; this
    brief is the toggleslash prompt-test and names no review step, so the PR carries no `## Skeptical review`
    section.

## Guesses

1. The prompt's phase order puts the phase 2 check (vectors) before the client, but nothing says the client
   code may not be written first. Where: PROMPT.md, phases 2 and 3. Assumed: the evaluator and the vector test
   came first and passed (phase 2) before the sync loop was written; the client's types were drafted earlier,
   in phase 1, as the mapping table.
2. No `language-notes/csharp.md` exists, and the prompt does not say what to do for a language without one.
   Where: PROMPT.md, the "language notes" step. Assumed: there are no notes to read; every language choice is a
   decision above.
3. The prompt says to look up current versions but not how. Where: PROMPT.md, phase 1 ("pin the toolchain").
   Assumed: ask the toolchain manager (mise) for the newest SDK and read Microsoft's support-policy page for end-of-support dates.
4. The prompt does not say where the test service goes in a language whose build is a solution of several
   projects. Where: PROMPT.md, "Entry points"; sdk-test-service.md. Assumed: a project of its own in the
   solution at `test-service/`, built by `bin/test` last and by `bin/test-service` only when its binary is
   missing.
5. Whether an end-of-support date is inclusive was unclear (.NET 8 and 9 end on 2026-11-10, the run date).
   Where: PROMPT.md, phase 1 ("minimum version... in support"). Assumed: supported until the end of that day is
   not "clearly in support", so the minimum is net10.0.
6. The checklist asks for a race detector "where the language has one" but does not say how to judge a stress
   test as standing in for it. Where: CHECKLIST.md, native tests, first line. Assumed: a test that runs every
   operation on many threads with `Close` racing, repeated, and fails on any raise or on a hang past a bound.
7. Where `Close` is called by code that runs inline from the SDK's own `onError`, the contract does not say
   whether `close` must return. Where: sdk-contract.md, 6.4 and 11. Assumed: `Close()` called on the SDK's own
   thread does not wait for itself (an `AsyncLocal` marker), and the caller that completes a
   `TaskCompletionSource` inline is documented in the README. See stuck points 1.
8. What `lastError` and the stream state read during a hold after a failed first load is not spelled out.
   Where: sdk-contract.md, 7.3 and 8. Assumed: the state is `disconnected` and `lastError` keeps the last code
   until a success replaces it.
9. Headers a .NET `HttpClient` adds itself were not listed in the prompt's header rule before the checklist
   ("allowed when `RUN.md` lists each"). Assumed: list them from a wire capture (see the checklist).
10. The shape of the stream-drop the harness does (clean close or an abrupt reset) is not stated. Where:
    sdk-test-service.md, "Running it". Assumed: either ends the read, and the SDK treats both as a stream that
    ended and reconnects with backoff.
11. Flaky timer tests, found by the planner's gate (one failure in four runs of `bin/test` on the first head;
    `TimerTests.Counts_either_side_of_a_utc_midnight_go_out_as_two_days`). Where: PROMPT.md phase 3 and
    CHECKLIST.md, the timer lines say "tested through a seam, not by waiting" but not how to step a fake clock
    safely. Cause: my `Wait.Advance` moved the `FakeTimeProvider` in steps with a fixed real pause (8 ms) between
    them, assuming the SDK's poll and flush loops had re-armed their delay by then. On a loaded machine a loop
    that is still between waking and re-arming misses the step, arms its delay from the later clock reading and
    is then due after the test has stopped advancing (the test timed out waiting for the usage report), or
    comes out late enough to break an upper bound on a gap. xunit runs the test classes in parallel, so a full
    `bin/test` is loaded by its own other tests. I reproduced it only under CPU load: the single test alone passed 30 of 30
    with 8 busy loops running, and with 12 busy loops running 15 of 15 full runs failed, in one of several
    timer tests each time (the usage `Retry-After` test failed in the runs I looked at). Assumed: the fault is
    in the tests, not the SDK (its loops were correct; the tests raced them). Fix: `Clock` counts armed timers and
    `Wait.Advance` waits for them (decision 15). Proof: 12 full test runs under 12 busy loops with no failure, and
    `bin/test` 10 passes of 10 in a row on the fixed head.

## Stuck points

None that needed outside help. One finding about the contract's reach, not a blocker:

1. A synchronous `Close()` called from code that runs inline from the SDK's `onError` deadlocks if that code
   is a continuation that was not told to run asynchronously. It showed up in my own watchdog test: a
   `TaskCompletionSource` completed inside `onError`, whose awaiting test body then called `Close()`, ran on
   the SDK's thread, and `Close()` waited for that very thread. Contract 6.4 says a hook and `onError` may
   call back into the client "without deadlock" as a SHOULD and names `status` and `variation`, not `close`.
   The SDK handles a direct `Close()` call in a callback; it cannot handle one made from a continuation. The
   README says to create such a source with `TaskCreationOptions.RunContinuationsAsynchronously`. The
   contract could say whether `close` from a callback must return.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md` phase 2, after the phase 2 check, add: "Do not write the client before this
   check has passed." (or say that order does not matter).
2. For guess 2, in `PROMPT.md`, in the step that reads `language-notes/<lang>.md`, add: "If there is none,
   say so in `RUN.md` and carry on: every language choice is then a decision."
3. For guess 3, in `PROMPT.md` phase 1, after "pin the toolchain", add: "Look versions up with the toolchain
   manager (`mise ls-remote <tool>`) and the vendor's release page for support dates; record what you used."
4. For guess 4, in `PROMPT.md`, "Entry points", add: "Where the build is several projects, the test service
   is one more project. `bin/test` builds it last; `bin/test-service` builds it only when missing."
5. For guess 5, in `PROMPT.md` phase 1, after "in security support", add: "A release whose support ends
   on the run date or earlier is not in support."
6. For guess 6, in `CHECKLIST.md`, first native-tests line, after "names the stress test that stands in", add:
   "(many threads, every operation, `close` racing, a bound on the wait)".
7. For guess 7 and stuck point 1, in `sdk-contract.md` 6.4, after "do not deadlock", add: "`close` called from a
   hook or `onError` returns without waiting for itself (a language where a continuation can run inline on
   the SDK's thread says how a caller avoids it)." Aimed at the contract, to the planner.
8. For guess 8, in `sdk-contract.md` 8, add a row for the state and `lastError` while a backoff is running.
9. For guess 9, in `CHECKLIST.md`, the header line, add: "Capture one request on the wire (a loopback
   listener that prints the head) and list what the client added."
10. For guess 10, in `sdk-test-service.md`, "Running it", say how the harness drops a stream.
11. For guess 11, in `PROMPT.md` phase 3, after "through a seam", add: "Step a fake clock only when the SDK is
    waiting on it (count the clock's armed timers), never after a fixed real pause: a pause passes on a quiet
    machine and fails on a loaded one." And in `CHECKLIST.md`, under the two executable checks, add: "`$L/bin/test`
    passes 10 times in a row, and once more with the CPU loaded (for example one busy loop per core), because a
    timer test that races the SDK's loops fails only under load."

## Checklist

The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin; count equals the phase 2 count: the
  phase 2 count command printed 1371 vectors and 17 pins; `VectorTests.Every_vector_and_every_pin_matches`
  reads `fixtures/evaluation/*.json` at test time and `bin/test` printed `1371` and `17`.
- [x] `bin/test` is stable: 10 passes of 10 in a row on the final head, and 12 full runs with no failure under 12
  busy loops (see guess 11).
- [x] The harness exits 0 with only the named skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors:
  1369 passed, 0 failed, 2 skipped`. `integer-accessor` is advertised, so no scenario is skipped.

What the harness checks

- [x] Both context shapes: harness `privacy.json` and the `kinds.json` vectors; the service builds the context
  with `Context.FromElement`.
- [x] Order of contract 4 and the mismatch rule: harness `evaluation.json`, `usage.json`.
- [x] Integer accessor, fraction is `TYPE_MISMATCH`, `2.0` is `2`: harness `evaluation.json`, and
  `AccessorTests.A_whole_number_in_signed_64_bits_is_an_integer`,
  `A_fraction_a_number_outside_64_bits_or_another_kind_is_a_type_mismatch`.
- [x] No operation raises: harness, every scenario (the service answers 500 with the error text on any raise).
- [x] `NOT_READY` at once before the first load: harness `load.json`.
- [x] `ready` settles after the first attempt, bounded, callable again, false after `close`: harness
  `load.json`, `close.json`.
- [x] An answer that is not a ruleset never replaces the one held: harness `status.json`, and
  `AccessorTests.A_body_that_is_not_a_ruleset_never_replaces_the_one_held`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining: harness `load.json`,
  `status.json`.
- [x] The stream, SSE parsing, refresh on change, a failed refresh ends the stream: harness `stream.json`,
  `load.json`, `status.json`.
- [x] The loop and jittered backoff: harness `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`.
- [x] Status fields, states and `lastError` codes: harness `status.json` and its shape check.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`.
- [x] Hook events and `onError`, a raising hook and a raising `onError`: harness `hooks.json` (the service has
  `onErrorThrows` and `hookThrows` switches).
- [x] `close` in order, twice at once, during a hold, final flush: harness `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`.
- [x] No context, context key or attribute in any request or `onError` message, and no extra header: harness
  `privacy.json` and the checks after every scenario. Source search: `grep -rn "ILogger\|Console\.\|Debug\.\|Trace\." src`
  finds nothing, so no log line exists; error values are built from the HTTP status, the exception type
  and the route (`Sync.cs`, `Usage.cs`, `Http.cs`) and never from a `Context`.

Native tests

- [x] Many threads, `close` racing, two `close` racing, no error or deadlock: `ConcurrencyTests.
  Every_operation_from_many_threads_with_close_racing_does_not_raise_or_hang`. .NET has no race detector;
  this stress test stands in for it (decision 13).
- [x] `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs:
  `ConcurrencyTests.Evaluation_returns_within_a_bound_while_the_first_ruleset_request_hangs`,
  `Evaluation_returns_within_a_bound_while_a_usage_flush_hangs`.
- [x] A reader sees one ruleset across a swap (the `etag` and `flagVersion` of a hook belong together):
  `ConcurrencyTests.A_hook_never_sees_an_etag_and_a_version_from_two_rulesets`. The state is one immutable
  `SyncState` swapped under a lock.
- [x] A hook and an `onError` that call back into the client do not deadlock:
  `ConcurrencyTests.A_hook_and_an_onError_can_call_back_into_the_client`, and
  `Close_called_from_inside_onError_does_not_wait_for_itself`.
- [x] A stream with no bytes for the idle limit is dropped as `timeout`; a keep-alive restarts it; on a real
  loopback socket, the length through the `StreamIdleTimeout` seam: `WatchdogTests.
  A_stream_that_goes_quiet_is_dropped_as_a_timeout_after_the_idle_limit`, `A_keep_alive_restarts_the_idle_limit`,
  and `A_stream_that_never_answers_its_request_is_dropped_by_the_request_timeout`.
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes (through
  `Clock`, a `FakeTimeProvider` that counts armed timers): `TimerTests.A_retry_after_above_300_holds_the_ruleset_route_for_300_seconds`,
  `A_retry_after_above_300_holds_the_stream_and_the_ruleset_for_300_seconds`,
  `A_retry_after_above_300_holds_usage_reports_for_300_seconds_and_keeps_the_counts`, and
  `A_retry_after_under_the_cap_is_held_as_given_and_a_date_is_ignored`.
- [x] Counts either side of a UTC midnight go out as two days: `TimerTests.
  Counts_either_side_of_a_utc_midnight_go_out_as_two_days`; batches of 500: `More_than_500_entries_go_out_in_batches_of_at_most_500`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`:
  `ConcurrencyTests.An_evaluation_that_raises_gives_the_fallback_evaluation_failed_and_one_onError` (through
  the `Evaluate` seam).
- [x] A hook that returns a future fails later: `n/a: no futures`. `Hooks.AfterEvaluation` is an `Action`.
- [x] A number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `AccessorTests.
  A_fraction_a_number_outside_64_bits_or_another_kind_is_a_type_mismatch` and the harness `huge` scenario.
- [x] A client never closed does not keep the process from exiting: `ProcessTests.
  A_client_that_is_never_closed_does_not_keep_the_process_from_exiting` runs `tests/NeverClosed` as a child
  process and asserts it exits. The loops are background tasks on pool threads.

Code and documents

- [x] `DESIGN.md` passes the phase 1 row check and `README.md` links to the mapping table
  (`DESIGN.md` section 1; README links `#1-the-mapping-table`).
- [x] Names keep the canonical meaning and defaults; reason kinds, error codes, stream states and `lastError`
  codes keep their spelling (`ReasonKind.PrerequisiteFailed`, `ErrorCode.NotReady`, `StreamState.Connected`; `Name`
  prints the contract's spelling). `Types.cs`, `DESIGN.md` section 1.
- [x] No platform regular expression in the evaluator: `grep -rn "Regex\|System.Text.RegularExpressions" src`
  finds only a comment in `Evaluation/Pattern.cs` that says the evaluator does not use it.
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none (`DESIGN.md` section 4,
  "Runtime: none").
- [x] No telemetry and no extra header: source search for request headers finds `Authorization` and `Accept`
  (`Http.cs:85-86`), `If-None-Match` (`Sync.cs:143`) and `Content-Type` (`Usage.cs:146`). A wire capture
  of one ruleset request from the SDK against a loopback listener showed `Host`, `Authorization`, `Accept`
  and nothing else. The HTTP library adds `Host` (the server's own name, which the request URL already
  carries) and, on the usage POST, `Content-Length` (the body's size); it sends no `User-Agent`, no
  `Accept-Encoding` (automatic decompression is off) and no `Connection` header. None identifies a user or a
  machine.
- [x] Every extra beyond the contract is one contract 13 allows: the extras are `IDisposable`/`IAsyncDisposable`
  on the client and `CloseAsync`, typed accessors, and the internal seams (`DESIGN.md`, "Test seams"). None sends
  anything new.
- [x] Test seams are internal options on `ClientOptions` or `FakeTimeProvider`, never a global or an
  environment variable the SDK reads (`TOGGLESLASH_COUNTS_FILE` is read by `bin/test` and the vector test,
  not by the SDK).
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and run with only
  the toolchain `mise.toml` pins (`mise exec -- ...` when mise is present).
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method (`bool`, `string`,
  `number`, `int`, `json`, `any`).
- [x] `.github/workflows/sdk-csharp.yml` follows the skeleton, filters on its paths and pins every action by
  commit SHA.

Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` passes with this file included.
- [x] `bun run format:check` passes.
- [x] Only `sdks/csharp/` and the one workflow changed: the phase 6 scope check prints `scope ok`.
- [x] No package was published and no name reserved. `ToggleSlash.Sdk` is only a project name; nothing was pushed
  to NuGet.
- [x] The commit subject is `chore(sdk-csharp): ...`, not `feat`, with no `Co-Authored-By` and no AI
  attribution.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: every heading is present (the heading check prints nothing).
