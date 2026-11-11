# Go SDK: run report

## Prompt version

- Prompt: 85275647d6cb3632e3f16376310fe4972b4e3327 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), through Claude Code
- Date: 2026-11-11
- Input: language Go; runtime chosen: the gc toolchain (the language's mainstream runtime); minimum version chosen: Go 1.26 in `go.mod` (see Decisions 2); package manager chosen: Go modules (the only one)
- Toolchain: `mise.toml` pins go 1.27.3 (`go version go1.27.3 linux/amd64`), golangci-lint 2.15.0 (`golangci-lint has version 2.15.0 built with go1.27.0`), zig 0.17.1 (`zig version` prints `0.17.1`, used only as the C compiler for `-race`)

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 53 passed of 53 top-level tests (57 more subtests pass), under `-race`, in `go test -race -count=1 -v ./...`
- Repeats: `bin/test` passed 10 runs in a row (exit 0, `vectors: 1371 pins: 17` each time), and then 3 more runs with 12 busy loops running (one per core, `nproc` is 12), all exit 0. The first loaded run failed, see Guesses 9: that was a test written wrongly, not an SDK race, and it is fixed; the 10 plain runs were repeated after the fix.

## Decisions

1. Lint and format: `go vet`, `golangci-lint` 2.15.0 with an explicit list (`bodyclose errcheck errorlint govet ineffassign misspell nilerr noctx staticcheck unconvert unused wastedassign`, default off) and `gofmt`, over a larger preset, so every rule is named and each omission carries a reason. `usestdlibvars` is left out with a comment: it wants `http.StatusOK` for every status literal in the table tests, and a literal 429 beside the header that carries it reads better. `bin/lint` fails on any `gofmt -l` output.
2. Minimum Go 1.26 over 1.27: the oldest release in upstream security support when the run started that has `context.WithTimeoutCause`, generic types, `min`/`max`/`clear` and `atomic.Pointer`. The module needs nothing newer. The pin in `mise.toml` is the newest the machine's `mise` offered.
3. No runtime dependency. The standard library has `net/http`, `crypto/sha256`, `sync` and `context`. `DESIGN.md` section 4 says so.
4. An own JSON reader (`internal/jsonvalue`) and an own WTF-8 layer (`internal/wtf8`) over `encoding/json`: `encoding/json` fails on `1e999` and turns a lone surrogate escape into U+FFFD, both of which the spec treats differently. `DESIGN.md` section 5.
5. An own pattern matcher (a Pike VM over code points) in `internal/eval/pattern.go`, no `regexp` in the evaluator (evaluation.md, "Patterns").
6. Options are functional (`WithStream`, `WithPollInterval`, ...) over a config struct, the Go idiom; durations are `time.Duration`, not milliseconds. The mapping is in `DESIGN.md` section 1.
7. `Variation` methods are typed per accessor (`BoolVariation`, `StringVariation`, `IntVariation` with `int64`, `FloatVariation`, `JSONVariation`, `AnyVariation`) with `Detail[T]` twins. `JSONVariation` and `AnyVariation` are the same method under two names, because the test-service protocol has both accessors and Go has one dynamic value type (`Value`).
8. Real clocks for every timer, and a `cfg.now` seam only for the UTC day and the Retry-After hold arithmetic; other seams (`withRandom`, `withWatchdog`, `withEvaluator`) are unexported options on `NewClient`, never a global or an environment variable. The SDK reads no environment variable.
9. `net/http` with its `DefaultTransport` cloned, `CheckRedirect` returning `http.ErrUseLastResponse` (a redirect is `http_3xx` and is never followed), and fixed error text only (`Error{Op, Code}`), so no URL, key or context reaches a message. A request's total time, body read included, is bounded by `context.WithTimeoutCause`.
10. The SDK's own goroutines are marked by goroutine id, so `Ready` and `Close` called from a callback on one of them do not wait (contract 6.4). The alternative, a context value, cannot cross the user's own callback goroutine.
11. `zig` is pinned in `mise.toml` as a fallback C compiler: `-race` needs cgo. `bin/test` uses `gcc`/`cc` when present and `zig cc` otherwise. I verified `zig cc` builds and runs the race detector here. The workflow installs zig for the same reason.
12. `ContextFromValue` is exported (not only `ToValue`) so the test service, a separate `main` package, can pass a decoded JSON context through. It adds no behaviour.
13. Go 1.27.3 is pinned while go.dev lists 1.27.4: `mise ls-remote go` did not show 1.27.4 on this machine. I pinned what the tool could install; a later run should look at go.dev too.
14. Test ports: tests use `httptest` on port 0 with a loopback listener. No fixed port.

## Guesses

1. A stream that answers `200` with an empty body (no event, then EOF). Where: sdk-contract.md section 7.2 and `stream.json`. Assumed: it is a stream that ended without `connected`, so state `disconnected`, `lastError` `stream_closed`, backoff as for any drop; the first load is not affected.
2. A `304` when the SDK holds no ETag for the ruleset (a server that sends `304` unasked). Where: sdk-contract.md section 7.1. Assumed: treated as an answer that is not a ruleset (`http_304` as an error, ruleset kept). With a held ruleset and a matching ETag, `304` is a successful sync (`lastSyncAt` moves).
3. An SSE `data:` line with an empty value, and a dispatch with no `data` lines at all. Where: sdk-contract.md section 7.2 ("per WHATWG"). Assumed: WHATWG rules: an event with an empty data buffer is not dispatched; a `data:` line with an empty value contributes an empty line to the buffer.
4. Whether the ruleset request sets an `Accept` header. Where: sdk-contract.md section 7.1, the privacy header list in section 12. Assumed: no, since the list allows it but nothing requires it; the stream request sets `Accept: text/event-stream`.
5. The `flagVersion` in a hook event for a flag the ruleset does not have, and when `version` is not an integer. Where: sdk-contract.md section 10. Assumed: absent flag gives a nil `FlagVersion`; the version is read as the TypeScript SDK's `versionOf` reads it (an integer-valued number, else nil).
6. The wording of the `onError` call when `close` cannot send counts because a hold is in force. Where: sdk-contract.md section 11. Assumed: `closed with N usage counts unsent: the server asked to wait`, as the TypeScript SDK's `finish` words it, where N counts pending (flag, index, day) entries.
7. Whether `Ready` called after `Close`, when the first attempt had settled, answers the settled value. Where: sdk-contract.md sections 5.3 and 11. Assumed: yes, the settled value stays fixed; only an unsettled `Ready` becomes false at close.
8. Which C compiler is "the toolchain `mise.toml` pins" when the language needs cgo for its race detector. Where: PROMPT.md, "Entry points" and CHECKLIST.md, native tests. Assumed: the pinned toolchain may include a C compiler (`zig cc`), and `bin/test` prefers a system one. Note: here `~/.local/bin/zig` shadows the mise zig outside `mise exec`, so a script that skips `mise exec` would pick up the wrong zig; the scripts always use `mise exec --`.
9. Whether the "once more with the CPU loaded" run may fail and what to do. Where: CHECKLIST.md, the two executable checks. Assumed: a failure is a bug in the test, fixed at its cause. The loaded run failed in `TestHookSeesTheAnswerAndCannotChangeIt`: it called the variation once before `Ready` (which counts only if the ruleset had already loaded) and then asserted exactly one count. Under load the first call was ready, so the count was 2. The first call was removed. No SDK change.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `docs/sdk-contract.md` section 7.2 after the stream-ends rules, add: "A `200` whose body ends without a `connected` event is a stream that dropped: `lastError` is `stream_closed`."
2. For guess 2, in `docs/sdk-contract.md` section 7.1, add one sentence: "A `304` while no ruleset is held is not a success: record it as `http_304`."
3. For guess 3, in `docs/sdk-contract.md` section 7.2, add: "An event whose data buffer is empty is not dispatched (WHATWG)."
4. For guess 4, in `docs/sdk-contract.md` section 7.1, say whether the ruleset request sets `Accept: application/json` or sends no `Accept`.
5. For guess 5, in `docs/sdk-contract.md` section 10, define `flagVersion` for a missing flag and a non-integer `version` in words, so it does not have to be read from the TypeScript source.
6. For guess 6, in `docs/sdk-contract.md` section 11, give the exact `onError` text for counts lost to a hold, or say any text is fine.
7. For guess 7, in `docs/sdk-contract.md` section 5.3, add: "`ready` after `close` answers the value it settled with; a `ready` that had not settled answers false."
8. For guess 8, in `docs/sdk-authoring/language-notes/go.md` and PROMPT.md "Entry points", say that `mise.toml` may pin a C compiler (zig) for a race detector that needs cgo, that the scripts must run under `mise exec --`, and that a `zig` in `~/.local/bin` can shadow it. Also note that `mise ls-remote go` can lag go.dev, so check go.dev for the newest release.
9. For guess 9, in `CHECKLIST.md`, the second line, add: "A test that fails only under load asserts a count it did not control: fix the test, not the load."

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every bucket pin, count equal to phase 2's: `internal/eval/vectors_test.go` `TestVectors` (11 vector files, 1371 vectors), `TestBucketPins` (17 pins), `TestCounts` prints `vectors: 1371 pins: 17`, matching the phase 2 count command.
- [x] `bin/test` 10 times in a row, and once under load: 10 runs, each exit 0 with `vectors: 1371 pins: 17`; then 3 loaded runs (12 busy loops, `nproc` 12), each exit 0 with the same counts. One earlier loaded run failed on a test bug, see Guesses 9.
- [x] The harness exits 0 with only the 2 named vector skips: `bun run conformance -- --service sdks/go/bin/test-service` printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`; `integer-accessor` is advertised, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes accepted: the harness (`privacy.json`, `kinds.json` vectors); native `TestInvalidContextsAreFallbacks`.
- [x] Result order and the mismatch rule with the SDK's codes: the harness (`evaluation.json`, `usage.json`); native `TestTypedAccessorsAndTheMismatchRule`, `TestEvaluationFailedIsOneOnError`.
- [x] `integer-accessor`: fraction is `TYPE_MISMATCH`, `2.0` is `2`: the harness `evaluation.json`; native `TestTypedAccessorsAndTheMismatchRule` (int64 limits, `2.0`, `1e20`, `1e999`), `TestWholeInt64`.
- [x] No failure makes an operation raise: the harness, every scenario; native `TestErrorCodes`, `TestAnUnusableURLIsANetworkError`, `TestOnErrorThatPanicsIsIgnored`.
- [x] `NOT_READY` at once before the first load: the harness `load.json`; native `TestNotReadyAnswersAtOnce`.
- [x] `ready` settles, bounded, fixed: the harness `load.json`, `close.json`; native `TestReadyIsBoundedByTheRequestTimeoutAndFixed`.
- [x] An answer that is not a ruleset never replaces the one held: the harness `status.json`; native `TestOnlyARulesetReplacesARuleset`, `TestErrorCodes`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: the harness `load.json`, `status.json`; native `TestLoadEvaluateAndStatus`, `TestTrailingSlashesAreRemoved`.
- [x] The stream: the harness `stream.json`, `load.json`, `status.json`; native `TestStreamConnectsAndFollowsChanges`, `TestSSEParser`.
- [x] The loop and jittered backoff: the harness `backoff.json`, `stream.json`; native `TestBackoffDelay`, `TestStreamDroppedAfterConnectingWaitsAndDoesNotPoll`, `TestStreamThatNeverConnectedPollsOnceThenRetries`.
- [x] `429` and `Retry-After` on every route: the harness `rate-limit.json`; native `TestParseRetryAfter`, `TestRetryAfterHoldsEventsAndIsCapped`, `TestRetryAfterValuesThatAreNotHolds`, `TestRetryAfterOnTheRulesetAndStreamRoutesIsOneSharedCappedHold`, `TestFirstLoadIsNotHeldByRetryAfter`.
- [x] Status fields, states, `lastError` codes: the harness `status.json` and the shape check on every status read; native `TestLoadEvaluateAndStatus`, `TestErrorCodes`.
- [x] Usage counts: the harness `usage.json`, `rate-limit.json`; native `TestUsageCountsPerFlagIndexAndDay`, `TestUsageBatchesOf500`, `TestUsageKeptOnRetryableAndDroppedOtherwise`, `TestUsageKeptBatchStopsTheFlushAndDroppedOneContinues`, `TestUsageNetworkAndTimeoutFailuresAreKept`, `TestFlushIntervalAndFlushOnClose`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: the harness `hooks.json`; native `TestHookSeesTheAnswerAndCannotChangeIt`, `TestOnErrorThatPanicsIsIgnored`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`; native `TestTwoRacingClosesBothReturn`, `TestCloseWhileTheStreamIsOpenAndQuiet`, `TestFlushIntervalAndFlushOnClose`.
- [x] No context, key or attribute in any request or message, no extra header: the harness `privacy.json` and the after-every-scenario check; native `TestUsageNeverCarriesAContext`, `TestHeaderAudit`, and the source search `TestNoRegexpAndNoContextLoggingInTheSource` (it reads every non-test `.go` file for log and print calls with a context, and finds none). The SDK writes no log line at all: `grep -rn "log\.\|fmt.Print\|os.Stderr" sdks/go --include=*.go` outside tests and the test service finds nothing.

### Native tests

- [x] Every operation from many goroutines, `close` racing `variation`, two `close` calls racing, under the race detector: `TestConcurrentUseWithCloseRacing` and `TestTwoRacingClosesBothReturn`, run under `-race` by `bin/test`.
- [x] SHOULD, TLS path: `TestTLS` (an `httptest` TLS server; the default client rejects its certificate as a `network` error, and a client trusting it, passed with `WithHTTPClient`, loads the ruleset).
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a flush hangs: `TestCloseIsBoundedWhileTheRulesetRequestHangs`, `TestCloseIsBoundedWhileAFlushHangs`, `TestEvaluationDoesNotBlockOnAHungFlush`, `TestBodyReadIsUnderTheTimeout`.
- [x] One ruleset or the other across a swap, `etag` and `flagVersion` paired: `TestSwapPairsTheETagWithItsRuleset`.
- [x] Callbacks that call back into the client do not deadlock, and `ready` from a callback returns: `TestCallbacksMayCallBackIntoTheClient`, `TestHookMayCallBackIntoTheClient`.
- [x] A silent stream is dropped as `timeout` at 60 s and a keep-alive restarts it, through a seam on a real loopback socket: `TestWatchdogEndsAQuietStreamAndAKeepAliveRestartsIt` (the seam is `withWatchdog`; the socket is a real `httptest` connection that goes quiet), and `TestOpeningAStreamIsBoundedByTheRequestTimeout`.
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes: `TestRetryAfterHoldsEventsAndIsCapped`, `TestRetryAfterOnTheRulesetAndStreamRoutesIsOneSharedCappedHold`, through the `withClock` seam.
- [x] Counts either side of UTC midnight go out as two days: `TestUsageSplitsAtUTCMidnight`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`: `TestEvaluationFailedIsOneOnError` (through the `withEvaluator` seam; also not counted, and no secret in the text).
- [x] n/a: no futures. A Go hook is a plain function; a hook that panics is `TestHookSeesTheAnswerAndCannotChangeIt` and the harness `hooks.json`.
- [x] Number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestTypedAccessorsAndTheMismatchRule` (`9223372036854774784` accepted, `9223372036854775807` read as 2^63 refused, `1e20`, `1e999`), the harness `huge` scenario.
- [x] A client never closed does not keep the process from exiting: `TestUnclosedClientDoesNotBlockExit` (a subprocess that builds a client, never closes it and exits; Go exits when `main` returns, goroutines or not).

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` links the mapping table: `DESIGN.md` section 1 holds the table, `README.md` links it and has a compact table of the same names.
- [x] Names keep the canonical meaning, defaults and spelling: `types.go` (`ReasonKind`, `ErrorCode`, `StreamState` constants carry the canonical strings), defaults in `toggleslash.go` (30 s, 30 s, 10 s, stream on).
- [x] The evaluator has no platform regex: `grep -rn regexp sdks/go --include=*.go` finds `internal/eval/pattern.go:6` (a comment saying there is none) and `test-service/main.go` (the test service's own route table, not the evaluator and not the SDK); `TestNoRegexpAndNoContextLoggingInTheSource` fails if the SDK's or the evaluator's imports ever name `regexp`.
- [x] Lint config names its rules and each omission carries a reason: `.golangci.yml` (default none, an enable list, one comment for the one omission I considered).
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none, `DESIGN.md` section 4 says so.
- [x] No telemetry and no header beyond the four: `grep -rn "Header.Set\|Header.Add" sdks/go --include=*.go` outside tests finds `Authorization` (`sync.go:152`), `If-None-Match` (`sync.go:191`), `Accept` (`sync.go:345`), `Content-Type` (`usage.go:169`). The table below is what `TestHeaderAudit` captured on a loopback listener, route by route.
- [x] Every extra is one contract 13 allows: the extras are `WithHTTPClient`, `Detail[T]`/typed accessors (the mapping), `ContextFromValue` and unexported test seams. None sends anything new.
- [x] Test seams are options, never a global or environment variable: `withClock`, `withRandom`, `withWatchdog`, `withEvaluator` in `toggleslash.go`; the SDK calls no `os.Getenv` (`grep -rn Getenv sdks/go --include=*.go` outside tests is empty).
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK and use only the pinned toolchain: each starts `#!/bin/sh`, `set -eu`, `cd "$(dirname "$0")/.."` and runs `mise exec --` when mise exists.
- [x] `test-service/README.md` maps every accessor: `bool`, `string`, `number`, `int`, `json` and `any` to a method, with the mapping-back notes.
- [x] The workflow follows the skeleton: `.github/workflows/sdk-go.yml`, path filters, every action pinned by commit SHA, `jdx/mise-action` installing `go golangci-lint zig`.

Headers `net/http` adds on its own, as captured by `TestHeaderAudit` (the SDK sets only the four above):

| Route | Headers the server saw | Added by `net/http`, and why it is not a user or machine id |
| --- | --- | --- |
| `GET /sdk/v1/ruleset` | `Accept-Encoding`, `Authorization`, `If-None-Match`, `User-Agent` | `Accept-Encoding: gzip` (transport compression, the same for every Go client); `User-Agent: Go-http-client/1.1` (library name and protocol version only, no host or user); `Host` (the URL's own authority, as HTTP requires) |
| `GET /sdk/v1/stream` | `Accept`, `Accept-Encoding`, `Authorization`, `User-Agent` | the same three; `Accept` is the SDK's `text/event-stream` |
| `POST /sdk/v1/events` | `Accept-Encoding`, `Authorization`, `Content-Length`, `Content-Type`, `User-Agent` | the same three; `Content-Length` is the body size, set because the body is a known-length reader |

`Connection` was not seen on any route (HTTP/1.1 keep-alive is the default and Go sends no header for it).

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` passes in the phase 5 check and again at the final gate.
- [x] `bun run format:check` passes (`All matched files use the correct format.`).
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints `scope ok`.
- [x] No package published, no name reserved: nothing outside the tree was touched; `README.md` says the module is not published.
- [x] Commit subject is a Conventional Commit (`chore(sdk-go): ...`, not `feat`), with no `Co-Authored-By` and no AI attribution line.
- [x] The workflow job is `runs-on: proxmox` with the same-repo `if:`, triggers `pull_request` and `push` only, installs its own toolchain; `bun scripts/check-workflows.ts .github/workflows/*.yml` printed `8 workflow(s) read, 7 name proxmox, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md` with evidence for each line: this file.
