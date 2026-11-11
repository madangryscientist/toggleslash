# Go SDK: run report

## Prompt version

- Prompt: dfdbb0542f6634ba9dd8310ce97ea7e7dfe702a5 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the
  start of the run)
- Model: Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-11
- Input: language Go; runtime chosen: the Go toolchain 1.27.4; minimum version chosen: Go 1.26 (the previous
  release, still in support on the run date; the SDK uses `for range n`, `atomic.Pointer`, `context.WithTimeoutCause`
  and `t.Context`, all in 1.24 or earlier, so 1.26 is a support choice, not a feature floor); package manager:
  Go modules, with no dependencies (`go.mod` has no `require`).
- Toolchain (`sdks/go/mise.toml` pins, and what the version commands print):
  - `go = "1.27.4"`: `go version` prints `go version go1.27.4 linux/amd64`. It is the newest release: the
    vendor's `go.dev/dl/?mode=json` lists 1.27.4 first, and `mise ls-remote go | sort -V | tail` ends on it
    (an unsorted `mise ls-remote go` does not end on the newest, as `language-notes/go.md` warns).
  - `zig = "0.17.1"`: `zig version` prints `0.17.1`. It is the C compiler `go test -race` needs; `bin/test` names
    it by path as `CC="$(mise where zig)/zig cc"`.
  - `golangci-lint = "2.15.0"`: prints `golangci-lint has version 2.15.0 built with go1.27.0`.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the
  phase 2 count command prints `1371 17`).
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 74 passed of 74 top-level tests (`go test -race -json ./...`), with 61 subtests passed of 61.
  The 74 include the evaluator's `TestVectors` and `TestBucketPins`, the `wtf8` and `jsonvalue` tests.
- Stability: `bin/test` passed 10 times in a row, and once more with 12 busy shell loops (`nproc` is 12) started
  before it and kept running until it ended: 11 of 11 exit 0, each printing `vectors: 1371 pins: 17`.
  The first series of ten failed on its run 3 with a data race in `TestBackoffDelay` (below, Decisions 17); the
  ten runs were started again from the first after the fix, and the numbers above are that second series.
- Defects found after the first green harness, all fixed in this run: (a) every non-number answer was `null`
  because the test service called `jsonvalue.Value.IsFinite`, which is true for a number only (the method name
  suggests it is true for any finite value; the harness caught it); (b) the SSE parser dropped a line or event
  over 1 MiB, which contract 7.2 forbids (found in phase 6, removed with its test); (c) `TestBackoffDelay` wrote
  `c.cfg.random` after `New` had started the sync goroutine (a race the race detector found on a loaded run);
  (d) `DESIGN.md` said `Ready` consults the callback mark and carried a "short grace" the code does not have
  (corrected to the code); (e) `DESIGN.md` named the wrong output directory for the test service binary.

## Decisions

1. `docs/sdk-authoring/language-notes/go.md` exists and was read (PROMPT.md phase 0, item 9). Its points are
   applied and named where they matter below.
2. Toolchain pin and minimum: Go 1.27.4 (newest, looked up as above) to develop and test on; the module declares
   `go 1.26` (previous release, in support on 2026-11-11). PROMPT.md phase 0 (pin the exact current stable
   release) and phase 1 item 6 (the minimum and why).
3. `zig` is pinned in `mise.toml` as the C compiler for `-race` (go.md, "Toolchain"; PROMPT.md "Entry points":
   "if a race detector needs one, `$L/mise.toml` may pin it, `bin/test` calls it by path"). `bin/test` and the
   README say so, and the workflow installs it.
4. The workflow installs the toolchain with `jdx/mise-action` (`install_args: go zig golangci-lint`), the
   skeleton's step, over `actions/setup-go`: one file (`mise.toml`) then names every tool (go.md).
5. Lint: `go vet`, `gofmt -l` and `golangci-lint` v2 (`.golangci.yml`: the `standard` set plus `bodyclose`,
   `errorlint`, `gocritic`, `misspell`, `noctx`, `unconvert`, `unparam`, `copyloopvar`, `intrange`, `nilerr`,
   `predeclared`, `revive` with a named rule list). `usestdlibvars` is left out (go.md: it wants `http.StatusOK`
   for every literal in a table test) and the reason is in a comment at the top of the file. No rule is ignored
   with a directive in the source and the config has no `exclude` list, so no ignore needs a reason.
6. The module is `github.com/madangryscientist/toggleslash/sdks/go` and the package `toggleslash` (PROMPT.md
   phase 1 item 7: "use this repository's `github.com/madangryscientist/toggleslash/sdks/<lang>`" and "name the
   package `toggleslash`" where `<lang>` is `go`). Nothing is tagged or registered.
7. The HTTP client is `net/http` with a cloned default transport that sets `DisableCompression` (so no
   `Accept-Encoding` is sent) and `CheckRedirect` returning `http.ErrUseLastResponse` (go.md), so a `3xx` is
   `http_3xx` and the `Authorization` header is never resent. The default `User-Agent` is kept and listed in the
   header audit (Checklist, Code and documents).
8. JSON: the SDK has its own reader and writer in `jsonvalue` (go.md; evaluation.md "Reading JSON"), keeping
   lone surrogates as WTF-8 (`internal/wtf8`) and refusing nesting deeper than 4096 levels (`DESIGN.md`, section
   5). `encoding/json` cannot hold a lone surrogate or `1e999`.
9. Errors are fixed text: a `*url.Error` carries the URL, so the SDK never passes one on and builds a `*SyncError`
   with a route name and a code (go.md; contract 12). `context.WithTimeoutCause` covers the body read, so a body
   that never ends is a `timeout` (go.md; `TestABodyThatNeverEndsIsATimeout`).
10. SSE has no cap on a line or an event (contract 7.2: "There is no cap on the length of a line or the size of an
    event; the watchdog and `requestTimeout` are the bounds. An SDK adds none of its own"). My first draft had a
    1 MiB cap; phase 6 found it against that sentence and removed it, with its test.
11. `Ready` settles after the first load records its error and before it calls `onError` (contract 5.3: "`ready`
    MAY settle first"; go.md), so a callback that calls `Ready` never waits for itself. `Close` is the call a
    callback may make that could wait: SDK goroutines mark themselves while they run an application callback and
    `Close` does not wait for a marked goroutine (go.md, contract 6.4). Because Go has no goroutine identity the
    mark belongs to the SDK goroutine and not to the caller: an application goroutine that calls `Close` while a
    callback runs also does not wait for that goroutine, which ends when the callback returns. That is the
    price of having no identity, and `TestCallbacksMayCallBackIntoTheClient` and `TestAHookMayCallClose` hold the
    deadlock-free part.
12. A recovered panic goes to `onError` as a `*PanicError` carrying the panic value (go.md). The value is the
    application's own (a hook that panicked), never the evaluation context; a panic inside `onError` is dropped.
13. `flagVersion` above 2^63 minus 1 is reported as none (docs/sdk-contract.md, hook event table: "an SDK that
    carries `flagVersion` as a signed 64-bit integer MAY report a version above 2^63 minus 1 as none").
14. A `429` on the first load holds the ruleset route like any other (contract 7.4, "every route"; harness
    `rate-limit.json`).
15. Test service layout: a `package main` in `sdks/go/test-service/`, in the same module, compiled to
    `sdks/go/.build/test-service`. `bin/test` builds it last and `bin/test-service` builds it only when missing
    (PROMPT.md "Entry points": "builds it only when it is missing (the harness waits 30 s for `listening`...)").
    The consequence is that `bin/test-service` runs a stale binary after a source change until `bin/test` runs;
    the phase 4 check runs `bin/test` first.
16. Test seams are unexported fields of the SDK's `config`, set by a test through a plain `func(*config)` option
    in the package's own tests (`watchdog`, `retryAfterCap`, `random`, `now`, the HTTP client): contract 13 and
    PROMPT.md phase 3. No global and no environment variable is read.
17. A test never writes to a client's `cfg` after `New`: the sync goroutine reads it. `TestBackoffDelay` did, and
    the race detector reported it on the third run of the first ten-run series; it now builds a second client
    with the seam in its constructor. This is a bug in the test (it changed something it did not control), as
    the checklist's stability line says to treat it.
18. `bounds_test.go` was added in phase 6 because the checklist's lines on many threads and on a bounded
    `variation` had no test of their own: `TestEveryOperationFromManyGoroutines` calls `Ready`, `Variation`,
    `VariationDetail`, the typed accessors and `Status` from 12 goroutines while the ruleset swaps and two
    `Close` calls race them, and waits for each worker to make progress before and after `Close`;
    `TestVariationIsBoundedWhileRequestsHang` holds a ruleset poll and a usage flush open together and times
    2000 evaluations.
19. `golangci-lint`'s `prealloc` is not enabled: it reports slices built in loops whose length the SDK does not
    know, which is noise here.

## Guesses

None that the documents left open after a search. Two points were close and are Decisions, not guesses: the
module path (PROMPT.md phase 1 item 7 settles it) and the order of `Ready` and `onError` (contract 5.3 settles
it). One point is worth a line in the prompt, below.

1. Whether a test may hold the 60 s watchdog and the 300 s cap through a package-private seam when the SDK is in
   a compiled language with an internal test package. Where: CHECKLIST.md, "Native tests" (watchdog line).
   Closest sentence: "tested through a seam, not by waiting: the seam sets the watchdog's length, and the test
   reads a real loopback socket that goes quiet". It settles the seam and the socket, and does not say whether a
   field of the SDK's own `config` set from the package's tests counts as "an option on `create` (which may be
   internal to the package)". Assumed: it does (the CHECKLIST line on seams says "which may be internal to the
   package"), and the test uses a real `httptest` loopback server.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `CHECKLIST.md`, "Code and documents", the seams line, after "(which may be internal to the
   package)", add: "a Go test in the same package sets an unexported config field through a function option;
   that is an internal option".
2. In `language-notes/go.md`, "Tests", add: "A test never writes to a client's config after `New`: the sync
   goroutine reads it. Give a second client the other value in its constructor" (decision 17; the race was found
   only by the ten-run loop, on a loaded machine).
3. In `language-notes/go.md`, "JSON", add: "Name the finite check on your own number type for what it checks.
   A `Value.IsFinite` that is true for a number only made every string and boolean answer `null` until the harness
   caught it" (defect (a); the harness found it at once, so this is a hint, not a guard).
4. In `PROMPT.md` phase 6, step 1, after "Walk CHECKLIST.md line by line", add: "Re-read the contract's
   'Bounds' bullets and every sentence that says 'no cap' or 'an SDK adds none of its own' against your parser and
   your client: a defensive limit you added in phase 3 is a deviation" (defect (b), caught only at the walk).

## Checklist

Paths are under `sdks/go` unless a command is shown. "Test" means a top-level Go test in that package.

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equal to the phase 2 command's:
  `internal/evaluator/vectors_test.go` (`TestVectors`, `TestBucketPins`) reads `fixtures/evaluation/` at test
  time and fails when it reads none; `bin/test` prints `vectors: 1371 pins: 17`; `bun -e '...'` (the phase 2
  count command) prints `1371 17`.
- [x] `bin/test` 10 times in a row, and once under CPU load: 11 of 11 exit 0 (see Results, Stability). The load
  was 12 `sh -c 'while :; do :; done'` loops at normal priority, started before `bin/test` and killed after it;
  the build ran under it (warm cache). One race failure on the first series (decision 17) restarted the count.
- [x] The harness exits 0 with only the 2 named vector skips: `scenarios: 81 passed, 0 failed, 0 skipped;
  vectors: 1369 passed, 0 failed, 2 skipped`. `integer-accessor` is advertised, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes: harness `privacy.json` and `kinds.json`; the test service passes a request context to
  `ContextFromValue`; native `TestContextConstructors`, `TestAMultiContextEvaluates`.
- [x] Result order and the mismatch rule: harness `evaluation.json`, `usage.json`; native `TestResultOrder`.
- [x] Integer accessor: harness `evaluation.json` (served `2.0` is `2`, a fraction and `huge` are `TYPE_MISMATCH`);
  native `TestIntegerAccessor`.
- [x] No operation raises: every scenario (the service answers 500 on a recovered panic and the scenario fails);
  native `TestNewAgainstARefusedConnectionDoesNotPanicAndReportsFixedText`,
  `TestAPanickingHookGoesToOnErrorAndTheEvaluationStillAnswers`, `TestAPanicInOnErrorIsIgnored`.
- [x] `NOT_READY` at once before the first load: harness `load.json`; native
  `TestNotReadyBeforeAndAfterAHungLoad`, `TestHookSeesFallbacksBeforeTheFirstLoad`.
- [x] `ready` settles, bounded, repeatable, false after `close` if unsettled: harness `load.json`, `close.json`;
  native `TestReadyIsFixedAtTheFirstSettle`, `TestNotReadyBeforeAndAfterAHungLoad`,
  `TestCloseWithAHungFirstLoadIsBounded`.
- [x] A non-ruleset answer never replaces the held one: harness `status.json`; native `TestBadRulesets`,
  `TestABadRulesetKeepsTheLastGoodOne`, `TestStatusCodesAreErrorsAndTheOldRulesetStays`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`;
  native `TestIfNoneMatchOnlyWithARulesetAndATag`, `TestA304WithNoRulesetHeldIsAnError`,
  `TestTrailingSlashesAreRemoved`.
- [x] The stream: harness `stream.json`, `load.json`, `status.json`; native `TestSSEParser`, `TestSSEEveryChunking`,
  `TestStreamSwapsOnChanged`, `TestAStreamTagEqualToTheHeldOneIsASyncWithNoFetch`,
  `TestAStreamFetchThatFailsEndsTheStreamAndCountsAsNotConnected`, `TestAStreamThatEndsCleanlyIsNotAnError`,
  `TestAStreamStatusIsAnError`.
- [x] The loop and the jittered backoff: harness `backoff.json`, `stream.json`; native `TestBackoffDelay`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; native `TestRetryAfterParsing`,
  `TestRetryAfterHoldsTheRulesetAndStreamTogether`, `TestRetryAfterIsCappedOnAllThreeRoutes`,
  `TestAHoldOnlyGrows`.
- [x] Status fields, states, `lastError` codes: harness `status.json` and the shape check on every status read.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`; native `TestUsageIsSummedPerFlagIndexAndDay`,
  `TestUsageIsBatchedAt500Entries`, `TestUsageOutcomes`, `TestADroppedBatchDoesNotStopTheFlush`,
  `TestAKeptBatchStopsTheFlushAndIsResentWithNewCounts`, `TestFlushesAreSerial`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: harness `hooks.json`; native
  `TestHookSeesTheServedResult`, `TestAPanickingHookGoesToOnErrorAndTheEvaluationStillAnswers`,
  `TestAPanicInOnErrorIsIgnored`.
- [x] `close` in order, twice at once, during a hold, with a final flush: harness `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`; native `TestCloseIsSafeTwiceAndConcurrently`, `TestCloseDoesTheFinalFlush`,
  `TestTheFinalFlushDoesNotWaitOutAHold` (the message is `closed with N usage counts unsent: the server asked
  to wait`), `TestNoRequestsAfterClose`, `TestNothingIsCountedAfterCloseBegins`.
- [x] No context, key or attribute in a request or an `onError` message, and no header beyond the protocol's:
  harness `privacy.json` and the after-every-scenario checks; native `TestUsageBodyCarriesNoContext`,
  `TestErrorsCarryFixedTextOnly`, `TestRequestHeadersAndRoutes`. The harness cannot see logs: the SDK writes
  none. Source search, run from `sdks/go`:
  `grep -n 'log\.\|slog\|fmt\.Print\|fmt\.Fprint\|os\.Std' $(git ls-files -o -c --exclude-standard '*.go' | grep -v _test.go | grep -v test-service)`
  prints nothing. `fmt.Sprintf`, `fmt.Errorf` and `errors.New` appear in `usage.go:118` (the fixed
  `closed with N usage counts unsent: ...` text, a count and no key), `types.go:117` (`PanicError`: the
  `Where` label and the application's panic value), `jsonvalue/value.go:375` and `:376` (fixed `ErrSyntax` and
  `ErrDepth` text), `sync.go:22 to 25` and `stream.go:89` (fixed sentinel texts). None takes a context, a
  context key, an attribute, a URL or a header value.

### Native tests

- [x] Every operation from many goroutines, `close` racing `variation`, two `close` calls racing, with no error,
  deadlock or race, under the race detector: `bin/test` runs `go test -race`.
  `TestEveryOperationFromManyGoroutines` (`Ready`, `Variation`, `VariationDetail`, the typed accessors, `Status`,
  12 goroutines, a swapping ruleset, two racing `Close` calls, progress awaited per worker before and after),
  `TestCloseRacingVariation`, `TestCloseIsSafeTwiceAndConcurrently` (16 callers),
  `TestManyGoroutinesAgainstASwappingRuleset`.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TestHTTPSWorksAndAnUntrustedCertificateIsANetworkError`
  (`httptest.NewTLSServer`; an untrusted certificate is a `network` error with fixed text).
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a usage
  flush hangs: `TestVariationIsBoundedWhileRequestsHang` (both hung at once, 2000 evaluations under 500 ms),
  `TestNotReadyBeforeAndAfterAHungLoad`, `TestAHungUsageRequestIsBoundedByTheRequestTimeout`.
- [x] One ruleset or the other across a swap, `etag` and `flagVersion` from the same ruleset:
  `TestManyGoroutinesAgainstASwappingRuleset` (the hook compares `"vN"` with the flag's version `N` under 8
  goroutines and 10 swaps).
- [x] SHOULD: callbacks that call back into the client do not deadlock, and `ready` from inside one does not
  wait: `TestCallbacksMayCallBackIntoTheClient` (`onError` calls `Status`, `Variation`, `Ready` and `Close`),
  `TestAHookMayCallClose`. `Ready` settles before `onError` runs for the first load (Decisions 11).
- [x] A stream with no bytes for 60 s is dropped as `timeout`, a keep-alive restarts the 60 s, through a seam on
  a real loopback socket: `TestTheWatchdogDropsAQuietStreamAndAKeepAliveRestartsIt` (`cfg.watchdog` 200 ms
  against an `httptest` server; the second half sends `: keep-alive` every 60 ms and the stream stays connected).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam:
  `TestRetryAfterIsCappedOnAllThreeRoutes` (three subtests; `cfg.retryAfterCap` 400 ms). The default is
  `defaultRetryAfterCap = 300 * time.Second` in `options.go:13`.
- [x] Counts either side of a UTC midnight go out as two days: `TestUsageIsSummedPerFlagIndexAndDay` (a fake
  `cfg.now` crosses 2026-11-11 to 2026-11-12 and two `(flag, index, day)` entries are sent).
- [x] An evaluation that raises inside the SDK gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`:
  `TestEvaluationFailedGoesToOnErrorOnceAndIsNotCounted`.
- [x] n/a: no futures. A hook is a plain function; a hook that panics is `TestAPanickingHookGoesToOnErrorAndTheEvaluationStillAnswers`.
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: harness `evaluation.json`
  (`huge`); native `TestIntegerAccessor` (`Int64Variation` is `int64`).
- [x] A client that is never closed does not keep the process alive: `TestANeverClosedClientDoesNotKeepTheProcessAlive`
  and `TestNeverClosedChild` (a child process creates a client, never closes it and exits). A goroutine does not
  keep a Go process alive once `main` returns, so this holds by the runtime's rule.

### Code and documents

- [x] `DESIGN.md` has the phase 1 rows (the row check prints nothing: `missing=0`), and `README.md` links to the
  mapping table (section 1 of `DESIGN.md`) and names the calls and options.
- [x] The mapping keeps the canonical names, defaults and spelling: `DESIGN.md` section 1; `ReasonRuleMatch ReasonKind
  = "RULE_MATCH"`, `ErrNotReady ErrorCode = "NOT_READY"`, `StreamConnected StreamState = "connected"` (string
  types whose values carry the canonical spelling).
- [x] No regular expression from the platform in the evaluator: `grep -rn 'regexp' --include=*.go .` from
  `sdks/go` finds nothing. The matcher is `internal/evaluator/pattern.go` and `pattern_parse.go`.
- [x] The lint configuration names its rule set and carries a reason for each rule it leaves out: `.golangci.yml`
  (header comment: the standard set plus the named linters; `usestdlibvars` omitted with its reason). It ignores
  no rule (decision 5).
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none (`go.mod` has no `require`; `DESIGN.md`
  section 4 says why `net/http`, `crypto/sha256` and the standard library suffice, and why the JSON reader is the
  SDK's own).
- [x] No telemetry; the source sets only `Authorization`, `Accept`, `If-None-Match` and `Content-Type`
  (`grep -n 'Header\.\(Set\|Add\)'` finds `sync.go:49` Authorization, `sync.go:51` Content-Type, `sync.go:55`
  If-None-Match, `stream.go:24` Authorization, `stream.go:25` Accept). Captured on a loopback listener
  (`httptest.NewServer`, a throwaway program in the scratch directory that drives `New` with poll-only and
  with the stream, recording `r.Host`, `r.ContentLength`, `r.TransferEncoding` and `r.Header` per route). The
  SDK's own headers, then what `net/http` added:

  | Route | SDK sets | Added by `net/http` | `r.Host` and framing (outside the header map) |
  | --- | --- | --- | --- |
  | `GET /sdk/v1/ruleset` | `Authorization` | `User-Agent: Go-http-client/1.1` | `r.Host` is the URL's `127.0.0.1:<port>`; no body, `ContentLength` 0 |
  | `GET /sdk/v1/ruleset` (conditional) | `Authorization`, `If-None-Match` | `User-Agent: Go-http-client/1.1` | same |
  | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `User-Agent: Go-http-client/1.1` | same |
  | `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `User-Agent: Go-http-client/1.1`, `Content-Length: 73` (the body's length) | `r.Host` as above; body only counts |

  Why each added one identifies no one: `User-Agent: Go-http-client/1.1` is the library name and the HTTP version
  and carries no host, user or SDK version. `Host` (`r.Host`) is the server's own address that the caller gave
  `New`. `Content-Length` is the size of the counts body. `Accept-Encoding` is not sent because
  `DisableCompression` is set (decision 7). No `Connection` header is sent: HTTP/1.1 keep-alive is the default and
  needs none. A request to an `https` URL adds the same set over TLS.
- [x] Every extra beyond the contract is one contract 13 allows: the typed accessors and `Int64Variation` (contract
  4, 13), the test seams (decision 16), `ContextFromValue` and `ContextFromJSON` (a way to build a context from
  the harness's JSON). None weakens contract 5 or sends anything new.
- [x] Test seams are unexported config fields set by a package-internal option, never a global or an environment
  variable (decision 16): `grep -rn 'os.Getenv' --include=*.go .` finds nothing outside `test-service/main.go`
  (which reads none either).
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable (`ls -l sdks/go/bin` shows `-rwxr-xr-x`),
  `cd` to the SDK and use only what `mise.toml` pins.
- [x] `test-service/README.md` maps every accessor of the protocol (`bool`, `string`, `number`, `int`, `json`,
  `any`) to an SDK method.
- [x] `.github/workflows/sdk-go.yml` follows the skeleton (it is `sdk-python.yml` with the toolchain step changed to
  `install_args: go zig golangci-lint`), filters on its paths, and pins every action by commit SHA.

### Repository

- [x] No em dash or en dash in any file written: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes.
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints `scope ok` (run last).
- [x] No package was published and no name reserved; no tag was made.
- [x] The commit subject is a Conventional Commit (`chore(sdk-go): ...`, not `feat`), with no `Co-Authored-By` and
  no AI attribution line in the commit or the PR body.
- [x] The workflow's job is `runs-on: ubuntu-latest`, has no `if:`, triggers only on `pull_request` and `push`,
  installs its own toolchain, and `bun scripts/check-workflows.ts .github/workflows/*.yml` passes (`8
  workflow(s) read, 0 failure(s)`).
- [x] `RUN.md` follows `RUN-TEMPLATE.md` with evidence for every line: the template-section check prints nothing.
