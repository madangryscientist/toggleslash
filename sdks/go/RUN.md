# Go SDK: run report

## Prompt version

- Prompt: `6125364d46a20b99b43762833ad1073f38248d8e` (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: claude-sonnet-5-5 (Claude Sonnet 5.5), in Claude Code
- Date: 2026-11-11
- Input: language Go; runtime chosen: the Go toolchain; minimum version chosen: Go 1.26 (the oldest release
  still supported, see `DESIGN.md` section 6); package manager chosen: Go modules (module
  `github.com/madangryscientist/toggleslash/sdks/go`, nothing published). This is a cold run (FS-131): the
  previous `sdks/go` and its workflow were removed from the tree before phase 0 and not read.
- Toolchain: `mise.toml` pins `go = "1.27.3"` (`go version go1.27.3 linux/amd64`), `zig = "0.17.1"`
  (`zig version` 0.17.1, the C compiler `-race` needs) and `golangci-lint = "2.15.0"`
  (`golangci-lint has version 2.15.0 built with go1.27.0`). go.dev lists 1.27.4 as newer, but
  `mise ls-remote go` stopped at 1.27.3, so 1.27.3 is the pin (language-notes/go.md, "`mise ls-remote go` can
  hide the newest releases").

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`;
  the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 56 top-level tests and 8 subtests passed, 0 failed (`go test -race -count=1 -v ./...`; 44 in
  the SDK package, 1 in the evaluator that runs every vector, 5 in `internal/wtf8`, 6 in `jsonvalue`)
- Repeat runs: `bin/test` passed 10 of 10 in a row (after one fix, see Decisions 14), and once more with
  12 busy shell loops (`nproc` = 12) running at normal priority.

## Decisions

1. **Own JSON reader, strings held as WTF-8** (`jsonvalue/`, `internal/wtf8/`), over `encoding/json`: it errors
   on `1e999` and replaces a lone surrogate, which the spec forbids (language-notes/go.md, "`encoding/json`").
   `DESIGN.md` section 5.
2. **No runtime dependency.** The standard library covers HTTP, SHA-256, `sync` and `math/rand/v2`.
   `DESIGN.md` section 4.
3. **Lint: `golangci-lint` 2.15.0 with the `standard` set plus `bodyclose`, `contextcheck`, `errorlint`,
   `gocritic`, `misspell`, `nilerr`, `noctx`, `unconvert` and `unparam`**, with `gofmt -l` and `go vet` in
   `bin/lint`. Each extra is named with its reason in `.golangci.yml`, as is each exclusion (tests and
   `errcheck`/`unparam`; `contextcheck` on `usage.go` and `test-service/main.go`, both deliberate).
   `usestdlibvars` is left off (language-notes/go.md, "Lint").
4. **Native tests use only `testing`, `net/http/httptest` and `net`.** No test framework.
5. **Test seams are unexported options** (`withClock`, `withJitter`, `withStreamIdle`, `withEvaluator`,
   `withTLS`), never a global or an environment variable (contract 13; PROMPT.md phase 3).
6. **`-race` uses zig as the C compiler, by path** (`CC="$(mise where zig)/zig cc"`), pinned in `mise.toml`
   and installed by the workflow (PROMPT.md, "Entry points": "if a race detector needs one, `$L/mise.toml` may
   pin it, `bin/test` calls it by path"; language-notes/go.md, "`-race` needs cgo"). The README says so.
7. **`bin/test` prints `vectors: <n> pins: <n>` by running `go test -v -run '^TestVectors$' ./internal/evaluator
   | grep 'vectors:'`.** A plain `go test` hides `t.Logf` when a test passes, and PROMPT.md, "Entry points"
   says `bin/test` "prints `vectors: <n> pins: <n>` even when all pass", so the extra verbose run is how Go
   meets that.
8. **The scripts re-exec themselves through `mise exec --` with the script's absolute path**
   (`self="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"`, computed before the `cd`). The first version
   re-ran `$0` after `cd`, which broke when the script was called by a relative path (the harness is given
   `sdks/go/bin/test-service`). PROMPT.md's "Entry points" says to call each tool through `mise exec -- <tool>`;
   re-exec of the script is this run's way to do that once for several tools.
9. **`bin/test` rebuilds `bin/.test-service` every time; `bin/test-service` builds it only when it is
   missing** (PROMPT.md, "Entry points"). A stale stub from phase 2 once shadowed the real binary; `bin/test`
   always rebuilding is what fixes that.
10. **Workflow toolchain step: `jdx/mise-action` limited to `go zig golangci-lint`**, over `actions/setup-go`
    plus a zig install. PROMPT.md phase 5 names both and says "Which one is a decision". Unlimited, the action
    also installs the root `.tool-versions` (.NET and a second Bun); `mise install` run locally did pull those
    in too, which cost time and nothing else.
11. **`Close` called from an SDK goroutine (an `onError` or a hook on the loop or flush goroutine) stops
    everything and returns without waiting** (contract 6.4, SHOULD, covers `ready`; this run applies the same
    rule to `close`, since a wait there would deadlock). Found by goroutine id (`onOwnGoroutine`).
12. **`Ready` called from an SDK goroutine before the first load settled answers whether a ruleset is held and
    does not wait** (contract 6.4).
13. **A client that is never closed does not block exit.** In Go a goroutine never keeps the process alive, so
    `TestNeverClosedClientDoesNotBlockExit` is true by construction; it is kept as a guard that nothing in
    `New` does a blocking wait or registers a finalizer that waits (contract 6.6).
14. **Test bug fixed in the 10 in a row check:** `TestSyncFailures/redirect_is_not_followed` read `onError`
    right after `Ready()`, but `Ready` settles before the loop goroutine calls `onError`, so one run in ten saw an
    empty list. The test now waits for the call (`eventually`), as does `TestNetworkFailure`, the other test that read `onError` after a
    failed first load. The SDK is unchanged. Under CHECKLIST.md's rule this is a decision,
    not a guess; the ordering itself is Guess 1.
15. **`go test -race` data races in two of my own tests** (`TestCloseAndReadyFromOnErrorOnTheLoopGoroutine`,
    `TestTwoCallbacksCloseAtOnce`): the callbacks read `c` before `New` returned. Fixed with an
    `atomic.Pointer[Client]` in the test (`clientRef`), not in the SDK.
16. **Header audit, captured on a loopback listener** (`TestHeaderAudit`, `go test -v`). Headers the SDK sets:
    `Authorization`, `Accept` (stream only), `If-None-Match` (conditional ruleset only) and `Content-Type`
    (events only). Headers `net/http` adds, none naming a user or a machine:

    | Route | What the client added |
    | --- | --- |
    | ruleset | `Host`, `User-Agent` |
    | ruleset (conditional) | `Host`, `User-Agent` |
    | stream | `Host`, `User-Agent` |
    | events | `Host`, `User-Agent`, `Content-Length` |

    `Host` is the server's own address. `User-Agent` is `Go-http-client/<n>`: the library and its version, no host
    or user. `Content-Length` is the size of the usage body. `Accept-Encoding` is not sent at all, because the
    transport sets `DisableCompression = true`: the SDK reads plain bodies and never asked for gzip.
    `Connection` does not appear on these requests.
17. **Redirects are not followed** (`CheckRedirect` returns `http.ErrUseLastResponse`), so `Authorization`
    never goes to another host and a 3xx is `http_3xx` (language-notes/go.md, "Redirects").
18. **Error text is fixed words and codes only.** A `*url.Error` carries the URL, so the SDK reports `network`
    or `timeout` and never `err.Error()` of a transport error (language-notes/go.md).
19. **The SDK writes no log line.** A source search finds no `log`, `slog`, `fmt.Print*` or `os.Std*` outside
    `test-service/`: `grep -rnE 'log\.|slog|fmt\.(Print|Fprint)|os\.Std' --include=*.go . | grep -v _test | grep -v
    test-service` printed nothing.
20. **Documents.** `README.md` links `DESIGN.md` section 1 for the mapping table instead of copying it, and does
    not name a registry or a published package.
21. **Go 1.26 minimum.** The oldest release Go still supports when the run was made; nothing the SDK uses needs
    more (`DESIGN.md` section 6).

## Guesses

1. **Whether `ready` may settle before `onError` has been told of the first failure.** Where: `docs/sdk-contract.md`,
   section 5 (guarantee 3) and section 8 ("An error sets `lastError` and calls `onError`"). Closest sentence:
   "`ready` settles after the first attempt." Why it does not settle it: it names no order between the
   `Ready` return and the `onError` call, and a test that reads `onError` after `Ready` can see either. Assumed:
   `Ready` settles first and `onError` is called straight after on the loop goroutine, so a callback that blocks does
   not hold `Ready` back (contract section 5, last paragraph: "an application callback ... that blocks, blocks
   whatever called it"). The tests wait for the call.
2. **The value an `afterEvaluation` event carries for an integer fallback above 2^53.** Where:
   `docs/sdk-contract.md`, section 10 (the event field table). Closest sentence: "`value` | what the caller is about to get."
   Why it does not settle it: the event's value is a JSON number, but the Go integer accessor returns `int64`,
   which a float64 cannot hold above 2^53. Assumed: the event shows the nearest float, and `EvaluationEvent.Value`
   says so in its doc comment. The caller still gets the exact `int64`.
3. **Whether `close` from the SDK's own goroutine should wait.** Where: `docs/sdk-contract.md`, section 6.4. Closest
   sentence: the SHOULD about `ready` called from a callback on the SDK's own thread "returns without waiting".
   Why it does not settle it: it names `ready`, not `close`, and `close` is the operation that waits for the loops
   (section 11, "in order"). Assumed: `close` from an SDK goroutine does the whole shutdown but returns at once
   (Decisions 11).
4. **How a first run measures "the CPU loaded".** Where: `docs/sdk-authoring/CHECKLIST.md`, "The two executable
   checks". Closest sentence: "one busy shell loop per logical CPU, so `nproc` of them, at normal priority".
   Why it does not settle it: it does not say whether the loops start before the script or whether the build step
   counts. Assumed: the loops start first and run for the whole `bin/test`, build included.

## Stuck points

None. There was no blocker in the contract, the harness, a scenario or a vector, and nothing needed outside help.

## Proposed prompt changes

1. For guess 1, in `docs/sdk-contract.md` section 5, guarantee 3, after "`ready` settles after the first
   attempt.", add: "It may settle before `onError` is told of that attempt's failure; a test must wait for the
   `onError` call, not assume it came first."
2. For guess 2, in `docs/sdk-contract.md` section 10, after the `value` row, add: "In a language whose integer
   accessor is wider than 2^53, the event may show the nearest float."
3. For guess 3, in `docs/sdk-contract.md` section 6.4, after the `ready` sentence, add: "`close` called the same way
   SHOULD also return without waiting for the shutdown it started."
4. For guess 4, in `CHECKLIST.md`, "The two executable checks", after "at normal priority", add: "start the loops
   before the first run and keep them running through it, the build included."
5. For decision 7, in `language-notes/go.md`, add: "`bin/test` prints the vector count with `go test -v -run
   '^TestVectors$' ./internal/evaluator | grep 'vectors:'`, because a plain `go test` hides `t.Logf` on a pass."
6. For decision 8, in `PROMPT.md`, "Entry points", after "calls each tool through `mise exec -- <tool>`", add: "A
   script that re-runs itself under `mise exec --` computes its own absolute path before the `cd`; `$0` is relative
   to the old directory afterwards."
7. For decision 14, in `language-notes/go.md`, add: "`Ready` settles before `onError` runs, so a test that reads
   the collected errors after `Ready()` waits for them (`eventually`)."
8. For decision 15, in `language-notes/go.md`, add: "A test callback that needs the client reads it through an
   `atomic.Pointer`, since the callback can run before `New` returns to the test."
9. For decision 16, in `language-notes/go.md`, "Headers `net/http` adds itself", replace "`Accept-Encoding: gzip`" with
   "`Accept-Encoding: gzip` (not sent when the transport's `DisableCompression` is true)" and note `Connection` does
   not show on a loopback request.

## Checklist

- [x] `bin/test` runs every vector but `buckets.json`, and every pin, and the count equals the phase 2
  command's: `internal/evaluator/vectors_test.go` (`TestVectors`); `bin/test` printed `vectors: 1371 pins: 17`,
  the count command printed `1371 17`.
- [x] `bin/test` passes 10 in a row and once under load: 10 of 10 ok (after the Decisions 14 fix; the first
  series had one failure in run 4, `TestSyncFailures/redirect_is_not_followed`), each printing `vectors: 1371
  pins: 17`; one run with 12 busy shell loops (`nproc` = 12) exited 0, all four packages `ok`.
- [x] The harness exits 0, skips only the 2 named vectors: `scenarios: 81 passed, 0 failed, 0 skipped; vectors:
  1369 passed, 0 failed, 2 skipped`; `integer-accessor` is advertised, so no scenario is skipped.
- [x] Both context shapes accepted: harness `privacy.json`, `kinds.json` vectors; native `TestLoadAndEvaluate`.
- [x] Result order and error codes: harness `evaluation.json`, `usage.json`; native `TestTypeMismatch`,
  `TestFlagNotFound`, `TestUnknownFlagAndNotReady`.
- [x] `integer-accessor`: harness `evaluation.json` (`huge` included); native `TestIntegerAccessorRange` and
  `TestTypeMismatch`.
- [x] No operation raises: harness, every scenario (the service answers 500 on a panic and the scenario fails);
  native `TestHookAndOnErrorPanics`, `TestEverythingAtOnce`.
- [x] Evaluating before the first load answers `NOT_READY`: harness `load.json`; native
  `TestUnknownFlagAndNotReady`.
- [x] `ready` settles once, bounded, repeatable, false after `close`: harness `load.json`, `close.json`; native
  `TestReadyIsFixedOnceSettled`, `TestCloseBeforeFirstLoadSettles`.
- [x] Not a ruleset never replaces the held one: harness `status.json`; native `TestBadRulesetKeepsTheHeldOne`,
  `TestSyncFailures`.
- [x] Ruleset request, `If-None-Match`, ETag, URL joining: harness `load.json`, `status.json`; native
  `TestConditionalRequestAndSync`, `TestHeaderAudit`.
- [x] The stream: harness `stream.json`, `load.json`, `status.json`; native `TestStreamEvents`,
  `TestStreamNon200`, `TestStreamKeepAliveDoesNotMoveLastSync`.
- [x] Loop and backoff: harness `backoff.json`, `stream.json`; native `TestDelay` (every failure count, with the
  jitter at both ends).
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; native `TestParseRetryAfter`,
  `TestRetryAfterOnAnyOtherStatusIsIgnored`.
- [x] Status fields, states, `lastError` codes: harness `status.json` and its shape check; native `TestSyncFailures`,
  `TestNetworkFailure`, `TestRequestTimeoutCoversTheBody`.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`; native `TestUsageBatchesOf500`, `TestUsageOutcomes`,
  `TestUTCMidnightSplitsTheDay`.
- [x] Hooks and `onError`, both raising: harness `hooks.json`; native `TestHookEvent`, `TestHookAndOnErrorPanics`.
- [x] `close` in order, twice, in a hold, with a final flush: harness `close.json`, `rate-limit.json`,
  `usage.json`, `status.json`; native `TestCloseTwice`, `TestUsage429HoldsUsageAndFinalFlushDoesNotWait`.
- [x] No context in any request or `onError` message, and no extra header: harness `privacy.json` and the
  per-scenario checks; native `TestPrivacyNothingOfAContextIsSent`, `TestHeaderAudit`, `TestSyncFailures`
  (error text holds neither URL nor key). Source search for logs: nothing found (Decisions 19).
- [x] Every operation from many goroutines, `close` racing `variation`, two `close` racing, under the race
  detector: `TestEverythingAtOnce` and `TestTwoCallbacksCloseAtOnce` (both in `concurrency_test.go`, run by
  `bin/test` under `go test -race`).
- [x] SHOULD, an `https` URL reaches the TLS path with a local certificate: `TestTLS` (`httptest.NewTLSServer`
  through the `withTLS` seam).
- [x] `variation` returns within a bound while a ruleset request or a usage flush hangs:
  `TestEvaluationDoesNotWaitOnAHungRulesetRequest`, `TestEvaluationDoesNotWaitOnAHungFlush`.
- [x] One ruleset or the other across a swap, `etag` and `flagVersion` from the same one: `TestSwapAtomicity`.
- [x] Callbacks that call back do not deadlock, `ready` from an SDK goroutine does not wait:
  `TestCallbacksMayCallBack`, `TestCloseAndReadyFromOnErrorOnTheLoopGoroutine`,
  `TestCloseFromOnErrorOnTheFlushGoroutine`.
- [x] A quiet stream is dropped as `timeout` after 60 s (through the seam, over a real loopback socket), and a
  keep-alive restarts it: `TestWatchdogEndsAQuietStream`, `TestKeepAliveRestartsTheWatchdog`,
  `TestStreamOpenIsBoundedByTheRequestTimeout`.
- [x] `Retry-After` above 300 held for 300 s on ruleset, stream and events: `TestParseRetryAfter` (301, 600 and
  a 20-digit number all give 300 s), `TestRetryAfterHoldsTheRulesetAt300Seconds`,
  `TestRetryAfterHoldsTheStreamToo`, `TestUsageHoldIsSeparateFromTheRulesetHold`.
- [x] Counts either side of UTC midnight go out as two days: `TestUTCMidnightSplitsTheDay`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call:
  `TestEvaluationFailed`.
- [x] Hooks returning futures: n/a, a Go hook is a plain function and Go has no futures.
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor:
  `TestIntegerAccessorRange` and the harness `huge` scenario.
- [x] A never-closed client does not keep the process from exiting: `TestNeverClosedClientDoesNotBlockExit`
  (true by construction in Go, Decisions 13).
- [x] `DESIGN.md` passes the phase 1 row check, `README.md` links the mapping table: `DESIGN.md` section 1,
  README "Mapping from the contract".
- [x] Mapping names, defaults and spellings are canonical: `types.go` (`ReasonKind*`, `ErrorCode*`, `Stream*` and
  `LastError*` constants carry the contract's strings).
- [x] No regex in the evaluator: `grep -rniE 'regexp|"regex' --include=*.go internal jsonvalue *.go | grep -v
  _test` finds only the comment at `internal/evaluator/pattern.go:6` ("no regexp package").
- [x] The lint configuration names its rule set and each ignore carries a reason: `.golangci.yml` (Decisions 3).
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none (`go.mod` has no `require`; `DESIGN.md`
  section 4).
- [x] No telemetry and no identifying header: headers set in source are `Authorization` (`sync.go:176`),
  `If-None-Match` (`sync.go:192`), `Accept` (`sync.go:282`), `Content-Type` (`usage.go:157`); the table in
  Decisions 16 lists what `net/http` adds, route by route.
- [x] Every extra is one contract 13 allows: the only exports beyond the contract are the typed accessors
  (`BoolVariation` and its siblings), the `Detail[T]` type and `NewContext`.
- [x] Test seams are options, never a global or an environment variable: Decisions 5; `grep -rn 'os.Getenv\|Setenv'
  --include=*.go .` finds one hit, in a test (`concurrency_test.go`, the exit test's child process), none in the SDK.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK, and need only the pinned
  tools: `ls -l sdks/go/bin`, each starts `#!/bin/sh` and `set -eu` and `cd "$(dirname "$0")/.."`.
- [x] `test-service/README.md` maps every accessor to an SDK method: `bool`, `string`, `number`, `int`, `json`
  and `any` in its table.
- [x] The workflow follows the skeleton, filters on its paths and pins every action by commit SHA:
  `.github/workflows/sdk-go.yml` (checkout `3d3c42e5...` v7.0.1, setup-bun `0c5077e5...` v2.2.0, jdx/mise-action by SHA).
- [x] No em dash or en dash: `bun scripts/check-dashes.ts` prints nothing and exits 0.
- [x] `bun run format:check` passes: `All matched files use the correct format.`
- [x] Only `sdks/go/` and the one workflow changed: the scope check prints `scope ok`.
- [x] No package published, no name reserved, no tag: nothing was pushed to a registry; `go.mod` names a module
  path only.
- [x] Commit subject is a Conventional Commit and not `feat`, with no `Co-Authored-By` or AI attribution:
  `chore(sdk-go): the Go server SDK regenerated cold from the authoring prompt`.
- [x] The workflow job is `runs-on: ubuntu-latest` with no `if:` guard, triggers only `pull_request` and `push`, installs
  its own toolchain, and `bun scripts/check-workflows.ts .github/workflows/*.yml` printed `8 workflow(s) read, 0
  failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: every section is present and the phase 6 section check prints nothing.
