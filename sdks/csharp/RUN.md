# C# SDK: run report

## Prompt version

- Prompt: 26598eee3b61a0b44edc1c6d50c5d4e4ad08b813 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start, and again after the last `git fetch origin`: `origin/main` had not moved, so no rebase and no change)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-12
- Input: language C#; runtime chosen: .NET 10 (`net10.0`, the newest LTS); minimum version chosen: `net10.0` (.NET 8 and 9 both left support on 2026-11-10, the day before this run); package manager chosen: the `dotnet` CLI and NuGet (nothing is published, so no registry is named)
- Toolchain: `mise.toml` pins `dotnet = "10.0.404"` (`mise exec -- dotnet --version` prints `10.0.404`); `global.json` pins the same number with `rollForward: disable`; mise 2026.11.0. The conformance harness runs on Bun 1.4.2 (the repository's pin, `BUN_VERSION` in the workflow). Runtime dependencies of the library: none. Test project: `Microsoft.NET.Test.Sdk` 18.11.0, `xunit` 2.9.3, `xunit.runner.visualstudio` 3.1.5, `Microsoft.Extensions.TimeProvider.Testing` 10.0.0. The test service is a `Microsoft.NET.Sdk.Web` project (Kestrel from the shared framework, no package). Analyzers: the SDK's own, `AnalysisMode` Recommended, with `dotnet format` run by `bin/lint`.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (exit 0; the only skips are the 2 named vectors, and no scenario is skipped because `integer-accessor` is advertised)
- Capabilities advertised: `integer-accessor`
- Native tests: 73 passed of 73 (xunit counts a theory case as a test: 45 test methods, 73 cases, of which `HoldTests` has 17 cases from 5 methods and `NumericTests` 19 from 3). `VectorTests.EveryVectorAndPinPasses` is one test over the 1371 vectors and 17 pins counted above. The other two projects (`ToggleSlash.Sdk.NeverClosed`, which a test runs as a process, and the test service) have no tests of their own.
- `bin/test` ten runs in a row on the committed tree: runs 1 to 10 each exit 0 with 73 passed of 73 and `vectors: 1371 pins: 17` (10 to 16 s each). Eleventh run with the CPU loaded (12 busy shell loops for `nproc` = 12, started before `bin/test` and killed after it ended): exit 0, 73 passed of 73, 13.1 s.
- Earlier flakes, fixed before those runs (see Decisions 24 and 25): without the fix, 3 of 6 full runs failed one or two of two timing tests.

## Decisions

1. Version lookup: `mise ls-remote dotnet` and Microsoft's `releases-index.json` give 10.0.404 as the newest 10.0 SDK, and 10 as the newest LTS; the support-policy dates put .NET 8 and 9 out of support on 2026-11-10, so `net10.0` is the minimum. Pinned in `mise.toml` and `global.json` (same number, `rollForward: disable`) as `docs/sdk-authoring/language-notes/csharp.md` says.
2. Test framework: xunit 2.9.3 over MSTest and NUnit: it is the framework `apps/server` already uses, and its `[Theory]` gives the vectors and numeric types their cases.
3. Lint and format: `dotnet format` (whitespace, style, analyzers, each `--verify-no-changes`) plus a warnings-as-errors build with the analyzers at `AnalysisMode` Recommended over a third-party linter, because it is the toolchain's own and needs no extra install. Each rule set to `none` has its reason in `.editorconfig` (a test, `SourceTests.EveryDisabledLintRuleHasAReasonCommentAboveIt`, holds that). `bin/lint` fails on a planted violation (checked by hand: exit 2).
4. CI toolchain: `actions/setup-dotnet` at the SHA the prompt gives, with `global-json-file: sdks/csharp/global.json`, so the workflow and `bin/*` read one number. `mise-action` was not used because the one file already names the SDK.
5. Namespace and package id: `ToggleSlash.Sdk` for both, with no host in the id (the NuGet convention); the entry class is `ToggleSlashClient`.
6. JSON: the SDK has its own JSON value type and parser (`Json/`) instead of `System.Text.Json`, because the evaluation spec needs the lone surrogate and `1e999` to survive and the standard type throws on a lone surrogate. DESIGN.md "JSON" says so and `SourceTests.NoSourceFileOfTheLibraryUsesSystemTextJsonOrRegex` holds it.
7. Redirects are not followed (`AllowAutoRedirect = false`) and cookies and automatic decompression are off, so the only headers the client adds are `Host` and `Content-Length` (language notes; the capture table below shows it).
8. Fake time: a `FakeTimeProvider` subclass that counts armed timers (`tests/ToggleSlash.Sdk.Tests/Support/CountingTimeProvider.cs`); a test advances only after `WaitForArmedAsync`, never after a fixed pause.
9. No race detector exists for .NET: `StressTests.EveryOperationFromManyThreadsWithCloseRacingNeverThrowsOrHangs` stands in (16 threads on every operation, `Close`, `CloseAsync` and `Dispose` racing, stream on, 5 ms intervals, three rounds, a 60 s bound on every wait).
10. A native context value the SDK cannot send as a JSON value (an object of another type, a cycle, nesting past 512) gives the fallback with `INVALID_CONTEXT`. Supported: a dictionary with string keys, an enumerable, strings, booleans, `null`, every .NET integer and floating-point width, `decimal`, `JsonValue`.
11. The generic `Variation<T>` maps the listed types only (`bool`, `string`, the ten integer and float types, `JsonValue`, and the `Nullable<>` of a listed value type); any other `T` gives the fallback with `TYPE_MISMATCH` (contract 4, "Typed languages").
12. Both blocking and asynchronous forms exist: `Ready()` and `ReadyAsync()`, `Close()` and `CloseAsync()`, `Dispose` and `DisposeAsync`, and a hook may be an `Action` or a `Func<_, Task>` (contract 13 allows idiomatic extras, and none of these weakens contract 5).
13. Callbacks are marked with an `AsyncLocal` flag on the SDK's own threads, so a `Close()` inside `OnError` starts the shutdown and returns, and a `Ready()` there answers from the state held, as `language-notes/csharp.md` allows. The README and DESIGN.md say all `TaskCompletionSource` creation must use `RunContinuationsAsynchronously`; the SDK, the tests and the test service do (`ToggleSlashClient.cs` lines 37 and 409; the test service creates none).
14. The 300 s `Retry-After` cap has no seam: a test reads the client's own hold time (`SyncHoldUntil`, `UsageHoldUntil`, internal) against a fake clock, so the real constant is tested (`HoldTests`).
15. `requestTimeout` and every interval are clamped to what a .NET timer can take (about 49.7 days) and long waits are cut into chunks, so a huge option cannot throw from the timer API.
16. The ready/close variants are both `public`; `FlushAsync` is `internal` (the stress test calls it) and is not an extra on the public surface.
17. Reading the other SDKs under `sdks/` and the C# reference evaluator under `apps/server/ToggleSlash.Evaluation` is committed code in the working tree, not something to guess, so the evaluator is a port of it onto the SDK's own JSON type (DESIGN.md "Evaluator (phase 2)"); the vectors and `SourceTests` hold the result.
18. A tool or config the prompt does not name (the `.slnx` format, `Directory.Build.rsp` that stops the compiler server and MSBuild node reuse, `UseArtifactsOutput`) is a decision here, not a guess: `UseArtifactsOutput` keeps `bin/` to the three scripts, as the language notes say.
19. The test service reads the reference `packages/sdk/test-service/main.ts` and its README for the shape of the protocol mapping. It hands the SDK the context and fallback as the `JsonValue` its parser made, so a lone surrogate reaches the evaluator.
20. `ToggleSlash.Sdk.NeverClosed` (a console program) is a project of the solution: a test runs it as a process and checks it exits with the client open. The tests project references it with `ReferenceOutputAssembly="false"` so it is built first.
21. Tests use a hand-written loopback HTTP/1.1 server (`Support/LoopbackServer.cs`) over a raw `TcpListener` instead of Kestrel, because they need to hold, reset and half-write connections, and to see header order.
22. The stream watchdog seam (`StreamWatchdog`) is an internal option; the 60 s default is the contract's, and the tests set 1.5 s over a real loopback socket (`StreamTests`).
23. Hold and cap tests read the internal hold times, not the wire, because the hold is a time and the contract's bound is on that time.
24. Found by repeated full runs, not by a single one: `LifecycleTests.EvaluationAnswersWhileARulesetRequestHangsAndWhileAUsageFlushHangs` took 8 s for a close bounded by about 4 s, and `StreamTests.AQuietStreamIsDroppedAsATimeoutAfterTheWatchdog` saw a drop 4 ms after the server wrote. Both were the suite loading itself: the 16 spinning stress threads starved the thread pool, which delayed other tests' real timers (a 2 s request timeout seen at 4 s). Fix: the stress test moved to its own class in a collection with `DisableParallelization = true`, so nothing runs beside it (`StressTests.cs`).
25. Same stream tests: the watchdog is now 1.5 s (was 0.8 s), the keep-alive server stays up for 3.2 s so the stream outlives the watchdog twice, and the server restarts its stopwatch before it writes, so the measured quiet time is an upper bound on what the client saw. Recorded as a test bug (the tests assumed a speed they did not control), not a guess.
26. Header capture: `dotnet` does not add `User-Agent`, `Accept-Encoding` or `Connection` when decompression is off, so only `Host` and `Content-Length` are in the table below.

### Header capture table

Captured by `ProtocolTests.EveryRouteSendsOnlyTheAllowedHeadersAndNoContext` on a loopback listener (the test prints a `HEADERS` line per route, and asserts the set). `Host` is `127.0.0.1:<port>` taken from the URL the caller gave; `Content-Length` is the length of the events body. Neither identifies a user or a machine.

| Route | Headers on the wire, in order | Added by the HTTP client |
| --- | --- | --- |
| `GET /sdk/v1/ruleset` | `Host`, `Authorization` | `Host` (from the URL) |
| `GET /sdk/v1/ruleset` (conditional) | `Host`, `Authorization`, `If-None-Match` | `Host` (from the URL) |
| `GET /sdk/v1/stream` | `Host`, `Authorization`, `Accept` | `Host` (from the URL) |
| `POST /sdk/v1/events` | `Host`, `Authorization`, `Content-Type`, `Content-Length` | `Host` (from the URL), `Content-Length` (the body's length) |

## Guesses

None. Every point where the prompt left a choice was one the prompt says to decide (tools, versions, naming: Decisions above) or one a document or the working tree settles (Decisions 1, 6, 7, 13, 17). No question was asked of the planner.

## Stuck points

None.

## Proposed prompt changes

1. For decisions 24 and 25, in `language-notes/csharp.md`, after "xunit runs test classes in parallel, so the suite loads itself.", add: "Put the stress test (many busy threads) in its own class with `[CollectionDefinition(DisableParallelization = true)]`: it starves the thread pool for every test beside it, and a real 2 s request timeout was seen at 4 s. A real-time bound in another test then holds when it is run alone and fails in the full suite, and only repeated full runs show it."
2. For decision 24, in `CHECKLIST.md`, "The two executable checks", after "a timer test that races the SDK's loops fails only under load.", add: "A test whose bound is real time and whose body is not the load also fails when another test class is the load; run the suite several times before the ten that count."
3. For decision 4, in `PROMPT.md` phase 5, after "(the toolchain step)", add: "`global-json-file` on `setup-dotnet` is a valid way to read a tree-local SDK pin."

## Checklist

Evidence is the test name (all in `tests/ToggleSlash.Sdk.Tests` unless said), or a command and what it printed.

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin: `VectorTests.EveryVectorAndPinPasses`; prints `vectors: 1371 pins: 17`, equal to the phase 2 count command (`1371 17`).
- [x] `bin/test` ten times in a row plus one CPU-loaded run: Results above (10 runs, each 73 of 73; the loaded run 73 of 73 with 12 busy loops, 13.1 s).
- [x] Harness exit 0, only the 2 named vector skips: `bun run conformance -- --service sdks/csharp/bin/test-service` printed `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, exit 0.

### What the harness checks

- [x] Both context shapes (contract 3): the harness, `privacy.json`, `kinds.json` vectors; no typed builder rejects a non-finite number.
- [x] Result order and mismatch rule (contract 4): the harness, `evaluation.json`, `usage.json`; `ClientTests.EveryAccessorServesItsKind`.
- [x] `integer-accessor` (contract 4): the harness, `evaluation.json`; `ClientTests.IntegerAccessorAcceptsAWholeFloatAndRefusesAFractionOrARangeOverflow`.
- [x] No operation raises: the harness (every scenario; the service answers 500 on anything raised); `StressTests`.
- [x] `NOT_READY` at once before the first load: the harness `load.json`; `ClientTests.ServesTheFallbackWithNotReadyUntilARulesetIsHeld`.
- [x] `ready` settles after the first attempt, false after `close` if unsettled: the harness `load.json`, `close.json`; `ClientTests.ReadyIsTrueAfterTheFirstLoadAndFalseWhenItFails`.
- [x] A non-ruleset answer never replaces the one held: the harness `status.json`; `ProtocolTests.ABadRulesetBodyIsBadRuleset`.
- [x] The ruleset request, `If-None-Match`, ETag kept exactly, URL joining: the harness `load.json`, `status.json`; `ProtocolTests.TheConditionalRequestIsSentOnlyWhenARulesetAndTagAreHeld`, `A304WithNoRulesetHeldIsHttp304`.
- [x] The stream: the harness `stream.json`, `load.json`, `status.json`.
- [x] The loop and the jittered backoff: the harness `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: the harness `rate-limit.json`; `HoldTests` (cap and parse).
- [x] Status fields, states, `lastError` codes: the harness `status.json`; `ProtocolTests.AnUnreachableServerIsNetworkAndAHangIsTimeout`.
- [x] Usage counts: the harness `usage.json`, `rate-limit.json`; `ProtocolTests.UsageIsKeptOnA5xxAndDroppedOnAnother4xx`.
- [x] Hook events and `onError`: the harness `hooks.json`; `ConcurrencyTests.AThrowingHookAndAThrowingOnErrorChangeNothing`.
- [x] `close` in order, twice at once, during a hold, final flush: the harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`; `HoldTests` (the final flush reports "closed with 2 usage counts unsent: the server asked to wait").
- [x] No context, key or attribute in any request or `onError` message; no header beyond the protocol's: the harness `privacy.json`; source search named: `SourceTests.TheLibraryLogsNothingAndPutsNoContextInAnErrorOrAMessage` (no logging API, and no exception constructor names a context, flag key or SDK key).

### Native tests

- [x] Every operation from many threads, `close` racing `variation`, two `close` calls racing: `StressTests.EveryOperationFromManyThreadsWithCloseRacingNeverThrowsOrHangs` (covers both the many-thread race and three racing closes; there is no race detector for .NET, so this stands in, with a 60 s bound on every wait).
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `LifecycleTests.ATlsServerWithALocalCertificateIsReachableAndTheDefaultRefusesIt` (a trusting handler works; the default refuses with `network`).
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and a flush hangs: `LifecycleTests.EvaluationAnswersWhileARulesetRequestHangsAndWhileAUsageFlushHangs` (2000 evaluations plus status reads under 5 s each time; close under 8 s with a 2 s request timeout).
- [x] A reader sees one ruleset or the other across a swap: `ConcurrencyTests.AHooksEtagAndFlagVersionBelongToTheSameRulesetUnderConcurrentSwaps`.
- [x] Hooks and `onError` that call back into the client do not deadlock; `ready` from a callback returns at once: `ConcurrencyTests.CallbacksThatCallBackIntoTheClientDoNotDeadlock` (`OnError` calling `Status`, `Variation`, `Ready`, `Close`) and `AHookThatCallsBackIntoTheClientDoesNotDeadlock`. The first load runs `OnError` (if it fails) before it settles `ready`, so the unsettled case can arise and is handled: `Ready()` on the SDK's own thread answers from the state held without waiting (Decision 13).
- [x] A silent stream dropped as `timeout` after the watchdog, and a keep-alive restarts it, through a seam on a real socket: `StreamTests.AQuietStreamIsDroppedAsATimeoutAfterTheWatchdog`, `AKeepAliveRestartsTheWatchdog`.
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes: `HoldTests.TheRulesetRouteCapsTheHoldAtThreeHundredSeconds`, `TheStreamRouteCapsTheHoldAtThreeHundredSeconds`, `TheEventsRouteCapsItsOwnHoldAndLeavesTheRulesetHoldAlone`.
- [x] Counts either side of UTC midnight go out as two days: `ProtocolTests.UsageIsSplitAtUtcMidnightAndSentInChunksOf500`.
- [x] An evaluation that raises: fallback, `ERROR EVALUATION_FAILED`, one `onError`: `ClientTests.ADeadEvaluatorGivesTheFallbackWithOneOnErrorAndNoCount`.
- [x] A hook that returns a task and fails later goes to `onError` and changes neither the answer nor the count: `ConcurrencyTests.ATaskHookThatFailsLaterGoesToOnErrorAndChangesNeitherTheAnswerNorTheCount`.
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `ClientTests.IntegerAccessorAcceptsAWholeFloatAndRefusesAFractionOrARangeOverflow`; the `huge` scenario.
- [x] A client never closed does not keep the process from exiting: `LifecycleTests.AClientThatIsNeverClosedDoesNotKeepTheProcessFromExiting` (runs `ToggleSlash.Sdk.NeverClosed` as a process; the SDK's loops are background tasks on the thread pool, started in `ToggleSlashClient.Create`).
- [x] A nullable-string accessor with a null fallback and another kind served gives `TYPE_MISMATCH`: `ClientTests.NullableStringAccessorWithANullFallbackRefusesAnotherKind`.
- [x] The generic accessor with an integer fallback type: a served `2.0` is `2`, a fraction or out-of-range value `TYPE_MISMATCH`; a type with no JSON kind gives the fallback and `TYPE_MISMATCH`: `ClientTests.GenericAccessorReadsIntegerTypesAndRefusesATypeWithNoJsonKind`.
- [x] The hook event's `value` for the integer accessor has the mapping table's type (`long`): `ClientTests.HookValueForTheIntegerAccessorIsALong`.
- [x] A context attribute of each numeric type matches as the JSON number of its value: `NumericTests.ANumericAttributeOfAnyCSharpNumericTypeMatchesAsTheJsonNumber` (17 types), `ANumericAttributeThatIsNotFiveDoesNotMatch`; the nesting limit of 512 is `ADeeplyNestedContextIsRefusedAsInvalidAtFiveHundredAndTwelveLevels`.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` links the mapping table: `DESIGN.md` "Mapping table (contract section 2)"; `README.md` "The mapping".
- [x] Names keep canonical meaning and defaults; closed sets keep their spelling: DESIGN.md "The closed sets keep their spelling"; `ReasonKind`, `ErrorCode` and `StreamState` members are spelled as the contract spells them (`Reason.cs`, `Types.cs`).
- [x] The evaluator has no platform regular expression: `SourceTests.TheEvaluatorUsesNoPlatformRegexAndNoSystemTextJson` (searches `Evaluation/` for `System.Text.RegularExpressions`, `Regex` and `System.Text.Json`, ignoring comment lines).
- [x] The lint configuration names its rule set and each ignored rule has a reason: `.editorconfig` header names it (AnalysisMode Recommended plus the style rules); `SourceTests.EveryDisabledLintRuleHasAReasonCommentAboveIt`.
- [x] Every runtime dependency has a line in DESIGN.md: there are none; DESIGN.md "Dependencies" says so.
- [x] No telemetry and no identifying header: `SourceTests.TheLibraryAddsOnlyTheFourAllowedRequestHeaders` (only `Accept`, `Authorization`, `ContentType`, `If-None-Match` appear in the source), and the capture table above (`Host` and `Content-Length` only, added by the client).
- [x] Every extra is one contract 13 allows: Decisions 12 and 16 (typed and generic accessors, blocking and async forms, a hook that returns a task, internal seams); none weakens contract 5 or sends anything new.
- [x] Test seams are `internal` options on `create`, or the standard fake-time facility, never a global or an environment variable: `ClientOptions.cs` (`TimeProvider`, `Jitter`, `Handler`, `StreamWatchdog`, `Evaluator`, internal); DESIGN.md "Test seams".
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and run with only the toolchain `mise.toml` pins: `ls -l sdks/csharp/bin`, each begins `set -eu` and `cd "$(dirname "$0")/.."` and uses `mise exec --` when mise is on PATH.
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method: `bool`, `string`, `number`, `int`, `json`, `any` rows in `test-service/README.md`.
- [x] The workflow follows the skeleton, filters on its paths, pins every action by SHA: `.github/workflows/sdk-csharp.yml`; `bun scripts/check-workflows.ts .github/workflows/*.yml` printed `8 workflow(s) read, 0 failure(s)`.
- [x] A `status` read racing a stream failure never shows its `lastError` beside `connected`: `ConcurrencyTests.StatusNeverShowsALastErrorBesideAConnectedStream`.

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exits 0; `SourceTests.NoSourceFileHoldsAnEmOrEnDash` for `.cs` and `.md` under `sdks/csharp`.
- [x] `bun run format:check` passes: `All matched files use the correct format.`
- [x] Only `sdks/csharp/` and the one workflow file changed: the phase 6 scope check prints `scope ok` (run before the commit).
- [x] No package published and no name reserved: nothing pushed to any registry; `IsPackable` is false in `Directory.Build.props`.
- [x] Commit subjects are Conventional Commits, none `feat`, no `Co-Authored-By` or AI attribution: one commit, `chore(sdk-csharp): the C# server SDK regenerated cold from the authoring prompt`.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:`, triggers only `pull_request` and `push`, installs its own toolchain: `.github/workflows/sdk-csharp.yml`; `check-workflows.ts` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line: this file.
