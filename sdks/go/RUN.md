# Go SDK: run report

## Prompt version

- Prompt: 26598eee3b61a0b44edc1c6d50c5d4e4ad08b813 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run and again before phase 6; `origin/main` had not changed that path in between)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-12
- Input: language Go; runtime chosen: the `gc` toolchain on the standard library; minimum version chosen: Go 1.26; package manager chosen: Go modules (`go.mod`)
- Toolchain: `mise.toml` pins `go 1.27.4` (`go version go1.27.4 linux/amd64`), `golangci-lint 2.15.0` (`golangci-lint has version 2.15.0 built with go1.27.0`) and `zig 0.17.1` (`zig version` prints `0.17.1`). `go.mod` has no `require` line: the SDK has no dependency outside the standard library, and its tests use `testing` and `net/http/httptest` only. The bun side is the repository's (`BUN_VERSION: "1.4.2"` in the workflow).

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command printed the same two numbers)
- Conformance: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 74 passed of 74 top-level tests under `go test -race -count=1 -v ./...` (56 in the root package, 4 in `internal/evaluator`, 8 in `internal/jsonvalue`, 6 in `internal/wtf8`; the test service has none), with 34 subtests beside them. `TestVectors` and `TestBucketPins` are two of the four evaluator tests; each loops over the vectors without subtests and its case count is the vectors line above.
- `bin/test` ten times in a row, on the committed tree: 10 of 10 green (`vectors: 1371 pins: 17` each time, 9 to 15 s each). An eleventh run under load (12 busy shell loops, one per `nproc`, started before `bin/test` and killed after it): green, 19 s, same counts. The first attempt at the ten failed on run 5 (see Decisions, 14), and the ten were started again from the first after the fix.

## Decisions

1. **Toolchain and minimum version.** Go 1.27.4 pinned (the newest stable release, from `mise ls-remote go`); `go 1.26` as the minimum, because Go supports its two newest majors (1.27 and 1.26) on the run date and nothing newer than 1.26 is used. Go has no LTS line, so the long-term-support rule does not apply. `DESIGN.md`, "Inputs".
2. **Lint and format.** `go vet`, `golangci-lint` v2 with the `standard` set plus nine named linters, and `gofmt -l`, over the SDK, its tests and the test service (`.golangci.yml`; the two disabled `revive` rules carry their reason in a comment). `golangci-lint` was chosen over running the linters one by one because it is the mainstream wrapper and one configuration covers them.
3. **Test framework.** The standard `testing` package with `httptest`, over a third-party assertion or mocking library, because the SDK needs real sockets and nothing else.
4. **Toolchain action.** `jdx/mise-action` with `working_directory: sdks/go` and `install_args: go golangci-lint zig`, over `actions/setup-go`, because `bin/test` also needs Zig and one action reads `mise.toml` for all three, so the versions cannot drift apart.
5. **Zig for the race detector.** `-race` needs cgo and so a C compiler; the runner and this machine are not assumed to have one. `mise.toml` pins `zig` and `bin/test` names it by path as `CC` (`CC="$(mise where zig)/zig cc"`), so a `zig` elsewhere on PATH cannot shadow it. This is what `language-notes/go.md` suggests.
6. **Public API shape.** Functional options (`Option`) over a config struct, so a `WithStream(false)` differs from not passing it; typed accessors only (`Bool`, `String`, `Float64`, `Int`, `JSON`, each with a `Detail` variant) because Go methods cannot take type parameters; `-1` (`NoVariation`) for "no variation index" and `NoVersion` for "no flag version" in place of a pointer. `Context` is an alias of `Value`, the SDK's own JSON type, which re-exports the `Kind` constants and has `MarshalJSON` and `String`. `DESIGN.md`, section 1.
7. **`WithHTTPClient` is public** (contract 13 lists a replaceable HTTP client as an allowed extra). The default client clones the default transport with `DisableCompression` (the SDK never asks for gzip, so a stream is never decoded) and does not follow redirects (`CheckRedirect` returns `http.ErrUseLastResponse`, per `language-notes/go.md`, so the key is never sent to another place and a `3xx` is `http_3xx`).
8. **JSON.** `encoding/json` is unusable (it fails on `1e999` and replaces a lone surrogate), so `internal/jsonvalue` is a hand-written reader and writer over WTF-8 strings (`internal/wtf8`), with the WHATWG UTF-8 decoder for a response body. The nesting limit is 10000: contract 7.1 allows a limit of at least 512, and Go cannot recover from stack exhaustion. The writer writes a non-finite number as `null`, as `JSON.stringify` does; a negative exponent is written without a leading zero (`1e-7`, not `1e-07`) to match it.
9. **Patterns are compiled when the ruleset is loaded**, not at the first evaluation, so an evaluation never compiles and a malformed pattern is known at load. The matcher is a Pike VM over code points (`internal/evaluator/pattern.go`); the platform's regular expression package is not used by the SDK (the test service imports `regexp` only to route its own URLs).
10. **SSE.** A CR ends a line at once and an LF that follows it, even in the next read, is skipped; this passes `stream: a lone CR ends a line` and `stream: CRLF line ends split across reads are read` in `fixtures/sdk/stream.json`. `DESIGN.md`, section 3.
11. **Errors that carry no URL.** A `net/http` error's text carries the request URL, so an error value the SDK builds uses fixed text (`GET /sdk/v1/ruleset failed: network error`). `TestNetworkErrorTextHasNoURL`. A dial error or an `http.Client.Timeout` is a network error; only the request context's own cause (`errTimeout`) is `timeout`.
12. **Usage requests are not abandoned by `Close`**, as contract 11 requires of the final flush, so `post` builds its context from `context.Background()` with the request timeout. `TestCloseFinalFlushIsBounded`, `usage: a usage request is bounded by the request timeout, close included`.
13. **Hook event `value` for the integer accessor** is a `Value` holding the nearest double, like every other accessor, so an integer above 2^53 in the event is the nearest double (contract 10, the `value` row names the language's JSON value type). `FlagVersion` is an `int64` with `NoVersion` for none; a version at or above 2^63 is `NoVersion`.
14. **A flaky test, fixed as a test bug.** On the first attempt at the ten runs, run 5 failed in `TestCallbacksMayCallBackIntoTheClient` (`Close from OnError deadlocked`). The cause was the test: its `OnError` function read the client through an atomic pointer that is stored after `New` returns, and a ruleset that failed fast could call it first; the nil client's panic was swallowed, so the signal never came. The callback now waits on a channel closed after the store. Run 300 times under `-race` afterwards, then the ten started again.
15. **Test seams** are unexported options on `New` (`withClock`, `withRandom`, `withWatchdog`, `withEvaluator`), set by the package's tests; no global and no environment variable the SDK reads. The loops' waits and the usage day go through a small `clock`; the request timeout and the stream watchdog use real timers because a real socket needs real time.
16. **The test service** answers a typed accessor given a fallback of another kind with 400, as the protocol allows; `any` and `json` map to `JSONVariationDetail`; the `int` answer goes back through a double.
17. **Reading allowed.** Sibling SDKs under `sdks/` and their `RUN.md` files were read where the contract was silent about a Go detail; the removed Go tree and its history were not read, as the brief required.

## Guesses

None. Every point where a document might have been silent was answered by the contract, the protocol page, the language notes, a scenario or a vector. The ones I weighed, and what answered them:

- Which result wins for a flag with no variation index and a null fallback: contract 4's order (no index returns the fallback with the evaluator's reason unchanged).
- `If-None-Match` only with a ruleset and an ETag held: contract 7.1, and `fixtures/sdk/load.json`.
- A stream attempt's `lastError` and `stream` change together: contract 6, item 5 (`TestStatusNeverShowsAStreamFailureBesideConnected`).

## Stuck points

None.

## Proposed prompt changes

These are not tied to a guess (there were none); each is a fact this run learned that the next Go run would otherwise find by running.

1. In `language-notes/go.md`: "A test that hands the SDK a callback and then reads the client from a variable stored after `New` returns races the first load: the callback must wait for the store (a channel closed after it), or the nil client's panic is swallowed by the SDK and the test sees only a timeout."
2. In `language-notes/go.md`: "`golangci-lint`'s `noctx` also flags `net.Listen`; use `(&net.ListenConfig{}).Listen(ctx, ...)` in the test service."
3. In `language-notes/go.md`: "`gocritic`'s `mapKey` rejects a map key with leading or trailing whitespace; use a table of structs for a test over strings such as `\t7 `."
4. In `CHECKLIST.md`, "Test seams": the example names `cfg.idle`; this run's seams are `withClock`, `withRandom`, `withWatchdog` and `withEvaluator`. No change needed, noted so the example is not read as a required name.

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin: `internal/evaluator/vectors_test.go` reads `fixtures/evaluation/*.json` from the repository at test time; `bin/test` prints `vectors: 1371 pins: 17`, equal to the phase 2 count command.
- [x] `bin/test` ten times in a row plus an eleventh under load: Results above; 10 of 10 green, the loaded eleventh green. The tree after the fix in Decisions 14 was not changed again; `RUN.md` does not count.
- [x] The harness exits 0 with only the 2 named vector skips: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, exit code 0, run again on the final tree. `integer-accessor` is advertised, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes: the harness, `privacy.json`, `kinds.json` vectors; `TestContextConstructors`, `TestNumericAttributeMatches`.
- [x] Result order and the mismatch rule: the harness, `evaluation.json`, `usage.json`; `TestTypeMismatchAndFallbacks`, `TestNotReadyBeforeFirstLoad`.
- [x] `integer-accessor`: a fraction is `TYPE_MISMATCH` and a served `2.0` is `2`: the harness, `evaluation.json`; `TestIntRangeEdges`.
- [x] No failure raises: the harness (the service answers 500 on anything raised); `TestEvaluatorPanicBecomesEvaluationFailed`.
- [x] `NOT_READY` at once before the first load: the harness, `load.json`; `TestNotReadyBeforeFirstLoad`, `TestEvaluationDoesNotWaitOnAHungRulesetRequest`.
- [x] `ready` settles, is bounded, may be called again, and is false after `close` if unsettled: the harness, `load.json`, `close.json`; `TestReadyIsBoundedAndFixed`, `TestCloseBeforeFirstLoadSettlesReadyFalse`.
- [x] A non-ruleset answer never replaces the one held: the harness, `status.json`; `TestBadRulesetKeepsTheLastGoodOne`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining: the harness, `load.json`, `status.json`; `TestSendsIfNoneMatchOnlyWithRulesetAndETag`, `TestNoETagMeansNoIfNoneMatch`, `TestTrailingSlashOnURL`.
- [x] The stream: the harness, `stream.json`, `load.json`, `status.json`; `TestSSEParser`, `TestStreamFlipArrivesAndTagEqualEventIsNoFetch`, `TestStreamNon200AndReconnect`.
- [x] The loop and the jittered backoff: the harness, `backoff.json`, `stream.json`; `TestDelay`, `TestPollingWhenStreamOff`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`; `TestRetryAfterHoldsRulesetAndStream`, `TestUsage429HoldAndFinalFlush`, `TestParseRetryAfter`.
- [x] Status fields, states and `lastError` codes: the harness, `status.json`, and the shape check on every status read; `TestStatusCodes`, `TestCleanStreamEndLeavesLastErrorAlone`.
- [x] Usage counts: the harness, `usage.json`, `rate-limit.json`; `TestUsageFlushOnInterval`, `TestUsageBatchesOf500`, `TestUsageFailureHandling`, `TestUsageOnlyForServedValues`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: the harness, `hooks.json`; `TestHookEventAndPanic`, `TestHookBeforeReadyHasNoETag`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`, `rate-limit.json`, `usage.json`, `status.json`; `TestUsage429HoldAndFinalFlush`, `TestConcurrentCloseAndEvaluate`.
- [x] No context, key or attribute in any request or `onError` message, no header beyond the protocol's: the harness, `privacy.json` and the after-every-scenario checks; native `TestNoRequestCarriesTheContext`, `TestHeaderAudit`. The source search for logs: `TestSourceNeverMentionsTheContextWhereRequestsAreBuilt` reads the request-building files, and `grep -ln '"log"\|log/slog\|fmt.Print\|os.Stderr\|os.Stdout'` over the SDK's non-test sources (the root package and `internal/`, not the test service, which prints its `listening` line) prints nothing: the SDK writes no log line. Error values are built from fixed text and a status code (Decisions, 11).

### Native tests

- [x] Every operation from many goroutines, `close` racing `variation`, two `close` calls racing, under the race detector: `TestEveryOperationFromManyGoroutines` (every accessor, `Ready`, `Status`, a swap by the server, two `Close` calls, a 30 s bound) and `TestConcurrentCloseAndEvaluate` (eight `Close` calls racing eight evaluators), both under `go test -race` in `bin/test`.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TestTLS` (an `httptest.NewTLSServer`; the SDK is given a client that trusts its certificate through `WithHTTPClient`).
- [x] `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs: `TestEvaluationDoesNotWaitOnAHungRulesetRequest`, `TestEvaluationDoesNotWaitOnAHungUsageFlush` (`contract_test.go`; 200 evaluations in under a second each).
- [x] A reader sees one ruleset or the other across a swap: `TestSnapshotPairsETagAndVersion` (a hook's `etag` and `flagVersion` belong together under concurrent swaps); the swap is one `atomic.Pointer[snapshot]` store (`toggleslash.go`, `snapshot`).
- [x] Callbacks that call back into the client do not deadlock, `ready` from a callback returns: `TestCallbacksMayCallBackIntoTheClient` (`Ready`, `Status` and `Close` from `OnError`), `TestHookMayCallBackIntoTheClient`. The order that rules out the unsettled case: the sync goroutine settles `ready` (under `smu`, in `refresh`) before it calls `reportFromLoop`, and the flush goroutine's callbacks run only after the first sync has settled or been abandoned by `Close`; `DESIGN.md`, section 2, "`ready` from a callback".
- [x] A stream with no bytes for 60 s is dropped as `timeout` and a keep-alive restarts it, through a seam on a real loopback socket: `TestStreamWatchdogDropsAQuietStreamAndKeepAliveResetsIt` (`withWatchdog`, an `httptest` server that goes quiet; the drop reaches the socket's `Read`); `TestStreamOpenTimeout` for the open.
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes: `TestRetryAfterHoldsRulesetAndStream` (the stream, and the shared hold), `TestRetryAfterCapOnRulesetAndEvents` (ruleset and events), `TestParseRetryAfter`, with the fake clock.
- [x] Counts either side of a UTC midnight go out as two days: `TestUsageSplitsAtTheUTCDay`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call: `TestEvaluatorPanicBecomesEvaluationFailed`.
- [x] Hooks that return a future: n/a: no futures (the hook is a plain function; a panic in it goes to `onError`, `TestHookEventAndPanic`).
- [x] `integer-accessor`: a number outside signed 64 bits is `TYPE_MISMATCH`: `TestIntRangeEdges` (2^63, -2^63 - 1025, `1e999`, a fraction), and the harness `huge` scenario.
- [x] A client never closed does not keep the process from exiting: met by construction, since Go goroutines do not count when the program exits. The only background work starts in `New`: `go c.syncLoop()` and `go c.flushLoop()` (`toggleslash.go`, lines 212 and 213).
- [x] A typed accessor whose type admits null, given a null fallback, answers `TYPE_MISMATCH`: n/a: no Go accessor's type can hold null (`StringVariation` takes a `string`, `IntVariation` an `int64`); `JSONVariation` takes a `Value`, whose zero value is null, and a null fallback accepts any kind (`TestTypeMismatchAndFallbacks`).
- [x] A generic accessor with an integer fallback type: n/a: no generic method (`IntVariation` is typed, covered by `TestIntRangeEdges`).
- [x] The hook event's `value` for the integer accessor has the type the mapping table names: it is the SDK's JSON `Value` (Decisions, 13; `DESIGN.md`, section 1, `Event`); `TestHookEventAndPanic` reads `Value.AsBool`, the harness's `hooks.json` reads the number.
- [x] A context attribute of each numeric type matches as the JSON number of its value: `TestNumericAttributeWidthsAllReachTheEvaluator` (every integer width, `float32`, `float64`) and `TestNumericAttributeMatches`.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check and `README.md` has the mapping table: `DESIGN.md` section 1; `README.md`, "Mapping from the contract".
- [x] Names keep their canonical meaning and defaults, and the closed sets keep their spelling: `ReasonKind`, `ErrorCode`, `StreamState` and `LastError` constants carry the canonical strings (`types.go`); defaults 30 s, 30 s, 10 s, stream on (`toggleslash.go`, lines 31 to 33 and the options in `New`).
- [x] The evaluator has no platform regular expression: `grep -rn regexp internal/` finds only a comment in `pattern.go` saying so.
- [x] The lint configuration names its rule set and each ignored rule carries a reason: the header comment and the two `revive` rules in `.golangci.yml`.
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none (`DESIGN.md`, section 4: "Runtime: none"); `go.mod` has no `require`.
- [x] No telemetry; the headers the SDK sets are `Authorization`, `Accept`, `If-None-Match` and `Content-Type` only (`TestHeaderAudit` fails on any other the SDK sets and allows only Go's `User-Agent`, `Content-Length`; a source search of `Header.Set` in `sync.go`, `usage.go`, `toggleslash.go` finds exactly those four). The headers the client adds, one request per route captured on a loopback listener (`httptest`), key shown as `<key>`:

  | Route | Set by the SDK | Added by `net/http` | Not sent |
  | --- | --- | --- | --- |
  | `GET /sdk/v1/ruleset` | `Authorization: Bearer <key>` | `User-Agent: Go-http-client/1.1`; `Host` (from the URL, read by a handler as `r.Host`, not in the header map) | `Accept-Encoding` (compression is off), `Content-Length` (no body), `Connection` (HTTP/1.1 keeps alive without a header) |
  | `GET /sdk/v1/ruleset`, conditional | `Authorization`, `If-None-Match: "v1"` | `User-Agent`, `Host` | as above |
  | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `User-Agent`, `Host` | `Accept-Encoding`, `Content-Length`, `Connection` |
  | `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `User-Agent`, `Host`, `Content-Length: 73` (the body's size) | `Accept-Encoding`, `Connection` |

  `User-Agent: Go-http-client/1.1` names the library and a protocol version, not a host or a user. `Host` is the server's own address from the URL the application gave. `Content-Length` is the size of a body that holds no context. None identifies a user or a machine.
- [x] Every extra is one contract 13 allows: `WithHTTPClient`, `Float64`/`Int`/`JSON` accessors, the context builders and `ParseContext`, `Event`/`PanicError` types; none weakens contract 5 or sends anything new.
- [x] Test seams are unexported options on `New`, never a global or an environment variable: Decisions, 15 (`toggleslash.go`, `withClock`, `withRandom`, `withWatchdog`, `withEvaluator`).
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and run with only the pinned toolchain: `ls -l sdks/go/bin` (all `-rwxr-xr-x`); each starts with `cd "$(dirname "$0")/.."` (`bin/test-service` runs `.build/test-service`, which `bin/test` builds); CI installs only `go golangci-lint zig`.
- [x] `test-service/README.md` maps every accessor to an SDK method: the table there (`bool`, `string`, `number`, `int`, `json`, `any`).
- [x] The workflow follows the skeleton, filters on its paths and pins every action by SHA: `.github/workflows/sdk-go.yml`; the checkout and Bun lines are copied from `ci.yml`, `jdx/mise-action@b44d12e7d0d8ee7461bb893e1b1993500433185b # v5.2.7` from `sdk-python.yml`; `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.
- [x] A `status` read racing a stream failure never shows that failure's `lastError` beside `connected`, under `-race`: `TestStatusNeverShowsAStreamFailureBesideConnected` (a stream that alternates `500` and a good stream, dropped every 3 ms, with `Status` read in a loop for 1.5 s and the test failing unless both states were seen).

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes: `All matched files use the correct format.`
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints `scope ok` (run before the commit).
- [x] No package was published and no name reserved: nothing was pushed to a registry; `go.mod` names a module path only.
- [x] Commit subject is a Conventional Commit, `chore(sdk-go): ...`, none `feat`, with no `Co-Authored-By` or AI attribution line.
- [x] The job is `runs-on: ubuntu-latest` with no `if:` guard, triggers `pull_request` and `push` only, installs its own toolchain, and `check-workflows.ts` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: this file.
