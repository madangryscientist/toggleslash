# Go SDK: run report

## Prompt version

- Prompt: 26834d89f8249d005e1fde14beaf08623d180988
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-11
- Input: language Go; runtime chosen: Go toolchain, no other runtime; minimum version chosen: 1.26 (`go.mod`);
  package manager chosen: Go modules
- Toolchain (`mise.toml`, and what the version commands print):
  - go 1.27.3 (`go version go1.27.3 linux/amd64`)
  - zig 0.17.1 (`zig version` prints `0.17.1`; used only as `zig cc` for the race detector's cgo)
  - golangci-lint 2.15.0 (`golangci-lint version`: 2.15.0 built with go1.27.0)

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2
  count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 85 passed of 85 (`go test -race -count=1 -v ./...`, top-level tests: the root package, `internal/eval`
  and `jsonvalue`; subtests are not counted)
- Repeat runs: `bin/test` passed 10 of 10 times in a row, each printing `vectors: 1371 pins: 17`, and once more with 12
  busy loops on a 12-core machine (`nproc` 12), also `vectors: 1371 pins: 17`.

## Decisions

1. Go pin is 1.27.3, not the newest release: `go.md` says `mise ls-remote go` hides the 2 newest, and 1.27.3 is the newest it lists.
2. Lint set (`.golangci.yml`): the `standard` default plus bodyclose, copyloopvar, errorlint, gocritic, misspell, nilerr, noctx, predeclared, revive (named rules), unconvert, unparam and wastedassign. gocritic is limited to its `diagnostic` tag, with the reason beside it, because the style tag only reported taste. `usestdlibvars` is not enabled.
3. No `encoding/json`: it errors on `1e999` and replaces a lone surrogate, both forbidden by evaluation.md (`go.md`). The SDK has its own `jsonvalue` package with WTF-8 strings.
4. The evaluator has its own Pike-VM pattern matcher in `internal/eval/regex.go` and imports no platform regexp (evaluation.md, "Patterns", "Cost").
5. Timing goes through an unexported `clock` interface (`clock.go`); the seams are unexported options (`withClock`, `withJitter`, `withRootCAs`, `withWatchdog`, `withEvaluator`), never a global or an environment variable (contract 13). `withHTTPClient` was written and then removed as unused.
6. Ruleset and ETag live in one `atomic.Pointer[snapshot]`, so a reader sees one ruleset or the other (contract 6.3).
7. For the 6.4 SHOULD, `Close` and `Ready` called from an SDK goroutine do not wait. The SDK knows its goroutines by id (`goroutine.go`, the id read from `runtime.Stack`), since Go has no thread-local.
8. Hook `FlagVersion` is -1 when there is no flag version, and the hook and result value type is `jsonvalue.Value`.
9. Typed accessors return the caller's typed fallback itself, so an `int64` fallback is not rounded through a float64.
10. `JSONVariation` is the same as `Variation`; the typed JSON accessor exists for the mapping table's sake.
11. The final usage flush uses a context derived from `context.Background()` with the request timeout, so closing does not cancel the flush that is part of closing (contract 11).
12. On a first-load failure the order is `onError`, then `ready` settles false (contract 5.3).
13. Fake-clock tests step the clock only when the SDK is waiting on it (they count the armed timers), never after a fixed real pause (PROMPT.md, phase 3).
14. The Go transport's `Accept-Encoding: gzip` is left on, including on the stream. The ToggleSlash server has no response compression (a search of `apps/server` found none). A compressing proxy could buffer SSE events; this is recorded, not changed.
15. The README installs through `go mod edit -require=...@v0.0.0 -replace=...` pointing at a checkout, because nothing is published and no module path may be reserved (PROMPT.md, rule 3).
16. `bin/test` names the pinned zig by path (`CC="$(mise where zig)/zig cc"`), because `~/.local/bin/zig` 0.16.0 shadows the pinned one even inside `mise exec` (see guess 1).
17. The test service answers 400 when a typed accessor is given a fallback of the wrong JSON kind (guess 2); the mapping is in `test-service/README.md`.
18. Header audit, run on a loopback listener, one request per route. The SDK sets only what the first column lists; the client added the rest.

    | Route | Set by the SDK | Added by `net/http` | Why none identifies a user or a machine |
    | --- | --- | --- | --- |
    | `GET /sdk/v1/ruleset` | `Authorization`; `If-None-Match` only when a non-empty ETag is held | `Host`, `User-Agent: Go-http-client/1.1`, `Accept-Encoding: gzip` | `Host` is the server address the caller gave; the User-Agent is the library's fixed string with no host or user; `Accept-Encoding` is a capability |
    | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `Host`, `User-Agent: Go-http-client/1.1`, `Accept-Encoding: gzip` | same as above |
    | `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `Host`, `User-Agent: Go-http-client/1.1`, `Accept-Encoding: gzip`, `Content-Length: 73` | same as above; `Content-Length` is the size of the body |

    `Connection` was not seen on any route.
19. Source searches for the privacy line: `grep -rn 'log\.\|slog\|fmt.Print' sdks/go --include=*.go` outside tests finds nothing in the SDK (the only prints are the test service's `listening` line and its fatal errors). Every error the SDK builds is `*Error{Op, Code}` with fixed text or a status code, and none is formatted with a context, key or attribute.
20. `regexp` appears in `test-service/main.go` (lines 16 and 51) only to route request paths. A search of the evaluator (`internal/eval`) and the SDK for `regexp` finds nothing.

## Guesses

1. The pinned zig is shadowed. Where: `language-notes/go.md`, toolchain. Closest sentence: "Run the toolchain under `mise exec --`, because a `zig` elsewhere on PATH (such as `~/.local/bin`) can shadow the pinned one (FS-124)". It does not settle it because on this machine `mise exec -- sh -c 'zig version'` still prints 0.16.0 (`~/.local/bin` comes first inside `mise exec` too). Assumed: `bin/test` names the pinned compiler by path with `mise where zig`, and falls back to `zig` on PATH only when mise is absent.
2. What a typed accessor does in the test service when the fallback has the wrong JSON kind. Where: `docs/sdk-test-service.md`, "Accessors". Closest sentence: "Each answers `variationDetail`'s three fields". It does not say what happens when the harness sends, say, a string fallback to `bool`. Assumed: the service answers 400 with its own error, since a Go caller cannot write such a call; the harness passed with that choice.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `language-notes/go.md`, after "can shadow the pinned one (FS-124)", add: "`mise exec` does not undo that, because the shadowing directory is earlier on PATH inside it too. Name the pinned compiler by path: `CC=\"$(mise where zig)/zig cc\"`."
2. For guess 2, in `docs/sdk-test-service.md`, "Accessors", after "Each answers `variationDetail`'s three fields", add one sentence saying what the service does with a fallback whose JSON kind the typed accessor cannot take (answer 400, or build the nearest value), so every language handles it the same way.

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json`, read from the repository at test time, and every pin; counts equal phase 2's: `bin/test` prints `vectors: 1371 pins: 17`; the phase 2 command prints `1371 17`. The reader is `internal/eval/vectors_test.go` (`TestFixtureCounts` fails on zero files or zero vectors).
- [x] `bin/test` 10 times in a row and once under load: 10 of 10 passes, then one pass with 12 busy loops (`nproc` 12); every run printed `vectors: 1371 pins: 17`.
- [x] The harness exits 0 with only the named skips: `bun run conformance -- --service sdks/go/bin/test-service` printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`. `integer-accessor` is advertised, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes: the harness (`privacy.json`, `kinds.json` vectors through the service) passed.
- [x] Result order and the mismatch rule: the harness (`evaluation.json`, `usage.json`) passed.
- [x] `integer-accessor` fraction and `2.0`: the harness (`evaluation.json`) passed; native `TestIntegerAccessorRefusesFractionsAndOutOfRange`.
- [x] No operation raises: the harness, every scenario, passed.
- [x] `NOT_READY` before the first load: the harness (`load.json`) passed.
- [x] `ready` settles, bounded, repeatable, false after `close`: the harness (`load.json`, `close.json`) passed; native `TestCloseIsSafeTwiceAndSettlesReady`.
- [x] A non-ruleset answer never replaces the held one: the harness (`status.json`) passed.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: the harness (`load.json`, `status.json`) passed.
- [x] The stream: the harness (`stream.json`, `load.json`, `status.json`) passed.
- [x] The loop and jittered backoff: the harness (`backoff.json`, `stream.json`) passed.
- [x] `429` and `Retry-After` on every route: the harness (`rate-limit.json`) passed.
- [x] Status fields, states and `lastError` codes: the harness (`status.json`, and the shape check on every status read) passed.
- [x] Usage counts: the harness (`usage.json`, `rate-limit.json`) passed.
- [x] Hook events and `onError`, both raising: the harness (`hooks.json`) passed; native `TestAPanickingHookAndAPanickingOnErrorChangeNothing`.
- [x] `close` in order, twice at once, during a hold, final flush: the harness (`close.json`, `rate-limit.json`, `usage.json`, `status.json`) passed.
- [x] No context, key or attribute in any request, `onError` message or log: the harness (`privacy.json` and the after-scenario checks) passed; source searches in Decisions 19; the header table is Decisions 18.

### Native tests

- [x] Many goroutines, `close` racing `variation`, two `close` calls racing, under the race detector: `TestEveryOperationFromManyGoroutinesWithCloseRacing` (8 goroutines, 4 racing `Close` calls) and `TestCloseIsSafeTwiceAndSettlesReady`, run by `bin/test` with `go test -race`.
- [x] SHOULD, TLS: `TestHTTPSWorksAndAnUntrustedCertificateFails` (`tls_test.go`, a local certificate through `withRootCAs`).
- [x] Evaluation returns within a bound while a ruleset request hangs and while a usage flush hangs: `TestEvaluatingWhileTheFirstLoadHangsIsNotReadyAtOnce`, `TestAHungUsageFlushNeverBlocksEvaluationAndCloseIsBounded`; also `TestACloseDuringAHungRulesetIsBounded` and `TestAHangingBodyIsCutOffByTheTimeout`.
- [x] A reader sees one ruleset across a swap: `TestHookEventETagAndVersionStayTogetherAcrossSwaps`.
- [x] Callbacks that call back into the client do not deadlock, and `ready` from a callback does not wait: `TestCloseFromOnErrorDoesNotWaitOnItself`, `TestCloseFromTheHookDoesNotDeadlock`, `TestCallbacksMayCallStatusAndVariation` (onError and the hook call `Status`, `BoolVariation` and `Ready`).
- [x] A quiet stream is dropped as `timeout` after the watchdog, and a keep-alive restarts it, on a real loopback socket: `TestAQuietStreamIsDroppedByTheWatchdog`, `TestKeepAlivesKeepTheStreamUp`, `TestAStreamThatNeverOpensTimesOut`, all through `withWatchdog`.
- [x] `Retry-After` above 300 held for 300 s on all three routes: ruleset `TestRetryAfterOnTheRulesetHoldsTheNextRequest` (the "capped" and "huge" cases), stream `TestRetryAfterOnTheStreamIsHonoured`, events `TestRetryAfterOnEventsAbove300IsHeldFor300Seconds` (nothing sent at 299 s, sent at 301 s).
- [x] Counts either side of UTC midnight go out as two days: `TestUsageIsSummedPerFlagIndexAndDay` (the fake clock crosses midnight; two `day` values in the body).
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call: `TestEvaluatorPanicGivesEvaluationFailed`.
- [x] Hook futures: n/a: the hook is a plain function.
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestIntegerAccessorRefusesFractionsAndOutOfRange`; the harness (`huge` scenario).
- [x] A client that is never closed does not hold the process: `TestNeverClosedClientDoesNotHoldTheProcess` (a Go program exits when `main` returns, whatever goroutines run).

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check (printed nothing, exit 0); `README.md` links to the mapping table in `DESIGN.md` section 1.
- [x] Names, defaults, reason kinds, error codes, stream states and `lastError` codes keep their spelling: `DESIGN.md` section 1 and the harness.
- [x] No platform regex in the evaluator: Decisions 20; `internal/eval/regex.go` is the SDK's own matcher.
- [x] The lint configuration names its rule set and gives each limit a reason: `.golangci.yml` (Decisions 2).
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none (`go.mod` has no `require`; `DESIGN.md` section 4 says "Runtime: none").
- [x] No telemetry; headers are only `Authorization`, `Accept`, `If-None-Match` and `Content-Type`: `grep -rn 'Header' sdks/go/*.go` outside tests finds only `sync.go` lines 142, 145, 219, 220 and `usage.go` lines 160, 161; the added headers are in the table in Decisions 18.
- [x] Every extra beyond the contract is one contract 13 allows: the unexported test seams (Decisions 5) and nothing else sent or changed.
- [x] Test seams are options on `New`, unexported, never a global or an environment variable: `client.go` lines 74 to 80; a search for `Getenv` in the SDK source finds nothing.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and use only the `mise.toml` toolchain: `ls -l sdks/go/bin`.
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method: the "Accessor mapping" table (bool, string, number, int, json, any).
- [x] The workflow follows the skeleton, filters on its paths and pins every action by commit SHA: `.github/workflows/sdk-go.yml`, three `uses:` lines, each a 40-character SHA with a version comment.

### Repository

- [x] No em dash or en dash: `bun scripts/check-dashes.ts` passes.
- [x] `bun run format:check` passes.
- [x] Only `sdks/go/` and the one workflow changed: the phase 6 scope check prints `scope ok`.
- [x] No package published, no name reserved, no tag: the README and `DESIGN.md` name no registry; the workflow publishes nothing.
- [x] Conventional Commit subject `chore(sdk-go): ...`, not `feat`, with no `Co-Authored-By` and no AI attribution line.
- [x] The workflow job is `runs-on: proxmox` with the same-repo `if:`, triggers only `pull_request` and `push`, installs its own toolchain with `jdx/mise-action`, and `bun scripts/check-workflows.ts .github/workflows/*.yml` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: every heading is present and this list has a line for every line of `CHECKLIST.md`.
