# Go SDK: run report

## Prompt version

- Prompt: 769e63a44538fb6f39ab21854f1fe5dca20bbbd7
- Model: claude-sonnet-5-5 (Sonnet 5.5)
- Date: 2026-11-12
- Input: language Go; runtime chosen: the `go` toolchain, minimum Go 1.26 (Go supports the two newest majors, the newest is 1.27, so 1.26 is the oldest in support on the run date); package manager chosen: Go modules (`go.mod`, no dependencies)
- Toolchain: `mise.toml` pins `go = "1.27.4"` (`go version` prints `go1.27.4 linux/amd64`), `golangci-lint = "2.15.0"` (`golangci-lint --version` prints `2.15.0 built with go1.27.0`), `zig = "0.17.1"` (`zig version` prints `0.17.1`, the C compiler `go test -race` needs). Bun 1.4.2 for the harness, as `ci.yml` pins it.

This is a cold run (FS-148): the previous `sdks/go` and its workflow were removed before the run started, and nothing
of them was read, in the tree or in git history.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (the two skips are the `load:` vectors the harness names; run twice in a row, both identical)
- Capabilities advertised: `integer-accessor`
- Native tests: 54 passed of 54 top-level tests across the module (the root package 40, `internal/evaluator` 2, `internal/jsonvalue` 10, `internal/sse` 2, `test-service` none), with 39 subtests beside them (`go test -race -count=1 -v ./...`, counting `=== RUN` lines)
- Stability: `bin/test` passed 10 times in a row on the final tree, and an eleventh time with 12 busy shell loops (`nproc` is 12) running from before `bin/test` started until it ended. Each run printed `vectors: 1371 pins: 17`. The first attempt at the ten failed on run 3 (Decision 9) and the ten started again from the first after the fix.

## Decisions

1. Lint and format: `golangci-lint` 2.15.0 with an explicit `default: none` rule set (errcheck, govet, ineffassign, staticcheck, unused, bodyclose, copyloopvar, errorlint, gocritic, misspell, nilerr, noctx, perfsprint, predeclared, unconvert, unparam, usetesting, wastedassign), plus `go vet` and `gofmt -s -l`, over a bare `go vet`, because the aggregator is the mainstream choice and `.golangci.yml` carries a reason beside each rule it leaves out. `exhaustive` is off (the reason kinds are a closed set but the evaluator's switches have a deliberate default), `contextcheck` is off (it flags the test service and the callbacks, which hold no context on purpose), `usestdlibvars` is off (noisy in table tests, `language-notes/go.md`, "Lint").
2. Test framework: the standard `testing` package under `go test -race`, over testify or ginkgo, because nothing the tests need is missing from it and the SDK then has no dependency at all. `go.mod` has no `require`.
3. Minimum Go 1.26, development on 1.27.4 (newest stable; `mise ls-remote go` and go.dev/dl agree). The `go` directive says 1.26 and `mise.toml` pins the newer one, so the minimum is tested only by reading the APIs used (`context.WithTimeoutCause` 1.21, `atomic.Pointer` 1.19, `math/rand/v2` 1.22, range over int 1.22, timer semantics of 1.23).
4. The integer accessor is `int64`, advertised as `integer-accessor`; there is no `int` form (contract 4, "Typed languages").
5. Both accessor shapes (contract 2, `accessor shape`): typed methods with `...Detail` twins, and generic `Variation[T]` and `VariationDetail[T]` over `Flaggable`.
6. `Reason`, `ReasonKind`, `ErrorCode` and `Value` are type aliases of the internal evaluator and JSON types, not copies, so a result reaches the caller without a conversion and the public constants are the evaluator's.
7. `EvaluationEvent.FlagVersion` is `*float64` (nil for a flag not in the ruleset), because ruleset numbers are binary64 and a version above 2^53 must not turn into a wrong integer.
8. Seams (clock, jitter, watchdog length, the evaluate function) are unexported fields of the SDK's own `config`, set in tests by option functions defined in `helpers_test.go`. No exported option reaches them, no global and no environment variable (contract 13, CHECKLIST "Test seams").
9. A test flake found and fixed during the ten runs (CHECKLIST: a failure under load is a decision, not a guess). Run 3 failed `TestBadRulesetKeepsTheLastGoodOne` with `timed out waiting for the poll`. Cause: the test waited for `clk.Armed() > 0` before stepping the fake clock, but the flush loop's timer is always armed, so the step could land before the sync loop had set its poll timer, and the poll timer was then set from the new time and never fired. The same wait was in five more places (`TestIfNoneMatchOnlyWithARulesetAndATag`, `TestRulesetAndTagSwapTogether` and others). All six now wait for `Armed() >= 2` (both loops armed), the reason is in the comment on `Armed`, and the fixed tests pass 100 times under `-race` with every CPU busy. The ten runs then restarted. The code under test was not wrong.
10. `PanicError.Error()` prints the recovered value (`toggleslash: <where> panicked: <value>`). The panic value is the application's own (a hook) or a Go runtime error from the evaluator, neither of which holds a context, and `language-notes/go.md` ("A recovered panic is a raised error") says the error carries the panic's value. The value is also on the exported `Value` field.
11. `Ready()` can settle before `onError` runs for the first load (`language-notes/go.md`, "Errors collected by `onError`"). Tests that read the collected errors wait for them. A `Ready` called from inside `onError` returns at once with the state held (`TestCallbacksMayCallBackIntoTheClient`).
12. `Close` does not wait for a worker goroutine that is inside an application callback (`task.go`), per `language-notes/go.md` ("The SDK's own goroutine"). Go has no goroutine identity. A goroutine the application's callback starts is the application's, and its `Close` waits like any other.
13. Workflow toolchain step: `jdx/mise-action` (v5.2.7, pinned by SHA) limited to the tools in `sdks/go/mise.toml` by `working_directory: sdks/go` and `install_args: go golangci-lint zig`, over `actions/setup-go`, because one file then names go, golangci-lint and zig and the workflow cannot drift from `mise.toml`. `actions/checkout` and `oven-sh/setup-bun` and `BUN_VERSION` are copied from `ci.yml`.
14. The pull request and commit carry no `Co-Authored-By` trailer and no "Generated with" line, though a harness reminder asked for both. PROMPT.md rule 6 and the repository's `AGENTS.md` forbid them, and the repo rule wins.
15. The `ob-spinoff` omnibus gates (`ob agent register`, `ob spinoff handoff`, the skeptical review legs) belong to the omnibus repository and not to this one; the phase checks of PROMPT.md are the gate here. The run hands off with the PR URL and does not merge.
16. Three doc files were written after the final ten runs (`README.md`, `DESIGN.md` edits, this `RUN.md`). None of them is read by any test or by `bin/lint`; the code and test tree was not touched after the ten runs started, and `git diff` of the `.go` files between the last run and the commit is empty.
17. The SSE parser holds a CR at the end of a read until the next byte arrives, because contract 7.2 says to "Parse Server-Sent Events as the WHATWG standard does (LF, CRLF and a lone CR end a line ... line endings may be split across reads)" and a CR can be the first half of a CRLF. `internal/sse/sse_test.go` (`lone CR`, `CR then LF in two chunks`) pins it. `DESIGN.md` "Disagreements" lists this and `ValueFrom(NaN)` (Guess 2); neither conflicts with the specification.

## Guesses

1. What `Native tests: <n> passed of <n>` counts. Where: `RUN-TEMPLATE.md`, "Results". Closest sentence: "(top-level tests; give the subtest or table-case count beside it)". It does not say whether tests in `internal/` packages and the test service count. Assumed: every package of the module, with subtests listed beside the total.
2. Whether a public API that takes a Go number must reject `NaN` and the infinities. Where: `docs/sdk-contract.md` section 3, "Contexts". Closest sentence: "A context is a single context or a multi context, exactly as evaluation.md defines them, including what is valid." The evaluator's context is JSON, which has neither, and the sentence says nothing about a typed builder's inputs. Assumed: `ValueFrom` accepts them, and the JSON writer prints a non-finite number as `null` (as `JSON.stringify` does); the evaluator sees only what a JSON body could carry.
3. Whether the README's `mise install` should be limited to the SDK's tools. Where: `PROMPT.md` phase 5 item 4, and `sdks/README.md`, "Running an SDK's checks". Closest sentence: "`jdx/mise-action` limited to the tools in `$L/mise.toml` (unlimited, it also installs the root `.tool-versions`: .NET and a second Bun)". That covers the workflow, not what a reader's local `mise install` does. Assumed: the README gives `(cd sdks/go && mise trust && mise install)` as `sdks/README.md` does and says no more.
4. How the "never-closed client does not keep the process alive" line applies to Go. Where: `CHECKLIST.md`, "Native tests", last line: "where the runtime has daemon threads or background tasks (contract 6.6, SHOULD)". Go goroutines never keep a process alive, but the line does not say whether that counts as "has the idea" (the line is either n/a or met). Assumed: met by construction, checked by reading `New` (two plain goroutines, nothing waits for them at exit), with no test, because a test cannot observe its own process exiting.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `RUN-TEMPLATE.md` "Results", after "Native tests:", add: "(every package of the SDK, test service included)".
2. For decision 9, in `language-notes/go.md`, add: "**A fake clock shared by two loops.** The sync loop and the flush loop both arm a timer, so a test that steps the clock to fire the poll waits for `Armed() >= 2`, never `> 0`: a step taken while only the flush timer is armed is lost and the poll timer is set from the new time. This showed as a failure in 1 of the first 3 `bin/test` runs."
3. For guess 4, in `CHECKLIST.md`, last line of "Native tests", add: "Go: goroutines never keep a process alive; mark the line met by construction and name `New`."
4. For guess 2, in `PROMPT.md` phase 3, in the paragraph on typed context builders, add: "A builder may accept a value JSON cannot carry (a NaN float); say in `DESIGN.md` what it becomes."

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every bucket pin, count equal to the phase 2 command's: `internal/evaluator/vectors_test.go` (`TestVectors`, `TestBucketPins`) reads `../../../../fixtures/evaluation` at test time; `bin/test` prints `vectors: 1371 pins: 17`. The harness runs 1369 vectors and skips the 2 `load:` ones, 1371 in all.
- [x] `bin/test` passes 10 times in a row, and an eleventh with the CPU loaded: ten runs of `sdks/go/bin/test` all exit 0 and print `vectors: 1371 pins: 17`; the eleventh ran with 12 busy shell loops (`nproc` = 12) started before `bin/test` and killed after it ended, and exits 0. See Results and Decision 9 (the first attempt failed on run 3 and the ten restarted).
- [x] The harness exits 0, only the 2 named skips: `bun run conformance -- --service sdks/go/bin/test-service` printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` twice in a row; `integer-accessor` is advertised so no scenario is skipped.

### What the harness checks

- [x] Both context shapes: the harness (`privacy.json`, `kinds.json` vectors); native `TestContextBuilders`, `TestFlagNotFoundAndInvalidContext`.
- [x] Result order and mismatch rule, SDK's own codes: harness `evaluation.json`, `usage.json`; native `TestTypeMismatchKeepsTheEvaluatorOutOfTheCount`, `TestNotReadyServesFallbacks`.
- [x] Integer accessor: served fraction is `TYPE_MISMATCH`, `2.0` is `2`: harness `evaluation.json`; native `TestIntegerAccessor` (8 cases).
- [x] No failure makes an operation raise: harness (every scenario); native `TestPanicsNeverReachTheCaller`.
- [x] Evaluating before the first load answers `NOT_READY` at once: harness `load.json`; native `TestNotReadyServesFallbacks`.
- [x] `ready` settles after the first attempt, bounded, may be called again, false after `close` if unsettled: harness `load.json`, `close.json`; native `TestReadyAndStatusAfterLoad`, `TestCloseSetsTheStreamClosedAndReadyFalse`.
- [x] A non-ruleset answer never replaces the one held: harness `status.json`; native `TestBadRulesetKeepsTheLastGoodOne` (4 bad bodies).
- [x] The ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`; native `TestIfNoneMatchOnlyWithARulesetAndATag`, `TestNotModifiedWithNothingHeldIsAFailure`.
- [x] The stream, SSE parsing, refresh on change, a failed refresh ends the stream: harness `stream.json`, `load.json`, `status.json`; native `TestStreamConnectedThenChanged`, `TestStreamFetchFailureEndsTheStreamAndBacksOff`, `internal/sse` `TestEvents`, `TestEveryByteBoundary`.
- [x] The loop and the jittered backoff: harness `backoff.json`, `stream.json`; native `TestBackoffGrowsWhileTheStreamWillNotOpen`, `TestPollingOnlyWaitsThePollInterval`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; native `TestRetryAfterParsing`, `TestRetryAfterIsIgnoredOffA429`.
- [x] Status fields, states and `lastError` codes: harness `status.json` and the shape check on every read; native `TestReadyAndStatusAfterLoad`.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`; native `TestUsageIsCountedPerFlagVariationAndDay`, `TestUsageBatchesStayUnder500`, `TestUsageKeptOnServerErrorAndDroppedOnClientError`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: harness `hooks.json`; native `TestHookSeesTheEvaluationAndItsTagAndVersion`, `TestPanicsNeverReachTheCaller`.
- [x] `close` in order, twice at once, during a hold, final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`; native `TestTwoRacingClosesBothReturnAfterTheWork`, `TestCloseSendsThePendingCountsOnce`, `TestUsageHoldOn429AndTheUnsentCloseMessage`.
- [x] No context, key or attribute in any request or `onError` message, no header beyond the protocol's: harness `privacy.json` and the checks after every scenario. Source search (run in `sdks/go`, non-test files): `grep -rn 'log\.\|fmt\.Print\|fmt\.Fprint\|os\.Std'` finds no log call or print (the two hits are comments saying the text "is safe to log"); the error builders are `errors.New` of two fixed texts, `fmt.Errorf("closed with %d usage counts unsent: ...")` (a count), `fmt.Errorf("unsupported attribute type %s", reflect.TypeOf(x))` (a type name, never the value), the `syncError` texts (`what + " failed: network error"`, `http_<status>`) and `PanicError` (Decision 10). Native `TestNetworkFailureTextHoldsNoSecret`, `TestUsageBodyHoldsNoContext`.

### Native tests

- [x] Every operation from many goroutines, `close` racing `variation`, two `close` calls racing, under the race detector: `TestManyGoroutinesEvaluateWhileClosing` (16 workers of 2000 evaluations and status reads, 4 closers, a 1 ms poll and flush) and `TestTwoRacingClosesBothReturnAfterTheWork`; `bin/test` runs `go test -race`. Both are named here, as the line allows.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TestTLSServer` (`httptest` TLS server, a transport trusting its certificate).
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs, and while a usage flush hangs: `TestEvaluationIsNotBlockedByAHungRequest`, `TestCloseIsBoundedWhileEverythingHangs`.
- [x] A reader sees one ruleset or the other across a swap: `TestRulesetAndTagSwapTogether` (4 goroutines, 20 swaps, hook `etag` and `flagVersion` checked together).
- [x] A hook and an `onError` that call back into the client do not deadlock, and `ready` from inside returns without waiting: `TestCallbacksMayCallBackIntoTheClient` (onError calling Close, Status, Variation and Ready; a hook calling Close; Ready from onError; a usage-error callback calling Close).
- [x] A stream with no bytes for 60 s is dropped as `timeout`, a keep-alive restarts it, through a seam on a real quiet loopback socket: `TestWatchdogEndsAQuietStream` (`withWatchdog(80ms)`).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes: `TestRetryAfterIsCappedOnRulesetAndStream` and `TestUsageHoldOn429AndTheUnsentCloseMessage` (events: no request at 299 s of a 100000 s header).
- [x] Counts either side of a UTC midnight go out as two days: `TestUsageIsCountedPerFlagVariationAndDay` (fake clock across 2030-03-04 to 2030-03-05).
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call: `TestEvaluationFailedWhenTheEvaluatorPanics` (through the `evaluate` seam).
- [x] Hook futures: n/a: the hook is a plain function, no futures.
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestIntegerAccessor` (cases for `2^63`, `1e30` and `1e999`, and the two edges that are served), harness `huge` scenario.
- [x] A never-closed client does not keep the process alive: satisfied by construction, `New` starts two plain goroutines and nothing waits for them at process exit (Guess 4).

### Code and documents

- [x] `DESIGN.md` passes the row check, and `README.md` links the mapping table: `README.md` "Mapping table" links `DESIGN.md#the-mapping-table` and summarises it.
- [x] Names keep the canonical meaning and defaults, and the closed sets keep their spelling: `DESIGN.md` "Closed sets" (`ReasonKind` values `OFF`, `TARGET_MATCH` and so on; `ErrorCode` values; `StreamState` values; `LastError` values `timeout`, `bad_ruleset`, `network`, `http_<status>`).
- [x] The evaluator has no platform regular expression: `grep -rn '"regexp"\|regexp\.' sdks/go/internal/evaluator` prints nothing; `pattern.go` is the spec's own dialect.
- [x] The lint configuration names its rule set and gives a reason for each rule it ignores: `.golangci.yml` (`default: none`, comments beside `exhaustive` and `contextcheck`, the `_test.go` exclusions), Decision 1.
- [x] Every runtime dependency has a line in `DESIGN.md`: none; `go.mod` has no `require`, and `DESIGN.md` "Dependencies" says the standard library only.
- [x] No telemetry, and no header beyond the protocol's; the client's own additions are listed below. Source search: `grep -rn 'Header\.\(Set\|Add\)'` in non-test files finds `Authorization` (`sync.go:121`), `If-None-Match` (`sync.go:157`), `Accept` (`sync.go:269`) and `Content-Type` (`usage.go:139`), and nothing else. `TestHeaderAudit` captured one request of each route on a loopback listener and fails on any header outside its list:

  | Route | Headers on the wire | Added by `net/http`, and why it identifies no one |
  | --- | --- | --- |
  | `GET /sdk/v1/ruleset` | `Authorization`, `User-Agent`, `Host` | `User-Agent: Go-http-client/1.1` names the library and its protocol version, not a host or a user. `Host: 127.0.0.1:<port>` is the URL the application gave, read by the handler as `r.Host`, not from the header map. |
  | `GET /sdk/v1/ruleset` (conditional) | the above and `If-None-Match` | `If-None-Match` is the ETag the server sent. |
  | `GET /sdk/v1/stream` | `Authorization`, `User-Agent`, `Host`, `Accept: text/event-stream` | as above. |
  | `POST /sdk/v1/events` | `Authorization`, `User-Agent`, `Host`, `Content-Type: application/json`, `Content-Length` | `Content-Length` is the size of the counts body. |

  No `Accept-Encoding` (the default transport is cloned with `DisableCompression`) and no `Connection` header appeared on any route.
- [x] Every extra beyond the contract is one contract 13 allows: the extras are `WithTransport` (an option), the generic `Variation[T]`, `Status` types and the `...Detail` twins (the accessor shapes contract 2 allows); none weakens contract 5 or sends anything new. The "Bounds" bullets were re-read against the parser and the client: depth 4096 (contract 7.1 allows a limit of at least 512), 500 per events request, 300 s `Retry-After` cap, the 60 s watchdog, and the pattern limits `docs/evaluation.md` "Limits" names (1024 code points, size 10000, count 100). The backoff exponent is capped at 16 inside `min(poll, 1 s * 2^k)`, which the poll interval already bounds, so it only guards the shift. No limit the contract does not name was left in.
- [x] Test seams are options on the package's own config, never a global or an environment variable: `grep -rn 'os.Getenv\|LookupEnv'` finds nothing outside tests; the seams are `config.clock`, `jitter`, `watchdog` and `evaluate`, set in `helpers_test.go` (Decision 8).
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK and use only the pinned toolchain: all three are `#!/bin/sh`, `set -eu`, `cd "$(dirname "$0")/.."`, use `mise exec --` when `mise` is on PATH, and are committed with mode 100755. `bin/lint` prints `0 issues.`.
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method: `bool` to `BoolVariationDetail`, `string` to `StringVariationDetail`, `number` to `FloatVariationDetail`, `int` to `IntVariationDetail`, `json` and `any` to `JSONVariationDetail`.
- [x] The workflow follows the skeleton, filters on its paths and pins every action by commit SHA: `.github/workflows/sdk-go.yml`; `bun scripts/check-workflows.ts .github/workflows/*.yml` reads 8 workflows with 0 failures.

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exits 0 with no output.
- [x] `bun run format:check` passes (including `mise.toml`, the workflow and the golangci config).
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints only `scope ok`.
- [x] No package was published and no name reserved: nothing was pushed to a registry; the module path is a name for the directory and is registered nowhere (`README.md` says so).
- [x] Commit subject is a Conventional Commit, `chore`, with no `Co-Authored-By` or AI attribution (Decision 14).
- [x] The workflow's job is `runs-on: ubuntu-latest`, no `if:` guard, triggers only `pull_request` and `push`, installs its own toolchain, and `check-workflows.ts` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of this list: this file; the phase 6 heading loop prints nothing.
