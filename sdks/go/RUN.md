# Go SDK: run report

Sweep run 16 (FS-150), Go regenerated cold. The removed `sdks/go` and its workflow were not read, in the
tree or in git history. Sibling SDKs and the TypeScript SDK were read where a point needed them.

## Prompt version

- Prompt: `01bdb4874f46968087cb4c357d0f73239f03cf42` (`git log -1 --format=%H -- docs/sdk-authoring`, main after FS-149)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-12
- Input: language Go; runtime chosen: Go 1.27.4 toolchain; minimum version chosen: Go 1.26 (`go.mod`);
  package manager chosen: Go modules (nothing is published, so no registry)
- Toolchain: `mise.toml` pins `go = "1.27.4"` (prints `go version go1.27.4 linux/amd64`),
  `golangci-lint = "2.15.0"` (prints `golangci-lint has version 2.15.0 built with go1.27.0`) and
  `zig = "0.17.1"` (prints `0.17.1`, the C compiler for the race detector). Bun 1.4.2 (the repository's
  `BUN_VERSION`) runs the harness.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 72 passed of 72 top-level tests (83 with subtests, none failing), under `-race`:
  the SDK package 53, `internal/evaluator` 3, `internal/jsonvalue` 11, `internal/wtf8` 5. The test service
  (`test-service`, `package main`) has no test files; the harness exercises it.
- Stability (checklist, "passes 10 times in a row"): ten consecutive `bin/test` runs on the committed
  tree, all exit 0 with `vectors: 1371 pins: 17`; then an eleventh with the CPU loaded (12 busy shell loops,
  `nproc` is 12, started before `bin/test` and killed after it, build included): exit 0, same counts. No
  flake, so the ten never restarted. The tree did not change after those runs except this file.
- Lint: `bin/lint` prints `0 issues.` (gofmt clean, `go vet` clean).

## Decisions

1. Lint and format: `gofmt -l`, `go vet` and golangci-lint v2 over `go vet`'s own set, with the named
   linters in `.golangci.yml` (bodyclose, contextcheck, errorlint, noctx, nilerr, gosec, misspell, unconvert,
   unparam, revive, predeclared, copyloopvar) on top of golangci-lint's standard set, because the SDK handles
   keys and network input and these catch leaked bodies, lost contexts and error comparison slips. Each
   exclusion carries its reason in the file: gosec and unparam in tests (throwaway fixtures); gosec in the
   test service (loopback tool); contextcheck in the test service and `usage.go` (the final flush is detached
   from the lifetime context on purpose, DESIGN.md section 2); gosec G404 for reconnect jitter in
   `toggleslash.go` (spread, not a secret); gosec G115 in `internal/wtf8` and `internal/jsonvalue` (masked or
   range-checked narrowing); revive `unused-parameter` in tests (handler signatures).
2. The typed accessors are methods, with one generic function `variation[T]` behind them (`client.go`),
   because Go methods cannot have type parameters. The order is the contract's section 4.
3. `StringVariation` maps a lone surrogate in the served string to U+FFFD: a Go `string` is bytes, and the
   public value must be valid UTF-8 text. `JSONVariation` keeps the lone surrogate in the `Value`
   (DESIGN.md section 5).
4. The HTTP transport is a clone of the default with `DisableCompression = true` (so no `Accept-Encoding`
   is sent: the header list is exactly the protocol's plus the three the client adds, see the table below)
   and `CheckRedirect` returning `http.ErrUseLastResponse` (a redirect is an HTTP status, not followed).
5. `SyncError` is an exported type (`Code`, `Usage`), so a caller of `WithOnError` can `errors.As` it. Its
   text is fixed: the code and nothing else (`TestSyncErrorTextHoldsOnlyTheCode`).
6. Usage requests (`POST /sdk/v1/events`) use `context.Background()` with the request timeout, not the
   lifetime context, so `Close` can wait for an in-flight flush and the final flush can run after the
   lifetime is cancelled. Close stays bounded by about twice the request timeout
   (`TestCloseReturnsWithinTwiceTheTimeoutWhenTheServerHangs`).
7. `Close` does not wait for a goroutine that is inside an application callback: each loop sets an atomic
   marker (`syncCB`, `flushCB`) around `onError`, and `waitFor` polls the done channel and the marker
   together. This is what contract section 6, item 4 asks (a `close` on the SDK's own thread SHOULD NOT wait
   for the work that thread is doing); `TestCloseFromInsideACallbackDoesNotDeadlock`.
8. The nesting limit of the JSON parser is 10000 levels; the contract's "Bounds" asks for at least 512. It
   is there because a Go stack overflow is fatal, not recoverable (`TestNestingLimit`).
9. `FlagVersion` follows the TypeScript engine's `versionOf` rule (`packages/engine/src`): the version of a
   flag in the ruleset, absent for a missing or malformed flag, absent above the largest `int64`.
   `internal/evaluator/flagversion_test.go`.
10. The stream has one `time.AfterFunc` timer, first the open bound and then the idle watchdog, reset by any
    bytes, a fetch pauses it (contract 7.2, "Bounds"). Retry-After is read on 429 only, digits only, and
    capped at 300 s (contract 7.4); the ruleset and stream share one hold (the sync goroutine only), events
    have their own under `flushMu`.
11. The toolchain step in the workflow is `jdx/mise-action` (as the other SDK workflows do), not
    `actions/setup-go`, because zig and golangci-lint come from the same `mise.toml` as go. It is limited to
    `go golangci-lint zig`, or it would also install the root `.tool-versions` (a second Bun, .NET).
12. The race detector needs cgo and the prompt says no C compiler is assumed, so `mise.toml` pins `zig` and
    `bin/test` sets `CC="$(mise where zig)/zig cc"` by path. Build output goes in `.build/` (ignored).
13. The zig pin: the `gh api` tag lookup gave a stale tag for the newest release, so the version
    was taken from `https://ziglang.org/download/index.json` and checked with `mise ls-remote zig`.
14. Test seams are unexported options of the package's own config (`withClock`, `withJitter`, `withIdle`,
    `withTLSConfig`, `withEvaluate`, `toggleslash.go`), set only by tests, never a global or an environment
    variable (contract 13).
15. The test service is `package main` in the SDK's own module (`test-service/`), so `go vet ./...` and
    `go build ./...` cover it and the SDK needs no second module.
16. `sdks/go/internal/...` packages: `jsonvalue` (parser and `Value`), `wtf8` (WTF-8 and UTF-16 order),
    `evaluator` (the spec). No third-party dependency, no `go.sum` (DESIGN.md section 4).

## Guesses

None. Each point I stopped on was settled by the contract, the protocol page, the checklist, `language-notes/go.md`
or the TypeScript SDK, and is recorded above as a decision with where it was settled.

## Stuck points

None.

## Proposed prompt changes

None.

## Checklist

The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equals phase 2's: `bin/test` prints
  `vectors: 1371 pins: 17` (`TestVectors`, `TestBucketPins` in `internal/evaluator/vectors_test.go`, read
  from `fixtures/evaluation/` at test time).
- [x] Ten runs in a row plus one under CPU load: see Results, "Stability". All eleven exit 0 with
  `vectors: 1371 pins: 17`.
- [x] The harness exits 0 with only the 2 named vector skips: `scenarios: 81 passed, 0 failed, 0 skipped;
  vectors: 1369 passed, 0 failed, 2 skipped`, exit 0 (`bun run conformance -- --service sdks/go/bin/test-service`).

What the harness checks (all met when the harness passes; named per line)

- [x] Both context shapes: harness `privacy.json` and `kinds.json` vectors.
- [x] Result order and the mismatch rule with the SDK's codes: harness `evaluation.json`, `usage.json`.
- [x] `integer-accessor`: harness `evaluation.json`; `TestIntVariationRange`.
- [x] No operation raises: the test service answers 500 on a panic, no scenario failed
  (`TestCallbackPanicsAreContained`).
- [x] `NOT_READY` at once: harness `load.json`; `TestNotReadyAndMismatch`.
- [x] `ready` settles, bounded, repeatable: harness `load.json`, `close.json`; `TestReadyAndVariation`.
- [x] A non-ruleset never replaces the held one: harness `status.json`; `TestBadRulesetKeepsTheOneHeld`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`;
  `TestConditionalRequestAndNotModified`, `TestNewWithTrailingSlashAndNilOption`.
- [x] The stream: harness `stream.json`, `load.json`, `status.json`; `TestStreamConnectsAndFollowsChanges`,
  `TestStreamFailedFetchEndsTheStream`, `TestSSEParserRules`.
- [x] Loop and jittered backoff: harness `backoff.json`, `stream.json`; `TestStreamBackoffDelayFormula`,
  `TestPollingIntervalOnAFakeClock`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`.
- [x] Status fields, states, `lastError` codes: harness `status.json`; `TestInitialStatus`,
  `TestStreamHTTPErrorIsReported`.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`; `TestUsageCountsPerFlagIndexAndDay`,
  `TestAtMost500EntriesPerRequest`, `TestUsageResultTable`.
- [x] Hook events and `onError`: harness `hooks.json`; `TestCallbackPanicsAreContained`.
- [x] `close` in order, twice, during a hold, with a final flush: harness `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`; `TestRetryAfterHoldsUsageAndFinalFlushReportsIt`.
- [x] No context, key or attribute in a request, an error or a log line: harness `privacy.json` and the
  after-scenario checks; `TestNoContextInLogsOrErrors` (`usage_test.go`). Source search from `sdks/go`:
  `grep -rn '"log"\|log\.\|slog\|fmt\.Print\|fmt\.Fprint' --include=*.go . | grep -v _test.go` finds only
  `test-service/main.go` (its own start-up lines: the listen address and two fatal errors, no context).
  Errors are built in three places only: `usage.go:125` (a count of unsent entries), `internal/jsonvalue/parse.go:16`
  (`ErrSyntax`, a fixed text, `TestErrorCarriesNoInput`) and `client.go:20` (`errTimeout`). `panicError`
  (`client.go`) wraps the application's or the evaluator's own panic value; the evaluator panics only with
  its own `invalidPattern{}` (`pattern_parse.go:49`), which it recovers and otherwise re-raises
  (`pattern_parse.go:336`), so a panic value is a Go runtime error, never a context's text.

Native tests

- [x] All operations from many goroutines, `close` racing `variation`, two closes racing, no error, deadlock or
  race, under `-race`: `TestManyGoroutinesAllOperations`, `TestCloseRacingVariation`,
  `TestCloseIsIdempotentAndRacing` (all under `go test -race`, `zig cc` as the C compiler).
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TestTLSWithALocalCertificate`
  (`sync_test.go`).
- [x] `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs:
  `TestVariationDoesNotBlockOnAHungRuleset`, `TestVariationDoesNotBlockOnAHungFlush`.
- [x] A reader sees one ruleset or the other across a swap, `etag` and `flagVersion` from the same one:
  `TestSwapAtomicityOfEventETagAndVersion` (the snapshot is one `atomic.Pointer`, DESIGN.md section 2).
- [x] SHOULD: callbacks may call back into the client, and `ready` from the SDK's own goroutine returns without
  waiting: `TestHooksAndCallbacksAreReentrant`, `TestOnErrorMayCallClientMethods` (`onError` runs on the sync
  goroutine and calls `Ready`), `TestCloseFromInsideACallbackDoesNotDeadlock`.
- [x] A stream that gets no bytes for 60 s is dropped as `timeout`, a keep-alive restarts it, through a seam on
  a real quiet loopback socket: `TestStreamWatchdog` (`withIdle(250ms)`, seven keep-alives, then silence;
  default `defaultIdle = 60 * time.Second`, `toggleslash.go`).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam:
  `TestRetryAfterOnRulesetHoldsAndCaps`, `TestRetryAfterCapOnRulesetAndStreamRoutes`,
  `TestRetryAfterCapOnEventsRoute` (`maxRetryAfter`, `sync.go:18`).
- [x] Counts either side of a UTC midnight go out as two days: `TestUTCMidnightGivesTwoDays`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`:
  `TestEvaluationFailedGivesFallbackAndOneOnError`.
- [x] n/a: no futures. A hook is a plain function; a hook that panics goes to `onError`
  (`TestCallbackPanicsAreContained`).
- [x] With `integer-accessor`, a served number outside signed 64 bits is `TYPE_MISMATCH`: `TestIntVariationRange`
  (the `int64` range), harness `huge`.
- [x] A client that is never closed does not keep the process from exiting: met by construction, Go goroutines
  never keep a process alive; the only goroutines the client starts are the two in `startLoops`
  (`client.go:122`).

Code and documents

- [x] `DESIGN.md` has the mapping table (section 1) and `README.md` links to it; every row of the contract's
  names has a line there.
- [x] Names keep the canonical meaning and defaults; reason kinds, error codes, stream states and `lastError`
  codes keep their spelling (`types.go`: `ReasonKind`, `ErrorCode`, `StreamState`, `SyncErrorCode`).
- [x] The evaluator has no platform regex: `grep -rn 'regexp' sdks/go --include=*.go` finds only
  `test-service/main.go:14,37` (the test service's own route table, not the evaluator); `internal/evaluator`
  has its own pattern matcher (`pattern.go`, `pattern_parse.go`).
- [x] The lint configuration names its rule set and every ignore has a reason beside it (`.golangci.yml`).
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none (section 4); no `go.sum`.
- [x] No telemetry, and request headers are only the protocol's: `grep -rn 'Header\.\(Set\|Add\)' --include=*.go`
  finds `Authorization` (`sync.go:40`), `Accept` (`sync.go:102`, `sync.go:270`), `If-None-Match` (`sync.go:104`)
  and `Content-Type` (`usage.go:171`). Captured on a loopback listener (a throwaway program outside the
  tree), one request per route; `TestHeadersSentOnEveryRoute` holds the allowlist.

  | Route | Set by the SDK | Added by `net/http` |
  | --- | --- | --- |
  | `GET /sdk/v1/ruleset` | `Authorization`, `Accept: application/json` | `Host` (from the URL), `User-Agent: Go-http-client/1.1` |
  | `GET /sdk/v1/ruleset`, conditional | the above and `If-None-Match` | `Host`, `User-Agent: Go-http-client/1.1` |
  | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `Host`, `User-Agent: Go-http-client/1.1` |
  | `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `Host`, `User-Agent: Go-http-client/1.1`, `Content-Length` |

  `User-Agent` names the library's runtime (`Go-http-client/1.1`), not the host or the user. `Host` is the
  server's own address, read by a handler outside the header map as `r.Host`. `Content-Length` is the body
  size. There is no `Accept-Encoding` (`DisableCompression`) and no `Connection` header.
- [x] Every extra beyond the contract is an idiomatic one that contract 13 allows: typed accessors, context builders and the unexported seams are on its list; the exported `SyncError` type and `ReadyContext` are the same kind (they add no request and weaken nothing in contract 5).
- [x] Test seams are options on the package's own config, not globals or environment variables (decision 14).
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and use only the
  `mise.toml` toolchain (`bin/lint` runs through `mise exec --`; `bin/test` names zig by path).
- [x] `test-service/README.md` maps every accessor (`bool`, `string`, `number`, `int`, `json`, `any`).
- [x] `.github/workflows/sdk-go.yml` follows the skeleton, filters on its paths, pins every action by commit SHA.

Repository

- [x] No em or en dash in a file written: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes (`mise.toml`, `.golangci.yml` and the workflow formatted with `bunx oxfmt`).
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints `scope ok`.
- [x] No package was published and no name reserved.
- [x] The commit subject is a Conventional Commit of type `chore`, with no `Co-Authored-By` and no AI attribution.
- [x] The workflow job is `runs-on: ubuntu-latest`, no `if:`, triggers `pull_request` and `push` only, installs
  its own toolchain; `bun scripts/check-workflows.ts .github/workflows/*.yml` reports `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line above.
