# Go SDK: design

Written before any code, to `docs/sdk-authoring/PROMPT.md` phase 1. The contract is
[`docs/sdk-contract.md`](../../docs/sdk-contract.md); evaluation is
[`docs/evaluation.md`](../../docs/evaluation.md). Section numbers below are the contract's.

The module is `github.com/madangryscientist/toggleslash/sdks/go` and the package is `toggleslash` (`go` is a
keyword, so the directory name cannot be the package name). Nothing is registered under that path.

## 1. The mapping table

Go has methods with no type parameters, so the typed accessor family is the shape: five value methods and five
detail methods. `ctx` below is a `toggleslash.Context` (an alias of the SDK's own JSON `Value`).

| Name | In Go |
| --- | --- |
| `create` | `toggleslash.NewClient(url, sdkKey string, opts ...Option) *Client`. It never returns an error and never panics. It starts two goroutines (the sync loop and the flush loop) and returns without waiting for the network. `url` and `sdkKey` are positional because both are required. |
| `ready` | `(*Client).Ready() bool`. Blocks until the first ruleset load has settled, bounded by `requestTimeout`, and returns whether a ruleset is held. The answer is fixed when the first attempt settles. After `Close` it returns false if it had not settled. |
| `variation` | `BoolVariation(flagKey string, ctx Context, fallback bool) bool`, `StringVariation(... string) string`, `IntVariation(... int64) int64`, `FloatVariation(... float64) float64` and `JSONVariation(... Value) Value`. |
| `variationDetail` | `BoolVariationDetail`, `StringVariationDetail`, `IntVariationDetail`, `FloatVariationDetail` and `JSONVariationDetail`, each returning `Detail[T]{Value T; VariationIndex *int; Reason Reason}`. |
| `status` | `(*Client).Status() Status`: a struct `{ETag string; LastSyncAt time.Time; Stream StreamState; LastError SyncError}`. An empty `ETag` or `LastError` and a zero `LastSyncAt` are "none". One consistent snapshot. |
| `close` | `(*Client).Close()`. No return value (it cannot fail). Safe to call twice and from many goroutines at once; every call returns when the same shutdown is done. It returns within about twice `requestTimeout`. |
| `url` | the first argument of `NewClient`. Trailing `/` characters are removed. Required. |
| `sdkKey` | the second argument of `NewClient`. Required. |
| `stream` | `WithStream(bool)`. Default on. |
| `pollInterval` | `WithPollInterval(time.Duration)`. Default 30 s. |
| `flushInterval` | `WithFlushInterval(time.Duration)`. Default 30 s. |
| `requestTimeout` | `WithRequestTimeout(time.Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(EvaluationEvent))`. Default none. Called on the caller's goroutine, synchronously, before the accessor returns. A panic in it goes to `onError`. |
| `onError` | `WithOnError(func(error))`. Default: ignore. A panic in it is recovered and ignored. |
| `accessor shape` | Typed methods only, no generic method (Go methods cannot take type parameters): `Bool`, `String`, `Int` (int64), `Float` (float64) and `JSON` (a `Value`), each as `XVariation` and `XVariationDetail`. The integer accessor rule of contract 4 applies to `Int`; the service advertises `integer-accessor`. `JSON` takes a `Value` fallback, and a null `Value` accepts any served kind. |
| `onError thread` | The goroutine that met the failure: the sync-loop goroutine for a ruleset or stream failure, the flush goroutine for a periodic usage failure, the caller's goroutine for a hook panic or an evaluation failure, and the `Close` caller's goroutine for the final flush's failures and the "closed with N usage counts unsent" message. No SDK lock is held when it is called. |

**Closed sets.** `ReasonKind`, `ErrorCode`, `StreamState` are string types with constants whose values are the
canonical spelling: `ReasonOff = "OFF"`, `ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"`,
`ReasonTargetMatch = "TARGET_MATCH"`, `ReasonRuleMatch = "RULE_MATCH"`, `ReasonFallthrough = "FALLTHROUGH"`,
`ReasonError = "ERROR"`; the error codes `ErrInvalidContext = "INVALID_CONTEXT"` and so on, including the SDK's
`ErrNotReady`, `ErrTypeMismatch` and `ErrEvaluationFailed`; the stream states `StreamOff = "off"`,
`StreamConnecting`, `StreamConnected`, `StreamDisconnected`, `StreamClosed`; and the `lastError` codes
(`http_<status>`, `timeout`, `bad_ruleset`, `network`) as an `ErrorCode`-like string type `SyncError` whose
values are built from those spellings. `Reason` is a struct `{Kind ReasonKind; RuleIndex int; FlagKey string;
Code ErrorCode}` (only the fields of its kind are set; `RuleIndex` and `FlagKey` are meaningful for
`RULE_MATCH` and `PREREQUISITE_FAILED`, `Code` for `ERROR`).

**Not given versus zero.** Options are functional (`Option func(*config)`). `config` holds each value and a
`set` flag for the ones whose default is on or non-zero, so `WithStream(false)` is "off", and an option that was
never passed is "default". A duration that is zero or negative falls back to the default (contract 2, not
tested).

**Context.** `Context` is an alias of `Value`, the SDK's own JSON value (section 5 below). A caller builds one
with the constructors (`String`, `Number`, `Bool`, `Null`, `Array`, `Object`), with `ValueOf(any)` from the usual
Go types (`map[string]any`, `[]any`, `string`, `bool`, numbers), or with `ParseJSON([]byte)` from raw JSON, which
keeps `1e999` and lone surrogates.

## 2. Concurrency

- **The ruleset and its ETag** live in one immutable `snapshot` struct (`ruleset`, `etag`), held in an
  `atomic.Pointer[snapshot]`. A load builds the new snapshot completely, then stores the pointer: one atomic
  swap replaces both, so a reader sees the old pair or the new pair. An evaluation does one `Load` and never
  takes a lock; the `etag` a hook reports is the one in the same snapshot the evaluation used.
- **Status** is `sync.Mutex`-guarded fields (`lastSyncAt`, `stream`, `lastError`) read together with the
  snapshot pointer inside one critical section, so `Status()` is one consistent snapshot. The mutex guards
  memory only: nothing holds it across I/O, a callback or a channel operation, so no evaluation (which does not
  use it) and no `Status()` call can wait on a request. `Status()` is also bounded by that tiny critical
  section.
- **Goroutines the SDK starts:** the sync loop (first load, stream, poll, backoff) and the flush loop. Both
  derive from one `context.Context` that `Close` cancels. In-flight requests are made with that context, so
  cancelling aborts them, and every wait is a `select` on a timer and the context.
- **Locks:** `stateMu` (status fields, above), `usageMu` (the pending counts map; held for one map update or one
  swap-out, never during a request), and `flushMu` (serialises flushes, so a flush that starts while another runs
  waits for it; it is held across a usage request, and only flushes and the final flush take it, never an
  evaluation: `usageMu` is what `Record` takes, and a flush never holds `usageMu` while it sends). The Retry-After
  holds are `atomic.Int64` Unix-nanosecond deadlines.
- **Close** is a `sync.Once` that cancels the context, flips `closing`, waits for the sync loop and flush loop
  to return (a channel closed by each), runs the final flush, settles `ready` with false if it had not settled,
  and closes a `done` channel. Every `Close` call, including the losing racers, then waits on `done`. The wait
  for a loop is abandoned if that loop is, at that moment, inside an application callback (`onError` called on
  the loop's own goroutine), so a `Close` called from `onError` on the SDK's goroutine returns instead of waiting
  for itself: an atomic counter of SDK-goroutine callbacks in progress is polled every 2 ms while waiting.
- **No SDK lock is held while `afterEvaluation` or `onError` runs.** Both are called with all locks released.
- **A `Ready` called from `onError`** on the sync goroutine does not wait for the load that goroutine runs: the
  first attempt settles `ready` before it calls `onError` for that attempt's failure.
- **The SDK starts no goroutine that keeps the process alive**: Go has no daemon goroutines, and `main` exiting
  ends them; a client never closed leaks two goroutines and no more, and the README says to call `Close`.
- **`afterEvaluation` and `onError` panics** are recovered. A recovered panic is turned into an `error` and sent
  to `onError` (for a hook), or ignored (for `onError`).

## 3. HTTP and SSE

- **The HTTP client** is `net/http` with a private `http.Transport` (a clone of the default one, so `Close` can
  call `CloseIdleConnections` on it without touching the application's). `CheckRedirect` returns
  `http.ErrUseLastResponse`, so a 3xx is an `http_3xx` error and `Authorization` is never replayed to a redirect
  target. Request failures are reported as the fixed codes `network` and `timeout`, never the `*url.Error` text,
  which carries the URL.
- **`requestTimeout` bounds a whole request, body included:** every ruleset and events request is made with
  `context.WithTimeoutCause(ctx, requestTimeout, errRequestTimeout)` and the body is read inside that context
  and `Close`d before the context is cancelled. A body that never finishes fails the read with the context's
  cause, which is classified `timeout`.
- **The stream** is opened with a timer that cancels the request if the response headers have not arrived
  within `requestTimeout`; the timer is stopped when `Do` returns. After that only the watchdog bounds reads: a
  `time.Timer` of 60 s is reset after each `Read` that returns bytes (and so after a keep-alive comment), and
  its callback cancels the stream's context with the cause `errWatchdog`, which is classified `timeout`. The
  watchdog length is an unexported field so a native test can shorten it against a real quiet loopback socket.
- **SSE parser** (`sse.go`), hand-written, to the WHATWG rules the contract names: bytes are scanned for LF, CRLF
  and a lone CR (a CR at the end of a read is held until the next byte says whether an LF follows), a leading
  BOM is skipped, a line starting with `:` is a comment, the field is split at the first `:` with one leading
  space of the value removed, `data` lines join with LF, the event name defaults to `message`, an event with no
  `data` line is dropped, and an event still incomplete when the stream ends is discarded. Only `event` and
  `data` are read. UTF-8 split across reads is safe because lines are assembled as bytes and decoded only when
  an event is complete (the decode is the WHATWG UTF-8 decoder used for bodies).
- **The loop** is the pseudocode of 7.3 verbatim, with the jitter drawn from `math/rand/v2` (the source is an
  unexported field so a test can pin it). Waits are `select` on a timer and the cancel context.

## 4. Dependencies

Runtime: **none**. The standard library has HTTP (`net/http`), the clock, `crypto/sha256` and `sync/atomic`;
JSON is the SDK's own reader (section 5) because `encoding/json` fails on `1e999` and rewrites a lone surrogate.

Test: the standard `testing` and `net/http/httptest` packages, no module. Lint: `go vet`, `gofmt`, and
`golangci-lint` v2 (a dev tool pinned in `mise.toml`, not a module dependency). `-race` needs cgo, so a C
compiler is a test dependency: `zig` (pinned in `mise.toml`, used as `zig cc`) provides it on any machine, and
CI uses the same pin.

## 5. JSON

`internal/jsonv` holds the one JSON value type and its reader and writer, and the SDK's `Value` is an alias of
it.

- **Value:** a small struct with a kind (null, bool, number, string, array, object), a `bool`, a `float64`, a
  `string`, a `[]Value` and an ordered object (insertion order kept, a map for lookup, duplicate keys keep the
  last value in the first key's position, as `JSON.parse` does). Object reads are own-property reads on that
  map only.
- **Numbers** are `float64` (IEEE-754 binary64). The reader scans a JSON number token by the grammar and hands
  the lexeme to `strconv.ParseFloat`, which rounds to nearest-even (`9007199254740993` becomes
  `9007199254740992`), returns `+Inf` with `ErrRange` for `1e999` (accepted: the value is Infinity, not an error)
  and keeps `-0` as negative zero. The writer prints a non-finite number as `null`, as `JSON.stringify` does,
  and negative zero as `-0`.
- **Strings** are Go strings holding **WTF-8**: UTF-8 in which a lone surrogate is written as its three-byte
  form (`ED A0..BF 80..BF`), which valid UTF-8 never contains. `"\ud800"` therefore survives; a pair
  `"😀"` is combined into one four-byte scalar by the reader. Operations whose meaning is UTF-16 code
  units (the ordinal comparisons, `contains`, `startsWith`, `endsWith` and the length arithmetic in the pattern
  engine) convert to `[]uint16` first, so an ordering of U+FFFF against U+10000 is the spec's.
- **Bodies** are decoded with a hand-written WHATWG UTF-8 decoder (one U+FFFD per maximal invalid subpart; a
  WTF-8 surrogate sequence in the *bytes* is invalid UTF-8 and is replaced, since only a JSON *escape* can
  produce a lone surrogate), after skipping a leading BOM. Depth is limited to 1024 levels (above the contract's
  512 minimum); deeper input is `bad_ruleset`.
- **The context** reaches the evaluator as a `Value`. A caller who has a `map[string]any` converts it with
  `ValueOf`; a caller who has JSON text uses `ParseJSON`, which keeps `1e999` and a lone surrogate. The test
  service uses `ParseJSON` on the harness's request body (not `encoding/json`).
- **Output** (the test service answers, `Value.MarshalJSON`) is written by the same package: strings are
  written as UTF-8 with a lone surrogate written as a `\uXXXX` escape, so the harness reads it back.

## 6. The minimum runtime and the toolchain

- **Minimum Go: 1.26** (`go 1.26` in `go.mod`). It is the oldest release in the vendor's support window (1.27
  and 1.26 are supported); the SDK uses `atomic.Pointer`, generics, `context.WithTimeoutCause`,
  `math/rand/v2` and `min`/`max`, all older than that. The toolchain pinned for development is newer (Go 1.27.3,
  `mise.toml`).
- **Lint and format:** `gofmt` (the language's formatter) and `go vet`, plus `golangci-lint` v2 with a short
  `.golangci.yml`, because it is the mainstream aggregator; `usestdlibvars` is not enabled.
- **Test framework:** the standard `testing` package with `httptest`, run under `-race`; table tests for the
  vectors. **Build tool:** `go build`. **Task entry points:** three `sh` scripts in `bin/`, which use `mise exec
  --` when `mise` is on PATH.
- **The integer accessor** returns `int64`. A served number is accepted when it is finite, has no fractional
  part (`2.0` is `2`), and lies in `[-2^63, 2^63)`; otherwise the caller gets its fallback and `TYPE_MISMATCH`.

## 7. Layout

```
sdks/go/
  go.mod  mise.toml  .golangci.yml  .gitignore
  DESIGN.md  README.md  RUN.md
  toggleslash.go    package doc, NewClient, options, the constants
  value.go          the public Value/Context aliases, constructors, ValueOf, ParseJSON
  types.go          Reason, Detail, Status, EvaluationEvent, closed sets
  client.go         the client: accessors, snapshot, hooks, Close
  sync.go           the loop: first load, stream, poll, backoff, Retry-After holds
  sse.go            the SSE parser
  usage.go          the usage counts and flushes
  http.go           the HTTP helpers: the transport, bounded requests, error classification
  *_test.go         the native tests (fake loopback servers)
  internal/jsonv/   JSON value, reader, writer, WTF-8 and UTF-16 helpers
  internal/eval/    the evaluator: ruleset loading and validation, contexts, references, clauses,
                    bucketing (SHA-256), the pattern parser and Pike VM, semver, ISO instants, evaluate
                    (vectors_test.go runs every vector and pin from fixtures/)
  test-service/     main.go (the conformance test service) and README.md (the accessor mapping)
  bin/lint  bin/test  bin/test-service
```

The evaluator is its own package with no network code and no import of the root package. The client imports
`internal/eval` and `internal/jsonv`. The test service is a `main` package in the same module.
