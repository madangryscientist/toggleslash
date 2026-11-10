# Go SDK: run report

## Prompt version

- Prompt: 180a02dfe6b363203e19f8b9d19a24cbbfb74927 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-11
- Input: language Go; runtime chosen: the Go toolchain with the standard library only; minimum version chosen: Go 1.26
  (`go 1.26` in `go.mod`); package manager chosen: Go modules (module
  `github.com/madangryscientist/toggleslash/sdks/go`, no third-party module, so no `go.sum`)
- Toolchain: pinned in `sdks/go/mise.toml`: `go = "1.27.3"` (`go version` prints `go1.27.3 linux/amd64`) and
  `golangci-lint = "2.15.0"` (`golangci-lint --version` prints `has version 2.15.0 built with go1.27.0`)

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2
  count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 56 passed of 56 top-level tests, 59 of 59 counting subtests, across the root package,
  `internal/evaluator`, `internal/jsonv` and `internal/wtf8` (`go test -json ./...`), under `-race` in `bin/test`.
  `bin/test` passed 10 of 10 times in a row, and once more with 12 busy loops (`nproc` is 12) running.

## Decisions

1. Lint: golangci-lint v2 with `default: standard` plus bodyclose, errorlint, gocritic, misspell, nilerr, noctx,
   unconvert, unparam and whitespace, over a plain `go vet`, because the extra linters catch the leaks and error
   comparisons a network client gets wrong. Format: the `gofmt` formatter through golangci-lint, plus `gofmt -l` in
   `bin/lint`. `bin/lint` prints `0 issues.`
2. Tests: the standard `testing` package with a fake server on `net/http/httptest`, over testify or a mock library,
   because the repository wants no dependency and the fake server also exercises the real socket paths.
3. JSON: `internal/jsonv` (a small parser and writer) over `encoding/json`, because the evaluation spec needs
   duplicate keys, number text, `-0`, big exponents and lone surrogates handled the same in every language, and
   `encoding/json` replaces invalid UTF-8 and loses number text.
4. SSE: a hand-written parser (`sse.go`), because `net/http` has none and the contract's rules (BOM, `\r`, `\r\n`,
   an event still incomplete when the stream ends is discarded) are short.
5. CI toolchain: `jdx/mise-action` over `actions/setup-go`, because `mise.toml` is the one place the versions are
   pinned and `bin/*` already use it. `install_args: go golangci-lint` stops `mise exec` installing the root
   `.tool-versions` tools too.
6. The reader of the first load: the first load's `onError` is delivered before `Ready` answers, and `Ready` called
   from the SDK's own goroutine (a hook or `onError`) returns at once. The goroutine is recognised by comparing
   goroutine ids read from `runtime.Stack`, because Go has no goroutine-local state. `Close` called from a callback
   on an SDK goroutine does not wait for that goroutine, and a reentrant `Close` by the same goroutine returns at once.
7. Usage requests use `context.Background()` plus `requestTimeout`, not the client's root context, so that `Close`
   can wait for a flush and send the final one. The flush loop sleeps on real timers; the sync loop sleeps through the
   `sleep` seam.
8. The SDK's own panics are reported to `onError` in fixed words (`toggleslash: evaluation failed unexpectedly`),
   because a panic value could carry a context or a key (contract 12). A panic in the application's hook is the
   application's own, so its value is passed as raised (an error as it is, anything else wrapped with its text).
9. `EvaluationEvent.FlagVersion` is a `*float64`, nil when the ruleset has no such flag, because a version is a JSON
   number and the field must say "absent".
10. Network errors reach `onError` and `Status.LastError` as `network` or `timeout` only: `net/http` error text
    carries the URL, which carries the host and could carry a path.
11. Redirects are not followed (`CheckRedirect` returns `http.ErrUseLastResponse`), so a redirect is an
    `http_<status>` failure and the key is never sent to a second host.
12. Seams are unexported options on `Create` (`withHTTPClient`, `withClock`, `withSleep`, `withJitter`,
    `withWatchdog`, `withEvaluator`), usable by the package's own tests only. The `Retry-After` cap is a config field
    with its default of 300 s, tested through the fake clock, so it has no seam of its own.
13. The test framework runs under `-race`, which needs cgo; `bin/test` therefore needs a C compiler on the machine
    (CI has one).

## Guesses

1. Which Go version to pin. Where: `PROMPT.md`, phase 1 (toolchain), and `language-notes`. Assumed: `go = "1.27.3"`,
   because `mise ls-remote go` hid the two newest releases at the time of the run and 1.27.3 was the newest listed;
   the module itself says `go 1.26`.
2. How deep nested JSON may go. Where: `docs/sdk-contract.md` 5.5 and `docs/evaluation.md` (ruleset parsing).
   Assumed: a depth limit of 512, after which the answer is not a ruleset (`bad_ruleset`), so a hostile server
   cannot overflow the stack.
3. What a ruleset with invalid UTF-8 does. Where: `docs/evaluation.md` (strings). Assumed: invalid bytes become
   U+FFFD, the same as a decoder that replaces, and the ruleset is kept.
4. How a non-finite number is written as JSON. Where: `docs/evaluation.md` (values). Assumed: NaN and infinities are
   written as `null` (`internal/jsonv/write.go`).
5. A `304` when no ruleset is held (the server answers one to a request that had no `If-None-Match`, which
   the SDK never sends without a held ruleset and tag). Where: `docs/sdk-contract.md` 7.1. Assumed: `http_304`
   as `lastError`, the same as any other unexpected status.
6. An empty `ETag` header. Where: `docs/sdk-contract.md` 7.1 ("the ETag kept exactly"). Assumed: empty means none,
   so no `If-None-Match` is sent next time.
7. A cap on one SSE line. Where: `docs/sdk-contract.md` 7.2 ("Bounds"). Assumed: none; the 60 s silence watchdog and
   the 1 MiB body cap on the server bound the stream, and a long line is a long `data:` field.
8. Whether `Retry-After` is read on a status other than 429. Where: `docs/sdk-contract.md` 7.4. Assumed: 429 only,
   as the section says; a `Retry-After` on a 503 is ignored.
9. What `Close` waits for when called from inside a callback. Where: `docs/sdk-contract.md` 6.4 and 11. Assumed: it
   does not wait for the goroutine it is called on (Decision 6).
10. How to list the headers `net/http` adds. Where: `CHECKLIST.md`, "Code and documents" (telemetry). Assumed: the
    list below, taken from one real request of each route to a loopback listener.

    | Route | Headers set by the SDK | Headers added by `net/http` |
    | --- | --- | --- |
    | `GET /sdk/v1/ruleset` | `Authorization`, `Accept: application/json`, `If-None-Match` (only with a held ruleset and tag) | `User-Agent: Go-http-client/1.1`, `Accept-Encoding: gzip`, `Host` |
    | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream`, `Accept-Encoding: identity` | `User-Agent: Go-http-client/1.1`, `Host` |
    | `POST /sdk/v1/events` | `Authorization`, `Accept: application/json`, `Content-Type: application/json` | `User-Agent: Go-http-client/1.1`, `Accept-Encoding: gzip`, `Content-Length`, `Host` |

    `User-Agent: Go-http-client/1.1` names the library and the HTTP version, not a host or a user. `Accept-Encoding:
    gzip` says the client can decompress. `Host` is the server's own address, which the client was told. `Content-Length`
    is the size of the usage body. `Connection` is not sent (HTTP/1.1 keep-alive is the default). The stream's
    `Accept-Encoding: identity` is set by the SDK so that a proxy does not buffer the stream through compression.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md` phase 1, after "pin the toolchain in `mise.toml`", add: "`mise ls-remote <tool>` can hide
   the newest releases; pin the newest version it lists and say so in `RUN.md`."
2. For guess 2, in `PROMPT.md` phase 2, after the paragraph on parsing the ruleset, add: "Limit the nesting depth of a
   parsed ruleset (512 is enough); a deeper document is not a ruleset (contract 5.5)."
3. For guess 10, in `CHECKLIST.md`, under the telemetry line, add: "Capture the request with a scratch program that is
   deleted afterwards, and list each route's headers in a table in `RUN.md`."
4. For decision 13, in `language-notes/go.md` (create it if absent), add: "`go test -race` needs cgo, so `bin/test`
   needs a C compiler; say so in the README."
5. For decision 5, in `PROMPT.md` phase 5, after "install_args", add: "A workflow that runs `mise exec` installs every tool
   in the repository's root `.tool-versions` too; set `install_args` to the SDK's own tools."

## Checklist

- [x] `bin/test` runs every vector but `buckets.json`, read at test time, and every pin; count equals phase 2's:
  `TestVectorsAndPins` (`internal/evaluator/vectors_test.go`) reads `fixtures/evaluation/` at run time;
  `bin/test` prints `vectors: 1371 pins: 17`, and the phase 2 count command prints `1371 17`.
- [x] `bin/test` 10 times in a row and once with the CPU loaded: 10 of 10 passed, and 1 of 1 with 12 busy loops
  (`nproc` is 12), each run of `bin/test` being all packages under `-race`.
- [x] The harness exits 0 with only the 2 named vector skips: `scenarios: 81 passed, 0 failed, 0 skipped;
  vectors: 1369 passed, 0 failed, 2 skipped`. `integer-accessor` is advertised, so no scenario is skipped.
- [x] Both context shapes accepted (contract 3): the harness, `privacy.json` and the `kinds.json` vectors.
- [x] Result order of contract 4 and the mismatch rule: the harness, `evaluation.json`, `usage.json`; native
  `TestReadyAndVariation`, `TestNotReadyBeforeAndAfterFailure`, `TestUsageNotCountedForFallbacksOrMismatches`.
- [x] With `integer-accessor`, a fraction is `TYPE_MISMATCH` and `2.0` is `2`: the harness, `evaluation.json`;
  native `TestIntAccessorRange`.
- [x] No failure makes an operation raise (contract 5.1): the harness, every scenario; native
  `TestHookAndOnErrorPanicsStayInside`.
- [x] `NOT_READY` at once before the first load: the harness, `load.json`; native `TestNotReadyBeforeAndAfterFailure`.
- [x] `ready` settles after the first attempt, bounded, may be called again, false after `close` if unsettled: the
  harness, `load.json`, `close.json`; native `TestReadyIsFixedAtTheFirstSettle`, `TestACloseDuringTheFirstLoadIsQuiet`.
- [x] An answer that is not a ruleset never replaces the one held: the harness, `status.json`; native
  `TestBadRulesetKeepsTheOldOne`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: the harness, `load.json`, `status.json`;
  native `TestIfNoneMatchOnlyWithHeldRulesetAndTag`, `TestNoETagMeansNoIfNoneMatch`.
- [x] The stream (events, SSE parsing, refresh, a failed refresh ends it): the harness, `stream.json`; native
  `TestSSEParser`, `TestStreamChangeReachesTheClient`, `TestStreamEventWithHeldTagIsASyncWithNoFetch`,
  `TestFailedFetchAfterAStreamEventEndsTheStream`, `TestStreamReconnectsAfterItDrops`.
- [x] The loop and the jittered backoff (contract 7.3): the harness, `backoff.json`, `stream.json`; native
  `TestReconnectDelayFollowsContract73`, `TestPollingWhenStreamIsOff`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`; native `TestRetryAfterParsing`,
  `Test429RetryAfterIsCappedAndHeldOnEveryRoute`.
- [x] Status fields, states and `lastError` codes: the harness, `status.json`; native `TestHTTPStatusIsTheLastError`,
  `TestStatusAfterClose`, `TestRedirectIsNotFollowed`.
- [x] Usage counts (what counts, summing, batches of 500, keep or drop): the harness, `usage.json`,
  `rate-limit.json`; native `TestUsageSumsPerFlagIndexAndDay`, `TestUsageChunksAt500Entries`, `TestUsageOutcomes`,
  `TestUsageKeepsCountsOnNetworkErrorAndSendsThemLater`, `TestPeriodicFlushSendsCounts`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: the harness, `hooks.json`; native
  `TestHookAndOnErrorPanicsStayInside`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`; native `TestCloseIsIdempotentAndBounded`, `TestFinalFlushDoesNotWaitOutAHold`,
  `TestNothingCountedAfterCloseBegins`.
- [x] No context, key or attribute in a request or `onError` message, and no header beyond the protocol's: the
  harness, `privacy.json` and the after-every-scenario checks; native `TestEventsBodyHoldsNoContext`,
  `TestRequestHeadersAreOnlyTheAllowedOnes`, `TestNetworkErrorTextHasNoURL`, `TestEvaluationFailureHasOneOnErrorAndNoCount`.
  Source search: `grep -rnE '\b(log|slog)\.|fmt\.(Print|Fprint)|println\(' --include=*.go .` finds nothing outside
  `test-service/main.go` (the test service's own startup and listen errors, not the SDK); the only error values built
  are fixed words (`panicError`, `access.go`), `failureError` (an operation and a code), and the closed-with-N-counts
  message (`usage.go`, `lost`), none of which holds a context, key or attribute.
- [x] Every operation from many goroutines, `close` racing `variation`, two `close` calls racing, with no error,
  deadlock or data race, under the race detector: `TestEveryOperationFromManyGoroutines` and
  `TestCloseRacingVariationAndAnotherClose` (20 rounds), run with `go test -race` in `bin/test`.
- [x] SHOULD: an `https` URL reaches the TLS path: `TestTLSWithTheServersCertificate` (a local certificate trusted
  through the `withHTTPClient` seam) and `TestTLSWithAnUntrustedCertificateFails` (the failure is `network`).
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a usage flush
  hangs: `TestVariationReturnsWhileRequestsHang`.
- [x] A reader sees one ruleset or the other across a swap, and the hook's `etag` and `flagVersion` belong together:
  `TestSwapPairsTheTagWithTheFlagVersion`. The client holds one immutable `view` behind an atomic pointer.
- [x] SHOULD: a hook and an `onError` that call back into the client do not deadlock, and a `Ready` called on the
  SDK's own goroutine returns without waiting: `TestCallbacksMayCallBackIntoTheClient`, `TestHookMayCloseAndEvaluate`.
- [x] A stream with no bytes for 60 s is dropped as `timeout`, a keep-alive restarts it, tested through a seam on a
  real loopback socket: `TestWatchdogRestartsAQuietStreamAndAKeepAliveHoldsIt` (the seam sets the watchdog's length).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam:
  `Test429RetryAfterIsCappedAndHeldOnEveryRoute` (fake clock).
- [x] Counts either side of a UTC midnight go out as two days: `TestUsageSumsPerFlagIndexAndDay`.
- [x] An evaluation that raises inside the SDK gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call:
  `TestEvaluationFailureHasOneOnErrorAndNoCount` (through the `withEvaluator` seam).
- [x] n/a: no futures. The hook is a plain function, so there is no later failure to route; a hook that panics is
  `TestHookAndOnErrorPanicsStayInside`.
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestIntAccessorRange`
  and the harness `huge` scenario in `evaluation.json`.
- [x] SHOULD: a client that is never closed does not keep the process from exiting: `TestNeverClosedClientDoesNotHoldTheProcess`
  (a child process that creates a client, never closes it and exits, within a bound). Go's goroutines never keep
  a process alive once `main` returns.
- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` has the mapping table: `DESIGN.md` section 1 holds a
  row for each of the six operations and each option; `README.md` ("The API") links to that table.
- [x] Mapping names keep canonical meaning and defaults, and the reason kinds, error codes, stream states and
  `lastError` codes keep their spelling: `types.go` (`ReasonKind` and `ErrorCode` are the evaluator's string types,
  `StreamState` constants, `LastError*` constants), checked by the harness's status shape check.
- [x] No platform regular expression in the evaluator: `grep -rn '"regexp"' internal/evaluator` finds nothing
  (`internal/evaluator/pattern.go` implements the pattern matcher itself). The only `regexp` import in the tree is
  `test-service/main.go:15`, which routes the test service's own URLs, and is not the evaluator.
- [x] The lint configuration names the rule set and each ignored rule carries a reason: `.golangci.yml` (the header
  comment names "standard" plus the nine linters; the one exclusion, errcheck and unparam in `_test.go`, has a
  comment beside it). No `//nolint` comment in the source.
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none; `DESIGN.md` section 4 says so, and
  `go.mod` has no `require`.
- [x] No telemetry and no header that identifies a user or a machine: the search
  `grep -rn 'Header.Set\|Header.Add' --include=*.go . | grep -v _test.go` finds `Authorization`, `Content-Type`,
  `Accept`, `If-None-Match` and the stream's `Accept-Encoding: identity` (`usage.go:175-177`, `sync.go:78,122,124`,
  `stream.go:30-31`). The headers `net/http` adds are listed in Guess 10 from a request captured on a loopback listener.
- [x] Every extra beyond the contract is one contract 13 allows: the only extras are the unexported test seams and
  the typed `Detail[T]`; none weakens contract 5 or sends anything new.
- [x] Seams are options on `Create`, never a global or an environment variable: `options.go` (`withHTTPClient`,
  `withClock`, `withSleep`, `withJitter`, `withWatchdog`, `withEvaluator`); a search for `os.Getenv` in the SDK
  finds nothing.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and run on the
  `mise.toml` toolchain: `ls -l sdks/go/bin` shows `-rwxr-xr-x` for all three; each starts with
  `cd "$(dirname "$0")/.."`.
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method: its table lists bool, string,
  number, int, json and the Detail twins.
- [x] `.github/workflows/sdk-go.yml` follows the skeleton, filters on its paths and pins every action by commit SHA:
  `actions/checkout@3d3c42e5...`, `oven-sh/setup-bun@0c5077e5...`, `jdx/mise-action@b44d12e7...` (lines 32, 35, 40),
  the same SHAs the other SDK workflows use.
- [x] No em dash or en dash: `bun scripts/check-dashes.ts` (run at the end of the phase 6 checks).
- [x] `bun run format:check` passes (run at the end of the phase 6 checks).
- [x] Only `sdks/go/` and the one workflow file changed: the phase 6 scope check prints `scope ok`.
- [x] No package was published and no name reserved: no registry command was run, no tag was made; the module path
  is a path, not a registration.
- [x] Commit subject is `chore(sdk-go): the Go server SDK regenerated cold from the authoring prompt`, no `feat`, and no
  `Co-Authored-By` or AI attribution in the commit or the pull request.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: the phase 6 section check prints nothing.
