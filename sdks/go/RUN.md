# Go SDK: run report

## Prompt version

- Prompt: 2a00dd1f0ac70bdf60728c6e488f2e2d7feb803d (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run; `origin/main` had not moved when the run ended, so the same commit)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), run as ticket FS-133 (sweep run 10, Go regenerated cold; `sdks/go` and `.github/workflows/sdk-go.yml` were removed with `git rm` first and not read from the tree or from history)
- Date: 2026-11-11
- Input: language Go; runtime chosen: Go 1.26 (`go 1.26` in `go.mod`); minimum version chosen: 1.26, the oldest release still supported; package manager chosen: Go modules (`go.mod`, no third-party dependency, so no `go.sum`)
- Toolchain (`mise.toml` pins, and what the version commands print):
  - go 1.27.4: `go version go1.27.4 linux/amd64`
  - golangci-lint 2.15.0: `golangci-lint has version 2.15.0 built with go1.27.0`
  - zig 0.17.1: `zig version` prints `0.17.1` (the C compiler for `-race`)

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371` and `pins: 17`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 35 passed of 35 (`go test -count=1 -v ./...`, top-level tests across the three packages with tests, plus 5 subtests), all under `-race` in `bin/test`
- Repeat runs: `bin/test` passed 10 times in a row (every run printed `vectors: 1371`, `pins: 17`), and once more with 12 busy shell loops running (`nproc` is 12) from before `bin/test` started until it ended: pass, same counts. The first series stopped at 9 passes because of a flaky test, see Decisions 12.

## Decisions

1. **Package name and module path.** Module `github.com/madangryscientist/toggleslash/sdks/go`, package `toggleslash`, because PROMPT.md phase 1 item 7 says exactly this ("name the package `toggleslash`" where the directory name is a keyword). Nothing is registered.
2. **No runtime dependency.** `encoding/json` fails on `1e999` and replaces a lone surrogate; `regexp` is RE2 and differs from the spec's patterns (evaluation.md, "Patterns"; language-notes/go.md). So the SDK has its own JSON reader and writer (`internal/jsonv`, strings kept as WTF-8) and its own Pike VM (`internal/evaluator/vm.go`). `DESIGN.md` section 4.
3. **Minimum Go 1.26, pinned toolchain Go 1.27.4.** 1.26 is the oldest release still supported on the run date; the code needs only `atomic.Pointer`, `min`/`max` and `for range int`. 1.27.4 is the newest `mise ls-remote go` lists (language-notes/go.md says to pin the newest it lists and say so here).
4. **Test framework and lint.** The standard `testing` package with `httptest`-style loopback servers over `net/http`; `gofmt -l`, `go vet` and `golangci-lint` v2 with the `standard` set plus bodyclose, errorlint, misspell, nilerr, noctx, unconvert, unparam and usestdlibvars (`.golangci.yml`, each named with a reason). language-notes/go.md says an earlier run dropped `usestdlibvars` as noisy in table tests; this run kept it for the non-test code and excluded it for `_test.go` with a comment, which keeps the check where it helps.
5. **`-race` needs cgo.** `bin/test` uses `cc`/`gcc` when the machine has one and otherwise the pinned zig by path (`CC="$(mise where zig)/zig cc"`, never the `zig` on PATH: `~/.local/bin/zig` here is 0.16.0 and shadows the pinned one, as language-notes/go.md warns). `mise.toml` pins zig 0.17.1. The workflow installs zig too (PROMPT.md "Entry points" says the runner is not assumed to have a C compiler), although `ubuntu-latest` has gcc and zig is then unused.
6. **Accessor shape.** Typed methods (`BoolVariation`, `StringVariation`, `IntVariation` with `int64`, `FloatVariation`, `JSONVariation`) plus the same five with a `Detail` suffix returning `Detail[T]`. A Go method cannot take type parameters, and a free generic function would hide the integer rule. Options are functional (`Option func(*options)`), so "not given" is distinct from false or zero.
7. **Three locks, and a flush lock.** The snapshot (ruleset plus ETag) is one immutable struct in an `atomic.Pointer`, so a reader sees one ruleset or the other (contract 6.3). `flushMu` is a third lock held across a usage request so two flushes cannot race; evaluation never takes it, which is what keeps `variation` away from a hung flush (contract 5.2; `TestEvaluationDoesNotWaitOnAHungFlush`).
8. **`Retry-After` holds use the real clock.** The `now` seam changes only the UTC day for usage counts and `lastSyncAt`. A hold is compared against `time.Now`, so a test cannot make one expire early by moving the fake clock; the holds are tested through the cap and with short real waits.
9. **Calls from the SDK's own goroutine (contract 6.4).** A goroutine id read with `runtime.Stack` (`TestGoroutineIDParses` guards the parse) tells `Ready` and `Close` they were called from a callback on the SDK's goroutine. `Ready` there returns the state held at once; `Close` there starts the shutdown on a new goroutine and returns. Both are SHOULDs and both are met.
10. **Extras (contract 13).** `Flush(ctx)` sends the pending counts now. There is no `WithHTTPClient`: the SDK owns its transport so `Close` can drop connections and a redirect is never followed (`TestARedirectIsNotFollowed`). The unexported seams `withJitter`, `withNow`, `withWatchdog`, `withEvaluator` and `withTransport` are options inside the package and are set only by tests.
11. **Pattern compile is eager.** A ruleset's patterns compile when the ruleset is built, so the cost bound of evaluation.md "Patterns" is paid at load, not on the first evaluation, and a bad pattern is known before any flag is evaluated. `FlagVersion` in a hook event is set only for a ready flag in the held ruleset, converted from the ruleset's number.
12. **A flaky test was fixed, not retried.** The first 10-run series failed on run 10 with `TestRequestsCarryOnlyWhatTheContractAllows` timing out waiting for the second ruleset. The fake stream said `connected` with whatever tag the server held when the stream opened, so the second load happened only when the stream happened to open after the test had set tag 2. The test now pushes a `changed` event after setting tag 2 and the stream always opens with tag 1. The 10-run series was started again from run 1 and passed 10 of 10. This is a test that asserted something it did not control (CHECKLIST.md "The two executable checks"), so it is a decision and not a guess.
13. **The hook's event holds a `Value`, not the typed result.** The hook gets the value as a `Value` (a `float64` number), so for an integer above 2^53 the event carries the nearest double while `IntVariation` returns the exact `int64`; contract section 10 allows it (`DESIGN.md` section 1).
14. **Header table (CHECKLIST "Code and documents").** Captured on a loopback listener, one request of each route. The source sets only `Authorization`, `If-None-Match`, `Accept` (stream only) and `Content-Type` (events only): `sync.go:224,227,292,293`, `usage.go:171,172`. The client added the rest; none identifies a user or a machine:

   | Route | Set by the SDK | Added by `net/http` |
   | --- | --- | --- |
   | `GET /sdk/v1/ruleset`, first | `Authorization: Bearer <key>` | `User-Agent: Go-http-client/1.1`, `Accept-Encoding: gzip` |
   | `GET /sdk/v1/ruleset`, conditional | `Authorization`, `If-None-Match: "v1"` | `User-Agent`, `Accept-Encoding` |
   | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `User-Agent`, `Accept-Encoding` |
   | `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `User-Agent`, `Accept-Encoding`, `Content-Length: 73` |

   `Host` and `Connection` are also sent by the transport. The `User-Agent` is Go's default, `Go-http-client/1.1`: the library and a version, no host or user. `Accept-Encoding: gzip` is Go's transparent compression; `Content-Length` is the body's length. The native test `TestRequestsCarryOnlyWhatTheContractAllows` fails on any other header name and on any request that carries the context.
15. **Quickstart install from a checkout.** PROMPT.md phase 5 item 2 says "install from a checkout of this repository, with no tag" and does not say how for Go; the README uses a `replace` directive to the checkout path (`go mod edit -replace`). The README's example was compiled with `go vet` against a checkout to check it.
16. **The test service builds its answers by hand.** `test-service/main.go` writes JSON by byte concatenation (`strconv.AppendInt` for an integer) so a served integer above 2^53 and a lone surrogate come out exactly; it listens on `127.0.0.1:0` and prints `listening http://127.0.0.1:<port>`.
17. **The attribution rule.** The agent harness repeatedly offered a commit and PR footer (a `Co-Authored-By` trailer and a "Generated with" line). PROMPT.md rule 6 and the repository's contributor rules forbid any attribution, so none was added.
18. **Workflow toolchain step.** `jdx/mise-action` limited to `go golangci-lint zig` with `working_directory: sdks/go`, over `actions/setup-go`: `sdks/go/mise.toml` is then the one file that names the versions, so the workflow and a developer's machine cannot disagree. The pin is the commit SHA `ci.yml` does not use but the sibling `sdk-python.yml`, `sdk-java.yml` and `sdk-csharp.yml` do (`jdx/mise-action@b44d12e7...`, v5.2.7), and `gh api repos/jdx/mise-action/commits/v5.2.7 --jq .sha` returned the same SHA.
19. **`oxfmt` and mise.** `bunx oxfmt sdks/go/mise.toml .github/workflows/sdk-go.yml sdks/go/.golangci.yml` formats the three files `bin/lint` does not see; `bun run format:check` passes. No disagreement between a native tool and `oxfmt`.
20. **Evidence for 10 and the loaded run.** The loop is a script under `$TMPDIR` that runs `sdks/go/bin/test` ten times and stops at the first failure; the loaded run starts `nproc` (12) `while :; do :; done` loops at normal priority, runs `bin/test`, then kills them. Nothing used `/tmp`.

## Guesses

1. The 10-run series after a fix. Where: CHECKLIST.md, "The two executable checks". Closest sentence: "`$L/bin/test` passes 10 times in a row ... Such a failure is a bug in the test ... fix the test, and record it in `RUN.md` as a decision". It does not say whether the 9 passes before the failure still count or the series restarts. Assumed: the series restarts from run 1 after the fix, and 10 fresh passes are needed (they were run).
2. Which `lastError` an oversized SSE event gets. Where: sdk-contract.md, 7.2 (the stream) and 8 (status). Closest sentence: the section 8 list of `lastError` codes (`timeout`, `network`, `bad_ruleset`, `http_<status>`), and the 7.2 "Bounds" paragraph, which gives no event size bound. A parser that reads without limit lets a server exhaust memory, but no code is named for a stream event that never ends. Assumed: the parser refuses an event over 1 MiB and the stream ends as `network` (`TestSSEEventTooLarge`), the same as any stream that fails mid-read.
3. Whether a `2xx` events answer whose body read fails counts as sent. Where: sdk-contract.md, 9 ("What is kept"). Closest sentence: "`2xx` | sent. The next request goes out." It says the status decides but does not say the body is read or ignored. Assumed: the status decides, the body is read (up to 1 MiB, under the same deadline) so the connection can be reused, and a read failure changes nothing about what was sent.
4. What "Native tests: n passed of n" counts. Where: RUN-TEMPLATE.md, "Results". Closest sentence: "Native tests: <n> passed of <n>". It does not say whether a Go subtest or a table case counts. Assumed: top-level `Test...` functions as `go test -v` lists them, with the subtest count beside.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `CHECKLIST.md` "The two executable checks", after "fix the test, and record it in `RUN.md` as a decision, not a guess.", add: "Then start the ten runs again from the first: passes before the failure do not count."
2. For guess 2, in `docs/sdk-contract.md` 7.2 "Bounds", add one sentence: "A server that sends an event larger than the SDK is willing to hold (a bound of the SDK's choosing, at least 1 MiB) ends the stream as `network`." (or name the code the contract wants).
3. For guess 3, in `docs/sdk-contract.md` section 9, after the `2xx` row, add: "The body of a `2xx` is read only to let the connection be reused; a failure reading it does not change the outcome."
4. For guess 4, in `RUN-TEMPLATE.md` "Results", write: "Native tests: <n> passed of <n> (top-level test functions; say what a subtest or a table case counts as)".
5. Not tied to a guess, in `language-notes/go.md`, under "`-race` needs cgo": add "The workflow's toolchain step can list `zig` in `install_args` even though `ubuntu-latest` has gcc, so one file (`mise.toml`) names every tool; `bin/test` then uses `cc` first and zig only when none exists." (this run found the line in PROMPT.md "Entry points" and the line in go.md easy to read as two different instructions.)

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equals phase 2's: `bin/test` prints `vectors: 1371` and `pins: 17`; `bun -e '...'` (the phase 2 command) prints `1371 17`. The tests are `TestVectors` and `TestBucketPins` in `internal/evaluator/vectors_test.go`, which read `fixtures/evaluation/` at test time.
- [x] `bin/test` 10 in a row, and once under load, counts recorded: 10 of 10 (`vectors: 1371`, `pins: 17` every run) after the fix in Decisions 12; one more with 12 busy loops started before and kept running to the end of `bin/test` including its build: pass, same counts. The first series failed at run 10 (Decisions 12, Guesses 1).
- [x] The harness exits 0, with only the 2 named skips (`integer-accessor` is advertised, so no scenario is skipped): `bun run conformance -- --service sdks/go/bin/test-service` printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, exit 0.

### What the harness checks

- [x] Both context shapes accepted (contract 3): the harness, `privacy.json`, the `kinds.json` vectors; native `TestLoadAndEvaluate`.
- [x] Results follow contract 4's order and the mismatch rule, with the SDK's own error codes: the harness, `evaluation.json`, `usage.json`.
- [x] With `integer-accessor`, a served fraction is `TYPE_MISMATCH` and a served `2.0` is `2`: the harness, `evaluation.json`; native `TestTypeMismatchAndIntegerRange`. Advertised in the service's `GET /` answer.
- [x] No failure makes an operation raise (5.1): the harness, every scenario (the service recovers a panic into a 500 and the scenario fails); native `TestHookAndOnErrorFailuresAreContained`.
- [x] Evaluating before the first load answers `NOT_READY` at once (5.2, 5.4): the harness, `load.json`; native `TestNotReadyAnswersAtOnceWhileTheRulesetRequestHangs`.
- [x] `ready` settles after the first attempt, bounded, and may be called again; false after `close` if not settled (5.3, 11): the harness, `load.json`, `close.json`; native `TestReadyReturnsFalseWhenItsContextEndsFirst`.
- [x] An answer that is not a ruleset never replaces the one held (5.5): the harness, `status.json`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining (7, 7.1): the harness, `load.json`, `status.json`; native `TestRequestsCarryOnlyWhatTheContractAllows`.
- [x] The stream: events, SSE parsing, refresh on change, a failed refresh ends the stream (7.2): the harness, `stream.json`, `load.json`, `status.json`; native `TestSSEParser` (chunks of 1 to 1 MiB), `TestSSECRLFSplitAcrossReads`.
- [x] The loop and the jittered backoff (7.3): the harness, `backoff.json`, `stream.json`; native `TestBackoffDoublesFromOneSecondAndIsJittered`.
- [x] `429` and `Retry-After` on every route (7.4, 9): the harness, `rate-limit.json`; native `TestRetryAfterIsReadAsDigitsAndCapped`, `TestAHoldOnlyLengthens`.
- [x] Status fields, states and `lastError` codes (8): the harness, `status.json`, the shape check on every status read; native `TestConstantsSpelling`.
- [x] Usage counts: what counts, summing, batches of 500, keep or drop by answer (9): the harness, `usage.json`, `rate-limit.json`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises (10): the harness, `hooks.json`; native `TestHookAndOnErrorFailuresAreContained`.
- [x] `close` in order, twice at once, during a hold, with a final flush (11): the harness, `close.json`, `rate-limit.json`, `usage.json`, `status.json`; native `TestCloseSendsTheCountsAndStopsCounting`.
- [x] No context, key or attribute in any request or `onError` message, no header beyond the protocol's (12): the harness, `privacy.json` and the after-scenario checks; native `TestRequestsCarryOnlyWhatTheContractAllows` (headers, path and body searched for the context's `needle` strings). Source search for logs and error values: `grep -n "log\.\|slog\|fmt.Print\|os.Stderr\|os.Stdout"` over the non-test SDK sources finds nothing; every `errors.New`/`fmt.Errorf` in the SDK builds fixed text (route, status number, or "an error was raised" from `describePanic`, `client.go:132`), never a context value, and a `*url.Error` (which carries the URL) is never reported, only its class.

### Native tests

- [x] Every operation called from many goroutines, `Close` racing `variation`, two `Close` calls racing, with no error, deadlock or race, under the race detector: `TestEveryOperationFromManyGoroutines` in `concurrency_test.go` (covers both the many-goroutine race and the two-close race), run under `go test -race` in `bin/test`.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TestTLSWithALocalCertificate` in `network_test.go`.
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs, and while a flush hangs: `TestNotReadyAnswersAtOnceWhileTheRulesetRequestHangs` and `TestEvaluationDoesNotWaitOnAHungFlush`.
- [x] A reader sees one ruleset or the other across a swap, and a hook's `etag` and `flagVersion` belong to the same ruleset: `TestSwapIsAtomic`.
- [x] A hook and an `onError` that call back into the client do not deadlock, and a `ready` from such a callback on the SDK's goroutine returns without waiting: `TestCallbacksMayCallBackIntoTheClient`.
- [x] A stream with no bytes for 60 s is dropped as `timeout`, a keep-alive restarts the 60 s, tested through a seam on a real loopback socket: `TestIdleStreamIsRestartedByTheWatchdog` (seam `withWatchdog`; the server keeps a stream alive with `: keep-alive` comments for several watchdog periods, then goes quiet).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam: `TestRetryAfterCapOnEveryRoute`.
- [x] Counts either side of a UTC midnight go out as two days: `TestUTCMidnightGivesTwoDays` (seam `withNow`).
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call: `TestEvaluationThatRaisesGivesTheFallbackAndOneError` (seam `withEvaluator`).
- [x] Hooks that return a future: `n/a: no futures` (the hook is a plain Go function `func(EvaluationEvent)`; one that panics is `TestHookAndOnErrorFailuresAreContained`).
- [x] With `integer-accessor`, a number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestTypeMismatchAndIntegerRange` (the `int64` range; also the harness `huge` scenario).
- [x] SHOULD: a client that is never closed does not keep the process from exiting: `TestAClientThatIsNeverClosedDoesNotKeepTheProcessAlive` in `process_test.go` re-executes the test binary as a child with a client whose request is in flight against a server that never answers, and requires exit within 15 s.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check; `README.md` links to the mapping table ("in `DESIGN.md`, section 1"): `DESIGN.md` section 1.
- [x] Names keep the canonical meaning, defaults and spelling: `TestConstantsSpelling` checks every `ReasonKind`, `ErrorCode`, `StreamState` and `LastError` constant against the spec's spelling; defaults in `options.go` (30 s, 30 s, 10 s, stream on).
- [x] The evaluator has no platform regular expression: `grep -rn "regexp" internal/evaluator/` finds only a comment in `vm.go:5` ("no backtracking and no regexp"); no import of `regexp` in any non-test SDK file.
- [x] The lint configuration names its rule set and each ignore carries a reason: `.golangci.yml` (header comment, a comment on every enabled linter, one exclusion with a comment).
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none; `DESIGN.md` section 4 says why the standard library will not do for JSON and for patterns, which are done in-package. `go.mod` has no `require`.
- [x] No telemetry and no identifying header: the source sets only `Authorization`, `Accept`, `If-None-Match` and `Content-Type` (`sync.go:224,227,292,293`, `usage.go:171,172`); the table in Decisions 14 lists what `net/http` adds, route by route, with why none identifies a user or a machine.
- [x] Every extra is one contract 13 allows (Decisions 10: `Flush(ctx)`, the seams) and none weakens contract 5 or sends anything new.
- [x] Seams are options on `New` (unexported, in-package), never a global or an environment variable: `options.go` (`withJitter`, `withNow`, `withWatchdog`, `withEvaluator`, `withTransport`); `grep -n "os.Getenv\|LookupEnv"` over non-test sources finds nothing. (`TestAClientThatIsNeverClosedDoesNotKeepTheProcessAlive` reads an environment variable in the test, not in the SDK.)
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable (`-rwxr-xr-x`), `cd` to the SDK and use only the toolchain `mise.toml` pins (through `mise exec --` when mise is on PATH).
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method (`bool`, `string`, `number`, `int`, `json`, `any`).
- [x] `.github/workflows/sdk-go.yml` follows the skeleton, filters on its paths, and pins every action by commit SHA: `actions/checkout`, `oven-sh/setup-bun` (copied from `ci.yml`) and `jdx/mise-action` (SHA resolved with `gh api`), each with its tag in a comment.

### Repository

- [x] No em dash or en dash in any file written: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes (`All matched files use the correct format.`).
- [x] Only `sdks/go/` and the one workflow file changed: the phase 6 scope check prints `scope ok`.
- [x] No package was published and no name reserved; the README names no registry.
- [x] Commit subject is a Conventional Commit, `chore(sdk-go): ...`, not `feat`, with no `Co-Authored-By` and no AI attribution line (Decisions 17).
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:` guard, triggers `pull_request` and `push` only, installs its own toolchain (`jdx/mise-action`), and `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: the seven sections in order, with evidence for every line of this list.
