# Go SDK: run report

## Prompt version

- Prompt: 8e82c4038a3fbd545f20d583e280e70766adc2fb (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run; `origin/main` was the same commit before phase 0 and before phase 6)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-11
- Input: language Go; runtime chosen: Go toolchain from `mise` (newest `mise ls-remote go`); minimum version chosen: Go 1.26 in `go.mod`; package manager chosen: Go modules (no dependency, so `go.sum` does not exist)
- Toolchain (pinned in `mise.toml`, as the version commands print them):
  - `go` 1.27.4 (`go version go1.27.4 linux/amd64`)
  - `zig` 0.17.1 (`zig version` prints `0.17.1`; the C compiler for `-race`, which needs cgo)
  - `golangci-lint` 2.15.0 (`golangci-lint has version 2.15.0 built with go1.27.0`)
  - `bun` 1.4.2 (the harness; not pinned by this directory)

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (run twice on the final code, the same both times)
- Capabilities advertised: `integer-accessor`
- Native tests: 64 passed of 64 top-level tests (71 with subtests), under `go test -race -count=1 -v ./...`; 0 failed
- Ten consecutive `bin/test` passes and one with the CPU loaded: 10 of 10 passes (`vectors: 1371 pins: 17` each time), then 1 pass with 12 busy shell loops on 12 CPUs started before `bin/test` and killed after it (`vectors: 1371 pins: 17`)

## Decisions

1. Version lookup: `mise ls-remote go`, `mise ls-remote zig` and `mise ls-remote golangci-lint`, newest release of each (language-notes/go.md, "Pin the newest"): go 1.27.4, zig 0.17.1, golangci-lint 2.15.0. The module's minimum is Go 1.26, the oldest release Go still supports, because the SDK needs `atomic.Pointer`, `min`/`max`, `for range n` and `context.WithTimeoutCause` and nothing newer.
2. Lint and format: `go vet`, `gofmt -l` and `golangci-lint` over `go vet` alone, because golangci-lint adds `errorlint`, `bodyclose` and `noctx`, which catch exactly the mistakes an HTTP client makes (an unclosed body, a request Close cannot cancel). `usestdlibvars` is left off with a reason in `.golangci.yml`, as language-notes/go.md says. The rule set and the reason for each rule are in comments in `.golangci.yml`.
3. Test framework: the standard `testing` package with `httptest`, because it is what Go projects use and needs no dependency.
4. Toolchain in CI: `jdx/mise-action` with `install_args: go zig golangci-lint` and `working_directory: sdks/go` over `actions/setup-go`, so the workflow installs the same three versions `mise.toml` pins (phase 5 skeleton, which names `jdx/mise-action`).
5. Options: functional options (`WithStream`, `WithPollInterval`, ...) over a config struct, because the contract's defaults then need no zero-value test and the test seams (`withClock`, `withJitter`, `withWatchdog`, `withEvaluate`) are unexported options that sit beside the public ones and are not reachable from outside the package (contract 13).
6. HTTP: `DisableCompression` is on, so `Accept-Encoding: gzip` is not sent and the SDK reads the bodies as sent. `CheckRedirect` returns `http.ErrUseLastResponse`, so a 3xx is `http_3xx` (contract 7.1; language-notes/go.md). The SDK copies a caller's `*http.Client` and sets those two things on the copy.
7. Errors: every error the SDK builds carries fixed text (a code, never a `*url.Error`, which holds the URL). A recovered panic goes to `onError` as an error carrying the panic value, which is how language-notes/go.md puts it.
8. JSON: a reader of the SDK's own (`internal/jsonv`) that holds strings as WTF-8, because `encoding/json` fails `1e999` and replaces a lone surrogate (DESIGN.md section 5; evaluation.md "Reading JSON"). Its depth limit is 2048, above the 512 the contract asks for.
9. Regular expressions: the evaluator has its own `matches` parser and matcher (`internal/evaluator/pattern.go`, `pattern_parse.go`); `regexp` is not imported anywhere in the module, the test service included. `TestNoRegexpPackage` holds that. The test service used `regexp` for its route table at first; it now splits the path with `strings.CutPrefix` and `strings.Cut` (`clientRoute`).
10. Integer accessor: `IntVariation` returns `int64`. A served number that is not a whole number, or lies outside int64, is `TYPE_MISMATCH`. The largest servable integer is 2^63 - 1024, the largest float64 below 2^63 (`TestIntAccessorRange`; the harness `huge` scenario). `FlagVersion` in the hook is `*int64`, clamped at `math.MaxInt64`.
11. Concurrency (DESIGN.md section 2): a ruleset and its ETag are one `atomic.Pointer[snapshot]`; six mutexes (`stateMu`, `holdMu`, `usageMu`, `flushMu`, `closeMu`, `loopMu`), none held across a callback or a network call. The SDK's goroutines mark themselves while they run an application callback, so `Ready` and `Close` called from inside that callback do not wait for the goroutine they are running on (contract 6.4).
12. Usage counts: a map keyed by (flag, index, UTC day), taken and swapped whole by a flush and merged back by `restoreUsage` when the answer says to keep them, so a count recorded during a flush is never lost.
13. A test that raced a fixed number of rounds failed once in the ten-run loop (`TestHookETagAndVersionStayPairedUnderSwaps`: "the ruleset never swapped"). Cause: a bug in the test, not the SDK. The fake server's stream sends `connected` once and never `changed`, so with the stream on the client had no way to learn of the 38 swaps and saw them only by chance when the stream dropped. The test now polls (`WithStream(false)`), waits until `Status().ETag` is the last version, then until every worker has made a call after that, and asserts that the hook saw at least two rulesets. 15 runs in a row under `-race` pass after the fix, and the ten-run loop was started again from the first.
14. Hooks: `afterEvaluation` runs after every evaluation, including the fallback ones (`NOT_READY`, a flag not found, `TYPE_MISMATCH`, `EVALUATION_FAILED`), with the count taken first (contract 10, 4; `TestHookEventFields`). A Go hook is a plain function, so the "future" line of the checklist is `n/a`.
15. `Close` is idempotent. A second caller waits for the first's shutdown (`closeDone`); a call from the `onError` of the final flush returns at once, since that call is the shutdown (`inFinalReport`; `TestOnErrorOfTheFinalFlushMayCallClose`).
16. Layout: `internal/wtf8` is split from `internal/jsonv` because both the JSON reader and the SSE parser need the WTF-8 and BOM helpers, and the evaluator needs the code-unit comparison.

## Guesses

1. What a callback does on a goroutine the application started. Where: sdk-contract.md section 6.4 ("on the SDK's own thread"). Closest sentence: the `ready` called from inside a hook "on the SDK's own thread returns without waiting". Go has no goroutine identity and no thread notion, so the sentence does not say how an SDK on this runtime tells its own goroutine from the application's. Assumed: the SDK's goroutines mark themselves around the callback they run; a callback that starts its own goroutine and calls `Ready` or `Close` there waits like any other caller. The text-book case (the callback itself) returns at once and is tested.
2. How large `flagVersion` can be. Where: sdk-contract.md section 10, the hook event's `flagVersion`. Closest sentence: it is the flag's `version` from the ruleset; the section gives no range. A ruleset number is a float64 and can exceed int64. Assumed: the Go field is `*int64` and the SDK clamps at `math.MaxInt64`, because a caller cannot use a version beyond that.
3. Whether an empty `connected` or `changed` tag may match an empty held ETag. Where: sdk-contract.md section 7.2. Closest sentence: a tag equal to the held ETag needs no fetch. A server that sent no ETag header leaves the held tag empty, and an empty tag in the event would then "equal" it and the SDK would never fetch again. Assumed: the comparison needs a non-empty held tag, as the conditional request does (7.1).
4. Which `net/http` headers appear on the wire and where to list them. Where: CHECKLIST.md, the header line, and language-notes/go.md. Closest sentence: "Headers the HTTP client adds on its own (`User-Agent`, `Accept-Encoding`, `Host`, `Content-Length`, `Connection`) are allowed when `RUN.md` lists each". The line does not say whether a header the client adds below the request map (Go sets `Host` from the URL and does not put it in the header map a server handler reads) counts as seen. Assumed: list `Host` in the table as added by the client from the URL, even though the handler sees it in `r.Host`, and say `Connection` did not appear on loopback.

## Stuck points

None. One `ob agent register` stamp early in the run could not reach the ob API (outside this repository's scope; later stamps worked); nothing in the prompt, the contract, the harness, a scenario or a vector blocked the work.

## Proposed prompt changes

1. For guess 1, in `language-notes/go.md`, after "SDK goroutines mark themselves while running an app callback", add: "A callback that starts its own goroutine and calls `Ready` or `Close` there is not on the SDK's goroutine and waits like any caller; say so in DESIGN.md."
2. For guess 2, in `docs/sdk-contract.md` section 10 (or `PROMPT.md` phase 1, the mapping table), after "`flagVersion`", add: "In a language whose integers are fixed-width, a version above the largest integer is clamped; say so in the mapping table."
3. For guess 3, in `docs/sdk-contract.md` section 7.2, after "a tag equal to the held ETag", add: "(an empty held tag never equals an event's tag)".
4. For guess 4, in `CHECKLIST.md`, the header line, after "(capture one request of each route on a loopback listener ...)", add: "A header the runtime sets from the URL and a server handler reads outside the header map (Go's `Host`) is listed in the table too."
5. For decision 13, in `language-notes/go.md`, add: "The test fake's stream sends no `changed` event unless the test makes it; a test that expects swaps with the stream on must push the event, or use polling."

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, all pass, count equals the phase 2 command's: `bin/test` prints `vectors: 1371 pins: 17`; `bun -e '...'` (phase 2) prints `1371 17`. `TestVectors` and `TestBucketPins` in `internal/evaluator/vectors_test.go` read `fixtures/evaluation/*.json` from the repository at test time and fail on zero files or vectors.
- [x] `bin/test` ten times in a row and once with the CPU loaded: 10 passes in a row, each printing `vectors: 1371 pins: 17`, then a loaded pass with 12 busy loops (`nproc` is 12), started before `bin/test` and kept running through its build and its tests, also `vectors: 1371 pins: 17`. A first loop failed once at run 2 (decision 13); the test was fixed and the ten runs started again from the first, twice more (the second time after a test-only edit of `source_test.go` and a comment); the counts above are from the last, complete loop on the final code
- [x] The harness exits 0, only the 2 named skips: `bun run conformance -- --service sdks/go/bin/test-service` prints `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, twice; `integer-accessor` is advertised, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes: the harness (`privacy.json`, `kinds.json` through the service). The test service passes the context as the JSON it arrived as (`toggleslash.ParseJSON`).
- [x] Result order and mismatch rule: the harness (`evaluation.json`, `usage.json`).
- [x] With `integer-accessor`, a fraction is `TYPE_MISMATCH` and `2.0` is `2`: the harness (`evaluation.json`); native `TestIntAccessorRange`.
- [x] No operation raises: the harness, every scenario; native `TestManyGoroutinesWithCloseRacing`.
- [x] `NOT_READY` at once before the first load: the harness (`load.json`); native `TestNotReadyBeforeTheFirstLoad`.
- [x] `ready` settles, bounded, can be called again, false after `close` if unsettled: the harness (`load.json`, `close.json`); native `TestReadyAfterCloseBeforeTheFirstLoadIsFalse`.
- [x] An answer that is not a ruleset never replaces the one held: the harness (`status.json`); native `TestBadRulesetKeepsTheLastGoodOne`, `TestBodyWithBOMInvalidUTF8AndDuplicateKeys`.
- [x] Ruleset request, `If-None-Match`, exact ETag, URL joining: the harness (`load.json`, `status.json`); native `TestConditionalRulesetRequest`, `TestNoETagMeansNoConditionalRequest`, `TestA304WithNoRulesetIsHTTP304`, `TestRedirectIsNotFollowed`.
- [x] The stream: the harness (`stream.json`, `load.json`, `status.json`); native `TestSSEParser`, `TestSSEParserLoneCRThenLF`, `TestStreamChangedEventFetchesTheRuleset`, `TestStreamFailedFetchEndsTheStream`, `TestStreamNon200IsHTTPCode`.
- [x] The loop and the jittered backoff: the harness (`backoff.json`, `stream.json`).
- [x] 429 and `Retry-After` on every route: the harness (`rate-limit.json`); native `TestParseRetryAfter`, `TestARetryAfterOnlyLengthensAHold`, `TestA429WithoutRetryAfterKeepsCountsAndHoldsNothing`.
- [x] Status fields, states, `lastError` codes: the harness (`status.json` and the shape check on every status read).
- [x] Usage counts: the harness (`usage.json`, `rate-limit.json`); native `usage_test.go` (`TestUsageCountsOnlyServedResults`, `TestUsageIsSentInBatchesOf500`, `TestFailedFlushKeepsTheCountsForTheNext`, `TestFailedFlushOfOtherStatusDropsTheCounts`).
- [x] Hook events, a hook that raises, an `onError` that raises: the harness (`hooks.json`); native `TestHookPanicGoesToOnErrorAndOnErrorPanicIsIgnored`, `TestHookEventFields`.
- [x] `close` in order, twice at once, during a hold, final flush: the harness (`close.json`, `rate-limit.json`, `usage.json`, `status.json`); native `TestCloseUnderAHoldReportsTheLostCounts`, `TestTwoClosesRaceAndShareOneShutdown`.
- [x] No context, key or attribute in any request or `onError` message, no extra header: the harness (`privacy.json` and the checks after every scenario); native `TestRequestsNeverCarryTheContext` and `TestHeaderAudit`. Source searches for logging and context data: `TestNoLogging` in `source_test.go` fails on an import of `log` or `log/slog`, on `fmt.Print*`, `fmt.Fprint*`, `os.Stdout`, `os.Stderr`, `print` or `println` in any non-test source of the module (the test service is skipped: it prints its listening line and its own errors, and never a context); the context reaches only the evaluator and the application's own hook. Every error the SDK builds carries a fixed text and a code (`syncFailure.toError`, the `Error` values in `usage.go`), never a URL, a context or an attribute.

### Native tests

- [x] Every operation from many goroutines, `Close` racing `Variation`, two `Close` calls racing, under the race detector: `TestManyGoroutinesWithCloseRacing` (32 goroutines, every operation, 4 closers racing, ruleset swaps during the run) and `TestTwoClosesRaceAndShareOneShutdown`; `bin/test` runs everything with `go test -race`.
- [x] SHOULD, TLS: `TestTLS` (`httptest.NewTLSServer`, a client with the server's root CA through `WithHTTPClient`; the unknown-CA case yields `network`). `httptest` logs `http: TLS handshake error ... bad certificate` for the second case; it is expected.
- [x] `variation` returns while a ruleset request hangs and while a usage flush hangs: `TestVariationNeverWaitsOnTheNetwork` (both routes hang; `Close` returns within the bound).
- [x] A reader sees one ruleset or the other: `TestHookETagAndVersionStayPairedUnderSwaps` (8 workers, 38 swaps, the hook panics on an unpaired etag and version; at least two rulesets seen).
- [x] Callbacks that call back into the client, `ready` from a callback returns: `TestReentrantCallbacksDoNotDeadlock`, `TestOnErrorOfTheFinalFlushMayCallClose`.
- [x] A silent stream is dropped as `timeout`, a keep-alive restarts the 60 s, through a seam and a real loopback socket: `TestStreamWatchdogDropsASilentStream` (`withWatchdog(150ms)` and a handler that goes quiet on a real socket) and `TestStreamKeepAliveCommentsRestartTheWatchdog`.
- [x] A `Retry-After` above 300 is held 300 s on the ruleset, stream and events routes: `TestRetryAfterIsCappedAt300SecondsOnEveryRoute` (through `withClock`).
- [x] Counts either side of a UTC midnight are two days: `TestUsageSplitsAtUTCMidnight`.
- [x] An evaluation that raises: the fallback, `ERROR EVALUATION_FAILED`, exactly one `onError`: `TestEvaluationFailedReportsExactlyOnce` (`withEvaluate` makes the evaluator panic).
- [x] A hook that returns a future: `n/a: a Go hook is a plain function and returns nothing`.
- [x] A number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestIntAccessorRange` (2^63 - 1024 and -2^63 serve; `9223372036854775807`, which reads as 2^63, a value below -2^63, `1e999` and a fraction do not); the harness `huge` scenario.
- [x] SHOULD, an unclosed client does not keep the process alive: `TestUnclosedClientDoesNotKeepTheProcessAlive` runs `testdata/unclosed/main.go` (creates a client, calls `Ready`, returns without `Close`) with `go run` and requires it to exit; Go ends the process when `main` returns whatever goroutines run.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check and `README.md` has the mapping table: `DESIGN.md` section 1 is the table; `README.md` "Mapping" repeats it.
- [x] The mapping's names keep the canonical meaning, defaults, reason kinds, error codes, stream states and `lastError` spelling: the constants in `types.go` (`NotReady`, `TypeMismatch`, `EvaluationFailed`, `FlagNotFound`, `SyncTimeout`, `SyncBadRuleset`, `SyncNetwork`, `http_<status>`, the stream states `off`, `connecting`, `connected`, `disconnected`, `closed`); the harness shape check passes on every status read.
- [x] No platform regular expression in the evaluator: `TestNoRegexpPackage` (no `regexp` import in any non-test source, the test service included; the test builds the name from two pieces); `grep -rn regexp sdks/go --include=*.go` finds nothing.
- [x] The lint configuration names its rule set and each ignored rule carries a reason: `.golangci.yml` (comments beside each linter and the disabled `revive` `unused-parameter` and `usestdlibvars`); `bin/lint` prints `0 issues`.
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none (`go.mod` has no `require`); `DESIGN.md` section 4 says "None at run time" and why `encoding/json` is not used.
- [x] No telemetry, no identifying header: the source sets `Authorization`, `Accept`, `If-None-Match` and `Content-Type` and nothing else (`sync.go`, `stream.go`, `usage.go`); `TestHeaderAudit` fails on any other header the server sees except those listed below. Captured on a loopback listener, route by route (what the server saw):

  | Route | Set by the SDK | Added by `net/http` |
  | --- | --- | --- |
  | `GET /sdk/v1/ruleset` | `Authorization`, `Accept: application/json` | `User-Agent: Go-http-client/<n>`, `Host` (from the URL; the server reads it as `r.Host`, not in the header map) |
  | `GET /sdk/v1/ruleset` with `If-None-Match` | `Authorization`, `Accept`, `If-None-Match` | `User-Agent`, `Host` |
  | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `User-Agent`, `Host` |
  | `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `User-Agent`, `Host`, `Content-Length` |

  Why none identifies a user or a machine: `User-Agent` is the fixed `Go-http-client/<version>` string of the standard library (no host, no user, no SDK version of ours); `Host` is the server's own address from the URL the application gave; `Content-Length` is the size of the body. `Accept-Encoding: gzip` is not sent because `DisableCompression` is on, and `Connection` did not appear on loopback.
- [x] Every extra is one contract 13 allows: typed accessors (`BoolVariation`, `StringVariation`, `NumberVariation`, `IntVariation` and their `Detail` forms), context builders (`UserContext`, `NewContext`, `NewMultiContext`), `WithHTTPClient` (an injected HTTP client), the unexported test seams, and the `Value` helpers (`ParseJSON`, `FromAny` and the builders) that let a caller pass `1e999` or a lone surrogate. None weakens contract 5 or sends anything.
- [x] Test seams are options on `New` or the standard facility: `withClock`, `withJitter`, `withWatchdog`, `withEvaluate` in `options.go` (unexported, so reachable only from the package's tests); no global and no environment variable is read (`grep -rn Getenv sdks/go --include=*.go` finds nothing in the SDK).
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK, and need only what `mise.toml` pins: `ls -l sdks/go/bin` shows mode 755 on all three; each begins with `cd "$(dirname "$0")/.."`.
- [x] `test-service/README.md` maps every accessor to an SDK method: the table in that file (`any`, `json`, `bool`, `string`, `number`, `int`).
- [x] `.github/workflows/sdk-go.yml` follows the skeleton, filters on its paths, pins every action by commit SHA: `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.

### Repository

- [x] No em dash or en dash in any file written: `bun scripts/check-dashes.ts` prints OK (after `git add -A`, since it reads tracked files); `TestNoDashesInSources` also holds it for the Go sources.
- [x] `bun run format:check` passes.
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints `scope ok`.
- [x] No package was published and no name reserved: the module path is a path only; nothing was pushed to a registry; `go.mod` has no `require`.
- [x] Commit subject is a Conventional Commit, not `feat`, with no `Co-Authored-By` or AI attribution: `chore(sdk-go): the Go server SDK regenerated cold from the authoring prompt`; `git log -1 --format=%B | grep -ci 'co-authored\|generated with'` prints `0`.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:`, triggers only `pull_request` and `push`, installs its own toolchain (`jdx/mise-action`), and `check-workflows` passes: as above.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: the phase 6 section check prints no `RUN.md lacks`.
