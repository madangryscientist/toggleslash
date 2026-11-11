# Go SDK: run report

Run 8 of the SDK sweep, ticket FS-129: the Go SDK regenerated cold. The old `sdks/go` and its workflow were
removed before the run and were not read, in the tree or in history.

## Prompt version

- Prompt: 0c125a70ea92a0198fa4d300fe59980529f6cf21 (`git log -1 --format=%H -- docs/sdk-authoring`; `origin/main` was at the same commit at the start and at the end)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-11
- Input: language Go; runtime chosen: the gc toolchain (the only mainstream one); minimum version chosen: the
  pinned Go 1.27.3 (`go.mod` says `go 1.27`; no older release was tried); package manager chosen: Go modules
  with no dependency
- Toolchain (`mise.toml`; the version commands printed the same): go 1.27.3 (`go version go1.27.3 linux/amd64`),
  golangci-lint 2.15.0 (`golangci-lint has version 2.15.0`), zig 0.17.1 (`zig version` prints `0.17.1`). zig is
  only the C compiler for `-race`.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command agrees)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (exit 0 on two runs in a row)
- Capabilities advertised: `integer-accessor`
- Native tests: 58 top-level tests, 1472 passes counting subtests and vectors, 0 failures, 1 skip
  (`TestHelperNeverClosedClient`, which does its work only when `TestANeverClosedClientDoesNotKeepTheProcessAlive` re-executes the test binary)

## Decisions

1. Typed accessor family over one generic method: Go methods cannot take type parameters, so `Bool`, `String`,
   `Int` (int64), `Float` (float64) and `JSON` each have a value and a `Detail` method. DESIGN.md section 1.
2. Functional options (`Option func(*config)`) over a config struct: `WithStream(false)` is then "off" and a
   never-passed option is "default" with no pointer fields. DESIGN.md section 1.
3. No runtime dependency. `encoding/json` fails on `1e999` and rewrites lone surrogates, so the SDK has its own
   reader, `internal/jsonv` (evaluation.md, "Reading JSON"). DESIGN.md sections 4 and 5.
4. Own regular-expression-free pattern engine in `internal/eval/pattern*.go`; `regexp` is not imported by any
   non-test file (`TestEvaluatorUsesNoPlatformRegex`).
5. Lint: `gofmt -l`, `go vet` and golangci-lint v2 (`standard` plus bodyclose, errorlint, noctx, govet with
   everything but fieldalignment and shadow, unparam, wastedassign, copyloopvar, intrange, misspell, thelper).
   The tool is not named by the prompt; every ignore in `.golangci.yml` has its reason beside it. `usestdlibvars`
   is not enabled: it adds nothing the rest does not catch here.
6. `bin/test` uses `zig cc` as the C compiler for `-race`. PROMPT.md "Entry points" says a script needs
   "nothing beyond what `$L/mise.toml` pins and `sh`" and also "if a race detector needs [a C compiler], the README
   says so and the workflow provides it". Pinning zig in `mise.toml` satisfies both, works on any machine with
   mise, and `CC` set by the caller wins. The README says a C compiler is needed.
7. CI toolchain: `jdx/mise-action` limited to `go golangci-lint zig` (`working_directory: sdks/go`), over
   `actions/setup-go`, because it installs the same three pins as `mise.toml` and so cannot drift.
8. "closed with N usage counts unsent: the server asked to wait": N is the number of pending entries, one per
   flag, variation and day. Settled by docs/sdk-contract.md line 432.
9. A usage request answered 5xx, 408 or 429, or failing on the network, keeps its counts; any other status drops
   them. docs/sdk-contract.md section 9 ("What is kept"). The error text is `POST /sdk/v1/events answered <status>`.
10. `Status.LastError` is a `SyncError` (the `http_<status>`, `timeout`, `bad_ruleset` and `network` codes), a type
    of its own, not the evaluation `ErrorCode`: the two sets have no member in common. DESIGN.md section 1 said
    `ErrorCode` in its first draft; it was corrected in this run to match the code.
11. `CheckRedirect` returns `http.ErrUseLastResponse`, so a 3xx is `http_3xx` and the key is never replayed to a
    redirect target. A caller's `WithHTTPClient` is copied and its redirect policy replaced.
12. Request failures are reported as the fixed codes `network` and `timeout`, never the `*url.Error` text, which
    carries the URL (contract 12 is about contexts, but a URL with a key in a query is the same hazard).
13. `requestTimeout` bounds a whole ruleset or events request, body included, with
    `context.WithTimeoutCause`. A stream is bounded to its first headers by a timer, then by a 60 s read
    watchdog. DESIGN.md section 3.
14. "A hook that returns a future" (native test list): `n/a: no futures`. `WithAfterEvaluation` takes a plain
    `func(EvaluationEvent)`.
15. "A client that is never closed does not keep the process from exiting": Go has no daemon goroutines, and
    `main` returning ends them. Proved by `TestANeverClosedClientDoesNotKeepTheProcessAlive`, which re-executes the
    test binary (`TestHelperNeverClosedClient`) with a client that is never closed and requires the child to exit
    inside 10 s after its stream opened.
16. Test seams are unexported options (`withClock`, `withJitter`, `withWatchdog`, `withEvaluator`) set by the
    package's own tests, never a global or an environment variable. The one `os.Getenv` is in a test, for the
    re-exec helper (`source_test.go`).
17. The test service answers 400 for a typed accessor whose fallback is null or of another kind, as
    docs/sdk-test-service.md, "Accessors" (line 81) allows ("a service MAY answer 400 to one"). That is a bad
    scenario, not an SDK fault.
18. The race-test flake found by the 10-run check (see the checklist, line 2) was a bug in the test, not the SDK,
    and was fixed in the test, as the checklist says to.
19. The module path is `github.com/madangryscientist/toggleslash/sdks/go`, a name only: nothing is registered
    under it, and the README says to use a `replace` directive from a checkout.
20. The package is named `toggleslash` and the directory is `go`, because `go` is a keyword.
21. `oxfmt` and the native tools never disagreed about a file: `mise.toml`, `.golangci.yml` and the workflow pass
    `bun run format:check` as written.

## Guesses

1. Whether `bin/test` may reach for a compiler that `mise.toml` pins but that is not the language's own
   toolchain. Where: PROMPT.md, "Entry points". Closest sentence: "needs nothing beyond what `$L/mise.toml` pins and
   `sh` (the CI runner is not assumed to have a C compiler: if a race detector needs one, the README says so and the
   workflow provides it)". It does not settle whether pinning a compiler in `mise.toml` counts as "providing" it, or
   whether the README line alone is enough. Assumed: pinning zig in `mise.toml`, using it in `bin/test` and
   installing it in the workflow is allowed (see decision 6).
2. How to read "one busy loop per core" for the loaded run. Where: CHECKLIST.md, "The two executable checks",
   second line. Closest sentence: "once more with the CPU loaded (one busy loop per core)". It does not say whether
   cores means logical CPUs or physical cores, or what to do on a machine where a SMT sibling is idle. Assumed:
   `nproc` busy shell loops (12 here). The run was green and took 7.4 s of test time, about the same as an idle run,
   so this load is gentle: a stronger load may find more.
3. Which capture counts as "one request of each route" when a route has two shapes. Where: CHECKLIST.md, "Code and
   documents", the telemetry line. Closest sentence: "capture one request of each route on a loopback listener".
   The ruleset route has two shapes (plain, and with `If-None-Match`) and the table below lists both. Assumed: one
   row per shape.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md` "Entry points", after "if a race detector needs one, the README says so and the
   workflow provides it", add: "A compiler that `mise.toml` pins and the script calls by path counts as the
   workflow providing it."
2. For guess 2, in `CHECKLIST.md`, after "(one busy loop per core)", add: "(logical CPUs, so `nproc` loops, each
   at normal priority)", or name a load tool so every run loads the machine the same way.
3. For guess 3, in `CHECKLIST.md`, after "capture one request of each route on a loopback listener", add: "(and one
   conditional ruleset request, which carries `If-None-Match`)".
4. Not tied to a guess, `CHECKLIST.md` "The two executable checks": the 10-run check found a real flake on its
   second run (a swap loop that finished before a reader goroutine had been scheduled). The line already says to
   fix the test; it could add "a test that races a fixed number of rounds against background workers must wait
   until the workers have made progress, not until the rounds are done", which is the shape of the failure.

## Header table

Captured on a loopback listener with a throwaway program (outside the tree) that ran a client with the stream on,
a read of a flag so a usage count existed, and a second client with the stream off and a 100 ms poll. Go 1.27.3,
HTTP/1.1. The `Authorization` value is the SDK key and is not shown.

| Route | Set by the SDK | Added by `net/http` |
| --- | --- | --- |
| `GET /sdk/v1/ruleset` | `Authorization` | `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1`, `Host` |
| `GET /sdk/v1/ruleset` (conditional) | `Authorization`, `If-None-Match` | `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1`, `Host` |
| `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1`, `Host` |
| `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1`, `Host`, `Content-Length` |

Why none identifies a user or a machine:

- `Host` is the address the caller gave. `Content-Length` is the size of the body.
- `Accept-Encoding: gzip` is the same on every Go program that uses `net/http` and says nothing about the host.
- `User-Agent: Go-http-client/1.1` names the library, with the HTTP version, and not the host, the user or the SDK
  version. The SDK does not set its own. A caller's `WithHTTPClient` transport may add more, and that is theirs.
- `Connection` is not sent on HTTP/1.1 by Go (keep-alive is the default), so there is nothing to list.

## Checklist

- [x] `bin/test` runs every vector but `buckets.json` and every pin, and the count equals phase 2's:
  `internal/eval/vectors_test.go` reads `fixtures/evaluation/*.json` at test time; `bin/test` prints
  `vectors: 1371 pins: 17`, the phase 2 count command printed the same.
- [x] `bin/test` passes 10 times in a row and once under load: second run of the first attempt FAILED
  (`TestHookETagAndFlagVersionStayPaired`: "no evaluations ran", the 200 swap rounds ended before one reader
  goroutine was scheduled). A test bug: it now swaps until 2000 evaluations have overlapped, bounded by
  `eventually`, and `go test -race -count=100` of that test passes. Then 10 of 10 passed from scratch
  (`vectors: 1371 pins: 17` each time) and one run with 12 busy loops (one per core) passed.
- [x] The harness exits 0 with only the 2 named skips: run twice, `rc=0` each time,
  `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`. `integer-accessor` is
  advertised, so no scenario is skipped.
- [x] Both context shapes: the harness, `privacy.json`, the `kinds.json` vectors (1369 vectors passed).
- [x] Result order and mismatch rule with the SDK's own codes: the harness, `evaluation.json`, `usage.json`;
  native `TestAccessors`.
- [x] Integer accessor (a fraction is `TYPE_MISMATCH`, `2.0` is `2`): the harness, `evaluation.json`;
  native `TestAccessors`.
- [x] Nothing makes an operation raise: the harness (the service answers 500 on a panic and no scenario got one);
  native `TestAnUnusableURLIsANetworkErrorNotAPanic`, `TestManyGoroutinesWithCloseRacing`.
- [x] Before the first load `NOT_READY` at once: the harness, `load.json`; native `TestNotReadyBeforeTheFirstLoad`.
- [x] `ready` settles after the first attempt, bounded, repeatable, false after `close` if unsettled: the harness,
  `load.json`, `close.json`; native `TestReadyIsFixedAfterTheFirstAttempt`.
- [x] A non-ruleset never replaces the held one: the harness, `status.json`; native
  `TestBadRulesetNeverReplacesTheOneHeld`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: the harness, `load.json`, `status.json`;
  native `TestPollsWithTheStreamOffAndSendsIfNoneMatch`, `TestTrailingSlashesAreRemovedFromTheURL`.
- [x] The stream: the harness, `stream.json`, `load.json`, `status.json`; native `TestSSEParser`,
  `TestSSEParserIsIndependentOfChunking`, `TestStreamConnectedFollowsChangesAndPollsNever`,
  `TestAFailedFetchEndsTheStreamAndCountsAsNotConnected`.
- [x] The loop and the jittered backoff: the harness, `backoff.json`, `stream.json`; native `TestDelay`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`; native `TestParseRetryAfter`,
  `TestRetryAfterOnlyCountsOnA429AndWhenItIsWholeSeconds`.
- [x] Status fields, states, `lastError` codes: the harness, `status.json` and the shape check on every read.
- [x] Usage counts: the harness, `usage.json`, `rate-limit.json`; native `TestUsageCountsAreSummedPerFlagVariationAndDay`,
  `TestUsageIsSentInBatchesOfAtMost500Entries`,
  `TestUsageIsKeptOn5xxAnd408And429AndNetworkErrorsAndDroppedOtherwise`.
- [x] Hook events and `onError`, with a hook that panics and an `onError` that panics: the harness, `hooks.json`;
  native `TestHookPanicGoesToOnErrorAndChangesNothing`, `TestOnErrorThatPanicsIsIgnored`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`,
  `rate-limit.json`, `usage.json`, `status.json`; native `TestCloseSendsTheCountsOnce`,
  `TestFinalFlushDoesNotWaitOutAHoldAndReportsTheLoss`, `TestManyGoroutinesWithCloseRacing`.
- [x] No context in any request or `onError` message, no header beyond the protocol's: the harness, `privacy.json`
  and the after-every-scenario checks. Source search for logging: `grep -rn 'log\.\|fmt.Print\|fmt.Fprint\|slog\.\|os.Stderr\|os.Stdout'`
  over the non-test, non-test-service `.go` files finds nothing; `TestSourceNeverLogs` holds it. Native
  `TestNoContextAppearsInAnyRequestOrError`.
- [x] Native: every operation from many goroutines, `close` racing `variation`, two `close` calls racing, under the
  race detector: `TestManyGoroutinesWithCloseRacing` (32 goroutines, every operation, three `Close` calls racing,
  a 20 s bound on the goroutines and 10 s on each `Close`), run by `bin/test` with `-race`.
- [x] SHOULD, an `https` URL reaches the TLS path with a local certificate: `TestTLSPath` (an `httptest` TLS server
  with the client given its certificate pool through `WithHTTPClient`).
- [x] `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs:
  `TestEvaluationNeverWaitsOnARequest`; `TestCloseReturnsWithinAboutTwiceTheRequestTimeoutWhenRequestsHang` for `Close`.
- [x] One ruleset or the other across a swap, `etag` paired with `flagVersion`: `TestHookETagAndFlagVersionStayPaired`
  (8 readers against 200 or more swaps of an `atomic.Pointer` snapshot, under `-race`).
- [x] SHOULD, callbacks that call back into the client do not deadlock, `ready` inside one returns:
  `TestCallbacksThatCallBackIntoTheClient`.
- [x] A stream with no bytes for 60 s is dropped as `timeout`, a keep-alive restarts the 60 s, through a seam and
  a real quiet loopback socket: `TestWatchdogDropsAQuietSocketAndAKeepAliveRestartsIt` (400 ms watchdog seam, 600 ms
  of keep-alives, drop at or after 600 ms, `lastError` `timeout`); also `TestAStreamThatDoesNotOpenInTimeIsATimeout`
  and `TestARulesetBodyThatNeverEndsIsATimeout`.
- [x] `Retry-After` above 300 held for 300 s on the ruleset, stream and events routes: `TestRetryAfterIsCappedAndHeldPerRoute`
  (99999 held exactly 300 s on each route, with the fake clock).
- [x] Counts either side of a UTC midnight go out as two days: `TestUsageSplitsAtUTCMidnight` (a UTC-5 clock).
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call:
  `TestEvaluationFailedFromAPanickingEvaluator`.
- [x] n/a: no futures. Hooks are plain functions; a panic at once is `hooks.json` and `TestHookPanicGoesToOnErrorAndChangesNothing`.
- [x] A number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestHugeIntegerIsATypeMismatch`;
  the harness `huge` scenario.
- [x] SHOULD, a client never closed does not keep the process from exiting: `TestANeverClosedClientDoesNotKeepTheProcessAlive`
  (see decision 15).
- [x] `DESIGN.md` passes the phase 1 row check and the README links to its table: README "The API" links
  `DESIGN.md#1-the-mapping-table` and repeats the shape in a short table.
- [x] Names keep the canonical meaning and defaults, and the closed sets keep their spelling: DESIGN.md section 1
  ("Closed sets"), constants in `types.go` (`ReasonRuleMatch = "RULE_MATCH"`, `StreamConnected`, and so on),
  defaults 30 s, 30 s, 10 s, stream on (`toggleslash.go`).
- [x] The evaluator has no platform regular expression: `grep -rn '"regexp' --include=*.go .` finds only the
  strings inside `source_test.go`; `TestEvaluatorUsesNoPlatformRegex` holds it.
- [x] The lint configuration names its rule set and each ignore has a reason: `.golangci.yml`.
- [x] Every runtime dependency has a DESIGN.md line: there is none (DESIGN.md section 4; `go.mod` has no `require`;
  `TestNoRuntimeDependencies`).
- [x] No telemetry, no identifying header: `grep -n 'Header.Set\|Header.Add' *.go` over non-test files finds
  `Authorization` (`http.go:62`), `If-None-Match` (`sync.go:133`), `Accept` (`sync.go:187`) and `Content-Type`
  (`usage.go:140`) only; `TestSourceSetsNoRequestHeaderBeyondTheFourTheContractNames` holds it. The headers
  `net/http` adds are in the table above.
- [x] Every extra beyond the contract is allowed by contract 13: `WithHTTPClient` (a transport and proxy hook, no new
  data sent) and the unexported seams. None weakens contract 5.
- [x] Test seams are options on `create` (unexported) and a fake clock: `withClock`, `withJitter`, `withWatchdog`,
  `withEvaluator` in `toggleslash.go`; no global and no environment read by the SDK
  (`grep -rn 'Getenv\|LookupEnv'` finds only `source_test.go`).
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK, `set -eu`, call tools
  through `mise exec --`: each starts `#!/bin/sh`; `ls -l sdks/go/bin` shows the executable bit; they run with the
  toolchain `mise.toml` pins.
- [x] `test-service/README.md` maps every accessor to a method: its table has `bool`, `string`, `number`, `int`,
  `json` and `any`.
- [x] The workflow follows the skeleton, filters on its paths, pins every action by commit SHA:
  `.github/workflows/sdk-go.yml`; `bun scripts/check-workflows.ts .github/workflows/*.yml` printed
  `8 workflow(s) read, 0 failure(s)`.
- [x] No em or en dash: `bun scripts/check-dashes.ts` passed.
- [x] `bun run format:check` passes: "All matched files use the correct format."
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints `scope ok` (run last, below).
- [x] No package was published and no name reserved: no registry account was used and no tag was made; the README
  says there is no package.
- [x] Commit subject is a Conventional Commit, not `feat`, with no `Co-Authored-By` and no AI attribution line: the
  subject is `chore(sdk-go): the Go server SDK regenerated cold from the authoring prompt`.
- [x] The workflow job is `runs-on: ubuntu-latest`, no `if:`, triggers `pull_request` and `push` only, installs its
  own toolchain, and `check-workflows.ts` passes: see the workflow line above.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: it has the seven sections in the template's order, with this file's
  "Header table" section between "Proposed prompt changes" and "Checklist".
