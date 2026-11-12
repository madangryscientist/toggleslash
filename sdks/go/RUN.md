# Go SDK: run report

## Prompt version

- Prompt: 26598eee3b61a0b44edc1c6d50c5d4e4ad08b813 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the
  start of the run and again before the phase 6 checks; the landing rebase did not move it)
- Model: DeepSeek V4.1 Flash (`opencode-go/deepseek-v4.1-flash`)
- Date: 2026-11-12
- Input: language Go; runtime chosen: the gc toolchain, Go's own; minimum version chosen: 1.26; package manager
  chosen: Go modules with the `go` command (no package manager and no dependency)
- Toolchain: `mise.toml` pins `go = "1.27.4"` (`go version` prints `go version go1.27.4 linux/amd64`),
  `golangci-lint = "2.15.0"` (`golangci-lint has version 2.15.0 built with go1.27.0 from 25c5369a on
  2026-10-16T11:03:09Z`) and `zig = "0.17.1"` (`zig version` prints `0.17.1`; it is the race detector's C
  compiler). The harness side is Bun 1.4.2 (`bun --version`). `go.mod` declares `go 1.26.0` and has no `require`
  block, so there is no module dependency to pin.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`, and the
  phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 74 passed of 74 (65 in package `toggleslash`, 9 in `internal/jsonx`), with 9 subtests beside them
  (`TestARetryAfterAboveTheCapIsHeldForThreeHundredSeconds` has 3, `TestCountsAreKeptForARetryableAnswerAndDroppedForAnother`
  has 6). Per file: `client_test.go` 12, `concurrency_test.go` 8, `evaluator_test.go` 4, `http_test.go` 4,
  `source_test.go` 5, `sse_test.go` 7, `sync_test.go` 12, `text_test.go` 3, `usage_test.go` 8, `vectors_test.go` 2,
  `internal/jsonx/jsonx_test.go` 9. The vector test loops over 1371 cases as one test and the pin test over 17, as
  the template says.
- Repeat runs: `bin/test` passed 10 times in a row on the committed tree (`ok ... 6.421s`, `9.093s`, `6.469s`,
  `6.747s`, `6.534s`, `6.539s`, `6.223s`, `6.303s`, `6.027s`, `6.005s` for the SDK package, each with
  `vectors: 1371 pins: 17`), then an eleventh time with 12 busy shell loops (`nproc` is 12) started before it and
  running until it ended (`ok ... 6.476s`). The tree changed several times before this series (the test fixes in
  Decisions 14 to 16, the header capture, the idle-connection close), and each time the ten started again from the
  first.
- Lint: `bin/lint` prints `0 issues.` (go vet, golangci-lint, gofmt). `bun run format:check` prints
  `All matched files use the correct format.`, `bun scripts/check-dashes.ts` passes, and
  `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.

Headers captured on a loopback listener, one request per route (`go test -run TestTheHeadersOnEachRoute -v .`
prints this table):

| Route | Headers on the wire |
| --- | --- |
| `GET /sdk/v1/ruleset` | `host="127.0.0.1:<port>"`, `accept-encoding="gzip"`, `authorization="Bearer <key>"`, `user-agent="Go-http-client/1.1"` |
| `GET /sdk/v1/ruleset` (conditional) | `host`, `accept-encoding="gzip"`, `authorization`, `if-none-match="\"v1\""`, `user-agent="Go-http-client/1.1"` |
| `GET /sdk/v1/stream` | `host`, `accept="text/event-stream"`, `accept-encoding="gzip"`, `authorization`, `user-agent="Go-http-client/1.1"` |
| `POST /sdk/v1/events` | `host`, `accept-encoding="gzip"`, `authorization`, `content-length="80"`, `content-type="application/json"`, `user-agent="Go-http-client/1.1"` |

The SDK itself names only `Authorization`, `Accept`, `If-None-Match` and `Content-Type`
(`test-service` aside). `Host` is the URL's host and port, and net/http puts it in `r.Host` rather than the header
map, which is why the capture reads it from there. `Accept-Encoding: gzip` is the transport asking for compression
it decodes itself; `User-Agent: Go-http-client/1.1` is the library and its version, with no host or user in it;
`Content-Length` is the length of the usage body; `Connection` was not sent (HTTP/1.1 keeps the connection alive
unless told otherwise). None of them identifies a user or a machine. `TestTheSDKNamesOnlyTheProtocolHeaders` and
`TestTheHeadersOnEachRoute` hold this.

## Decisions

Choices the prompt left open on purpose, and points a document settles. Asked the planner: none, so every open
choice below is a decision.

1. **Go version lookup and pin.** `mise ls-remote go` listed `1.27.4` newest, and `https://go.dev/dl/?mode=json`
   agreed (`go1.27.4`, `stable: true`); `https://endoflife.date/api/go.json` (read on the run date) has 1.27 and
   1.26 in support and 1.25 end of life on 2026-08-19. Pinned 1.27.4, minimum 1.26 in `go.mod`. The notes' warning
   that `mise ls-remote` can hide releases is why the vendor page was checked too.
2. **Runtime and package manager: Go's own gc toolchain and `go` modules.** The prompt leaves the runtime open for
   Go (one implementation, no alternative); the module has no dependency, so there is nothing to pin in `go.mod`
   and `go.sum` does not exist.
3. **HTTP layer: `net/http` over a hand-written socket client.** TLS, chunked framing, HTTP/1.1 and gzip are in the
   standard library, and `context.WithTimeoutCause` plus reading the body under the same context is contract 7.1's
   "whole request, body included" with no hand-written state machine. The stream's open gets its own goroutine and
   timer, so the open is bounded by the request timeout while the reads afterwards are bounded by the watchdog
   (7.2). Redirects are not followed (`CheckRedirect` returns `http.ErrUseLastResponse`), so a 3xx is an
   `http_<status>` error and `Authorization` is never replayed. The notes list the headers this client adds; they
   are in the table above and in the checklist.
4. **Lint and format: `go vet`, `golangci-lint` and `gofmt -l` over alternatives**, the three the language's own
   tooling provides. The rule set is `default: standard` plus `errorlint`, `misspell`, `nilerr`, `unconvert` and
   `unparam`, named in `.golangci.yml`; the one exclusion is the test service's path, whose panics are the
   protocol's 500 answers. `usestdlibvars` is not enabled, as the notes say it is noisy in table tests. **Tests:
   `testing`**, which ships with Go. `golangci-lint` needs `version: "2"` at the top of its file (the v2 format),
   which is where this configuration starts.
5. **Toolchain in CI: `jdx/mise-action` over `actions/setup-go`**, limited to `go golangci-lint zig`, so one
   `mise.toml` pins every tool the `bin/` scripts call and the root `.tool-versions` (.NET and a second Bun) is
   not installed. The zig is pinned there because the race detector needs a C compiler, and `bin/test` names it by
   path (`CC="$(mise where zig)/zig cc"`), as the notes require.
6. **Accessor shape: both typed methods and one generic method.** `Variation` and `VariationDetail` take an `any`
   fallback and compare JSON kinds; `BoolVariation`, `StringVariation`, `Float64Variation` and `Int64Variation`
   (each with a `Detail` form) are the typed accessors. `Int64Variation` alone applies contract 4's integer rule:
   contract 4's "a generic method whose result has the fallback's static type (Java, C#)" does not describe Go's
   `any` result, so `Variation` with an `int64` fallback is the number kind, as Python's `variation` is.
7. **`Stream` is a `*bool` and durations are `time.Duration`.** The contract wants an option whose default is on to
   tell "not given" from false: a nil pointer is "not given", a pointer to false is off. A zero duration is "not
   given" and a negative one falls back to the default (contract 2's SHOULD), and a value above a year is capped.
8. **One atomic snapshot for the status and the ruleset.** The held ruleset, its ETag, `lastSyncAt`, the stream
   state, `lastError` and the closing flag live in one `clientSnapshot` behind an `atomic.Pointer`. A swap replaces
   the ruleset and the ETag together, `status` is one moment (contract 6.5), and no evaluation takes a lock
   (contract 6.2). Writers serialise on a mutex held for a struct copy and a store only.
9. **The SDK's own goroutines mark themselves while they run a callback.** Go has no goroutine identity, so
   "am I on my own goroutine" becomes an atomic counter around each callback on `runSync` or `runFlush`; `Ready`
   and `Close` read it and return at once instead of waiting for their own work (contract 6.4), as the notes
   describe. The unsettled-`ready` case cannot arise anyway, because a failed first load settles `ready` before it
   reports (client.go, `fail`).
10. **JSON: a hand-written reader and a WTF-8 string model.** `encoding/json` refuses `1e999` and rewrites a lone
    surrogate, so `internal/jsonx` is written to the "Reading JSON" rules: binary64 numbers, a last-value duplicate
    key, a byte order mark skipped, and the WHATWG decoder's maximal invalid subpart. The public context takes
    `map[string]any`, so a caller's `1e999` or lone surrogate survives through the same reader the test service
    uses. Nesting deeper than 2048 levels is refused as `bad_ruleset`; the contract's floor is 512 and a test reads
    512.
11. **No pattern cache.** The TypeScript engine caches compiled patterns in a map; a Go map shared by goroutines
    needs a lock, and a lock on the evaluation path is exactly what contract 6.2 forbids, so each clause compiles
    its pattern (they are short) instead.
12. **`lastSyncAt` is a `time.Time`, the zero value meaning never.** Contract 8 says "a point in time in the
    language's type"; the test service converts it to whole epoch milliseconds.
13. **An exported `Error` with a `Code`.** Contract 13 allows a logger adapter; a small error type with a
    `LastError` code lets an application route on the failure without parsing text, and it changes no guarantee.
    Its message is fixed text, never a URL, a key or anything about a context.
14. **Test fix, not a guess: the untrusted-certificate test waited for `onError` after `Ready`.** Contract 5.3 says
    `ready` and the `onError` call for one failed attempt have no set order, so the test now waits for the report
    (and then checks the code). This failed 4 runs in 10 before the fix; the ten started again from the first.
15. **Test fix: the onError-on-the-SDK-goroutine test installed its failing handler after the stream had opened.**
    The stream that was already open never failed, so the second report never came and the test timed out once in
    ten runs. The handler is installed before the client now, so each stream attempt fails at once.
16. **Test fix: the status-race test sampled `lastError` at one moment.** A stream `connected` event clears it, so
    a single sample after 200 ms could find it empty; the test now counts the failures the `onError` callback saw
    and requires at least one, so the race is exercised and the assertion does not race the loop.
17. **Test seams are fields of the package's own `config`**: `now`, `monotonic`, `jitter`, `streamIdle`,
    `roundTripper` and `raiseEvaluation`. `New` leaves them at their defaults and the tests in the package set
    them, which is contract 13's "options on create, which may be internal to the package". No global and no
    environment variable is read by the SDK (`TestTheSDKReadsNoEnvironmentVariableAndNoGlobalConfig`).
18. **A fact learned by running `-race`: `CGO_ENABLED=1` is exported beside `CC`.** With `CC` pointing at zig but
    cgo off, the race detector has no compiler; `bin/test` exports both.
19. **A fact learned by running `gofmt`: `gofmt -l .` walks the directory**, so one call covers the SDK, its tests
    and the test service, and `bin/lint` says so in its comment.
20. **A fact learned while writing the tests: a hook that evaluates a flag recurses into itself.** The hook runs
    after every evaluation, including the one it starts, so a test hook may read `Status` and `Ready` but not
    `Variation`; the `onError` callback may call all three, because it is not on the evaluation path.
21. **A fact learned while writing the tests: a fake clock shared with the SDK's goroutines needs a mutex in the
    test.** The hold's `remaining` is read from the sync goroutine while the test moves the clock, which the race
    detector catches; `fakeClock` in `sync_test.go` locks both sides.
22. **A fact from the notes, confirmed here: `net/http` reads `Host` from the request line into `r.Host`, not into
    the header map**, so the wire capture reads it there and the table says so.

## Guesses

None. Every point that a document or a fixture does not settle was a choice the prompt left open on purpose, and
those are the decisions above. No question was put to the planner.

## Stuck points

None.

## Proposed prompt changes

Each is a fact this run learned and would have liked in the notes; there are no guesses to tie them to, so each
names the decision that found it.

1. For decision 4, in `docs/sdk-authoring/language-notes/go.md`, after the "Lint" bullet, add: "`golangci-lint` 2.x
   reads its file only with `version: \"2\"` at the top; without it the run refuses the configuration, and
   `gofmt -l <dir>` walks a directory, so one call checks every root."
2. For decision 18, in `docs/sdk-authoring/language-notes/go.md`, after the "-race needs cgo" bullet, add: "Export
   `CGO_ENABLED=1` beside `CC`: with a C compiler named but cgo off, `go test -race` still has no compiler."
3. For decision 20, in `docs/sdk-authoring/language-notes/go.md`, after the "A test callback that needs the
   client" bullet, add: "A test hook may read `Status` and `Ready`; a hook that calls `Variation` recurses into
   itself, once per evaluation it starts."
4. For decision 21, in `docs/sdk-authoring/language-notes/go.md`, after the "A fake clock shared by two loops"
   bullet, add: "The test's fake clock is read from the SDK's goroutines, so its read and its move need a mutex, or
   `-race` fails on the test itself."
5. For decision 11, in `docs/sdk-authoring/language-notes/go.md`, after the "Printing the vector and pin counts"
   bullet, add: "A pattern cache shared by goroutines would put a lock on the evaluation path (contract 6.2);
   compiling each clause's pattern is cheaper and races with nothing."

## Checklist

Every line of `CHECKLIST.md`, in its order.

- [x] `bin/test` runs every vector but `buckets.json` and every pin, all pass, count equals the phase 2 command's:
  `test_vectors`, `test_bucket_pins`; `bin/test` prints `vectors: 1371 pins: 17`, the phase 2 command prints
  `1371 17`.
- [x] `bin/test` passes 10 times in a row, and an eleventh with the CPU loaded, counts in Results: ten runs,
  `ok ... 6.421s` to `9.093s`, then `ok ... 6.476s` under 12 busy loops. The test fixes made on the way are
  Decisions 14 to 16.
- [x] The harness exits 0, with only the 2 named vector skips and no scenario skipped:
  `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (with `integer-accessor`
  advertised).
- [x] Both context shapes accepted: harness `privacy.json` and the `kinds.json` vectors through the service;
  `test_vectors`; `TestAnInvalidContextIsAnErrorNotAPanic` (a multi context is accepted, eight invalid ones are
  errors).
- [x] Results follow contract 4's order and the mismatch rule, with the SDK's own error codes: harness
  `evaluation.json`, `usage.json`; `TestTheAccessors` (kinds, a null fallback, a served null, a fallback of no JSON
  kind), `TestBeforeTheFirstLoadEveryAnswerIsNotReady`.
- [x] With `integer-accessor`, a served fraction is `TYPE_MISMATCH` and a served `2.0` is `2`: harness
  `evaluation.json`; `TestTheAccessors`.
- [x] No failure of the server, the network or a context makes an operation panic: every harness scenario (the
  service answers 500 on a panic); `TestAnInvalidContextIsAnErrorNotAPanic`, `TestANetworkFailureIsReportedWithoutTheURL`,
  `TestABodyThatNeverFinishesIsATimeout`.
- [x] Evaluating before the first load answers `NOT_READY` at once: harness `load.json`;
  `TestBeforeTheFirstLoadEveryAnswerIsNotReady` (bounded at 100 ms).
- [x] `ready` settles after the first attempt, bounded, callable again, false after `close` if unsettled: harness
  `load.json`, `close.json`; `TestReadyIsFixedAtTheFirstAttempt`, `TestCloseBeforeTheFirstLoadSettlesReadyFalse`.
- [x] An answer that is not a ruleset never replaces the one held: harness `status.json`;
  `TestAnAnswerThatIsNotARulesetReplacesNothing`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining: harness `load.json`, `status.json`;
  `TestTheFirstLoadServesAndTheNextRequestIsConditional` (the tag is `"v1"`, quotes included),
  `TestTheURLIsJoinedWithoutDoublingTheSlash`.
- [x] The stream: events, SSE parsing, refresh on change, a failed refresh ends the stream: harness `stream.json`,
  `load.json`, `status.json`; all seven `TestSSE*` tests, `TestTheStreamDeliversAChangeWithoutAPoll`,
  `TestTheStreamReconnectsAfterAFailedRefresh`.
- [x] The loop and the jittered backoff: harness `backoff.json`, `stream.json`; `TestTheLoopPollsAndPicksUpAChange`,
  `TestTheStreamWatchdogDropsAQuietStream`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; `TestARetryAfterAboveTheCapIsHeldForThreeHundredSeconds`
  (ruleset, stream and events, through the clock seam), `TestARetryAfterThatIsNotWholeSecondsIsIgnored`,
  `TestAFlushDuringAHoldSendsNothing`, `TestCloseDuringAHoldSaysTheCountsWereLost`.
- [x] Status fields, states and `lastError` codes: harness `status.json` and its shape check on every status read;
  `TestTheFirstLoadServesAndTheNextRequestIsConditional`, `TestReadyIsFixedAtTheFirstAttempt`,
  `TestCloseStopsTheStreamAndThePollsAtOnce`.
- [x] Usage counts, summing, batches of 500, keep or drop by answer: harness `usage.json`, `rate-limit.json`;
  `TestCountsAreSummedPerFlagVariationAndDayAndSentOnClose`, `TestAFlushOfMoreThanFiveHundredEntriesIsSeveralRequests`,
  `TestCountsAreKeptForARetryableAnswerAndDroppedForAnother`, `TestAFlushWithNothingToSendSendsNothing`.
- [x] Hook events and `onError`, a hook that panics, an `onError` that panics: harness `hooks.json`;
  `TestTheHookEvent`, `TestAHookThatPanicsGoesToOnError`, `TestOnErrorOnAnSDKGoroutineMayReadTheClient` (the
  service's `onErrorThrows` is the harness's side of the last one).
- [x] `close` in order, twice at once, during a hold, with a final flush: harness `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`; `TestTwoClosesSendOneFinalFlush`, `TestCloseStopsTheStreamAndThePollsAtOnce`,
  `TestCloseDuringAHoldSaysTheCountsWereLost`, `TestAUsageRequestIsBoundedByTheRequestTimeout`.
- [x] No context, context key or attribute in any request or `onError` message, and no header beyond the protocol's:
  harness `privacy.json` and the after-every-scenario checks; the header table in Results;
  `TestTheSDKNamesOnlyTheProtocolHeaders`, `TestTheSDKWritesNoLogLine` (the SDK builds every error value from fixed
  text and writes no log line at all), `TestANetworkFailureIsReportedWithoutTheURL`.
- [x] Every operation of contract 2 from many goroutines, `close` racing `variation`, two `close` calls racing,
  under the race detector: `TestEveryOperationFromManyGoroutines` (variation, status, ready, close, two closes),
  `TestCloseRacingEvaluation`, `TestTwoClosesSendOneFinalFlush`; all of `go test -race ./...`.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TestATrustedCertificateWorksOverHTTPS`
  (a minted certificate trusted through a pool, verification intact), `TestAnUntrustedCertificateIsANetworkError`.
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a usage flush
  hangs: `TestAHangingRulesetRequestDoesNotHoldAnEvaluation`,
  `TestAHangingFlushDoesNotHoldAnEvaluation` (2000 evaluations each).
- [x] A reader sees one ruleset or the other across a swap, and a hook's `etag` and `flagVersion` belong to the same
  ruleset: `TestTheEtagAndTheFlagVersionComeFromOneRuleset` (40 publishes under a reader, under `-race`).
- [x] A hook and an `onError` that call back into the client do not deadlock, and `ready` from a callback on an SDK
  goroutine returns at once: `TestCallbacksMayCallBackIntoTheClient` (status and ready from a hook),
  `TestOnErrorOnAnSDKGoroutineMayReadTheClient` (status, variation and ready from `onError` on the sync goroutine),
  `TestReadyAndCloseFromOnErrorOnTheSDKGoroutine` (ready and close from it, both returning at once). The
  unsettled-`ready` case cannot arise: `fail` settles `ready` before it reports (Decision 9).
- [x] A stream with no bytes for 60 s is dropped as `timeout`, and a keep-alive restarts the 60 s, through a seam and
  a real loopback socket: `TestTheStreamWatchdogDropsAQuietStream` and `TestAKeepAliveRestartsTheWatchdog`, both
  with `cfg.streamIdle` shortened to 60 ms over httptest's loopback listener.
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam:
  `TestARetryAfterAboveTheCapIsHeldForThreeHundredSeconds` with a fake clock (`cfg.monotonic`), which checks the
  hold is exactly 300 s and passes after 301 s.
- [x] Counts either side of a UTC midnight go out as two days: `TestCountsEitherSideOfAUTCMidnightAreTwoDays`
  (`cfg.now`, 23:59:59Z then 00:00:01Z).
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call:
  `TestAnEvaluationThatRaisesIsEvaluationFailed` (the `raiseEvaluation` seam, contract 13's "an evaluator that
  raises"), which also checks nothing was counted.
- [x] A hook's future that fails later: n/a, no futures. A Go hook is a plain function and its return value is
  ignored, so there is no later failure to report (contract 10).
- [x] With `integer-accessor`, a number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: harness
  `evaluation.json` (`huge`); `TestTheAccessors` (1e20 refused, 2^63 - 1024 accepted).
- [x] A client never closed does not keep the process from exiting: met by construction, since Go goroutines never
  keep a process alive. `newClient` (client.go) is the function that starts `runSync` and `runFlush`; nothing in
  the SDK calls `os.Exit` (`TestTheSDKWritesNoLogLine`'s search).
- [x] A typed accessor whose type admits null: n/a: the accessor's type cannot hold null. `StringVariation` takes a
  `string`, `BoolVariation` a `bool`, `Int64Variation` an `int64`, so a null fallback cannot be given; the generic
  `Variation` with a nil fallback accepts any kind, which `TestTheAccessors` covers.
- [x] A generic accessor with an integer fallback type: n/a for the integer rule, because Go's generic method has no
  static fallback type (Decision 6); `Variation` with an `int64` fallback is the number kind, and
  `Int64Variation` applies contract 4's rule (`TestTheAccessors`). A fallback of a class with no JSON kind gives the
  fallback and `TYPE_MISMATCH`: the `[]string` fallback assertion in `TestTheAccessors`.
- [x] The hook event's `value` for the integer accessor has the type the mapping table names: `int64`, from
  `TestTheHookEvent` (flag `number` served 42).
- [x] A context attribute of each numeric type matches as the JSON number of its value:
  `TestEveryNumericTypeInAContextIsANumber` (int, int8 to int64, uint to uint64, float32, float64, and `true`,
  +Inf and NaN which must not match).
- [x] `DESIGN.md` passes the phase 1 row check and `README.md` links to the mapping table: the check prints
  `phase 1 row check: ok`; README.md's "The API in one table" links to `DESIGN.md#1-the-mapping-table-contract-2`.
- [x] The names in the mapping table keep the canonical meaning and defaults, and the closed sets keep their
  spelling: DESIGN.md section 1, and the string types `ReasonKind`, `ErrorCode`, `StreamState`, `LastError` in
  types.go whose constants are the contract's spelling (`LastError("http_429")` for a status).
- [x] The evaluator has no regular expression from the platform: `TestTheSDKImportsNoRegularExpressionPackage`
  (every non-test file under the SDK), and `pattern.go` is the hand-written parser and Pike VM.
- [x] The lint configuration names the rule set it enables, and each rule it ignores carries a reason:
  `.golangci.yml` names `default: standard` plus five linters in comments; its one exclusion is a path (the test
  service), with the reason beside it.
- [x] Every runtime dependency has a line in `DESIGN.md` saying why the standard library will not do: there is none;
  DESIGN.md section 4 says so and `go.mod` has no `require` block.
- [x] No telemetry and no header that identifies a user or a machine: the header table in Results, captured on
  loopback route by route; `TestTheSDKNamesOnlyTheProtocolHeaders`, `TestTheSDKReachesOnlyTheThreeRoutes` (no other
  route and no other host), `TestTheSDKWritesNoLogLine`.
- [x] Every extra beyond the contract is one contract 13 allows: typed accessors and the generic one, the `Error`
  type with a code, and the package's own config seams; none weakens contract 5 or sends anything new (nothing else
  is ever sent: `TestTheSDKReachesOnlyTheThreeRoutes`).
- [x] Test seams are options on `create` or the package's own config, never a global or an environment variable:
  Decision 17; `TestTheSDKReadsNoEnvironmentVariableAndNoGlobalConfig`.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and run with only what
  `mise.toml` pins: `ls -l sdks/go/bin` shows mode 755 for all three; each starts `#!/bin/sh`, `set -eu`, then
  `cd "$(dirname "$0")/.."`, and goes through `mise exec --` when mise is on PATH.
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method: its accessor table (`bool`,
  `string`, `number`, `int`, `json` and `any`).
- [x] `.github/workflows/sdk-go.yml` follows the skeleton, filters on its paths and pins every action by commit SHA:
  checkout (`3d3c42e5...`, v7.0.1), setup-bun (`0c5077e5...`, v2.2.0) and mise-action (`b44d12e7...`, v5.2.7) each
  carry a 40-character SHA with the version in a comment; `check-workflows.ts` prints `8 workflow(s) read, 0
  failure(s)`.
- [x] Languages with threads: a `status` read racing a stream failure never shows that failure's `lastError` beside
  `stream: connected`: `TestStatusNeverShowsConnectedBesideAStreamError`, four readers while the stream fails and
  reconnects, under `-race`.
- [x] No em dash or en dash in any file written: `bun scripts/check-dashes.ts` passes.
- [x] `bun run format:check` passes: `All matched files use the correct format.`
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints `scope ok`.
- [x] No package was published and no name reserved: nothing was uploaded, `go.mod` names a path in this repository
  and no `go install` or `go get` was run against a proxy.
- [x] Commit subjects are Conventional Commits, none `feat`, with no `Co-Authored-By` or AI attribution: one commit,
  `chore(sdk-go): the Go server SDK regenerated cold from the authoring prompt`.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:` guard, triggers only `pull_request` and `push`,
  installs its own toolchain (`mise-action`), and `check-workflows.ts` passes: `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of this list: this file.
