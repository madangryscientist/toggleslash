# Go SDK: run report

## Prompt version

- Prompt: 8a5998785117be9a6cd1a715aef7b1981855d7cc (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run and again at phase 6; unchanged)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), run as ticket FS-96 in a T3 Code thread
- Date: 2026-11-10
- Input: language Go; runtime chosen: the Go toolchain with the standard library only (no runtime dependency); minimum version chosen: Go 1.26 (`go 1.26` in `go.mod`), developed on 1.27.3; package manager chosen: Go modules (module `github.com/madangryscientist/toggleslash/sdks/go`, package `toggleslash`)
- Toolchain: `mise.toml` pins `go = "1.27.3"` and `golangci-lint = "2.15.0"`. `go version` prints `go version go1.27.3 linux/amd64`; `golangci-lint --version` prints `golangci-lint has version 2.15.0 built with go1.27.0`. The race detector needs cgo, so a C compiler must exist (it does on this machine and on `ubuntu-latest`).

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`, equal to the phase 2 count command's `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (exit 0; the 2 skips are the two named `load:` vectors)
- Capabilities advertised: `integer-accessor`
- Native tests: 69 top-level tests passed of 69 (109 counting subtests), all under `go test -race -count=1`; also green at `-count=3`. Each of the harness's 81 scenarios and the vectors are separate from these.

## Decisions

1. Module path `github.com/madangryscientist/toggleslash/sdks/go` with the package named `toggleslash` (`go` is a keyword), over a path that ends in a name: a consumer imports it with an alias or just uses `toggleslash.`.
2. Minimum Go 1.26, over the newest toolchain: Go supports the two newest releases (1.27 and 1.26) and the SDK uses nothing newer than 1.26 (`DESIGN.md` "The minimum runtime"), nothing was built or run on 1.26 itself. CI and `mise.toml` use 1.27.3, the version on the machine.
3. No runtime dependency, over `encoding/json` plus a regexp or SSE library: the stdlib covers HTTP, SHA-256 and sync. `DESIGN.md` "Dependencies" says so.
4. A hand-written JSON reader and writer in `internal/jsonvalue`, over `encoding/json`: it errors on `1e999` and replaces a lone surrogate with U+FFFD, which the spec forbids. Strings are held as WTF-8 (`internal/wtf8`) so a lone surrogate survives and comparison is by UTF-16 code unit.
5. Typed accessors (`Bool`, `String`, `Int` as `int64`, `Float64`, `JSON`) plus `...Detail` twins returning a generic `Detail[T]`, over one generic method: Go methods cannot have type parameters, and a typed method keeps the kind check inside the SDK.
6. Options are functional (`WithStream`, `WithPollInterval`, `WithFlushInterval`, `WithRequestTimeout`, `WithAfterEvaluation`, `WithOnError`) over a config struct: the idiom, and `New` stays two required arguments.
7. Test seams (`httpClient`, `now`, `jitter`, `idle`, `evaluate`) are fields of an unexported `config` that `newConfig` builds and `start(cfg)` takes. Tests are in package `toggleslash` and reach them; no exported option, no global, no environment variable.
8. The ruleset and its ETag sit in one immutable `snapshot` behind an `atomic.Pointer`; `statusMu`, `usageMu` and `flushMu` guard the rest; no lock is held across a network call or a callback. Callbacks run on a `worker` so `Close` from inside one cannot deadlock.
9. Redirects are not followed (`CheckRedirect` returns `http.ErrUseLastResponse`, a 3xx is `http_3xx`): Go's client would otherwise resend `Authorization` on a same-host redirect.
10. A network error reports fixed text only (`network`, `timeout`), never the `*url.Error` text, which carries the URL.
11. The events route applies `Retry-After` for 429 only, as contract 9 says (the TypeScript SDK applies it more broadly; see guess 9).
12. `NoVariation = -1` for the variation index of a fallback in `Detail`, over `*int`: a plain int is easy to compare; the test service writes it as `null`.
13. `Value` is a type alias for the internal JSON value with constructors (`Null`, `Bool`, `Number`, `String`, `Array`, `Object`), so a caller can build an attribute or read a JSON flag without importing an internal package.
14. Lint: `golangci-lint` (standard set plus `errorlint`, `bodyclose`, `misspell`, `unconvert`) with `gofmt -l` and `go vet`, over `staticcheck` alone: one pinned tool covers them. `bin/lint` rewrites nothing. `usestdlibvars` was tried and dropped: it wants `http.StatusOK` for every status literal in the tests, which makes the table tests less readable.
15. CI toolchain: `jdx/mise-action` limited to `go golangci-lint` (as the Python workflow does), over `actions/setup-go` plus a golangci-lint action: one file (`mise.toml`) names both versions, where setup-go would split them across three places.
16. `bin/test-service` runs the binary `bin/test` built into `.build/` and rebuilds only when it is missing, so the harness's 30 s wait never covers a cold compile.
17. Test service state: a `map` of entries under one mutex, each entry with its own small mutex for the hook events and error messages; the handler recovers a panic into a 500 with its text.
18. An unclosed client test is a re-exec of the test binary as a subprocess (`TestUnclosedClientHelper` with an environment variable that only the test reads), over an in-process check, because only a separate process can show that it exits.

## Guesses

1. The checklist's native hook-future line is not applicable to Go and `PROMPT.md` and `CHECKLIST.md` do not say how to mark such a line. Where: CHECKLIST.md, "Native tests", the future line. Assumed: the hook is a plain synchronous func, there are no futures; the line does not apply and RUN.md says so.
2. The contract names Python and Ruby for unbounded integers, and the checklist says the `huge` scenario applies "in a language with unbounded integers too", but it is not said that a language with `int64` must also pass it. Where: sdk-contract.md section 4 "Typed languages"; CHECKLIST.md. Assumed: `integer-accessor` is advertised for Go too (PROMPT.md says "Go counts"), and the integer accessor returns `TYPE_MISMATCH` for a fraction or a value outside signed 64 bits, with `2.0` read as `2`.
3. Go adds `User-Agent: Go-http-client/1.1` (or 2.0), `Accept-Encoding: gzip`, `Host` and `Content-Length` itself, and the checklist allows "only `Authorization`, `Accept`, `If-None-Match` and `Content-Type`". Where: CHECKLIST.md, "Code and documents", telemetry line; contract 12. Assumed: those four are shown not to identify a user or a machine (see Checklist), and the source scan holds the four the SDK sets.
4. The phase 4 example commands and the `bin/` examples in `PROMPT.md` lean on Python (`python -m`, `mise exec`). Where: PROMPT.md "Entry points". Assumed: the Go scripts do the same job (find `mise.toml`, `cd` to the SDK, run native commands) with a `run` function like the Python ones.
5. The spec says `version` is "carried for the API; evaluation does not read it", but it is not said whether a flag with a missing or non-count `version` or a missing `salt` is malformed. Where: evaluation.md "Flag" table and "Errors at load" (the header and body checks). Assumed: `salt` (a string, may be empty) and `version` (a count) are required for a well-formed flag; a flag lacking either is `ERROR MALFORMED_FLAG`. No vector contradicts this; the TypeScript engine or the C# evaluator may read it differently, and this run did not compare them.
6. Duplicate keys in a JSON object, a leading byte order mark, and a ruleset body of unlimited size are not specified. Where: sdk-contract.md section 7.1 and evaluation.md. Assumed: the last value wins and the first position is kept; a leading BOM is skipped; there is no size cap on the ruleset body (the request timeout bounds the read).
7. Whether a `3xx` is `http_<status>` or follows the redirect is not said. Where: contract 7.1, 8. Assumed: not followed, reported as `http_3xx` (decision 9).
8. The CI toolchain step offers two options and says "which one is a decision" but not how to confirm the action's tool list name `golangci-lint` is a mise tool name. Where: PROMPT.md phase 5, item 4. Assumed: the tool names in `install_args` are the same as the keys in `mise.toml`; checked by reading the Python workflow, not by a CI run, which this run cannot make.
9. The TypeScript SDK applies `Retry-After` on the events route for statuses other than 429, while the contract says 429. Where: sdk-contract.md section 9 against `packages/sdk/src`. Assumed: the contract wins (decision 11), and the harness agrees (`rate-limit.json` passes).
10. The `hooks` and `onError` privacy rule says no context in `onError` messages, but a Go `error` can wrap a cause that does. Where: contract 10, 12. Assumed: the SDK's own `*Error` carries a fixed message, an operation name and a code, never a wrapped cause; a source scan (`TestNoLoggingAndNoContextInErrors`) holds that.
11. The test-service protocol says the harness waits 30 s for `listening`, but a compiled language builds in more than that cold. Where: sdk-test-service.md "Starting". Assumed: `bin/test` builds the binary as its last step, `bin/test-service` builds only if missing (decision 16). In CI the order `bin/lint`, `bin/test`, then the harness makes that work; run alone on a clean checkout the first start may be slow.
12. Go's `net/http` server does not notice a client disconnect until the handler reads or writes, so a test handler that hangs must first drain the request body. Where: not in any document; it cost this run one hung test. Assumed: every hung test handler drains the body first.

## Stuck points

None. No contract, harness, scenario or vector bug was found, and no outside help was needed.

## Proposed prompt changes

1. For guess 1, in `CHECKLIST.md`, after "Where hooks may return a future,", add: "A language whose hook is a plain function marks the line `n/a: no futures` in RUN.md."
2. For guess 2, in `CHECKLIST.md` "Native tests", change "in a language with unbounded integers too" to "in a language with `int64` or unbounded integers", and name the native test.
3. For guess 3, in `CHECKLIST.md` "Code and documents", after "finds only `Authorization`, `Accept`, `If-None-Match` and `Content-Type`", add: "plus headers the HTTP client adds on its own (`User-Agent`, `Accept-Encoding`, `Host`, `Content-Length`), listed in RUN.md with the reason each does not identify a user or a machine".
4. For guess 4, in `PROMPT.md` "Entry points", give the three scripts as language-neutral pseudocode (cd, find the pinned toolchain, run) and move the Python text into `language-notes/python.md`.
5. For guess 5, in `docs/evaluation.md` "Ruleset", add one sentence saying what an evaluator does with a flag that lacks a required field.
6. For guess 6, in `docs/sdk-contract.md` section 7.1, add: "A duplicate JSON key keeps the last value; a leading byte order mark is skipped; the SDK need not cap the body".
7. For guess 7, in `docs/sdk-contract.md` section 8, add: "A redirect is not followed; its status is the `http_<status>` code".
8. For guess 11, in `PROMPT.md` "Entry points", add: "A compiled language builds in `bin/test` and `bin/test-service` runs the built binary, rebuilding only if it is missing."
9. For guess 12, add `language-notes/go.md` with these facts, dated 2026-11-10 and this run's prompt hash: `encoding/json` errors on `1e999` and replaces a lone surrogate, so write the reader; `net/http` adds `User-Agent`, `Accept-Encoding`, `Host` and `Content-Length`; a hung test handler must drain the request body; `-race` needs cgo and a C compiler; `context.WithTimeoutCause` bounds a body read; `CheckRedirect` must return `http.ErrUseLastResponse`; `go vet` and `golangci-lint` are the pinned lint; `usestdlibvars` is noisy in table tests; `pkill -f 'go test'` kills the shell that ran it, so stop a test by its process id.

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every bucket pin: `TestVectors` and `TestBucketPins` in `internal/eval/vectors_test.go` read `../../../../fixtures/evaluation` at test time; `bin/test` printed `vectors: 1371 pins: 17`, and the phase 2 count command printed `1371 17`.
- [x] The harness exits 0 with only the 2 named vector skips: `bun run conformance -- --service sdks/go/bin/test-service` printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, exit 0. No scenario skips because `integer-accessor` is advertised.

### What the harness checks

- [x] Both context shapes: the harness (`privacy.json`, the `kinds.json` vectors) passed.
- [x] Result order and the mismatch rule: the harness (`evaluation.json`, `usage.json`) passed; native `TestTypedAccessors`, `TestEvaluatorPanicGivesFallbackAndOneOnError`.
- [x] `integer-accessor`, a fraction is `TYPE_MISMATCH`, `2.0` is `2`: harness `evaluation.json`; native `TestTypedAccessors`.
- [x] Nothing raises: harness (every scenario; the service answers 500 on a panic); native `TestHookAndOnErrorPanicsNeverReachTheCaller`, `TestAnUnusableURLIsAnetworkFailureNotAPanic`.
- [x] `NOT_READY` at once: harness `load.json`; native `TestNotReadyBeforeTheFirstLoad`.
- [x] `ready` settles, bounded, repeatable, false after close: harness `load.json`, `close.json`; native `TestReadySettlesAfterTheFirstAttemptAndIsFixed`, `TestReadyIsBoundedByTheRequestTimeout`, `TestReadyIsFalseAfterCloseIfNothingLoaded`.
- [x] A non-ruleset never replaces the one held: harness `status.json`; native `TestOnlyAJSONObjectWithFlagsAndSegmentsReplacesTheRuleset`, `TestEmptyFlagsAndSegmentsIsAValidRuleset`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`; native `TestIfNoneMatchIsTheETagAsReceivedAndOnlyWhenHeld`, `TestNoETagMeansNoIfNoneMatch`, `TestURLTrailingSlashIsStripped`.
- [x] The stream, SSE parsing, refresh on change: harness `stream.json`, `load.json`, `status.json`; native `TestSSEParser` (17 cases), `TestStreamChangedEventFetchesOnlyANewTag`, `TestConnectedEventClearsLastError`, `TestStreamNon200IsHTTPStatusAndPollsAfterwards`.
- [x] The loop and the jittered backoff: harness `backoff.json`, `stream.json`; native `TestDelayIsBoundedAndJittered`, `TestAStreamThatWasUpIsRetriedWithoutAPoll`, `TestNonPositiveDurationsFallBackToTheDefault`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; native `TestRetryAfterMustBeWholeDigits`, `TestUsageHoldAfter429AndCloseReportsTheUnsentCounts`.
- [x] Status fields, states, `lastError` codes: harness `status.json` and the shape check; native `TestHTTPFailureCodes`, `TestANetworkFailureIsNetworkAndNamesNothing`.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`; native `TestUsageIsSummedAndSentOnCloseWithTheExactBody`, `TestBatchesOfAtMost500`, `TestAFailedFlushIsKeptAndStopsTheFlush`, `TestARefusedBatchIsDroppedAndTheFlushContinues`, `TestOnlyServedResultsAreCounted`.
- [x] Hook events, `onError`, a hook that raises and an `onError` that raises: harness `hooks.json`; native `TestHookEventCarriesWhatTheCallerGot`, `TestHookAndOnErrorPanicsNeverReachTheCaller`.
- [x] `close` in order, twice at once, during a hold, with a final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`; native `TestCloseIsPromptAndLeavesNoStreamOpen`, `TestConcurrentUseUnderRace` (two racing `Close` calls), `TestNothingIsCountedAfterClose`.
- [x] No context, key or attribute in any request or `onError` message, and no extra header: harness `privacy.json` and the after-scenario checks; native `TestRequestsCarryOnlyAllowedHeadersAndNoContext`. Source search: `TestNoLoggingAndNoContextInErrors` (in `source_test.go`) walks every non-test `.go` file and fails on `log`, `fmt.Print*`, `println` or a context, key or attribute in an `&Error{... Message:}`.

### Native tests

- [x] Many goroutines at once, `Close` racing `Variation`, two `Close` calls racing, with the race detector: `TestConcurrentUseUnderRace` (16 goroutines), run under `go test -race`.
- [x] `Variation` returns within a bound while a ruleset request hangs and while a usage flush hangs: `TestVariationIsBoundedWhileTheNetworkHangs` (its handler hangs both the ruleset route and the events route, and a count is recorded for the flusher to hang on).
- [x] One ruleset or the other across a swap: `TestASwapExposesOneRulesetOrTheOther` (a hook's `etag` and `flagVersion` and the served value agree under concurrent swaps).
- [x] A hook and an `onError` that call back into the client do not deadlock: `TestCallbacksMayCallTheClient`; also `TestCloseFromInsideACallback`.
- [x] A quiet stream dropped as `timeout`, a keep-alive restarts the window, through a seam on a real loopback socket: `TestAQuietStreamIsDroppedAsATimeout`, `TestAKeepAliveRestartsTheIdleWindow`.
- [x] A `Retry-After` above 300 held for 300 s on the ruleset, stream and events routes: `TestRetryAfterAbove300IsHeldFor300OnTheRulesetAndStreamRoutes`, `TestUsageRetryAfterAbove300IsHeldFor300` (clock seam).
- [x] Counts either side of a UTC midnight go out as two days: `TestCountsEitherSideOfUTCMidnightGoOutAsTwoDays`, `TestTheDayIsUTCNotLocal`.
- [x] An evaluation that panics gives the fallback, `EVALUATION_FAILED` and one `onError`: `TestEvaluatorPanicGivesFallbackAndOneOnError`.
- [ ] n/a, hooks may return a future: Go hooks are plain synchronous funcs, there is no future; a hook that panics is `TestHookAndOnErrorPanicsNeverReachTheCaller`.
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestTypedAccessors` (the `int64` range, `-2^63`, `1e999`); harness `evaluation.json` (`huge`).
- [x] A client never closed does not keep the process from exiting: `TestAnUnclosedClientDoesNotKeepTheProcessAlive` (a subprocess re-exec). It is close to trivial in Go, where goroutines never keep a process alive after `main` returns, but it is run.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check (the mapping table has a row per operation and option); `README.md` links to the table (`DESIGN.md#the-mapping-table`).
- [x] Names keep the canonical meaning and defaults, and the reason kinds, error codes, stream states and `lastError` codes keep their spelling (`types.go`: `ReasonRuleMatch` is `RULE_MATCH`, `ErrorCode` is `NOT_READY` and so on, `SyncErrorCode` is `http_503`, `timeout`, `bad_ruleset`, `network`); the harness checks the spelling on the wire.
- [x] No platform regular expression: `TestNoPlatformRegexpInTheSDK` finds no `regexp` or `regexp/syntax` import in any non-test file; the matcher is `internal/eval/pattern_parse.go` and `pattern.go` (a Pike VM).
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none, and "Dependencies" says so; `go.mod` has no `require`.
- [x] No telemetry and no identifying header: `TestOnlyTheAllowedHeadersAreSet` finds only `Authorization` (`sync.go:188`, `sync.go:238`, `usage.go:164`), `Accept` (`sync.go:239`), `If-None-Match` (`sync.go:190`) and `Content-Type` (`usage.go:165`). Go's `net/http` itself adds `User-Agent` (the fixed string `Go-http-client/<n>`), `Accept-Encoding: gzip`, `Host` (the server the user configured) and `Content-Length`. None carries a user, a machine, a host name or a key: the User-Agent names the Go HTTP client library and is the same on every machine, the Accept-Encoding is a capability, and the other two describe the request. They are not set by the SDK and are not removable without replacing the transport.
- [x] Every extra is one contract 13 allows, and none weakens contract 5: the typed accessors and `...Detail` twins, `UserContext` / `NewContext` / `MultiContext` / `WithAttribute` constructors, the public `Value` type with `ParseJSON`, and `NoVariation`. None sends anything new.
- [x] Test seams are not a global or an environment variable: the five fields of the unexported `config` (decision 7). `TestNoEnvironmentVariablesAreRead` scans every non-test source file for `os.Getenv`, `os.LookupEnv` and `os.Environ` (comment lines skipped); there is no scan for package-level state, and the only package variables are the Value constructor aliases, the accessor tables, one sentinel error and a byte order mark, none of them written after start. The only test that reads an environment variable is `TestUnclosedClientHelper`, in a test file.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and use `mise exec` for the pinned toolchain (`ls -l sdks/go/bin`).
- [x] `test-service/README.md` maps every accessor (`bool`, `string`, `number`, `int`, `json`, `any`) to an SDK method.
- [x] `.github/workflows/sdk-go.yml` follows the skeleton, filters on its paths and pins every action by SHA (`actions/checkout` v7.0.1, `oven-sh/setup-bun` v2.2.0 and `jdx/mise-action` v5.2.7, each with its tag in a comment, the same SHAs as `ci.yml` and `sdk-python.yml`). This run cannot run the workflow; it was checked by `bunx oxfmt --check` and by reading it against the Python one.

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exit 0.
- [x] `bun run format:check` passes (`All matched files use the correct format.`, 354 files).
- [x] Only `sdks/go/` and `.github/workflows/sdk-go.yml` changed: the phase 6 scope check printed `scope ok`.
- [x] No package published, no name reserved, no tag: nothing was run outside the worktree except `git push` of branch `fs-96`.
- [x] Commit subject `chore(sdk-go): a Go server SDK built from the authoring prompt`: a Conventional Commit, not `feat`; no `Co-Authored-By` and no AI attribution line.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: the section grep printed nothing, and every line of the list above has its evidence.
