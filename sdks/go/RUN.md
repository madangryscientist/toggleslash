# Go SDK: run report

## Prompt version

- Prompt: 9fc28510c0b4d780a1250d7fbcc26d6c51465d8b (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), run in the Claude Code harness
- Date: 2026-11-11
- Input: language Go; runtime chosen: the gc toolchain (the mainstream Go); minimum version chosen: Go 1.26;
  package manager chosen: Go modules with `go build` and `go test`
- Toolchain: `mise.toml` pins `go = "1.27.4"` (`go version` prints `go1.27.4 linux/amd64`),
  `zig = "0.17.1"` (the C compiler `-race` needs; `mise where zig` is a directory holding `zig`, so
  `CC="$(mise where zig)/zig cc"` works) and `golangci-lint = "2.15.0"` (`golangci-lint has version 2.15.0 built with go1.27.0`).
  Around them: mise 2026.11.0, Bun 1.4.2 (as `ci.yml`). The run was a cold regeneration (FS-135): the earlier Go SDK
  and its history were not read; the TypeScript SDK and the sibling SDKs were fair reading.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`, and
  the phase 2 count command agrees)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (three runs
  in a row, the same line each time)
- Capabilities advertised: `integer-accessor`
- Native tests: 69 passed of 69 top-level tests (48 in the root package, 2 in `internal/evaluator`, 8 in
  `internal/wjson`, 4 in `internal/wtf8`, 7 in `internal/sse`), with 1379 subtests passed under `-race`
  (`go test -race -count=1 -v ./...`; the vectors are most of them)
- Stability: `bin/test` passed 10 times in a row after its last edit (the ten runs were restarted from the
  first when `bin/test` changed), and once more with 12 busy shell loops (`nproc` is 12) started before it and
  kept until it ended. Conformance passed three times in a row.

## Decisions

1. **Runtime and minimum version.** The gc toolchain; Go 1.26 as the minimum (`go 1.26` in `go.mod`). The prompt's input
   table says "the oldest version still supported upstream"; Go supports its two newest releases and 1.27 is current.
   The code needs only 1.24. DESIGN.md, "Minimum runtime and toolchain".
2. **Pinned toolchain.** Go 1.27.4, the newest `mise ls-remote go` lists, checked against go.dev (language-notes/go.md,
   "`mise ls-remote go` can hide the newest releases"). zig 0.17.1 and golangci-lint 2.15.0 are the newest `mise ls-remote` lists.
3. **Module and package name.** Module `github.com/madangryscientist/toggleslash/sdks/go`, package `toggleslash`
   (PROMPT.md phase 1 item 7 settles both; `go` is a keyword). Nothing is registered under it.
4. **Lint and format.** `go vet`, `gofmt -l` and golangci-lint v2 (`bin/lint`). Rule set: the `standard` group
   (errcheck, govet, ineffassign, staticcheck, unused) plus bodyclose, copyloopvar, errorlint, gocritic, misspell,
   noctx, nilerr, unconvert and unparam, each with its reason in `.golangci.yml`. `usestdlibvars` is left out
   (language-notes/go.md, "Lint"). The only ignores are errcheck on `(io.Closer).Close` and
   `(net/http.ResponseWriter).Write`, with reasons beside them.
5. **Tests.** The standard `testing` package, `net/http/httptest` and `-race`. `-race` needs cgo, so `bin/test` sets
   `CC="$(mise where zig)/zig cc"` (language-notes/go.md, "`-race` needs cgo"; the `<dir>/zig` layout was
   confirmed on this machine). Without `mise` it uses `$CC`, then `cc`. The workflow installs zig with the rest.
6. **Workflow toolchain step.** `jdx/mise-action` limited to `go zig golangci-lint` over `actions/setup-go`, because one
   file (`mise.toml`) then names every tool and the workflow cannot drift from it (PROMPT.md phase 5 makes the choice a
   decision). Its pin `b44d12e7d0d8ee7461bb893e1b1993500433185b # v5.2.7` was resolved with
   `gh api repos/jdx/mise-action/git/ref/tags/v5.2.7`, which answers that commit; the checkout and setup-bun lines
   are copied from `ci.yml`.
7. **Own goroutines.** Contract 6.4 (sdk-contract.md lines 168 to 170) says a `ready` or `close` called on "the SDK's
   own thread" must not wait. Go has no thread identity and no goroutine-local state, so the client keeps a set of
   the goroutine ids of its three goroutines (sync, flush, shutdown), read from `runtime.Stack`. See Guess 3.
8. **JSON.** `internal/wjson` is a strict RFC 8259 reader and writer holding strings as WTF-8, because `encoding/json`
   fails the spec's `1e999` and lone-surrogate rules (evaluation.md "Reading JSON"; language-notes/go.md,
   "`encoding/json`"). Response bytes pass the WHATWG UTF-8 decoder first (`internal/wtf8`).
9. **No generic accessor.** Go has no generic methods, so there are typed methods (`BoolVariation`, `StringVariation`,
   `IntVariation` returning `int64`, `FloatVariation`, `JSONVariation`) and a `Detail[T]` result. Contract 4,
   "Typed languages", settles that the integer accessor is the 64-bit signed type.
10. **Options.** Functional options; an absent option is "not given", and a duration of zero or less is the default
    (DESIGN.md, mapping table). `WithHTTPClient` copies the caller's client with `CheckRedirect` replaced and `Timeout` zero.
11. **Redirects and transport.** The default transport is a clone of `http.DefaultTransport` with `DisableCompression: true`
    (so no `Accept-Encoding`) and `CheckRedirect` returning `http.ErrUseLastResponse`, so a 3xx is `http_3xx` and
    `Authorization` is never resent (language-notes/go.md, "Redirects"). Errors carry fixed text only (`RequestError{Route, Code}`),
    never a `*url.Error`.
12. **Stream open and watchdog.** The open is bounded by a timer of `requestTimeout` that is stopped when the headers
    arrive; after that a 60 s `time.AfterFunc` is reset after every read that returns bytes and cancels the request with a
    cause (contract 7.2, "Bounds"). The request body read is bounded by `context.WithTimeoutCause`.
13. **Fake clock for tests.** A hand-written `clock` interface is an unexported option (`withClock`), with `withJitter` and
    `withWatchdog` beside it (PROMPT.md phase 3: seams are options internal to the package). The test clock keeps one waiter
    list per duration and steps only a duration someone is waiting on (`release(t, d)`), never after a fixed real pause; a
    wait of zero or less returns at once. A first version with one shared tick channel woke the wrong sleeper and hung
    the stream tests, so it was replaced rather than patched.
14. **Real-time tests.** Three tests use short real windows because they must reach a real socket: the watchdog test
    (200 ms watchdog, keep-alives every 40 ms), the open-timeout test (150 ms) and the body-timeout test. All passed 10 times
    in a row and once under full CPU load, so none asserts something it did not control. If one ever flakes, widen the window
    and record it here.
15. **Entry points.** `bin/test` runs `go test -race ./...`, then the vector and pin tests alone to print the one
    line `vectors: <n> pins: <n>`, then builds `.build/test-service` as its last step; `bin/test-service` runs that binary
    and builds it only when it is missing (PROMPT.md "Entry points"). `.build/` is in `.gitignore`.
16. **Extras (contract 13).** `WithHTTPClient`, `WithLogger` (each `onError` value becomes a `slog` warning when no
    `WithOnError` is given), `Flush`, `ReadyContext`, and the context builders (`NewContext`, `NewKindContext`, `With`,
    `MultiContext`, `ContextFromJSON`, `ContextFromValue`, `ContextFromMap`). None weakens a guarantee of contract 5 or sends anything.
17. **The hook event's `Value` for `IntVariation`.** It is a JSON `Value`, so above 2^53 it carries the nearest double while
    the caller still gets the exact `int64` (DESIGN.md, "The hook event").
18. **Disagreements between a reference and the spec.** None found, so DESIGN.md's closing section ("Where this design reads a
    reference against the spec") has no entry; every vector in `fixtures/evaluation/` passes.
19. **Conformance service.** One process per run, an `http.Server` on `127.0.0.1:0` that prints `listening http://127.0.0.1:<port>`
    to stdout (unbuffered in Go), and a recover at the handler that answers 500 with the text. Accessors map as in
    `test-service/README.md`; `number` goes to `FloatVariationDetail`, `int` to `IntVariationDetail`, and `json` and `any` to
    `JSONVariationDetail` with the fallback as a `Value`.

## Guesses

1. **How `bin/test` gets `pins: <n>`.** Where: PROMPT.md "Entry points" and language-notes/go.md, "Printing the vector count".
   Closest sentence: "`bin/test` gets `vectors: <n>` from `go test -v -run '^TestVectors$' ./internal/evaluator | grep 'vectors:'`".
   The prompt requires the line `vectors: <n> pins: <n>`, but the note's command prints only the vector count, because the pins
   are logged by a different test, so following the note gives no pins figure. This run's first `bin/test` followed the note and
   printed `vectors: 1371` alone; the gap showed only when phase 6 reread "Entry points". Assumed: run both tests
   (`-run '^(TestVectors|TestBucketPins)$'`) and join the two counts into the one line.
2. **Whether a warm build cache counts for the loaded run.** Where: CHECKLIST.md, "The two executable checks", second line.
   Closest sentence: "started before `bin/test` and kept running until it ends, its build included". It does not say if
   a cached build counts as the build. Go caches compiled packages, and `bin/test` uses `-count=1` so tests always rerun but
   compilation is cached. Assumed: the loaded run with a warm cache counts, because the load covers every test and the
   test-service link; it took 16 s against about 18 s unloaded.
3. **What "the SDK's own thread" means in Go.** Where: sdk-contract.md section 6.4. Closest sentence: "A `ready` called from such a
   callback on the SDK's own thread SHOULD NOT wait for the load that thread runs". Go has goroutines and no thread
   or goroutine identity in the standard library, and neither the contract nor language-notes/go.md says how to tell a call is
   on one. Assumed: compare the caller's goroutine id (from `runtime.Stack`) with the three the SDK started. Tests:
   `TestReadyFromACallbackDoesNotWait` and `TestCloseFromACallbackDoesNotWaitForItself`.
4. **Whether a panic value may be formatted into an `onError` error.** Where: sdk-contract.md section 12. Closest sentence: the
   rule that no `onError` message carries a context, key or attribute. A recovered panic from the SDK's own evaluation or from
   the application's hook is reported as `toggleslash: <what> panicked: <value>`. The text of a panic is the Go runtime's or the
   application's own, and the evaluator panics only with an internal `invalidPattern{}` that it recovers itself, so no context
   reaches it. The section does not say whether formatting a recovered value is allowed. Assumed: yes, and
   `TestNoLogCarriesAContext` checks the errors from a failing load.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `language-notes/go.md`, "Printing the vector count", after "`bin/test` gets `vectors: <n>` from ...", add:
   "The pins are logged by `TestBucketPins`, not `TestVectors`. Run both with `-run '^(TestVectors|TestBucketPins)$'`, read the two
   counts and print one line `vectors: <n> pins: <n>`, which is what PROMPT.md's Entry points require."
2. For guess 2, in `CHECKLIST.md`, second line of "The two executable checks", after "its build included", add: "(a warm build
   cache is fine where the toolchain caches; the load covers every test run)", or, if a cold build is meant, "after clearing the build cache".
3. For guess 3, in `language-notes/go.md`, add a bullet: "**Telling a call is on the SDK's own goroutine.** Go has no goroutine
   identity. Read the goroutine id from the first line of `runtime.Stack(buf, false)` ('goroutine N [') and keep a set of the ids of
   the goroutines the client starts; compare on `Ready` and `Close` (contract 6.4)."
4. For guess 4, in sdk-contract.md section 12 (a change for the planner, not this run), after the no-context rule, add: "A recovered
   panic or exception from the SDK's own code or an application hook may be formatted into an `onError` value: it is the code's
   own text, not the context."

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin: `bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count
  command printed the same two figures (`TestVectors` reads `../../fixtures/evaluation` at test time and fails on none read;
  `TestBucketPins`, `internal/evaluator/vectors_test.go`).
- [x] `bin/test` 10 times in a row, and once under load: 10 of 10 passed after the last change to `bin/test`; one run under 12
  busy loops (`nproc` = 12) started before it passed in 16 s; counts as in Results. No test needed a fix for load.
- [x] The harness exits 0, with only the 2 named vector skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed,
  0 failed, 2 skipped`, three runs.

### What the harness checks

- [x] Both context shapes: the harness, `privacy.json`, `kinds.json` vectors (all pass through the service).
- [x] Result order and the mismatch rule: the harness, `evaluation.json`, `usage.json`; native `TestTypeMismatchIsNotCounted`,
  `TestNotReadyServesTheFallback`.
- [x] `integer-accessor`: the harness, `evaluation.json` (the `huge` scenario passes); native `TestIntegerAccessor`.
- [x] No operation raises: the harness (every scenario; the service answers 500 on a panic and none does).
- [x] `NOT_READY` at once: the harness `load.json`; native `TestNotReadyServesTheFallback`.
- [x] `ready` settles bounded and may be called again: the harness `load.json`, `close.json`.
- [x] A non-ruleset never replaces the one held: the harness `status.json`; native `TestBadRulesetKeepsTheLastGoodOne`, `TestBadShapes`.
- [x] The ruleset request, `If-None-Match`, ETag kept exactly: the harness `load.json`, `status.json`; native `TestConditionalRequest`,
  `TestNotModifiedWithNoRulesetIsAnError`.
- [x] The stream: the harness `stream.json`, `load.json`, `status.json`; native `TestStreamChangeFetchesTheRuleset`,
  `TestStreamDoesNotFetchWhenTheTagIsHeld`, `TestStreamFetchFailureEndsTheStream`, `TestStreamEndedCleanlyLeavesLastError`;
  parser tests in `internal/sse/sse_test.go` (7 tests).
- [x] The loop and the jittered backoff: the harness `backoff.json`, `stream.json`; native `TestBackoffDelay`.
- [x] `429` and `Retry-After` on every route: the harness `rate-limit.json`; native `TestRetryAfterParsing`.
- [x] Status fields, states and `lastError` codes: the harness `status.json` (and its shape check on every read).
- [x] Usage counts: the harness `usage.json`, `rate-limit.json`; native `TestUsageIsSummedPerFlagIndexAndDay`, `TestUsageBatchesOf500`,
  `TestUsageServerErrorKeepsTheCounts`, `TestUsageRefusedBatchIsDropped`, `TestUsageNetworkFailureKeepsTheCounts`.
- [x] Hook events, a hook that raises, an `onError` that raises: the harness `hooks.json`; native `TestHookEventFields`,
  `TestHookPanicGoesToOnErrorAndTheAnswerIsTheSame`, `TestOnErrorThatPanicsIsIgnored`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`; native `TestCloseTwiceAndConcurrently`, `TestCloseSendsTheFinalFlush`,
  `TestCloseDuringAHoldReportsTheLostCounts`.
- [x] No context, key or attribute in a request or `onError`, no header beyond the protocol's: the harness `privacy.json` and its
  after-every-scenario checks. Log search: `grep -rnE "log\.|slog\.|fmt\.(Print|Fprint)|os\.Std(err|out)|println" --include=*.go sdks/go
  | grep -v _test.go | grep -v test-service` finds only `options.go:28`, `options.go:107` and `client.go:104` to `106`, the opt-in
  `WithLogger`, which logs `err.Error()` of a `RequestError` (route and code), the unsent-count message or a panic text (guess 4);
  no other file writes a log line. Native `TestNoLogCarriesAContext`.

### Native tests

- [x] Every operation from many goroutines, `close` racing `variation`, two `close` calls racing, under the race detector:
  `TestEveryOperationRaces` (close_test.go) and `TestCloseTwiceAndConcurrently`; `bin/test` runs everything under `-race` with zig as
  the C compiler.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TestHTTPSThroughALocalCertificate` (`httptest.NewTLSServer`,
  with `WithHTTPClient(srv.Client())`, the client that trusts that server's certificate).
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a usage flush hangs:
  `TestVariationDoesNotWaitOnTheNetwork` (both routes hang, 1000 calls under 2 s); `TestCloseIsBoundedWhileARequestHangs` for `close`.
- [x] One ruleset or the other across a swap, `etag` and `flagVersion` together: `TestHookSeesAConsistentPairUnderSwaps`.
- [x] A hook and an `onError` that call back in, and `ready` from a callback does not wait (SHOULD): `TestCallbacksMayCallBackIn`,
  `TestReadyFromACallbackDoesNotWait`, `TestCloseFromACallbackDoesNotWaitForItself`.
- [x] A stream with no bytes for 60 s is dropped as `timeout`, and a keep-alive restarts the 60 s, through a seam on a real
  loopback socket: `TestStreamWatchdogAndKeepAlive` (`withWatchdog(200ms)`, keep-alives every 40 ms for 700 ms with the stream still
  connected, then silence gives `disconnected` and `timeout`); `TestStreamOpenTimeout` for the open.
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through the fake clock:
  `TestRetryAfterIsCappedOnEveryRoute` (three subtests).
- [x] Counts either side of a UTC midnight go out as two days: `TestUsageIsSummedPerFlagIndexAndDay` (usage_test.go lines 36 to 40 step the
  clock from 23:59:59 to 00:00:00 UTC).
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`:
  `TestEvaluationThatPanicsGivesTheFallback`.
- [x] Hooks that return a future: n/a: no futures (a Go hook is a plain `func(EvaluationEvent)`).
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestIntegerAccessor` (`9223372036854775807`
  reads as 2^63 and is refused; a fraction and a string likewise) and the harness `huge` scenario.
- [x] A client never closed does not keep the process from exiting (SHOULD): `TestNeverClosedClientDoesNotKeepTheProcessAlive` (the test re-runs
  its own binary as a child that creates a client and returns without closing it, and the child must exit within 20 s).

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check (printed nothing, exit 0) and has the mapping table (sixteen rows, `README.md` links it).
- [x] Names keep their canonical meaning and defaults; the closed sets keep their spelling: DESIGN.md, "The closed sets keep their spelling"
  (`ReasonKind`, `ErrorCode`, `StreamState` string types with the contract's literals; `Status.LastError` a plain string).
- [x] The evaluator has no platform regular expression: `grep -rn regexp --include=*.go sdks/go/internal/evaluator` finds only the two
  comments in `result.go:7` and `pattern.go:7` that say it imports none; `pattern.go` is the spec's parser and Pike VM over code points.
- [x] The lint configuration names its rule set and each rule carries a reason: `.golangci.yml` (header comment and one comment per linter and
  per errcheck exclude).
- [x] Every runtime dependency has a line in DESIGN.md: there is none (DESIGN.md, "Dependencies"; `go.mod` has no `require`).
- [x] No telemetry; request headers: the source sets `Authorization`, `Accept`, `If-None-Match` and `Content-Type` only (`grep -rn
  "Header\.\(Set\|Add\)" sdks/go --include=*.go` outside tests prints `http.go:75` (the loop over the per-route list) and `http.go:147`,
  `148`). `TestHeadersSent` checks every recorded request. One request per route was captured on a raw loopback socket, and the
  headers Go's `net/http` added on its own are:

  | Route | Headers on the wire | Added by `net/http` |
  | --- | --- | --- |
  | `GET /sdk/v1/ruleset` | `Host`, `User-Agent: Go-http-client/1.1`, `Accept: application/json`, `Authorization: Bearer <key>` | `Host`, `User-Agent` |
  | `GET /sdk/v1/ruleset` with `If-None-Match: "v1"` | the same four plus `If-None-Match` | `Host`, `User-Agent` |
  | `GET /sdk/v1/stream` | `Host`, `User-Agent: Go-http-client/1.1`, `Accept: text/event-stream`, `Authorization` | `Host`, `User-Agent` |
  | `POST /sdk/v1/events` | `Host`, `User-Agent: Go-http-client/1.1`, `Content-Length: 73`, `Authorization`, `Content-Type: application/json` | `Host`, `User-Agent`, `Content-Length` |

  Why none identifies a user or a machine: `Host` is the server's own address, the one the operator configured. `User-Agent`
  is the fixed library string `Go-http-client/1.1` with no version of the application, host name or user. `Content-Length` is the
  body size. `Accept-Encoding` is not sent (`DisableCompression`) and `Connection` did not appear on a loopback request,
  as language-notes/go.md says. `TestHeadersSent` also allows `Accept-Encoding` but asserts it is absent.
- [x] Extras are ones contract 13 allows: Decisions 16 and 17; none weakens contract 5 or sends anything new.
- [x] Test seams are package options: `withClock`, `withJitter`, `withWatchdog` and `WithHTTPClient` in `options.go` and
  `helpers_test.go`; the SDK reads no environment variable (`grep -rn "Getenv\|LookupEnv"` outside tests finds only a doc comment
  in `toggleslash.go:8`).
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are mode 100755 in the index, `cd` to the SDK and use only `mise.toml`'s tools
  (`mise exec --`, zig by path).
- [x] `test-service/README.md` maps every accessor: `bool`, `string`, `number`, `int`, `json`, `any`, plus the capability.
- [x] `.github/workflows/sdk-go.yml` follows the skeleton, filters on its paths, pins every action by SHA, `ubuntu-latest`, no `if:`:
  `bun scripts/check-workflows.ts .github/workflows/*.yml` printed `8 workflow(s) read, 0 failure(s)`.

### Repository

- [x] No em or en dash: `bun scripts/check-dashes.ts` printed nothing.
- [x] `bun run format:check` passes (`All matched files use the correct format`), with `mise.toml` and the workflow formatted by `bunx oxfmt`.
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check printed `scope ok`.
- [x] Nothing published and no name reserved: no registry account, upload, tag or package name; `go.mod` names a module path under the repository only.
- [x] The commit subject is `chore(sdk-go): the Go server SDK regenerated cold from the authoring prompt`, no `Co-Authored-By` and no AI attribution
  line anywhere.
- [x] The workflow is `runs-on: ubuntu-latest`, no `if:` guard, triggers `pull_request` and `push` only, installs its own toolchain
  (`jdx/mise-action` with `install_args: go zig golangci-lint`), and `check-workflows.ts` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: the section-heading check printed nothing.
