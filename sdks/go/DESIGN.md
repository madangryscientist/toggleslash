# Go server SDK: design

Written before any code, for `docs/sdk-contract.md` (the contract), `docs/evaluation.md` (the spec) and
`docs/sdk-test-service.md` (the harness protocol). Module `github.com/madangryscientist/toggleslash/sdks/go`;
the package is named `toggleslash` because `go` is a keyword.

## 1. The mapping table

Go names, with Go's idioms: functional options, methods on `*Client`, a generic `Detail[T]`.

| Name | In Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client`. It starts the loop goroutine and the flush goroutine and returns at once. It returns a client even for a bad url or key (every request is then a `network` or `http_401` error). |
| `ready` | `(*Client).Ready() bool`. Blocks until the first load attempt settles or `Close` runs; the answer is fixed at the first settle. |
| `variation` | One method per accessor, no `Detail`: `BoolVariation`, `StringVariation`, `IntVariation`, `FloatVariation`, `JSONVariation`, each `(flagKey string, ctx Context, fallback T) T`. |
| `variationDetail` | One method per accessor with a `Detail` suffix: `BoolVariationDetail`, `StringVariationDetail`, `IntVariationDetail`, `FloatVariationDetail`, `JSONVariationDetail`, each returning `Detail[T]{Value T, VariationIndex *int, Reason Reason}`. `VariationIndex` is `nil` when there is none. |
| `status` | `(*Client).Status() Status`, a struct value: `ETag string` (empty is none), `LastSyncAt time.Time` (zero is none), `Stream StreamState`, `LastError string` (empty is none). One snapshot taken under one lock. |
| `close` | `(*Client).Close()`. Safe to call twice and concurrently; every call returns when the shutdown is done. No return value, nothing to fail. |
| `url` | `toggleslash.New`'s first argument (required). Trailing `/` characters are removed. |
| `sdkKey` | `toggleslash.New`'s second argument (required). |
| `stream` | `toggleslash.WithStream(bool)`. Default on. |
| `pollInterval` | `toggleslash.WithPollInterval(time.Duration)`. Default 30 s. |
| `flushInterval` | `toggleslash.WithFlushInterval(time.Duration)`. Default 30 s. |
| `requestTimeout` | `toggleslash.WithRequestTimeout(time.Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `toggleslash.WithAfterEvaluation(func(EvaluationEvent))`. Default none (a nil function is the same). |
| `onError` | `toggleslash.WithOnError(func(error))`. Default ignore. The value is a `*toggleslash.Error`. |
| `accessor shape` | Typed methods, five of them, each with a `...Detail` twin, and no generic method on the client. `bool`, `string`, `int` (Go `int64`), `float` (`float64`) and `json` (a `Context`, the SDK's own JSON `Value`). The harness's `any` accessor uses `JSONVariation`. The `integer-accessor` capability is advertised: a served number with a fractional part, or outside the `int64` range, is `TYPE_MISMATCH`. |
| `onError thread` | The goroutine that met the failure, called inline, never under a lock: the loop goroutine for ruleset and stream failures, the flush goroutine for usage failures and the counts lost at close, and the caller's own goroutine for a hook that panicked or an evaluation that panicked. A panic in `onError` is recovered and ignored. |

**Closed sets.** Each is a named string type with constants whose values are the canonical spelling, so
`ReasonKindPrerequisiteFailed` is the string `"PREREQUISITE_FAILED"` and can be compared, printed and
switched on:

- `ReasonKind`: `OFF`, `PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`.
- `ErrorCode`: `INVALID_CONTEXT`, `FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`, `MALFORMED_FLAG`,
  `MALFORMED_SEGMENT`, `PREREQUISITE_CYCLE`, `PREREQUISITE_DEPTH_EXCEEDED`, `NOT_READY`, `TYPE_MISMATCH`,
  `EVALUATION_FAILED`.
- `StreamState`: `off`, `connecting`, `connected`, `disconnected`, `closed`.
- `lastError` is a plain `string` holding `http_<status>`, `timeout`, `bad_ruleset` or `network` (the `http_`
  codes cannot be a fixed set of constants); `LastErrorTimeout`, `LastErrorBadRuleset` and `LastErrorNetwork`
  are the fixed three.
- `Reason` is a struct: `Kind ReasonKind`, `RuleIndex *int` (set for `RULE_MATCH`), `FlagKey string` (set for
  `PREREQUISITE_FAILED`), `ErrorCode ErrorCode` (set for `ERROR`).

**Not given versus false or zero.** Options are functional (`Option`), so an option the caller did not pass
is simply absent and the default stays; `WithStream(false)` is a real false and `WithPollInterval(0)` is a
real zero. A zero or negative duration falls back to the default (contract section 2: "SHOULD fall back"),
and the fallback is not tested. `stream` has no "not given" ambiguity because the options struct is private
and starts from the defaults.

## 2. Concurrency

- **The ruleset and its ETag.** One immutable `snapshot{ruleset *evaluator.Ruleset, etag string}` and an
  `atomic.Pointer[snapshot]`. A sync builds a whole new snapshot and stores the pointer once, so a reader sees
  the old ruleset with the old ETag or the new one with the new ETag, never a mix. `variation` loads the
  pointer once per call and uses that snapshot for the evaluation, the hook's `etag` and `flagVersion`.
- **Goroutines.** Exactly two: the *loop* goroutine (the first load, then the stream and poll loop of
  contract 7.3; it reads the stream body itself) and the *flush* goroutine (one `flushInterval` timer, then a
  flush). Both are plain goroutines, which do not keep a process alive, so a client that is never closed does
  not block exit (contract 6.6). A root `context.Context` made with `WithCancelCause` is cancelled by `Close`;
  every ruleset and stream request derives from it, so cancelling abandons them. Usage requests derive from
  `context.Background()` instead, because a flush already running when `Close` starts finishes (contract 11).
- **`Close`.** The first call marks the client closing under the state lock (stream state `closed`, `closing`
  flag set, so nothing is counted any more) and cancels the root context. Every call then waits for the loop
  goroutine to exit and for the flush goroutine to exit. The flush goroutine's last act is the final flush,
  so waiting for it is waiting for the final flush. A `Close` called from a callback running on either SDK goroutine (an `onError` on the loop
  goroutine or on the flush goroutine) stops everything and returns without waiting for anything (contract
  6.4). It does not wait for its own goroutine, and it does not wait for the other one either: two callbacks
  on the two goroutines may call `Close` at the same moment, and each waiting for the other would deadlock.
  The caller's goroutine id is read from `runtime.Stack`'s first line and compared with the two ids the
  goroutines recorded when they started. The stopped goroutines finish on their own, and a later `Close`
  from any other goroutine waits for them.
  `Close` is bounded by about two request timeouts: an abandoned ruleset or stream request returns at once,
  and the flush goroutine's running flush and the final flush each stop at their first failed request.
- **Locks.** `mu` (a `sync.Mutex`) guards the status fields (`lastSyncAt`, `lastError`, stream state,
  `closing`) and the two Retry-After holds, and the snapshot store happens under it too so `Status` reads one
  consistent view. `usageMu` guards the usage map. `flushMu` serialises flushes. `mu` and `usageMu` are held
  only for memory operations: never across a network call, a hook or `onError`. `variation` takes `usageMu`
  for one map increment and never `mu` or `flushMu`, so a hung server cannot slow an evaluation. The flush
  takes `usageMu` only to swap the map out and to merge kept counts back.
- **Callbacks.** No SDK lock is held while `afterEvaluation` or `onError` runs. `Ready` returns at once when
  the first attempt has settled, and the first attempt settles *before* its `onError` is called, so a `Ready`
  from that `onError` on the loop goroutine does not wait for the load that goroutine is running.

## 3. HTTP and SSE

- **Client.** `net/http` with one `http.Transport` per client (a clone of `http.DefaultTransport` with
  `DisableCompression: true`, so no `Accept-Encoding` is added), and one `http.Client` with no `Timeout` and
  `CheckRedirect` returning `http.ErrUseLastResponse`, so a 3xx is an `http_3xx` error and `Authorization`
  never follows a redirect. `Close` calls `CloseIdleConnections`.
- **`requestTimeout` over the whole request, body included.** Each ruleset and usage request gets
  `context.WithTimeoutCause(parent, requestTimeout, errRequestTimeout)` and the body is read with
  `io.ReadAll` before `cancel`, so the deadline bounds the connect, the headers and the body read. A failure
  whose context cause is `errRequestTimeout` is `timeout`; one while the root context was cancelled by
  `Close` is abandoned and reported to nobody; anything else is `network`. Error text is never taken from the
  `*url.Error` (it carries the URL): only the fixed codes are reported.
- **The stream.** The open is bounded by a `time.AfterFunc(requestTimeout, cancel)` on the request's context,
  stopped when the response headers arrive. After that a watchdog `time.AfterFunc(60 s)` (the length is an
  unexported seam so a test can shorten it on a real socket) cancels the context with a watchdog cause; every
  successful `Read` resets it, so a keep-alive comment restarts the 60 s. The loop goroutine reads the body
  in a `for` with a 4 KiB buffer and feeds the parser.
- **SSE.** A short hand-written parser to the WHATWG algorithm, working on bytes so a read may split a UTF-8
  sequence or a CRLF anywhere: a leading BOM is skipped, LF, CRLF and a lone CR end a line (a CR at the end of
  a read waits for the next byte to know if an LF follows), a line starting with `:` is a comment, `event` and
  `data` are the only fields read, `data` lines join with LF, an event with no `data` line is dropped, the name
  is `message` when none was given, and an event still incomplete at the end of the stream is discarded. Field
  values are decoded with the same lossy UTF-8 decoder as ruleset bodies.
- **Clock, jitter and other seams.** A `clock` interface (`Now`, `NewTimer`), a jitter function, the stream
  idle length, the evaluator function and a `*tls.Config` (for a local certificate), all unexported options, so the native tests can assert the waits of 7.3, the 300 s Retry-After cap and the UTC midnight
  split without sleeping. The real clock wraps `time`. Request deadlines, the stream open bound and the
  watchdog always use real time.

## 4. Dependencies

Runtime: none. The standard library has `net/http`, `crypto/sha256`, `context`, `sync` and `math/rand/v2`.
`encoding/json` is not used (see section 5). Test: the standard `testing` and `net/http/httptest`, `net`,
nothing else. Lint: `gofmt`, `go vet` and `golangci-lint` (section 6). `zig` is a toolchain pin only: it is
the C compiler `-race` needs.

## 5. JSON

- **Own parser, own value type.** `encoding/json` errors on `1e999` and replaces a lone surrogate with
  U+FFFD, both of which the spec forbids (`language-notes/go.md`). The public package `jsonvalue` holds
  `Value{kind, num float64, str string, arr []Value, obj map[string]Value}`, a recursive-descent parser over
  bytes, and a writer.
- **Numbers.** The grammar is checked by hand (RFC 8259: no leading zeros, no `+`, no bare `.`) and the
  literal goes to `strconv.ParseFloat(…, 64)`, which is correctly rounded: `9007199254740993` is
  `9007199254740992`, `-0` keeps its sign, and `1e999` comes back as `+Inf` with an `ErrRange` that the parser
  ignores (underflow gives `0`).
- **Strings are WTF-8** held in a Go `string`. A `😀` pair of escapes becomes the one code point
  U+1F600 (four UTF-8 bytes); a lone `\uD800` to `\uDFFF` becomes the three bytes `ED A0..BF xx`, which a
  Go string can hold. Raw bytes inside a string go through the WHATWG UTF-8 decoder (one U+FFFD per maximal
  invalid subpart), so raw invalid bytes never become a surrogate. Object keys are the same strings.
  Duplicate keys keep the last value, a leading BOM is skipped, and nesting deeper than 10000 levels is
  refused (the contract's minimum is 512).
- **The context.** `type Context = jsonvalue.Value`, so a caller passes exactly the value the vectors carry,
  `1e999` and lone surrogates included. `jsonvalue.Parse(raw)` reads one; `jsonvalue.From(any)` builds one from
  `map[string]any`, `[]any`, strings, bools and numbers; `toggleslash.NewContext(kind, key, attrs)` builds the
  common single context. The zero `Value` is JSON null, which the evaluator answers as `INVALID_CONTEXT`.
- **Comparing and searching strings.** The spec orders strings by UTF-16 code unit. Where both strings are
  free of bytes `0xED` to `0xFF` the byte order is the same and the byte functions are used; otherwise the
  comparison, `contains`, `startsWith` and `endsWith` work on the two strings converted to UTF-16 code units
  (a WTF-8 surrogate becomes its own unit). For hashing a WTF-8 surrogate is replaced with U+FFFD
  (`EF BF BD`) first. For `matches` both pattern and candidate are decoded to code points, a lone surrogate
  being itself.

## 6. Minimum runtime and toolchain

- **Go 1.26.** The oldest release still supported upstream when the run was made (1.27 and 1.26 are). The SDK
  uses generics, `atomic.Pointer`, `context.WithTimeoutCause`/`WithCancelCause`, `math/rand/v2` and
  `slices`/`maps`, all in far older releases, so nothing needs more. The toolchain pin is 1.27.3, the newest
  `mise ls-remote go` lists (go.dev also lists 1.27.4; per `language-notes/go.md` the run pins the newest
  mise lists and says so in `RUN.md`).
- **Package manager and build:** Go modules and the `go` tool; no extra build tool. `go.mod` has no `require`.
- **Test framework:** the standard `testing` package, run with `-race -count=1`. `-race` needs cgo and so a C
  compiler: `mise.toml` pins `zig` and `bin/test` sets `CC="$(mise where zig)/zig cc"` by path.
- **Lint and format:** `gofmt -l`, `go vet ./...` and `golangci-lint` 2.x with an explicit config, the current
  mainstream Go lint driver.
- **The integer accessor** uses `int64`, the 64-bit signed type.

## 7. Package layout

```
sdks/go/
  go.mod                  module github.com/madangryscientist/toggleslash/sdks/go, go 1.26
  mise.toml               go, zig, golangci-lint pinned
  toggleslash.go          options, New, Client, Ready, Status, Close
  variation.go            accessors and the section 4 result rules
  types.go                the closed sets, Reason, Detail, Status, EvaluationEvent, Error
  sync.go, sse.go         the ruleset request, the stream, the loop
  usage.go                counts and the flush
  jsonvalue/              the JSON Value, the parser, the writer (public: it is the Context type)
  internal/evaluator/     the evaluator, to docs/evaluation.md (no network code)
  internal/wtf8/          WTF-8 helpers: lossy UTF-8 decode, UTF-16 compare, code points
  test-service/main.go    the harness's test service (package main in the same module)
  bin/test, bin/lint, bin/test-service
  .golangci.yml           the golangci-lint rule set
  README.md, DESIGN.md, RUN.md
```

The evaluator tests (`internal/evaluator`) read `fixtures/evaluation/` by a relative path; the client tests
(`client_test.go`, `timing_test.go`, `concurrency_test.go`, with `helpers_test.go`) run a loopback fake
server in the test process.

## 8. Disagreements with the reference implementations

None found. The Go evaluator passes every vector in `fixtures/evaluation/` (1371 vectors and 17 bucket pins), so it and the TypeScript engine agree on them.
