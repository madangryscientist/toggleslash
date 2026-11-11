# Go SDK: run report

## Prompt version

- Prompt: a9b9795cef46847d5a9024752e5a37fcec69c460 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Sonnet 5.5 (`claude-sonnet-5-5`), through Claude Code
- Date: 2026-11-11
- Input: language Go; runtime chosen: the `go` toolchain with the standard library only; minimum version chosen:
  Go 1.26 (`go 1.26` in `go.mod`); package manager chosen: Go modules, with no external requirement
- Toolchain: `mise.toml` pins `go = "1.27.4"` (`go version` prints `go1.27.4 linux/amd64`),
  `golangci-lint = "2.15.0"` (`golangci-lint --version` prints `has version 2.15.0`) and `zig = "0.17.1"`
  (`zig version` prints `0.17.1`; it is the C compiler the race detector needs, set as `CC="zig cc"` in `bin/test`)

## Results

- Vectors: 1371 passed of 1371, bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`, equal to the
  phase 2 count command's numbers). Through the service the harness runs 1369 of them and skips the 2 named ones.
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 91 passed of 91 top-level tests (121 subtests and table cases beside them), under `go test -race`
- Repeat runs: `bin/test` passed 10 times in a row on the final tree (10 of 10, each printing `vectors: 1371 pins: 17`); once more with 12 busy shell loops
  (`nproc` is 12) started before `bin/test` and kept running until it ended: passed, same counts

## Decisions

1. Minimum Go is 1.26, the oldest release in upstream security support on the run date (Go supports the two newest
   majors, 1.27 and 1.26). The code uses only features 1.26 has. Settled by PROMPT.md's rule to take the oldest supported
   version; recorded in `DESIGN.md` section 6.
2. Pinned toolchain is Go 1.27.4, the newest stable release in `mise ls-remote go`, with `golangci-lint` 2.15.0 and `zig`
   0.17.1. Zig is there only because `go test -race` needs a C compiler and the machine's default is not pinned.
3. Lint is `go vet`, `gofmt -l` and `golangci-lint` v2 with `default: standard` plus bodyclose, copyloopvar, durationcheck,
   errorlint, gosec, intrange, misspell, nilerr, noctx and unconvert; govet runs with every analyzer except fieldalignment
   and shadow. Each exclusion in `.golangci.yml` has a comment (gosec in tests and the test service; G115 in
   `internal/jsonv`, where a narrowing conversion is range-checked; nilerr in `http.go` and `sync.go`, where an error is
   deliberately turned into the SDK's own code). Format is `gofmt`. Tests are the standard `testing` package.
4. No runtime dependency. `net/http`, `crypto/sha256` and `sync/atomic` do the work (`DESIGN.md` section 4), so the
   dependency line check has nothing to list.
5. JSON is a hand-written reader and writer in `internal/jsonv`, not `encoding/json`: `encoding/json` fails on `1e999`
   and replaces a lone surrogate with U+FFFD, and `docs/evaluation.md` requires both to survive (`DESIGN.md` section 5).
   `encoding/json` is used only to marshal an arbitrary Go struct that a caller passes as a context.
6. The evaluator has its own pattern parser and Pike VM (`internal/evaluator/pattern.go`, `regex.go`), because
   `docs/evaluation.md` ("Patterns", "Cost") forbids the platform engine and `regexp` is RE2 with different syntax.
7. Strings are held as WTF-8 so a lone surrogate keeps its code unit for `contains`, `startsWith`, `endsWith` and the
   order operators (`DESIGN.md` sections 5 and 8).
8. `flagVersion` on a flag whose only fault is a missing segment reference follows `docs/sdk-contract.md` line 410
   ("or none when the ruleset has no such flag or the flag is malformed"), not the TypeScript reference
   (`packages/sdk/src/client.ts`, `versionOf`), which reports none for it. The contract settles it (`DESIGN.md` section 8).
9. The accessor shape is five typed methods with a `Detail` twin each (Go methods cannot be generic): `Bool`, `String`,
   `Int` (int64), `Float` (float64) and `JSON`. `IntVariation` is the integer accessor; a fraction or a number outside
   int64 is `TYPE_MISMATCH` and `2.0` is `2` (contract 4, "Typed languages").
10. Options are functional options, so "not given" is distinct from zero; a zero or negative duration falls back to the
    default (contract 2 says an SDK SHOULD).
11. The package is named `toggleslash` and the module path is `github.com/madangryscientist/toggleslash/sdks/go`; nothing
    is registered under it (rule 3).
12. A context is an `any`: the SDK's own `Value`, a `map[string]any`, JSON text as `[]byte` or `json.RawMessage`, or any
    value `encoding/json` can marshal. An unusable one is `ERROR INVALID_CONTEXT`, never a panic.
13. The test service answers 400 to a typed accessor whose fallback is null or of another kind, and `int` with a fallback
    that is not an integer. `docs/sdk-test-service.md` line 81 says a service MAY answer 400 to one. `any` and `json` take
    any fallback.
14. A client that is never closed does not keep the process alive: both loops are goroutines, and a Go process exits when
    `main` returns (contract 6.6, SHOULD). `TestNeverClosedClientDoesNotHoldTheProcess` runs a helper process to prove it.
15. TLS is tested with `httptest.NewTLSServer`: `TestTLS` reaches the TLS path with the server's certificate trusted, and
    `TestTLSUntrustedCertificateIsANetworkError` shows the default is verification (contract 7, SHOULD).
16. The watchdog's length, the clock and the jitter's random source are fields of the SDK's own config that package tests
    set through the unexported options `withIdle`, `withClock` and `withRandom`, not a global or an environment variable (contract 13, PROMPT.md phase 3).
17. `Detail[T]` is a generic struct and the accessor methods are not generic; the test service holds `Value` for the
    `json` and `any` accessors and writes a non-finite number as `null`.
18. `bin/test-service` builds the binary into `.build/` on first use (git-ignored) and re-executes itself under `mise exec`
    so the pinned toolchain is the one that runs (PROMPT.md, "Entry points").
19. Two tests were added during phase 6 because a checklist line had no test of its own:
    `TestVariationAndStatusDoNotWaitOnAHungRequest` (a hung ruleset request and a hung flush; it uses a 500 ms request
    timeout because `Close` in the test's cleanup waits for the final flush, bounded by about the timeout) and
    `TestRetryAfterOnTheRulesetIsCappedAt300Seconds` (the cap on the ruleset route; the stream route has
    `TestRetryAfterIsCappedAt300Seconds` and the events route `TestUsageHoldAfter429`).
20. Three test races found by the repeat runs, all fixed in the tests and not in the SDK (the checklist says a timer test
    that races fails only under load, and that the fix is a decision, not a guess):
    - Run 9 of the first ten: `TestNoETagMeansNoConditionalRequest` timed out. The usage flush timer and the poll timer
      are both 30 s by default, so `fireNext(exactly(30s))` could fire the flush timer before the poll timer was armed, and
      the poll then waited forever on a clock that did not move. Every test that fires a 30 s timer now sets
      `WithFlushInterval(time.Hour)`.
    - `TestBackoffJitterBounds`, found by 150 loaded repeats: it read the last timer armed, which could be the flush timer.
      It now waits for a timer of the expected length. The unused helper `notRequest` was removed.
    - The loaded run: `TestConditionalRequestAndNotModified` called `fc.advance(time.Minute)` right after `Ready`, which
      fires the poll timer early (a third request, so "exactly two" never held) or not at all, depending on whether the sync
      goroutine had armed it. It now waits until the poll timer is armed, then advances.
    After each fix the ten runs started again from the first. Before the final ten, 30 full runs of the package under 12
    busy loops passed.

## Header audit

One request of each route, captured on a loopback listener with a small program (it sets `Authorization: Bearer KEY`
itself; the capture lists every header the Go client sent, with the Go `Host` read from `r.Host`):

| Route | SDK sets | Added by `net/http` | Host | Content-Length |
| --- | --- | --- | --- | --- |
| `GET /sdk/v1/ruleset` | `Authorization` | `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1` | `127.0.0.1:<port>` | 0 |
| `GET /sdk/v1/ruleset` (conditional) | `Authorization`, `If-None-Match` | `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1` | `127.0.0.1:<port>` | 0 |
| `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1` | `127.0.0.1:<port>` | 0 |
| `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1`, `Content-Length` | `127.0.0.1:<port>` | 73 |

Why each added header does not identify a user or a machine: `Accept-Encoding: gzip` is the same on every Go client and
names a codec; `User-Agent: Go-http-client/1.1` names the library and the HTTP version, not a host, a user or an SDK
version; `Host` is the URL the application configured; `Content-Length` is the body's length. `TestHeadersOnEveryRoute`
holds this in the test suite: any other header fails it. A source search for request headers
(`grep -rn 'Header.Set\|Header.Add\|header{' --include=*.go .` outside tests and the test service) finds only
`http.go:76` (the loop that applies the list), `usage.go:119` (the events list: `Authorization`, `Content-Type`),
`sync.go:110` and `sync.go:112` (`Authorization`, `If-None-Match`) and `sync.go:200` and `sync.go:201`
(`Authorization`, `Accept`).

## Guesses

1. Whether adding a test, or fixing a test, in phase 6 restarts the ten consecutive runs. Where: CHECKLIST.md, "The two executable checks",
   second line. Closest sentence: "The ten runs then start again from the first", which follows "Such a failure is a bug in the
   test" and so reads as being about a flaky-test fix; it does not say what a new test does to a run that has already
   passed five times. Assumed: any change to the tree restarts the ten, so the runs are of the tree that is committed.
2. Whether the "CPU loaded" run counts toward the ten or is separate. Where: CHECKLIST.md, same line. Closest sentence:
   "passes 10 times in a row, and once more with the CPU loaded". Assumed: separate, an eleventh run.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `CHECKLIST.md`, "The two executable checks", after "The ten runs then start again from the first.",
   add: "Any change to the tree after a run (a fix, a new test, a comment) also starts them again: the ten are of the tree
   that is committed."
2. For guess 2, in the same line, after "and once more with the CPU loaded", add: "(an eleventh run, which does not
   replace one of the ten)".

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, and its count equals phase 2's: `bin/test` prints
  `vectors: 1371 pins: 17` (`internal/evaluator/vectors_test.go`: `TestVectors` reads `fixtures/evaluation/*.json` at test
  time, `TestBucketPins` reads `buckets.json`).
- [x] `bin/test` passes 10 times in a row and once more under load: 10 of 10 consecutive passes, then 1 pass with 12 busy loops (see Results and decision 20).
- [x] The harness exits 0 with only the 2 named vector skips: `bun run conformance -- --service sdks/go/bin/test-service`
  printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` and exited 0 (no scenario skipped, since `integer-accessor` is advertised).

### What the harness checks

- [x] Both context shapes: the harness, `privacy.json`, the `kinds.json` vectors run through the service.
- [x] Result order and mismatch rule: the harness, `evaluation.json`, `usage.json`; natively `TestTypeMismatch`,
  `TestEvaluatorErrorsKeepTheirReason`, `TestNotReadyBeforeTheFirstLoad`.
- [x] Integer accessor: the harness, `evaluation.json`; natively `TestIntegerAccessorRange`.
- [x] No failure makes an operation raise: the harness, every scenario (the test service answers 500 on a raise);
  natively `TestEvaluationFailureIsOneOnError`, `TestContextForms`.
- [x] `NOT_READY` at once before the first load: the harness, `load.json`; `TestNotReadyBeforeTheFirstLoad`.
- [x] `ready` settles, may be called again, false after `close` if unsettled: the harness, `load.json`, `close.json`;
  `TestCloseDuringAHungFirstLoad`.
- [x] A non-ruleset answer never replaces the held one: the harness, `status.json`; `TestBadRulesetKeepsTheLastGoodOne`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: the harness, `load.json`, `status.json`;
  `TestConditionalRequestAndNotModified`, `TestNoETagMeansNoConditionalRequest`, `TestETagIsKeptExactly`,
  `TestTrailingSlashesAreRemoved`, `TestNotModifiedWithNoRulesetIsAnError`.
- [x] Stream, SSE parsing, refresh, failed refresh ends the stream: the harness, `stream.json`, `load.json`, `status.json`;
  `TestSSEParser` (each case also byte by byte), `TestChangedFetchesAConditionalRuleset`, `TestFailedFetchEndsTheStream`,
  `TestConnectedWithTheHeldTagMakesNoFetch`, `TestIncompleteEventAtTheEndIsDiscarded`.
- [x] Loop and jittered backoff: the harness, `backoff.json`, `stream.json`; `TestBackoffGrowsAndIsJittered`,
  `TestBackoffJitterBounds`, `TestSuccessfulStreamResetsTheBackoff`, `TestStreamOffWaitsThePollIntervalEveryTime`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`; `TestRetryAfterShapes`, `TestOnly429ReadsRetryAfter`,
  `TestAHoldNeverShrinks`, `TestFirstLoadIsNotHeld`, `TestUsageHoldAfter429`, `TestUsageHoldIsSeparateFromTheRulesetHold`.
- [x] Status fields, states and `lastError` codes: the harness, `status.json`; `TestStatusCodesBecomeLastError`,
  `TestStreamOffStatus`, `TestStreamStatusOnNon200`.
- [x] Usage counts: the harness, `usage.json`, `rate-limit.json`; `TestUsageBodyIsExact`, `TestUsageBatchesOf500`,
  `TestUsageStatusDecisions`, `TestUsageFailureKeepsTheRestAndStops`, `TestNeverCountsAFallback`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: the harness, `hooks.json`;
  `TestHookPanicGoesToOnError`, `TestOnErrorPanicIsIgnored`, `TestHookSeesNotReady`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`; `TestCloseSendsTheFinalFlush`, `TestCloseReportsCountsHeldBack`, `TestCloseIsIdempotentAndConcurrent`.
- [x] No context, key or attribute in any request, `onError` message or log: the harness (`privacy.json` and the checks
  after every scenario); natively `TestUsageNeverNamesTheContext`, `TestNetworkErrorTextHoldsNoURLOrKey`,
  `TestStatusHoldsNothingSecret`. Source search, outside tests and the test service:
  `grep -rn 'log\.\|slog\|fmt.Print\|fmt.Fprint\|os.Stderr' --include=*.go .` finds nothing, so the SDK writes no log line; an
  error value is built only by `newSyncError(what, code)` from fixed words and a code (`types.go`), which holds no URL,
  key or context.

### Native tests

- [x] Every operation from many goroutines, `Close` racing `variation`, two `Close` calls racing, under the race detector:
  `TestEveryOperationFromManyGoroutines`, `TestCloseRacingVariation`, `TestCloseIsIdempotentAndConcurrent`, all under
  `go test -race` (`bin/test`).
- [x] SHOULD, TLS: `TestTLS` and `TestTLSUntrustedCertificateIsANetworkError` (`httptest.NewTLSServer`).
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a flush hangs:
  `TestVariationAndStatusDoNotWaitOnAHungRequest` (2000 evaluations and `Status` reads in under 3 s with both hung).
- [x] One ruleset or the other across a swap: `TestHookAndRulesetStayPaired` (`etag` and `flagVersion` of the same snapshot
  under concurrent swaps), `TestHookSeesTheSnapshotItEvaluated`; the swap is one `atomic.Pointer` store of a struct.
- [x] SHOULD, `afterEvaluation` and `onError` calling back into the client: `TestCloseFromTheHook`, `TestCloseFromOnError`
  (calls `Ready` and `Close` from inside), `TestCloseFromTheFinalFlushCallback`.
- [x] 60 s watchdog through a seam and a real socket: `TestIdleWatchdogOnARealSocket` (a loopback socket that goes quiet,
  `cfg.idle` of 300 ms), `TestIdleWatchdogOnTheFakeClock`, `TestStreamOpenIsBoundedByTheRequestTimeout`.
- [x] `Retry-After` above 300 held for 300 s on all three routes: `TestRetryAfterIsCappedAt300Seconds` (stream), `TestRetryAfterOnTheRulesetIsCappedAt300Seconds` (ruleset),
  `TestUsageHoldAfter429` (events: `Retry-After: 100000` holds until 400 s on a 100 s flush), all on the fake clock.
- [x] Counts either side of a UTC midnight go out as two days: `TestUsageSplitsAtUTCMidnight`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`:
  `TestEvaluationFailureIsOneOnError`.
- [x] Hooks that may return a future: n/a: no futures (the hook is a plain `func(Event)`).
- [x] With `integer-accessor`, a number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor:
  `TestIntegerAccessorRange`, and the harness `evaluation.json` scenario with the flag `huge` (`1e20`).
- [x] SHOULD, a never-closed client does not keep the process alive: `TestNeverClosedClientDoesNotHoldTheProcess`.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check and `README.md` links the mapping table (`DESIGN.md` section 1, linked
  from `README.md`'s mapping summary).
- [x] Names keep their canonical meaning and defaults, and the closed sets keep their spelling: `DESIGN.md` section 1, the
  string constants in `types.go` (`ReasonOff` is `"OFF"`, `ErrInvalidContext` is `"INVALID_CONTEXT"`, `StreamOff` is `"off"`).
- [x] No regular expression from the platform in the evaluator: `grep -rn 'regexp\|"regex' --include=*.go .` outside tests
  finds only `test-service/main.go:15` (the test service's own route match, not the evaluator); `internal/evaluator` has none.
- [x] The lint configuration names its rule set and each ignore has a reason: `.golangci.yml`.
- [x] Every runtime dependency has a `DESIGN.md` line: there is none (`DESIGN.md` section 4; `go.mod` has no `require`).
- [x] No telemetry and no identifying header: the header audit table above; `TestHeadersOnEveryRoute`.
- [x] Every extra is one contract 13 allows: typed accessors ("typed accessors or a generic accessor"), the unexported
  options that inject a clock, a jitter source and the watchdog length ("options and test seams for its own tests"), and
  `Value` with `ParseJSON`, the SDK's JSON value type so a context can keep `1e999` and lone surrogates (a context form,
  not a new operation). None makes a guarantee of contract 5 weaker or sends anything new.
- [x] Test seams are fields of the SDK's own config that the package's tests set (`cfg.idle`, `cfg.clock`, `cfg.random`,
  through the unexported options `withIdle`, `withClock` and `withRandom`), never a global or an environment variable the SDK
  reads: `options.go` lines 41 to 43 and 109 to 111, `clock.go`.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable (`ls -l bin`: `-rwxr-xr-x`), re-execute under
  `mise exec` and run with only the toolchain `mise.toml` pins.
- [x] `test-service/README.md` maps every accessor (`bool`, `string`, `number`, `int`, `json`, `any`) to an SDK method.
- [x] `.github/workflows/sdk-go.yml` follows the skeleton, filters on its paths (`sdks/go/**`, `fixtures/**`, the contract, the
  test-service protocol, the harness and itself) and pins every action by commit SHA.

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` passed in phase 5 and again before the commit.
- [x] `bun run format:check` passes.
- [x] Only `sdks/go/` and the one workflow file changed: the phase 6 scope check prints `scope ok`.
- [x] No package published and no name reserved.
- [x] The commit subject is a Conventional Commit, `chore`, with no `Co-Authored-By` or AI attribution.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:` guard, triggers `pull_request` and `push` only, installs its own
  toolchain, and `bun scripts/check-workflows.ts .github/workflows/*.yml` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of this list.
