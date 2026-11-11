# Go server SDK: design

Module `github.com/madangryscientist/toggleslash/sdks/go`, package `toggleslash` (the directory is `go`, a
keyword, so the package takes the product's name). It is written to
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) and evaluates to
[`docs/evaluation.md`](../../docs/evaluation.md). Nothing is published under that path.

## 1. The mapping table

| Name | In Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client`. It starts the loops and returns at once. It never returns an error and never panics: a missing URL or key shows up as a failed sync (`lastError` and `onError`). |
| `ready` | `(*Client).Ready() bool`. Blocks until the first attempt settles (at most about `requestTimeout`), then answers the same value for ever. |
| `variation` | `(*Client).Variation(flagKey string, ctx Context, fallback Value) Value`, plus the typed accessors `BoolVariation`, `StringVariation`, `NumberVariation`, `IntVariation` and `JSONVariation`. |
| `variationDetail` | `(*Client).VariationDetail(flagKey, ctx, fallback Value) Detail`, plus `BoolVariationDetail`, `StringVariationDetail`, `NumberVariationDetail`, `IntVariationDetail` and `JSONVariationDetail`, which return `TypedDetail[T]`. |
| `status` | `(*Client).Status() Status`, one consistent snapshot: `ETag string`, `LastSyncAt *time.Time` (nil if never), `Stream StreamState`, `LastError string` (empty if none; the codes are the constants `SyncTimeout`, `SyncBadRuleset` and `SyncNetwork`, and `"http_<status>"`). |
| `close` | `(*Client).Close()`. Idempotent, safe from any goroutine, returns within about twice `requestTimeout`. |
| `url` | first argument of `New` (required). Trailing `/` characters are removed. |
| `sdkKey` | second argument of `New` (required). |
| `stream` | `WithStream(bool)`. Default on. |
| `pollInterval` | `WithPollInterval(time.Duration)`. Default 30 s. |
| `flushInterval` | `WithFlushInterval(time.Duration)`. Default 30 s. |
| `requestTimeout` | `WithRequestTimeout(time.Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(EvaluationEvent))`. Default none. |
| `onError` | `WithOnError(func(error))`. Default ignore. |
| `accessor shape` | Both. The generic `Variation` and `VariationDetail` take and return the SDK's `Value` (fallback kind decides the mismatch rule; a null fallback accepts any kind). The typed accessors are `Bool`, `String`, `Number` (`float64`), `Int` (`int64`) and `JSON` (`Value`), each with a plain and a `Detail` form. `Int` applies the integer rule of contract section 4. |
| `onError thread` | A goroutine of the SDK, never the caller's, except for the errors raised on the caller's own goroutine by `Variation`: a hook that panics and an evaluator that panics are reported to `onError` on the calling goroutine, because that is where they were raised (contract section 10 runs the hook there). A panic inside `onError` is recovered and ignored. |

**Closed sets keep their spelling.** `ReasonKind` and `ErrorCode` are string types (aliases of the evaluator's), with
the constants `ReasonOff = "OFF"`, `ReasonTargetMatch`, `ReasonRuleMatch`, `ReasonFallthrough`,
`ReasonPrerequisiteFailed` and `ReasonError`, and one `ErrorCode` constant per code: `NotReady`,
`TypeMismatch`, `EvaluationFailed` and the evaluator's seven (`InvalidContext`, `FlagNotFound` and so on). `StreamState`
is a string type (`"off"`, `"connecting"`, `"connected"`, `"disconnected"`, `"closed"`). A `Reason` is
`{Kind ReasonKind, RuleIndex int, FlagKey string, Code ErrorCode}`; the fields a kind does not carry are the zero
value. The variation index is an `int`, and the constant `NoVariation = -1` means none. An error reaching
`onError` is a `*Error{Source, Code, Message, Err}`; a recovered panic is a `*PanicError{Value}` in its `Err`.

**"Not given" versus zero.** Options are functional options (`type Option func(*config)`). The defaults
are written into the `config` struct before any option runs, and an option replaces a default only when it
is called. A non-positive duration passed to an option is ignored, so the default stays (contract section 2
says an SDK SHOULD do that). `WithStream(false)` is therefore distinct from not calling it.

## 2. Concurrency

- **The ruleset and its ETag are one immutable value**, `*snapshot{rules *evaluator.Ruleset; etag string}`,
  held in an `atomic.Pointer[snapshot]`. A sync builds a new `snapshot` and stores it in one `Store`; a
  reader does one `Load` and so always sees a ruleset with the ETag it came with. `Variation` takes no lock
  for this. A hook's `etag` and `flagVersion` both come from the snapshot the evaluation used.
- **Goroutines.** `New` starts two, each through `startLoop`: the sync loop (first load, then the stream or
  poll loop of contract 7.3) and the flush loop (usage). `Close` cancels one `context.Context` they share, so
  the ruleset and stream requests abort, waits until the count of running loops is zero, then does the final
  usage flush on the calling goroutine. Neither loop keeps the process alive: they are goroutines, and a Go
  program exits when `main` returns whatever goroutines run, so an unclosed client holds nothing
  (`testdata/unclosed` proves it).
- **Locks.** None is held across I/O or a callback. `stateMu` guards the status fields (stream state, last
  sync time, last error); `holdMu` the two Retry-After hold times (routes, usage); `usageMu` the counter map;
  `flushMu` makes flushes serial; `closeMu` the first-caller flag of `Close`; `loopMu` the loop count.
  `Variation` takes `usageMu` for one map increment and nothing else. The flusher swaps the map out under
  the lock and sends the copy.
- **Callbacks.** `afterEvaluation` runs on the caller's goroutine, after every `Variation` and
  `VariationDetail` (a fallback included), with no SDK lock held; its `etag` and `flagVersion` come from the
  one snapshot the evaluation used. `onError` is called through `deliver`, which recovers a panic and ignores
  it. A panic recovered from a hook or the evaluator is passed to `onError` as a `*PanicError` carrying the
  panic's value.
- **Calls from the SDK's own goroutine (contract 6.4).** Go has no goroutine identity, and parsing
  `runtime.Stack` is banned by the language note. `reportFromLoop` takes the calling loop out of the count
  `Close` waits for (`loopDone`) while the application callback runs and puts it back (`loopResume`) after, so a
  `Close` called from `onError` does not wait for the goroutine that is running it. `Ready` needs no such
  mark: it settles before the first error is reported. The final flush's errors are reported from the
  `Close` caller with `inFinalReport` set, so a `Close` called from those callbacks returns at once, being
  the shutdown itself. A callback that runs on the caller's goroutine (`afterEvaluation`) is not marked: it is
  an ordinary caller, and nothing it waits on waits for it. The imprecision is that a second `Close` from a
  callback on a different goroutine that is not an SDK goroutine waits for the shutdown, as any caller does.
- **`ready`.** A `chan struct{}` closed once, when the first attempt settles or the client is closed; the
  answer (`bool`) is stored before the close, so it is fixed.
- **Usage** is a `map[usageKey]int64` under `usageMu` (`usageKey{flag string; index int; day string}`, the day
  taken from the clock at the moment of the count, in UTC). Flushes are serial under `flushMu`; a flush that
  fails with a status the contract keeps puts its untried entries back with `restoreUsage`.
- **`flagVersion`.** The evaluator's `FlagVersion` clamps a version at or above 2^63 to `math.MaxInt64`, since
  the hook field is an `int64`; a version beyond that has no meaning for a caller.

## 3. HTTP and SSE

- **Client.** `net/http` with its own `*http.Client{Transport: &http.Transport{DisableCompression: true, ...}}`
  and `CheckRedirect` returning `http.ErrUseLastResponse`, so a 3xx is `http_3xx` and `Authorization` is never
  replayed. The SDK key travels in `Authorization: Bearer <key>`. Gzip is off because the ruleset is read as
  is and the request header list is then shorter.
- **`requestTimeout` bounds the whole request, body included**, with `context.WithTimeoutCause` on the request's
  context: the cause is read back to tell `timeout` from `network`. The body is read under that context with
  `io.ReadAll`, so a server that trickles the body is cut. A `*url.Error` is never passed on (its text holds the
  URL); only fixed text is (`network`, `timeout`, `bad_ruleset`, `http_<n>`).
- **The stream** is opened with a context bounded by `requestTimeout` only until the response headers are
  read; then the bound is dropped and a 60 s watchdog (a `time.Timer` reset by every non-empty read) cancels
  the request if no bytes arrive. A clean end of the body is not an error.
- **SSE.** A hand-written parser over `bufio.Reader`, to the WHATWG algorithm: lines end at CRLF, LF or CR,
  a line that starts with `:` is a comment, the `event` and `data` fields are collected, a blank line
  dispatches (data lines joined by LF, a trailing LF removed, an empty data buffer dispatches nothing), a
  single leading space after the colon is dropped, a UTF-8 BOM at the start is skipped. `id` and `retry` are
  ignored.
- **The body** is decoded with the WHATWG UTF-8 decoder (an invalid sequence becomes U+FFFD by maximal
  subpart), after a BOM skip, before the JSON parser sees it.

## 4. Dependencies

None at run time: `net/http`, `crypto/sha256`, `sync`, `time`, `context` and `strconv` are all in the
standard library. `encoding/json` is not used, for the reason in section 5. Test and lint tools: the `testing`
package (the mainstream Go test framework; `-race` needs cgo, so `zig cc` is pinned and used as the C
compiler), `go vet`, `gofmt -l` and `golangci-lint` (the linters are listed in `.golangci.yml` with the reason
for each).

## 5. JSON

`encoding/json` fails on `1e999` and replaces a lone surrogate with U+FFFD, and both are required to
survive (the spec's "Reading JSON"), so the SDK has its own reader, `internal/jsonv`:

- A `Value` is `{kind, bool, float64, string, []Value, *Object}`. An `Object` keeps an ordered key slice
  and a map; a duplicate key keeps the last value and the first position.
- Numbers are read with `strconv.ParseFloat(text, 64)`; a range error returns `+-Inf`, which is what the spec
  wants (`1e999` is Infinity, not an error); `9007199254740993` reads as `9007199254740992`; `-0` is
  kept as negative zero.
- Strings are held as **WTF-8**: valid UTF-8, plus a lone surrogate as its three-byte form (`ED A0..BF xx`).
  An escape pair `😀` is combined into the four-byte code point; two escapes that are not a high
  followed by a low stay lone. Byte equality on a WTF-8 string is therefore equality of UTF-16 code units.
  The ordered operations (`lt` and friends, substring ops) convert both operands to UTF-16 code units before
  comparing, because the spec orders by code unit and `"😀"` must sort before `"～"`.
- Hashing replaces a lone surrogate by U+FFFD (`EF BF BD`) before SHA-256, as the spec says.
- Nesting is limited to 2048 levels (`jsonv.MaxDepth`; the contract asks for at least 512); beyond it the parse fails, which
  for a ruleset is `bad_ruleset`.
- The body decoder strips a BOM and replaces invalid UTF-8 like WHATWG. The public type is `toggleslash.Value`
  (an alias), with `ParseJSON`, `FromAny` and builders, so a caller can pass `1e999` or a lone surrogate in a
  context, and the test service passes the context as the JSON it arrived as.

## 6. Minimum runtime and toolchain

- **Go 1.26** is the minimum in `go.mod` (`go 1.26`): the oldest release in upstream security support when
  this was written (Go supports the two newest), and it has the generics, `atomic.Pointer`, `min`/`max`,
  `for range n` and `context.WithTimeoutCause` the SDK uses. The toolchain pinned for builds is the newest
  `mise ls-remote go` lists, **1.27.4**, in `mise.toml`, with `zig 0.17.1` (C compiler for `-race`) and
  `golangci-lint 2.15.0`.
- **Test framework:** the standard `testing` package, table tests and `httptest`, because it is what every Go
  project uses and needs no dependency.
- **Lint and format:** `go vet`, `gofmt -l` and `golangci-lint` (mainstream), no warnings allowed.
- **Build tool:** `go build`, `go test`; the scripts in `bin/` wrap them under `mise exec --` when mise is on
  PATH.
- **Integer accessor:** `IntVariation` returns `int64`.

## 7. Package layout

```
sdks/go/
  go.mod                    module github.com/madangryscientist/toggleslash/sdks/go
  mise.toml                 go, zig, golangci-lint pins
  .golangci.yml             the lint rules, each with its reason
  client.go options.go types.go context.go value.go
                            the public API: Client, options, result and status types, Context and Value
  sync.go stream.go sse.go  the ruleset request, the stream, and the SSE parser (contract 7)
  usage.go                  the usage counts and their flush (contract 9)
  *_test.go                 native tests over one loopback fake server (helpers_test.go)
  testdata/unclosed/        a program that never closes its client and must exit
  internal/wtf8/            WTF-8 strings: lone surrogates, UTF-16 units, the WHATWG lossy decoder
  internal/jsonv/           the JSON reader/writer
  internal/evaluator/       the evaluator (no network): ruleset, clauses, patterns, semver, dates, bucket
                            and the vector test (TestVectors, TestBucketPins)
  test-service/main.go      the conformance test service (package main, compiled by bin/test)
  bin/lint  bin/test  bin/test-service
  .build/                   build output (ignored)
```

## 8. Recorded disagreements between references and the spec

None found so far. Any found while writing the evaluator is added here with both readings.
