# Go server SDK: design

Written before any code (phase 1 of `docs/sdk-authoring/PROMPT.md`). The module is
`github.com/madangryscientist/toggleslash/sdks/go`, and the package is named `toggleslash` (the
directory is called `go`, which is a keyword). A caller writes
`import toggleslash "github.com/madangryscientist/toggleslash/sdks/go"`.

## 1. The mapping table (contract section 2)

Every function and option below is in package `toggleslash`. `New` returns a `*Client`.

| Name | In Go |
| --- | --- |
| `create` | `New(url, sdkKey string, opts ...Option) *Client`. It starts two goroutines and returns at once; it never waits for the network and never fails. `url` and `sdkKey` are the first two arguments, so a caller cannot forget them. |
| `ready` | `(*Client).Ready(ctx context.Context) bool`. It blocks until the first load has finished (or `ctx` is done, which answers false without fixing the answer) and answers whether a ruleset is held. `Ready()` takes no timeout of its own: the first attempt is bounded by `requestTimeout`. |
| `variation` | Typed methods, no generic method: `BoolVariation`, `StringVariation`, `IntVariation` (`int64`), `FloatVariation` (`float64`) and `JSONVariation` (a `Value`), each `(flagKey string, ctx Context, fallback T) T`. |
| `variationDetail` | The same five with a `Detail` suffix: `BoolVariationDetail` and so on, each returning `Detail[T]` (`Value T`, `VariationIndex *int`, `Reason Reason`). `VariationIndex` is nil whenever `Value` is the fallback. |
| `status` | `(*Client).Status() Status`: one struct copied under one lock (`ETag string`, `HasETag bool`, `LastSyncAt time.Time`, `Stream StreamState`, `LastError LastError`), so the four fields are one snapshot. `LastSyncAt.IsZero()` means never. `LastError` is a string type whose empty value means none. |
| `close` | `(*Client).Close()`. No error to return: it never fails. Safe to call twice and from many goroutines. |
| `url` | First argument of `New`. Required. Trailing `/` characters are removed. |
| `sdkKey` | Second argument of `New`. Required. |
| `stream` | `WithStream(bool)`. Default on. A functional option, so "not given" is the default (on) and `WithStream(false)` is off. |
| `pollInterval` | `WithPollInterval(time.Duration)`. Default 30 s. A value of zero or less is ignored (the default stays). |
| `flushInterval` | `WithFlushInterval(time.Duration)`. Default 30 s. Zero or less is ignored. |
| `requestTimeout` | `WithRequestTimeout(time.Duration)`. Default 10 s. Zero or less is ignored. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(EvaluationEvent))`. Default none. A nil function is ignored. |
| `onError` | `WithOnError(func(error))`. Default: ignore. A nil function is ignored. |
| `accessor shape` | Typed methods only, no generic method (a Go method cannot take type parameters, and a free generic function over the client would hide the integer rule). Five accessors and five detail forms: `Bool`, `String`, `Int` (`int64`), `Float` (`float64`), `JSON` (`Value`). |
| `onError thread` | The goroutine where the failure happened: the sync goroutine for a ruleset or stream failure, the flush goroutine for a usage failure, the goroutine that called `Close` (or a shutdown goroutine, if `Close` was called from an SDK goroutine) for a failure at close, and the caller's own goroutine for an error a hook raised or an evaluation raised. No SDK lock is held when it runs. |

Extras the contract allows (section 13), none of which changes a guarantee: `WithHTTPClient` is **not**
offered (the SDK owns its transport so `Close` can drop its connections), `Flush(ctx)` sends the
pending counts now, and the unexported seams for tests are `withJitter`, `withNow` and `withWatchdog`,
set only by the tests in this package.

**The closed sets** keep their spelling. `Reason` is a struct with the fields `evaluation.md` gives a reason (`Kind ReasonKind`,
`Code ErrorCode` for an `ERROR`, `RuleIndex int` for `RULE_MATCH`, `FlagKey string` for `PREREQUISITE_FAILED`; the others are the zero value), and
`ReasonKind`, `ErrorCode`, `StreamState` and `LastError` are string types with constants whose
**value** is the canonical spelling: `ReasonOff ReasonKind = "OFF"`, `ErrNotReady ErrorCode = "NOT_READY"`,
`StreamConnected StreamState = "connected"`, `LastErrorTimeout LastError = "timeout"`. The
`http_<status>` error is built as `LastError("http_" + status)`, with `LastErrorBadRuleset` and
`LastErrorNetwork` for the others. A Go string type is a closed set only by convention, so the tests
check every constant against the spec's spelling.

**Telling "not given" from false or zero.** Options are functional (`type Option func(*options)`).
`New` starts from a struct holding the defaults (`stream: true`, the three durations) and each option
overwrites one field. An option that is not passed leaves the default; `WithStream(false)` writes
false. A non-positive duration is ignored (contract section 2 allows it and does not test it).

**The event** handed to `WithAfterEvaluation` is `EvaluationEvent{FlagKey string, Context Context,
Value Value, VariationIndex *int, Reason Reason, ETag string, HasETag bool, FlagVersion *int64}`.
`Value` is a `Value` (the SDK's JSON value), whose number is a `float64`. For an `IntVariation` that
serves an integer above 2^53 the event therefore carries the nearest double while the caller gets the
exact `int64`, which contract section 10 allows. (A served number always arrives as binary64, so the
exact integer is the double's own value; the event and the return only differ in type.)

## 2. Concurrency (contract section 6)

- **State.** The ruleset and its ETag are one immutable struct, `snapshot{rs *evaluator.Ruleset;
  etag string; hasETag bool}`, held in an `atomic.Pointer[snapshot]`. A sync builds the new
  snapshot and stores the pointer once: a reader sees the old pair or the new pair, never a ruleset
  with the other's tag. `variation` loads the pointer once and uses that snapshot for the evaluation,
  the event's `etag` and `flagVersion`.
- **Goroutines.** Two, both started by `New`: the **sync goroutine** (the first load, then the loop of contract 7.3) and the
  **flush goroutine** (a timer loop). Nothing else; a request is made on the goroutine that needs it.
  `Close` cancels a context both loops watch (so a request in flight is abandoned at once, which is
  what gives the "no error after close" rule), waits for both through a `sync.WaitGroup`, then runs
  the final flush on the closing goroutine with a fresh context bounded by `requestTimeout` per request.
  Shutdown runs under a `sync.Once`, so a second or concurrent `Close` blocks in the `Once` until the
  first has finished, and every caller returns when the shutdown is done.
- **Locks.** `statusMu` guards the status fields and the Retry-After holds (copy in, copy out, a few
  field writes). `usageMu` guards the count map. `readyOnce`/a channel settles `ready`. None is held
  across a request, a callback or a sleep, and `variation` takes at most `usageMu` for one map add after
  the evaluation is done, which the flush holds only to swap the map out. So a hung server cannot
  slow an evaluation: the request code never takes a lock it keeps while waiting on the network.
- **Callbacks.** `afterEvaluation` and `onError` run with no SDK lock held, and `afterEvaluation` runs on the
  caller's goroutine before the call returns. Both are called inside a `recover`, so a panic goes to
  `onError` (a hook) or is dropped (an `onError` that panics).
- **The SDK's own goroutine.** Go has no thread identity, so the SDK keeps a set of the goroutine ids it
  owns (the sync goroutine, the flush goroutine, and a goroutine while it runs the final flush),
  read from the `goroutine N [` header of `runtime.Stack` when each starts. `Ready` and `Close` read
  the caller's id, which costs one `runtime.Stack` call and only on those two methods, never on
  `variation`. A `Ready` on an SDK goroutine returns the state held without waiting; a `Close` on one
  starts the shutdown on a new goroutine and returns, so a callback that calls back into the client
  cannot deadlock it. This uses text the runtime documents only as a stack trace, so a test holds the
  parse and the code falls back to "not an SDK goroutine" if it fails.
- **`close` bound.** At most `requestTimeout` for the abandoned loop requests to return (they are
  cancelled, so in practice immediately) plus `requestTimeout` per final usage request. A flush of
  several requests can exceed twice `requestTimeout`; the final flush stops at the first request that
  fails, so one hung request is the bound: under `2 * requestTimeout` plus scheduling.
- **A client never closed** keeps no process alive: goroutines do not stop `main` from returning.

## 3. HTTP and SSE

- **Client.** `net/http` with one `*http.Transport` per `Client` (cloned from the defaults, no
  proxy from the environment is a Go default we keep) and `CheckRedirect` returning
  `http.ErrUseLastResponse`, so a `3xx` is an `http_<status>` error as the contract allows. No
  `Accept-Encoding` is set by the SDK; Go's transport adds `gzip` and decodes it, which is the default
  of the standard library and is listed in `RUN.md` with `User-Agent`, `Host` and `Content-Length`.
- **`requestTimeout` covers the body.** Each ruleset and events request gets
  `ctx, cancel := context.WithTimeoutCause(parent, requestTimeout, errRequestTimeout)` and the body is
  read to its end before `cancel` runs, so the deadline covers connect, headers and the whole body. A
  deadline error is `timeout`; any other transport error is `network`; a cancel by `Close` is dropped.
  Errors are turned into fixed text, never the `*url.Error` string (it holds the URL).
- **The stream.** The request context is cancelled by an open timer set to `requestTimeout` until the
  response headers arrive (the timer is stopped then); after that a second timer, the watchdog (60 s),
  is reset after every `Read` that returns bytes, and when it fires it cancels the request with cause
  `errWatchdog`, which is `timeout`. The body is read with `Read` into a 4 KiB buffer.
- **SSE parser** (`sse.go`). Feeds on byte chunks and holds a partial line across reads: a decoder
  state for UTF-8 (bytes are assembled into lines first, and a line is decoded as a whole with
  the WHATWG replacement rule, so a code point split across reads is whole when decoded), a pending
  `CR` flag (a `LF` right after a `CR` is the same line ending), a leading byte order mark skipped once at
  the start of the stream, a comment line ignored, `field: value` and `field:value` (one leading space
  removed from the value), `data` lines joined with LF, `event` set, `id` and `retry` ignored. A blank
  line dispatches: an event with no `data` line is dropped, the name is `message` when unset, and an event
  incomplete when the stream ends is discarded.

## 4. Dependencies

- **Runtime: none.** The standard library has HTTP (`net/http`), SHA-256 (`crypto/sha256`), time and
  atomics. It does **not** have a JSON reader that keeps what the spec needs (`encoding/json`
  fails on `1e999`, and replaces a lone surrogate with U+FFFD), so the SDK has its own, below. It does not
  have a regular expression engine the spec accepts (`regexp` is RE2 and differs on `\d`, `\w`, `.`),
  so the evaluator has its own Pike VM.
- **Test and lint:** `go test` with the standard `testing` package and `net/http/httptest`; `go vet`;
  `golangci-lint` (v2) for static checks; `gofmt -l` for format; `zig cc` as the C compiler for
  `go test -race` (the race detector needs cgo, and the machine has no system C compiler we
  rely on). The conformance harness is `bun run conformance` from the repository.

## 5. JSON

- **Own reader and writer** in `internal/jsonv`. The value type is `jsonv.Value`, a small struct
  (`kind`, a `bool`, a `float64`, a `string`, a `[]Value` and a `map[string]Value`). The package
  re-exports it as `toggleslash.Value`, so a caller can build and read one.
- **Numbers** are `float64` from `strconv.ParseFloat` on the token the grammar allowed. Its range error
  is ignored, so `1e999` is `+Inf` and a number too small is zero. `9007199254740993` rounds to even and
  reads as `9007199254740992`; `-0` stays negative zero.
- **Strings** are held as **WTF-8** in a Go `string`: UTF-8, plus a lone surrogate kept as its own three
  bytes (`ED A0 80` for `\ud800`). A valid surrogate pair in an escape is joined into one four byte
  code point. So two equal UTF-16 strings are two equal Go strings, and `string` equality is the spec's
  equality. Ordering (`lt`, `gt`) goes through UTF-16 code unit order in `jsonv.Compare`, and hashing replaces a
  lone surrogate with U+FFFD, as the spec says. `DecodeRune` reads a code point from WTF-8.
- **Input bytes** go through the WHATWG UTF-8 decode (one U+FFFD per maximal invalid subpart) before
  parsing; a BOM is skipped. A duplicate key keeps the last value. Nesting deeper than 1000 levels is
  refused (`bad_ruleset`); the contract demands at least 512.
- **Writing** escapes a lone surrogate as `\udXXX`, so the test service's answers are valid JSON; an
  infinity or NaN has no JSON form and is written as `null`, as `JSON.stringify` does.
- **The context** is a `toggleslash.Context`, which is `Value` (an alias): a caller builds one with
  `toggleslash.ParseContext(rawJSON)`, which keeps `1e999` and lone surrogates, or with
  `toggleslash.ContextFrom(map[string]any)` (and `NewUserContext(key)`) for ordinary Go values.

## 6. Minimum runtime and toolchain

- **Go 1.26**, which is `go 1.26` in `go.mod`. It is the oldest release that is still supported when
  this was written (the supported releases are 1.27.4 and 1.26.11), and the code uses only
  `atomic.Pointer` (1.19), `context.WithTimeoutCause` (1.21), the `min` and `max` builtins (1.21) and
  `for range int` (1.22). The pinned toolchain in `mise.toml` is **go 1.27.4**, the newest the mise
  registry lists.
- **Test framework:** the standard `testing` package, run with `go test -race`. `zig 0.17.1` (pinned in
  `mise.toml`) is the C compiler for `-race`, called by path from `bin/test`.
- **Lint and format:** `go vet`, `golangci-lint 2.15.0` (the `standard` linters plus a named extra set in
  `.golangci.yml`) and `gofmt -l`. Build tool: `go build`, through `go test`.
- **Integer accessor:** `IntVariation` uses `int64`. A served number with a fractional part, or outside
  `[-2^63, 2^63)`, gives the fallback with `TYPE_MISMATCH`; `2.0` is the integer 2. The check is on the `float64`:
  `math.Trunc(f) == f` and `-9223372036854775808 <= f < 9223372036854775808`.

## 7. Package layout

```
sdks/go/
  go.mod                  module github.com/madangryscientist/toggleslash/sdks/go
  mise.toml               go, golangci-lint, zig
  toggleslash.go          package doc, New, Close, Ready, Status
  options.go              Option, defaults
  types.go                Value alias, Context, Reason, Detail, Status, closed sets
  context.go              ParseContext, ContextFrom
  client.go               accessors and the evaluation path (section 4 of the contract)
  sync.go                 the sync loop, the ruleset request, backoff, Retry-After
  sse.go                  the SSE parser and the stream reader
  usage.go                counts, the flush, the Retry-After hold for events
  internal/jsonv/         the JSON value, reader, writer and WTF-8 helpers
  internal/evaluator/     evaluation.md: ruleset loading, evaluation, the Pike VM, semver, dates, bucketing
  test-service/main.go    the conformance test service (package main, same module)
  test-service/README.md  accessor mapping and the protocol notes
  bin/test bin/lint bin/test-service
  .golangci.yml .gitignore README.md RUN.md DESIGN.md
```

The evaluator imports only `internal/jsonv` and the standard library (no network code). The tests live next to
the code in `_test.go` files; the vector test is `internal/evaluator/vectors_test.go` and reads
`../../../../fixtures/evaluation` relative to its package directory.
