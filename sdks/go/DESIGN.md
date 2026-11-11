# Go server SDK: design

This is the design of the Go server SDK, written before any code. The contract is
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), the evaluator is
[`docs/evaluation.md`](../../docs/evaluation.md). The module is
`github.com/madangryscientist/toggleslash/sdks/go` and the package is named `toggleslash` (`go` is a keyword).
Nothing is registered under that path.

## 1. The mapping table

| Name | In Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client`. It starts two goroutines (the sync loop and the usage flush loop) and returns at once without waiting for the network. It never returns nil and never panics. |
| `ready` | `(*Client).Ready() bool`. Blocks until the first ruleset request has ended (bounded by `requestTimeout`), then returns the same answer on every call. After `Close` it returns false if it had not settled. |
| `variation` | Typed methods: `BoolVariation`, `StringVariation`, `IntVariation` (int64), `FloatVariation` (float64) and `JSONVariation` (a `toggleslash.Value`), each `(flagKey string, ctx any, fallback T) T`. |
| `variationDetail` | `BoolVariationDetail`, `StringVariationDetail`, `IntVariationDetail`, `FloatVariationDetail`, `JSONVariationDetail`, each returning `Detail[T]{Value T, VariationIndex *int, Reason Reason}`. `VariationIndex` is nil for a fallback. |
| `status` | `(*Client).Status() Status{ETag string, LastSyncAt time.Time, Stream StreamState, LastError string}`. `ETag` is `""` and `LastSyncAt` is the zero `time.Time` when there is none; `LastError` is `""` when there is none. |
| `close` | `(*Client).Close()`. No error is returned: it never fails. Safe to call any number of times, from any goroutine. |
| `url` | the first argument of `New`. Required. Trailing `/` characters are removed. |
| `sdkKey` | the second argument of `New`. Required. |
| `stream` | `WithStream(bool)`. Default on. |
| `pollInterval` | `WithPollInterval(time.Duration)`. Default 30 s. |
| `flushInterval` | `WithFlushInterval(time.Duration)`. Default 30 s. |
| `requestTimeout` | `WithRequestTimeout(time.Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(Event))`. Default none. |
| `onError` | `WithOnError(func(error))`. Default: ignore. |
| `accessor shape` | Typed methods, five of them, each with a `Detail` twin (see `variation` and `variationDetail`): `Bool`, `String`, `Int` (int64, the integer accessor of contract section 4), `Float` (float64) and `JSON` (any JSON kind; the fallback's kind is the one compared, a null fallback accepts any). Go methods cannot be generic, so there is no single generic method. |
| `onError thread` | The goroutine that hit the failure. A sync or stream failure is reported on the SDK's sync goroutine, a usage failure on the SDK's flush goroutine (or on the goroutine that called `Close`, for the final flush), and a hook or evaluator panic on the caller's goroutine. No SDK lock is held while it runs. |

Closed sets keep their spelling as typed string constants: `ReasonKind` (`ReasonOff` is `"OFF"`, `ReasonPrerequisiteFailed`,
`ReasonTargetMatch`, `ReasonRuleMatch`, `ReasonFallthrough`, `ReasonError`), `ErrorCode` (`ErrInvalidContext`
is `"INVALID_CONTEXT"` and the rest of the evaluator's codes, then `ErrNotReady`, `ErrTypeMismatch` and
`ErrEvaluationFailed`) and `StreamState` (`StreamOff` is `"off"`, `StreamConnecting`, `StreamConnected`,
`StreamDisconnected`, `StreamClosed`). A `lastError` code is a plain string, `"timeout"`, `"bad_ruleset"`,
`"network"` or `"http_<status>"` (constants `LastErrorTimeout`, `LastErrorBadRuleset`, `LastErrorNetwork`).

"Not given" against zero: the options are functional options (`WithStream(false)` is distinct from not passing
`WithStream`), and the client holds defaults that a given option overwrites. A duration that is zero or negative
is treated as not given (contract section 2 says an SDK SHOULD fall back to the default).

A **context** is an `any`. The SDK accepts the SDK's own `toggleslash.Value` (a JSON value that keeps `1e999`
and lone surrogates, see section 5), a `map[string]any`, a `json.RawMessage` or `[]byte` holding JSON text, and
any other value that `encoding/json` can marshal (a struct with tags). An unusable context is not a panic: it
is `ERROR INVALID_CONTEXT`. The `Event` the hook receives has the typed fields `FlagKey string`, `Context any`
(the value the caller passed), `Value any` (the Go value the caller is about to get: `bool`, `string`, `int64`,
`float64` or `Value`), `VariationIndex *int`, `Reason Reason`, `ETag string` and `FlagVersion *int64`. The hook's
event carries the exact int64 for the integer accessor; the test service writes it back as the nearest double.

## 2. Concurrency

- **The ruleset and its ETag** live in one immutable struct `snapshot{rules *evaluator.Ruleset; etag string}`
  held in an `atomic.Pointer[snapshot]`. A load builds the new snapshot completely and stores the pointer in
  one step, so a reader sees the old ruleset with the old ETag or the new one with the new ETag, never a mix.
  `variation` loads the pointer once and uses that snapshot for the evaluation, the hook's `ETag` and the hook's
  `FlagVersion`.
- **Goroutines.** `New` starts the sync goroutine (first load, then the loop of contract 7.3) and the flush
  goroutine (one sleep per flush interval, then a flush). Both select on the client's lifetime context, which
  `Close` cancels. Neither is a "daemon" and neither can keep a Go process alive: a Go program exits when `main`
  returns whatever goroutines run. A test holds that with a helper process.
- **Locks.** `mu` guards the status fields (`lastSyncAt`, `stream`, `lastError`) and the snapshot swap, so
  `Status` is one consistent snapshot. `usageMu` guards the pending-count map and the usage hold. `flushMu`
  serialises flushes. Each is held for a few map or field operations only: no lock is held across an HTTP call,
  an evaluation, a hook or `onError`. An evaluation takes none of them (an atomic load, then the evaluator,
  which is a pure function over immutable data), and recording a count takes `usageMu` for one map add, which no
  network call ever holds.
- **Callbacks.** Errors are collected while a lock is held and reported after it is released. A goroutine that
  runs an application callback raises an atomic counter around the call.
- **`close`.** The first caller cancels the lifetime context (which abandons in-flight ruleset and stream
  requests without reporting them), settles `ready` false if it had not settled, waits for the sync goroutine
  and the flush goroutine, sends the final usage flush, then closes the `done` channel. A concurrent or later
  `Close` waits on `done`. Go has no goroutine identity, so a `Close` made from inside an application callback
  on an SDK goroutine cannot be told from one made elsewhere; the counter above stands in: an SDK goroutine that
  is running a callback is not waited for (it is doing nothing else until the callback returns), and a `Close`
  that finds another close in progress while any callback is running returns at once instead of waiting.
- **`ready`** waits on a channel closed once by the first load. The first load settles `ready` before it reports
  its failure to `onError`, so a `Ready` from `onError` returns the fixed answer at once.
- **Bounds.** `Close` is bounded by the in-flight usage request (`requestTimeout`) plus the final flush
  (`requestTimeout`), about twice `requestTimeout`. Ruleset and stream requests are abandoned at once.

## 3. HTTP and SSE

- **Client.** `net/http` with one `http.Transport` per client (cloned from the default, so `Close` can call
  `CloseIdleConnections`) and `CheckRedirect` set to return `http.ErrUseLastResponse`: a 3xx is an `http_3xx`
  error and the key is never sent to a second URL. `Client.Timeout` is not used.
- **`requestTimeout` bounds the whole request, body included.** Each ruleset or events request gets a context with
  a cancel cause, and a timer from the SDK's clock cancels it with the cause `errTimeout` after `requestTimeout`.
  The timer is stopped only after the body has been read and closed, so a server that sends the headers and then
  stalls the body ends in `timeout`.
- **The stream.** One request, `Accept: text/event-stream`. The open (until the response headers arrive) is bounded
  by `requestTimeout` with the same kind of timer; once the headers are in, that timer is replaced by the 60 s
  watchdog, re-armed before every `Read` and stopped while an event is handled (a ruleset fetch the event asks
  for has its own `requestTimeout`). The watchdog fires by cancelling the request's context with `errTimeout`.
  The idle length is a seam, so a test can use 300 ms on a real socket.
- **SSE** is parsed by a short hand-written state machine (`sse.go`) over bytes: LF, CR and CRLF end a line (a
  CR then LF split over two reads is one end), a line starting with `:` is a comment, a leading BOM is skipped,
  `data` lines are joined with LF, an event with no `data` line is dropped, the name defaults to `message`,
  and an event still incomplete when the stream ends is never emitted. Each line is decoded as UTF-8 by the
  WHATWG decoder (`jsonv.DecodeUTF8`) after it is complete, so a character split across reads is whole by then.
  No line or event size cap.
- **Retry-After** is read on a 429 only, as whole decimal digits with surrounding spaces ignored, capped at
  300 s. The hold is a point in time compared with the clock, not a sleep.
- **Time** comes from a small clock interface (`Now`, `AfterFunc`) with a real implementation; tests inject a
  fake one for the UTC day, the 300 s cap and the backoff. Randomness for the jitter is an injected function.

## 4. Dependencies

Runtime: none. The standard library has HTTP (`net/http`), SHA-256 (`crypto/sha256`) and atomics. `encoding/json`
is not used to read the ruleset, a context from the test service or a vector (section 5); the SDK uses it only
to marshal an arbitrary Go struct passed as a context. Test and lint: `go test` (the standard test framework, with
`-race`), `go vet`, `gofmt` and `golangci-lint` (the standard Go lint runner, one tool for the usual set of
analyzers). The race detector needs cgo, so `mise.toml` pins `zig` and `bin/test` uses it as the C compiler.

## 5. JSON

`encoding/json` cannot read what `evaluation.md` requires: it fails on `1e999` and replaces a lone surrogate with
U+FFFD. So `internal/jsonv` is a hand-written reader and writer.

- **Value.** A struct with a kind (null, bool, number, string, array, object), a `float64`, a Go `string`, a slice
  and an ordered map. The zero value is null, and null and absent are the same everywhere, so an object `Get` of a
  missing name returns null.
- **Numbers** are read by a strict JSON number grammar scan and then `strconv.ParseFloat`, so they are binary64:
  `1e999` is `+Inf` (the range error is ignored), `9007199254740993` reads as `9007199254740992`, and `-0.0`
  stays `-0`.
- **Strings are WTF-8.** A Go string holds UTF-8 bytes, and a lone surrogate (JSON `"\ud800"`) is stored as its
  generalised 3-byte form (`ED A0 80`), which is not valid UTF-8 but is a valid Go string. A high escape directly
  followed by a low escape is joined to one supplementary code point. Everything that needs a different view
  converts from that one: the regex engine reads code points (3-byte surrogates stay one code point each),
  the string order and substring ops read UTF-16 code units (so a lone `\ud83d` is found inside `"😀"` and
  `"\udc00"` sorts above it, as the spec says) and the hash input replaces each such form with U+FFFD.
  Equality is byte equality, which equals code unit equality because the reader always joins a pair.
- **Input bytes** from a server go through the WHATWG UTF-8 decoder first (`DecodeUTF8`: one U+FFFD per maximal
  invalid subpart, a leading BOM skipped), then the parser. A duplicate object key keeps the last value. Nesting
  deeper than 10000 is `bad_ruleset`, which is above the 512 minimum; there is no size cap.
- **Public type.** `toggleslash.Value` is a type alias of `jsonv.Value`, with `ParseJSON([]byte) (Value, error)`,
  constructors and accessors, so a caller can build a context that keeps `1e999` and lone surrogates.

## 6. Minimum runtime, toolchain

- **Pinned toolchain** (`mise.toml`): Go 1.27.4 (the newest stable release, from `mise ls-remote go` and
  go.dev), `golangci-lint` 2.15.0 and `zig` 0.17.1.
- **Minimum Go: 1.26** (`go 1.26` in `go.mod`), the oldest release still in upstream security support: Go
  supports the two newest majors, which on the run date are 1.27 and 1.26. The code uses generics, `atomic.Pointer`,
  `context.WithTimeoutCause`, the `min` and `max` builtins and range over integers, all of which 1.26 has.
- **Lint and format:** `go vet`, `gofmt -l` and `golangci-lint` with a named rule set and a comment for every
  ignore (`.golangci.yml`). **Tests:** the standard `testing` package, run with `go test -race`. **Build tool:**
  the `go` command; there is no Makefile.
- **Integer accessor:** `IntVariation` returns `int64`. A served number with a fractional part or outside
  the signed 64-bit range is `TYPE_MISMATCH`; `2.0` is accepted.
- **Package manager:** Go modules, with no external requirement.

## 7. Package layout

```
sdks/go/
  go.mod  mise.toml  .gitignore  .golangci.yml
  README.md  DESIGN.md  RUN.md
  toggleslash.go  options.go  types.go  context.go     the public API (package toggleslash)
  client.go  sync.go  sse.go  usage.go  http.go         the client, sync loop, SSE parser, usage reporter
  *_test.go                                             native tests (race, close, hang bounds, headers, TLS, ...)
  internal/jsonv/                                       the JSON reader and writer
  internal/evaluator/                                   the evaluator: validate, context, clauses, semver, dates,
                                                         patterns (parser and Pike VM), bucketing, segments;
                                                         vectors_test.go (TestVectors, TestBucketPins)
  test-service/main.go  test-service/README.md          the conformance test service (package main, built with ./...)
  bin/lint  bin/test  bin/test-service
```

The evaluator package has no network code. The test service is one more package in the module, so `go build ./...`,
`go vet ./...` and `go test ./...` compile it with everything else; `bin/test` also builds its binary into
`.build/`, which `.gitignore` ignores.

## 8. Where this SDK reads the spec and the TypeScript reference differently

- **`flagVersion` on a flag whose only fault is a segment reference.** `docs/sdk-contract.md` (line 410) says
  the version is the flag's `version` in that ruleset, "or none when the ruleset has no such flag or the flag is
  malformed". The TypeScript SDK (`packages/sdk/src/client.ts`, `versionOf`, near line 123) reports it only for an
  entry whose status is `ready`, so a flag with a missing or malformed segment reference reports none. This SDK
  follows the contract: a flag is malformed when its header or its body fails validation, and a flag whose body
  is valid but names a missing segment still reports its version. A version above 2^63 minus 1 is reported as
  none, which the contract allows for an SDK that carries it as a signed 64-bit integer.
- **The evaluator holds strings as WTF-8.** A lone surrogate from a `\uD800` escape survives as the three bytes
  ED A0..BF xx, so `contains`, `startsWith`, `endsWith` and the order ops see the same UTF-16 code units the
  spec names. Only the bucketing hash replaces a lone surrogate with U+FFFD, as the spec says.
