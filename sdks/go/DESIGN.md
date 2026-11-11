# ToggleSlash Go SDK: design

Written before any code (PROMPT.md, phase 1). The package is `toggleslash`, in the module
`github.com/madangryscientist/toggleslash/sdks/go`. Nothing is registered under that path.

## 1. The mapping table

Names follow Go's usual casing. `Client` is the type `NewClient` returns; every method is safe to call from
many goroutines at once.

| Name | In Go |
| --- | --- |
| `create` | `toggleslash.NewClient(url, sdkKey string, opts ...Option) *Client`. It returns a client and never an error: it starts the sync and flush goroutines and does not wait for the network. A bad URL makes each request a `network` error. |
| `ready` | `(*Client).Ready() bool`. Blocks until the first ruleset load has finished, then answers whether a ruleset is held. It is bounded by the request timeout, the answer is fixed when the first attempt settles, and after `Close` it answers `false` if it had not settled. |
| `variation` | The accessors of "accessor shape" below, for example `(*Client).BoolVariation(flagKey string, ctx Context, fallback bool) bool`. |
| `variationDetail` | The matching `...VariationDetail` methods, for example `(*Client).BoolVariationDetail(...) Detail[bool]`. A `Detail[T]` has `Value T`, `VariationIndex *int` (nil when the value is the fallback) and `Reason Reason`. |
| `status` | `(*Client).Status() Status`, a struct copied under one lock: `ETag string` (empty: none), `LastSyncAt time.Time` (zero: never), `Stream StreamState`, `LastError string` (empty: none, else one of the `LastError...` codes). |
| `close` | `(*Client).Close()`. No return value, never panics, safe to call twice and concurrently. Returns within about twice the request timeout. |
| `url` | the first argument of `NewClient`, required. Trailing `/` characters are removed. |
| `sdkKey` | the second argument of `NewClient`, required. |
| `stream` | `toggleslash.WithStream(bool)`. Default on. The option stores a `*bool`, so "not given" (on) is told from `false`. |
| `pollInterval` | `toggleslash.WithPollInterval(time.Duration)`. Default 30 s. Zero or negative falls back to the default (contract section 2 allows it). |
| `flushInterval` | `toggleslash.WithFlushInterval(time.Duration)`. Default 30 s. Zero or negative falls back to the default. |
| `requestTimeout` | `toggleslash.WithRequestTimeout(time.Duration)`. Default 10 s. Zero or negative falls back to the default. |
| `hooks.afterEvaluation` | `toggleslash.WithAfterEvaluation(func(EvaluationEvent))`. Default none. A nil function is ignored. It runs on the caller's goroutine, synchronously. A panic in it is recovered and goes to `onError`. |
| `onError` | `toggleslash.WithOnError(func(error))`. Default ignore. A panic in it is recovered and ignored. |
| `accessor shape` | Typed methods, each with a `...Detail` twin: `BoolVariation`, `StringVariation`, `IntVariation` (int64), `FloatVariation` (float64), `JSONVariation` (a `Value` that must be an array or an object, fallback kind decides) and `AnyVariation` (a `Value`; a null fallback accepts any served kind). No generic method: Go methods cannot take type parameters, and a free generic function over the fallback's type could not tell `int64` from `float64` for the contract section 4 integer rule. |
| `onError thread` | The goroutine that hit the failure: the sync goroutine for a ruleset or stream failure, the flush goroutine (or, for the final flush, the shutdown goroutine) for a usage failure, the caller's goroutine for a hook that panicked or an evaluation that raised. It is called synchronously and never while the SDK holds one of its own locks. |

**Closed sets keep their spelling.** `ReasonKind` is a string type with the constants `ReasonOff`
(`"OFF"`), `ReasonPrerequisiteFailed`, `ReasonTargetMatch`, `ReasonRuleMatch`, `ReasonFallthrough` and
`ReasonError`. `ErrorCode` is a string type holding `"INVALID_CONTEXT"`, `"FLAG_NOT_FOUND"`,
`"SEGMENT_NOT_FOUND"`, `"MALFORMED_FLAG"`, `"MALFORMED_SEGMENT"`, `"PREREQUISITE_CYCLE"`,
`"PREREQUISITE_DEPTH_EXCEEDED"`, `"NOT_READY"`, `"TYPE_MISMATCH"` and `"EVALUATION_FAILED"`.
`StreamState` is a string type: `"off"`, `"connecting"`, `"connected"`, `"disconnected"`, `"closed"`. The
`lastError` codes are the strings `http_<status>`, `timeout`, `bad_ruleset` and `network`
(`LastErrorTimeout`, `LastErrorBadRuleset`, `LastErrorNetwork` are constants; the HTTP one is built).
A `Reason` carries `Kind`, `Code` (set for `ERROR`), `RuleIndex *int` (set for `RULE_MATCH`) and
`PrerequisiteKey string` (set for `PREREQUISITE_FAILED`).

**Telling "not given" from false or zero.** Options are functions over an unexported config whose fields are
pointers (`stream *bool`) or checked against zero (`pollInterval <= 0` takes the default). Nothing is
read from the environment.

**Contexts.** A `Context` wraps the SDK's own JSON value, so a context keeps `1e999` and lone surrogates.
Builders: `UserContext(key)`, `NewContext(kind, key)` with `.With(attr, value)` for attributes and
`MultiContext(...)`; `ContextFromJSON([]byte)` reads raw JSON through the SDK's own reader, and
`ContextFromMap(map[string]any)` converts native values. The zero `Context` and any context that cannot be
built are invalid and evaluate to `ERROR INVALID_CONTEXT`, never a panic.

## 2. Concurrency

- **The ruleset and its ETag** are one immutable `snapshot` struct (`*eval.Ruleset`, the ETag string). A
  sync builds the new ruleset off to the side and replaces the pointer with one `atomic.Pointer.Store`
  inside the status lock, so a reader sees the old pair or the new pair and a hook's `etag` and
  `flagVersion` belong to the ruleset it evaluated. `variation` loads the pointer with no lock.
- **Locks.** `statusMu` (an `RWMutex`) guards `lastSyncAt`, `stream`, `lastError` and the snapshot swap, and
  is held only for field assignments, never across I/O or a callback. `usageMu` guards the counts map and
  is held for one map add or a swap-out. `flushMu` makes flushes serial and is held across the usage
  requests; `variation` never takes it, and nothing that `variation` takes is held by a request. `holdMu`
  guards the two Retry-After deadlines. `closeOnce` and a `done` channel make `Close` idempotent. No lock is
  held while `afterEvaluation` or `onError` runs: errors found under `flushMu` are collected and reported
  after it is released.
- **Goroutines.** Two, started by `NewClient`: the sync goroutine (first load, then the loop of contract
  7.3) and the flush goroutine (a timer loop). Both watch a `stop` channel and a context cancelled by
  `Close`. `Close` starts one shutdown goroutine on the first call (a `sync.Once`); it cancels the sync
  context, closes `stop`, waits for both goroutines, runs the final flush with its own context, settles
  `ready` false if unsettled and closes `done`. Every `Close` call waits on `done`. Bound: the sync
  goroutine is abandoned at once (its requests carry the cancelled context), a flush already running is
  bounded by one `requestTimeout` per request and the final flush by the same, so `Close` returns within
  about twice `requestTimeout`.
- **Callbacks that call back in.** The SDK records the goroutine id of each goroutine it owns (sync, flush,
  shutdown). `Ready` and `Close` check the caller against that set (`goid()` reads the id from
  `runtime.Stack`; only these two operations pay for it, never `variation`). From an SDK goroutine,
  `Ready` returns the state held without waiting, and `Close` starts shutdown and returns without waiting
  for the work that goroutine is in the middle of (contract 6.4).
- **A client never closed** keeps no process alive: goroutines do not hold a Go program open, and the
  idle HTTP connections are the transport's own.
- **The race detector** runs for the whole test suite (`go test -race`).

## 3. HTTP and SSE

- **Client.** `net/http` with a `*http.Client` whose `Transport` is a clone of the default one, owned by the
  `Client` and closed (`CloseIdleConnections`) on `Close`, and whose `CheckRedirect` returns
  `http.ErrUseLastResponse`, so a 3xx is an `http_3xx` error and `Authorization` is never resent to a
  redirect target. An option (`WithHTTPClient`) injects another client for tests.
- **Whole request, body included.** Each ruleset and events request runs under
  `context.WithTimeoutCause(parent, requestTimeout, errRequestTimeout)`; the context is cancelled only after
  the body has been read and closed, so the timer covers connect, headers and body. A `context.Cause` of
  `errRequestTimeout` is `timeout`; a cancel by `Close` is silent; anything else is `network`. Error text
  reported to `onError` is fixed text (`network`, `timeout`, `bad_ruleset`, `http_<n>`), never a `*url.Error`
  string, which carries the URL.
- **The stream.** Opening is bounded by `requestTimeout` with a timer that cancels the request until the
  response headers arrive (then the timer is stopped). After that a watchdog `time.Timer` of 60 s is reset
  after every `Read` that returns bytes; when it fires it cancels the request with cause `errWatchdog`, so
  the blocked `Read` returns and the cause makes `timeout`. The length is the unexported option
  `withWatchdog` for tests.
- **SSE parser.** Hand written (`sse.go`), about 80 lines, to WHATWG: it reads bytes, treats LF, CRLF and a
  lone CR as line ends (a CR at the end of a read waits for the next byte to see if an LF follows), strips
  a leading BOM, ignores comment lines, reads only `event` and `data`, strips one space after the colon,
  joins several `data` lines with LF, drops an event with no `data` line, names an event `message` when no
  name was given, and discards an event unfinished when the stream ends. UTF-8 split across reads is kept
  because it works on bytes and only decodes a whole line.
- **The body.** A ruleset body is read whole, a leading BOM skipped, bytes that are not valid UTF-8 replaced
  with U+FFFD per maximal invalid subpart (Go's `utf8` decoding of one invalid byte at a time differs, so
  the SDK scans with `utf8.DecodeRune` and extends the invalid run exactly as WHATWG does), then parsed by the
  SDK's own reader.

## 4. Dependencies

Runtime: none. The standard library has `net/http`, `crypto/sha256`, `sync`, `context`, `time`. Test: none
beyond the standard `testing` and `net/http/httptest` packages. Lint: `go vet`, `gofmt` and
`golangci-lint` (pinned in `mise.toml`), which are tools and not dependencies of the module. `zig` is pinned
only as a C compiler for `-race`, see section 6.

## 5. JSON

`encoding/json` cannot do what the spec asks: it fails on `1e999` (a number out of range) and replaces a
lone surrogate escape with U+FFFD. So the SDK has its own reader (`internal/jsonvalue`).

- A `Value` is a small tagged struct: kind (null, bool, number, string, array, object), the number as a
  `float64` read with `strconv.ParseFloat` on the literal (which returns `+Inf` with a range error for
  `1e999`; the reader accepts that, as binary64 says, and reads `9007199254740993` as
  `9007199254740992`), the string, a slice of values, and an object as a map plus its keys. A duplicate key
  keeps the last value. `-0.0` stays negative zero. Depth is limited to 1000 levels (the contract asks at
  least 512).
- **Strings are WTF-8 Go strings.** A well-formed pair of `😀` escapes becomes the 4-byte UTF-8
  sequence; a lone surrogate (from `\ud800` or an unpaired half) is encoded as the 3-byte generalized
  form (`ED A0 80`), which Go strings hold as plain bytes. `internal/wtf8` decodes them into code points
  (surrogates included) for comparison, pattern matching and hashing, and the evaluator hashes the same
  bytes the spec names: for a lone surrogate it hashes the code point's UTF-16 or WTF-8 form as
  `docs/evaluation.md` says, which is read in phase 2 and recorded below if it needs a decision.
- **Ordinal string comparison is by UTF-16 code unit**, as the spec says, not by Go's byte order. `wtf8`
  provides `CompareUTF16`.
- The reader also writes JSON (`Write`), used by the test service to answer with a value as it arrived and
  by the events body (which holds only strings and whole numbers, so it uses `strconv`).

## 6. The minimum runtime and the toolchain

- **Pin:** Go 1.27.3, the newest release `mise ls-remote go` lists on the run date. go.dev also lists 1.27.4
  as stable; mise hides it (its minimum-release-age setting). The pin follows the tool, as the language
  notes say, and RUN.md records it as a guess.
- **Minimum:** `go 1.26` in `go.mod`. Upstream supports the two newest majors, and 1.27 is current, so 1.26
  is the oldest in security support; the SDK uses `atomic.Pointer`, `context.WithTimeoutCause`,
  `min`/`max`, `slices`, `maps` and range-over-int, all in 1.22 or earlier, so nothing newer is needed.
- **Test framework:** the standard `testing` package. **Lint:** `go vet`, `gofmt -l` and `golangci-lint`
  v2 with an explicit linter list (`.golangci.yml`; each ignored rule has a reason beside it).
- **Race detector:** `go test -race` needs cgo and so a C compiler. `bin/test` uses `gcc` or `cc` when one is
  on `PATH` and otherwise `zig cc` (zig is pinned in `mise.toml`), so the self-hosted runner, which has no C
  compiler, still runs `-race`. The README says so.
- **Integer accessor:** `IntVariation` returns `int64`. A served number with a fractional part, or outside
  `[-2^63, 2^63)`, is `TYPE_MISMATCH`; `2.0` is `2`. The test service advertises `integer-accessor`.
- **Build tool:** the `go` command and plain `go.mod`. **Package manager:** Go modules (nothing to install).

## 7. The package layout

```
sdks/go/
  go.mod                 module github.com/madangryscientist/toggleslash/sdks/go
  toggleslash.go         package doc, Client, NewClient, options, Ready, Status, Close
  types.go               Reason, ErrorCode, StreamState, Status, Detail, EvaluationEvent
  context.go             Context and its builders
  variation.go           the typed accessors and the contract section 4 result order
  sync.go                the ruleset request, the stream, the loop, backoff, Retry-After holds
  sse.go                 the Server-Sent Events parser
  usage.go               usage counts and the flush
  internal/jsonvalue/    the JSON reader and writer, the Value type
  internal/wtf8/         WTF-8 helpers: code points, UTF-16 comparison, UTF-8 repair
  internal/eval/         the evaluator (no network code), the pattern parser and Pike VM, semver, dates
  *_test.go              client tests, a loopback fake server, the concurrency tests
  internal/eval/vectors_test.go   every fixtures/evaluation vector and bucket pin
  test-service/          package main, the conformance test service (README.md maps the accessors)
  bin/lint, bin/test, bin/test-service
  mise.toml, .golangci.yml, .gitignore, README.md, DESIGN.md, RUN.md
```

The test service is one more `main` package in the same module, so `go build ./...` and `go vet ./...`
compile it with the tests. Build output goes to `.build/`, which `.gitignore` lists.
