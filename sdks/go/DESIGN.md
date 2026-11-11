# Go SDK design

The Go server SDK, built cold from `docs/sdk-authoring/PROMPT.md`. It follows
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates to
[`docs/evaluation.md`](../../docs/evaluation.md). Module `github.com/madangryscientist/toggleslash/sdks/go`,
package `toggleslash`. Nothing is published under that path.

## The mapping table

Required values are positional arguments of `New`, so there is no "not given" for them. Every optional
value is a functional option (`Option`); an option that is absent is "not given", which is how `stream`
tells "on by default" from `WithStream(false)` and how a duration tells "default" from zero.

| Canonical | Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client`. It starts two goroutines (the sync loop and the usage flush loop), does the first ruleset request on the sync goroutine and returns at once. It never returns an error and never panics. |
| `ready` | `(*Client).Ready() bool`. Blocks until the first load attempt has settled (at most about `requestTimeout`), then answers the same value every time. After `Close` it answers `false` if it had not settled. `(*Client).ReadyContext(ctx) bool` is the same wait that also returns `false` when `ctx` ends first (an extra, contract 13). |
| `variation` | One typed method per accessor: `BoolVariation(key, ctx, fallback bool) bool`, `StringVariation(key, ctx, fallback string) string`, `IntVariation(key, ctx, fallback int64) int64`, `FloatVariation(key, ctx, fallback float64) float64` and `JSONVariation(key, ctx, fallback Value) Value`. All take a `Context`. |
| `variationDetail` | `BoolVariationDetail`, `StringVariationDetail`, `IntVariationDetail`, `FloatVariationDetail` and `JSONVariationDetail`, each returning `Detail[T]{Value T, VariationIndex *int, Reason Reason}`. `VariationIndex` is `nil` whenever the value is the fallback. |
| `status` | `(*Client).Status() Status`: `ETag string` (empty is none), `LastSyncAt time.Time` (zero is never), `Stream StreamState`, `LastError string` (empty is none). One consistent snapshot taken under one short lock. |
| `close` | `(*Client).Close()`. No return value, safe to call twice or concurrently, never panics. Every call returns when the shutdown is done (about `2 * requestTimeout` at most), except a call made on one of the SDK's own goroutines, which starts the shutdown and returns at once. |
| `url` | first argument of `New`. Trailing `/` characters are removed. An unusable URL makes every request a `network` error. |
| `sdkKey` | second argument of `New`. |
| `stream` | `WithStream(bool)`. Default on. |
| `pollInterval` | `WithPollInterval(time.Duration)`. Default 30 s; a value of zero or less is the default. |
| `flushInterval` | `WithFlushInterval(time.Duration)`. Default 30 s; zero or less is the default. |
| `requestTimeout` | `WithRequestTimeout(time.Duration)`. Default 10 s; zero or less is the default. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(EvaluationEvent))`. Default none; a nil function is ignored. |
| `onError` | `WithOnError(func(error))`. Default ignore; a nil function is ignored. |
| `accessor shape` | Typed methods for `bool`, `string`, `int` (an `int64`), `number` (a `float64`) and `json`/`any` (a `Value`, where a null `Value` fallback accepts any served kind). Go has no generic methods, so there is no one generic method. Each has a `...Detail` twin. There is no `any` typed accessor beyond `JSONVariation`. |
| `onError thread` | The goroutine that met the failure. A failed ruleset or stream attempt is reported on the sync goroutine, a failed usage request on the flush goroutine (or on the caller's goroutine for `Flush`, or on the shutdown goroutine for the final flush), and a failed hook or an evaluation that panicked on the caller's goroutine. No SDK lock is held. |

Extras (contract 13): `WithHTTPClient(*http.Client)`, `WithLogger(*slog.Logger)` (turns each `onError` value into a
`slog` warning, used when no `WithOnError` is given), `(*Client).Flush()`, the `Context` builders, and
`ReadyContext`.

**The closed sets keep their spelling.** `ReasonKind` is a string type with the constants `ReasonOff`
(`"OFF"`), `ReasonPrerequisiteFailed`, `ReasonTargetMatch`, `ReasonRuleMatch`, `ReasonFallthrough` and
`ReasonError`. `ErrorCode` is a string type with `ErrInvalidContext` (`"INVALID_CONTEXT"`) through
`ErrEvaluationFailed`, including `ErrNotReady` and `ErrTypeMismatch`. `StreamState` is a string type with `StreamOff`
(`"off"`), `StreamConnecting`, `StreamConnected`, `StreamDisconnected` and `StreamClosed`. `Status.LastError` is a plain
string holding `http_<status>`, `timeout`, `bad_ruleset` or `network`, because `http_<status>` is open.
`Reason` is `{Kind ReasonKind, Code ErrorCode, RuleIndex int, FlagKey string}`, where `Code` is set for `ERROR`,
`RuleIndex` for `RULE_MATCH` and `FlagKey` for `PREREQUISITE_FAILED`.

**Contexts.** `Context` wraps a JSON `Value`. `NewContext(key)` is a `user` context; `NewKindContext(kind, key)`;
`(Context).With(name, any)` returns a copy with an attribute; `MultiContext(...)` builds a multi context;
`ContextFromJSON([]byte)` keeps the JSON exactly (`1e999`, lone surrogates); `ContextFromValue(Value)`; and
`ContextFromMap(map[string]any)`. A JSON text that does not parse becomes an invalid context, so the caller gets
`ERROR INVALID_CONTEXT` and its fallback, never an error value.

**The hook event.** `EvaluationEvent{FlagKey, Context, Value Value, VariationIndex *int, Reason, ETag string, FlagVersion *int}`.
`Value` is the JSON value the caller is about to get. For `IntVariation` above 2^53 the event carries the nearest
double (a `Value` is a JSON value, whose numbers are binary64); the caller still gets the exact `int64`.

## Concurrency

- **The ruleset and its ETag** are one immutable `*snapshot{ruleset, etag}` behind an `atomic.Pointer`. A sync builds
  the new ruleset off to the side and stores the pointer once. A reader loads the pointer once per evaluation, so a
  hook's `etag` and `flagVersion` always come from the same ruleset. Evaluation takes no lock at all.
- **`status` fields** (the snapshot pointer, `lastSyncAt`, the stream state, `lastError`) change under one small mutex
  `stMu`, held only for assignments, never across I/O or a callback; `Status()` reads them under it, so it is one
  snapshot.
- **Usage counts** sit in a map under `usageMu`, held only for the add or the swap. A flush swaps the whole map out under
  `usageMu`, then sends with no lock but `flushMu`. `flushMu` serialises flushes and is held across I/O, but no
  evaluation ever takes it.
- **Goroutines.** Two, both started by `New`: the sync goroutine (first load, then the loop of contract 7.3 including the
  stream) and the flush goroutine (a `Sleep(flushInterval)` then a flush, repeat). `Close` runs one more, the shutdown
  goroutine, once (`sync.Once`): cancel the client's context (stops the loops and abandons their in-flight requests),
  wait for the sync and flush goroutines (which includes a flush that is running), then send the final flush
  and close `done`. Every `Close` call waits on `done`. A request abandoned by `Close` records no error and calls no
  `onError`.
- **Own-goroutine calls.** Go has no goroutine-local state, so the SDK keeps a small set of the goroutine ids of its three
  own goroutines (read from `runtime.Stack`). `Ready` and `Close` check it only: a `Ready` on an own goroutine answers
  with the state held and does not wait; a `Close` on an own goroutine starts the shutdown and does not wait for it.
  Callbacks run on those goroutines and no SDK lock is held while one runs, so a callback may call `Status`, `variation`,
  `Ready` or `Close`.
- **Never-closed client.** Goroutines do not keep a Go process alive.
- **Callbacks that panic** (`afterEvaluation`, `onError`) are recovered. A hook's panic goes to `onError`; an `onError` panic
  is dropped.

## HTTP and SSE

- **Client.** `net/http`. The default is a clone of `http.DefaultTransport` with `DisableCompression: true` and a
  `CheckRedirect` that returns `http.ErrUseLastResponse`, so a 3xx is `http_3xx` and `Authorization` is never resent to
  another place. `WithHTTPClient` uses the caller's client, copied with the same `CheckRedirect` and a zero `Timeout`.
- **`requestTimeout` covers the body.** Each ruleset and events request gets `context.WithTimeoutCause(client, requestTimeout,
  errTimeout)` that lives until the body is fully read and closed, so a server that sends headers and then stalls ends as
  `timeout`.
- **The stream.** Its request context is `WithCancelCause` of the client's. A timer (`requestTimeout`) cancels it
  with `errTimeout` unless it is stopped when the response headers arrive. After that a 60 s watchdog (`time.AfterFunc`,
  `Reset` after every read that returns bytes) cancels it with `errWatchdog`, which unblocks the body read. The cause
  decides the `lastError` code: `errTimeout` and `errWatchdog` are `timeout`, a cancelled client is silent, anything else
  is `network`. No error text is kept: a `*url.Error` carries the URL, so only fixed codes are reported.
- **SSE** is a hand-written parser (`internal/sse`, about 100 lines) to the WHATWG rules: it works on bytes, so UTF-8 split
  across reads and a CR LF split across reads are safe; LF, CRLF and a lone CR end a line; a leading BOM is skipped; `:` lines
  are comments; only `event` and `data` are read; several `data` lines join with LF; an event with no `data` line is dropped;
  the name defaults to `message`; an event incomplete at the end of the stream is never emitted. Field values are decoded with
  the WHATWG UTF-8 decoder (`internal/wtf8`).

## Dependencies

- **Runtime: none.** The standard library has HTTP (`net/http`), SHA-256 (`crypto/sha256`) and the rest. The JSON reader
  is written (below) because `encoding/json` cannot hold what the spec requires.
- **Test:** the standard `testing`, `net/http/httptest`, and `-race`.
- **Lint:** `go vet`, `gofmt` and `golangci-lint` (pinned in `mise.toml`), plus `zig` as the C compiler that `-race` needs.

## JSON

`encoding/json` errors on `1e999` and replaces a lone surrogate with U+FFFD, and the spec forbids both. `internal/wjson`
is a strict RFC 8259 reader and writer with its own `Value` (null, bool, number as `float64`, string, array, object
with insertion order). Numbers go through `strconv.ParseFloat`, so `1e999` is `+Inf`, `-0` is `-0` and `9007199254740993`
is `9007199254740992`. A string is a Go `string` holding **WTF-8**: well-formed UTF-8 plus the three-byte form of a lone
surrogate, so `"\ud800"` survives, while a surrogate pair written as two escapes joins into one code point. A duplicate
key keeps the last value (at the first key's position). Nesting is refused beyond 1024 levels (the contract needs 512).
Response bytes are first decoded with the WHATWG UTF-8 decoder (`internal/wtf8`, one U+FFFD per maximal invalid subpart)
and a BOM is skipped. The writer escapes a WTF-8 surrogate as `\udXXX` and writes a non-finite number as `null`.
The public `Value` is an alias of this type, and `ContextFromJSON` is how a raw context reaches the SDK unchanged.

## Minimum runtime and toolchain

- **Minimum Go 1.26** (`go 1.26` in `go.mod`): Go supports its two newest releases; with 1.27 current that is 1.27 and 1.26.
  The code needs only 1.24 (`t.Context`, `context.WithTimeoutCause`, `min`, `atomic.Pointer`, generics), so this is the support
  policy, not a feature need.
- **Pinned toolchain** (`mise.toml`): Go 1.27.4 (the newest `mise ls-remote go` lists, checked against go.dev), zig 0.17.1 (the C
  compiler for `-race`), golangci-lint 2.15.0.
- **Lint and format:** `go vet`, `gofmt -l` and `golangci-lint` (the Go mainstream); **tests:** the standard `testing` package;
  **build:** `go build`.
- **Integer accessor:** `IntVariation` returns `int64`.

## Package layout

```
sdks/go/
  go.mod, mise.toml, .golangci.yml, .gitignore
  toggleslash.go, options.go   the package, New, Options
  client.go                     the evaluation methods and the result order (contract 4)
  context.go, value.go          Context and the public Value alias
  sync.go, stream.go            the ruleset request, the loop, the stream (7)
  usage.go                      the usage counts and the flush (9)
  http.go, clock.go, goid.go    HTTP helpers, the clock seam, own-goroutine detection
  *_test.go                     native tests with a loopback fake server (package toggleslash)
  internal/wtf8                 WTF-8 helpers, the WHATWG UTF-8 decoder, UTF-16 ordering
  internal/wjson                the JSON reader and writer
  internal/evaluator            the evaluator of docs/evaluation.md (no network, no regexp) and its vector test
  internal/sse                  the SSE parser
  test-service/main.go          the conformance test service, README.md
  bin/lint, bin/test, bin/test-service
  DESIGN.md, README.md, RUN.md
```

The evaluator is its own package with no import beyond the standard library, `wtf8` and `wjson`. The test service is one more
`main` package of the same module, so `go build ./...` and `go vet ./...` compile it with the tests, and `bin/test` builds
it to `.build/test-service` for `bin/test-service` to run.

## Where this design reads a reference against the spec

Recorded in `RUN.md` under Decisions, with file and line, when one is found.
