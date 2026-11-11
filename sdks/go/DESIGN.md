# ToggleSlash Go server SDK: design

This is the design the SDK in this directory was built to (phase 1 of
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md)). The contract is
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), evaluation is
[`docs/evaluation.md`](../../docs/evaluation.md) and the harness protocol is
[`docs/sdk-test-service.md`](../../docs/sdk-test-service.md). Section numbers below are the contract's.

The module is `github.com/madangryscientist/toggleslash/sdks/go` and the package is `toggleslash`
(`import toggleslash "github.com/madangryscientist/toggleslash/sdks/go"`). Nothing is registered under that
path.

## 1. The mapping table (contract 2)

Durations are `time.Duration`. `create` is `toggleslash.New`, which returns a `*toggleslash.Client`. Options are
functional options (`toggleslash.Option`) after the two required arguments.

| Name | Go | Default and meaning |
| --- | --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client` | Starts two goroutines and returns at once; it does not wait for the network. It never returns nil and never panics. |
| `ready` | `client.Ready() bool` | Blocks until the first ruleset attempt has settled (bounded by `requestTimeout`, which covers the name lookup, the connection and the body). True if a ruleset is held. The answer is fixed at the first settle. After `Close` it is false if it had not settled. |
| `variation` | `client.Variation(flagKey, ctx, fallback jsonvalue.Value) jsonvalue.Value`, and the typed `BoolVariation`, `StringVariation`, `Float64Variation`, `Int64Variation` | The generic method takes the expected kind from the fallback (a JSON null accepts any kind). |
| `variationDetail` | `client.VariationDetail(...) Detail[jsonvalue.Value]`, and `BoolVariationDetail`, `StringVariationDetail`, `Float64VariationDetail`, `Int64VariationDetail` | `Detail[T]` has `Value`, `VariationIndex` (`*int`, nil when the value is the fallback) and `Reason`. |
| `status` | `client.Status() Status` | One snapshot: `ETag`, `LastSyncAt` (`time.Time`, zero when never), `Stream` (`StreamState`), `LastError` (`string`, empty when none). |
| `close` | `client.Close()` | Stops, joins, sends the final usage flush. Safe twice and from many goroutines. Returns within about twice `requestTimeout`. |
| `url` | first argument of `New` (`string`, required) | The server's base URL. Trailing `/` are removed. |
| `sdkKey` | second argument of `New` (`string`, required) | A server key. |
| `stream` | `toggleslash.WithStream(bool)`; default true | `WithStream(false)` is poll only. |
| `pollInterval` | `toggleslash.WithPollInterval(d)`; default 30 s | |
| `flushInterval` | `toggleslash.WithFlushInterval(d)`; default 30 s | |
| `requestTimeout` | `toggleslash.WithRequestTimeout(d)`; default 10 s | The whole request, body included. |
| `hooks.afterEvaluation` | `toggleslash.WithAfterEvaluation(func(toggleslash.EvaluationEvent))`; default none | Called on the caller's goroutine before the evaluation call returns. A nil function is ignored. |
| `onError` | `toggleslash.WithOnError(func(error))`; default ignore | A nil function is ignored. |
| `accessor shape` | Both: the generic `Variation` and `VariationDetail` (kind from the fallback), and typed `Bool`, `String`, `Float64` and `Int64` variants of each. | `Int64Variation` is the integer accessor: contract 4's fractional and signed 64-bit rules apply to it, and only to it. `Float64Variation` is the `number` accessor with no integer rule. The `json` accessor is the generic method with an array or object fallback. |
| `onError thread` | The SDK's own goroutine for what that goroutine did (a failed ruleset or stream attempt on the sync goroutine, a failed usage request on the flush goroutine); the caller's goroutine for a hook that panicked and for `EVALUATION_FAILED`; the goroutine that called `Close` for a usage request in the final flush and for counts lost at close. | No SDK lock is held while it runs. A panic in it is recovered and ignored. |

**The closed sets keep their spelling.** `toggleslash.ReasonKind`, `toggleslash.ErrorCode` and
`toggleslash.StreamState` are string types whose constants carry the canonical spelling as their value
(`ReasonRuleMatch ReasonKind = "RULE_MATCH"`, `ErrNotReady ErrorCode = "NOT_READY"`, `StreamConnected
StreamState = "connected"`). `Reason` is a struct (`Kind`, `Code` for an `ERROR`, `RuleIndex` for
`RULE_MATCH`, `FlagKey` for `PREREQUISITE_FAILED`). `Status.LastError` is a plain string (`http_503`,
`timeout`, `bad_ruleset`, `network`) with the constants `LastErrorTimeout`, `LastErrorBadRuleset` and
`LastErrorNetwork`.

**"Not given" versus false or zero.** A functional option that was not passed is not in the list, so the
default stands; `WithStream(false)` is a passed `false`. A non-positive duration passed to an interval or the
timeout falls back to the default (contract 2, SHOULD).

**Contexts.** `toggleslash.Context` wraps a JSON value. It is built with `NewContext(key, attributes)`,
`NewKindContext(kind, key, attributes)`, `NewMultiContext(contexts...)`, `ContextFromJSON(data)` (invalid
JSON makes an invalid context) or `ContextFromValue(v)`. The zero `Context` is invalid. An invalid context is
never a panic: the caller gets its fallback with `ERROR INVALID_CONTEXT`.

**Hook values.** `EvaluationEvent.Value` is a `jsonvalue.Value`, which holds a number as a `float64`. A served
number is read as binary64 whatever the SDK does (the ruleset parser reads it so), so the nearest double of
an integer above 2^53 is the served number itself; `Int64Variation` still returns the exact `int64`.

## 2. Concurrency (contract 6)

- **The ruleset, its ETag and the status** live in one immutable `snapshot` struct (ruleset, ETag,
  `lastSyncAt`, stream state, `lastError`) behind one `atomic.Pointer[snapshot]`. A reader loads the pointer
  once, so an evaluation sees the old ruleset and ETag or the new pair, and `Status` is one consistent
  snapshot. The `etag` and `flagVersion` a hook reports come from the same load as the ruleset the evaluation
  used. Writers (the sync goroutine, `Close`) replace the pointer under one short `sync.Mutex`
  (`writeMu`) that covers a struct copy and a store and never an I/O call or a callback.
- **Evaluation takes no lock a request holds.** `Variation` reads the snapshot pointer, runs the evaluator
  (pure, no I/O), takes the usage mutex (`usageMu`, an in-memory map add held for a few instructions and never
  across a request: a flush swaps the map out under it and sends afterwards) and calls the hook. A hung
  server cannot slow it.
- **Goroutines.** `New` starts the sync goroutine (first load, then the stream and poll loop of 7.3) and the
  flush goroutine (the interval timer). Both are plain goroutines: they do not keep the process alive, so a
  client that is never closed does not stop `main` from returning (contract 6.6). Both watch a context that
  `Close` cancels.
- **Locks.** `writeMu` (snapshot writes), `usageMu` (the counts map), `flushMu` (flushes are serial; held
  during a flush's requests, never taken by an evaluation, and released before `onError` runs), `closeMu`
  (which `Close` call is first), and the hold times (`atomic.Int64`). None is held while `afterEvaluation` or
  `onError` runs, so a callback that calls `Status`, `Variation`, `Ready` or `Close` cannot deadlock.
- **`Close`.** The first call sets the stream state to `closed`, cancels the sync context (abandoning the
  ruleset and stream requests, which report nothing), waits for the sync goroutine, signals the flush
  goroutine to stop and waits for it (a flush already running finishes under its own per-request bound),
  then sends the final flush on the calling goroutine. Every other call waits on a `done` channel the first
  call closes. Usage requests run on a context `Close` does not cancel, so a count is not sent twice.
- **The SDK's own goroutine (6.4).** Go has no goroutine identity. `Ready` needs no mark: the first load
  settles it after it records its error and before it calls `onError`, so a callback that calls `Ready` never
  waits for itself (contract 5.3 allows that order). `Close` is the other call a callback may make: each
  goroutine the SDK starts marks itself with an atomic counter while it runs an application callback, and
  `Close` does not wait for a goroutine so marked (it is doing nothing more until the callback returns): it
  stops the rest and returns. The mark belongs to the SDK goroutine, not to the caller, so an application
  goroutine that calls `Close` while a callback is running also does not wait for that goroutine; the
  goroutine ends when the callback returns. A goroutine the callback itself starts is the application's and
  waits like any other caller on the work that is not marked.
- **Panics.** A panic from a hook, from `onError` or from the evaluator is recovered at the call site.
  The hook's and the evaluator's go to `onError` as a `*PanicError` carrying the panic value; `onError`'s own
  is dropped.
- **Race detector.** `bin/test` runs `go test -race`, which needs cgo and a C compiler: `mise.toml` pins `zig`
  and `bin/test` names it as `CC` by path.

## 3. HTTP and SSE

- **Client.** `net/http` with a cloned default transport that sets `DisableCompression` (no
  `Accept-Encoding`), and `CheckRedirect` returning `http.ErrUseLastResponse`, so a `3xx` is an `http_3xx`
  error and the `Authorization` header is never resent anywhere. A seam option replaces the `*http.Client`
  in tests.
- **`requestTimeout` covers the body.** A ruleset or events request runs under
  `context.WithTimeoutCause(parent, requestTimeout, errTimeout)`; the body is read to the end inside the
  same context before the cancel, so a body that never finishes is a `timeout`. The cause, not the error
  text, decides `timeout` versus `network` versus "caused by close".
- **Errors are fixed text.** A `*url.Error` carries the URL, so an error is never passed on: the SDK builds a
  `*SyncError` with a route name and one of `network`, `timeout`, `bad_ruleset`, `http_<n>`. No URL, header,
  key, context or response text is in one.
- **The stream.** The open is bounded by `requestTimeout` with a timer that cancels the request until the
  headers arrive (the timer is stopped when `Do` returns). After that a 60 s watchdog timer is reset after
  every `Read` that returns any bytes (keep-alives included); when it fires it cancels the request with
  `errTimeout` and the blocked `Read` returns. The watchdog length is a seam option. A `200` with a body that
  ends cleanly is not an error. The body is read in chunks and fed to the parser, so a chunk can end anywhere.
- **SSE parser (7.2).** Hand-written, byte-driven, to the WHATWG rules: a leading BOM is skipped, LF, CRLF and
  a lone CR end a line (a CR at the end of a chunk is remembered so that an LF starting the next chunk is
  swallowed), a line starting with `:` is a comment, a field has an optional single space after the colon,
  only `event` and `data` are read, several `data` lines join with LF, an event with no `data` line is
  dropped, the name is `message` when none was given, and an event unfinished at the end of the stream is
  discarded. UTF-8 is handled on whole lines, so a code point split across reads is not broken. There is no
  cap on a line or an event (7.2: "An SDK adds none of its own"); the watchdog is the bound.
- **Retry-After (7.4).** Read on a `429` only, accepted only as optional spaces, one or more ASCII digits and
  optional spaces, capped at 300 s (a seam option sets the cap for the cap test). Two holds (ruleset and
  stream together, and usage) are stored as a wall-clock deadline; a later `429` only extends it.

## 4. Dependencies

Runtime dependencies: none. The standard library has HTTP (`net/http`), SHA-256 (`crypto/sha256`) and
concurrency. It does not have a JSON reader that fits (below), so the SDK has its own, with no dependency.

Test dependencies: none (`testing`, `net/http/httptest`, `net`). Lint: `go vet`, `gofmt` and `golangci-lint`
(pinned in `mise.toml`, no import into the module). `zig` is pinned in `mise.toml` only as the C compiler the
race detector needs; it is not a dependency of the module.

## 5. JSON

`encoding/json` is not usable for the ruleset or for a context: it reports an error on `1e999` and replaces
a lone surrogate escape with U+FFFD, and the spec forbids both. So `jsonvalue` (a public package, because a
caller builds contexts from it) holds a hand-written JSON reader and writer:

- **Numbers** are `float64`. The reader checks the JSON number grammar itself and converts with
  `strconv.ParseFloat` on the checked text; an out-of-range result (`1e999`) is `+Inf`, not an error
  (`strconv` returns the infinity with `ErrRange`, which the reader ignores). `9007199254740993` reads as
  `9007199254740992`, `-0.0` keeps its sign, and `1.0` equals `1`.
- **Strings** are Go strings holding **WTF-8**: UTF-8 in which a lone surrogate (U+D800 to U+DFFF) is
  written as its three-byte form, which valid UTF-8 never contains. A `😀` escape pair is joined
  into the one code point; a lone half stays as a three-byte sequence and compares equal only to the same
  lone half. Raw (unescaped) bytes in the input are first decoded as the WHATWG UTF-8 decoder does, one
  U+FFFD per maximal invalid subpart (`internal/wtf8`), so the body is parsed and kept (contract 7.1).
  `wtf8` also decodes code points (for patterns), converts to UTF-16 units (for ordering and substring ops
  when a lone surrogate is involved) and encodes the bytes that are hashed (a lone surrogate becomes
  U+FFFD there only).
- **Objects** keep the last value of a duplicate key, in the position of the first. A leading byte order
  mark is skipped. Nesting deeper than 4096 levels is refused (`bad_ruleset` for a ruleset), over the 512
  the contract requires.
- **Contexts.** `Context` holds a `jsonvalue.Value`, so a context with `1e999` or a lone surrogate (the
  vectors' contexts) reaches the evaluator as it is. A Go value built by `NewContext` is converted by
  `jsonvalue.From`, which sanitises invalid UTF-8 in Go strings.

## 6. Minimum runtime and toolchain

- **Minimum runtime:** Go 1.26 (`go 1.26` in `go.mod`). It is the oldest release the Go team still supports
  (1.26 and 1.27 are, 1.27.4 being the current stable), so the SDK does not ask a user to run a release that
  gets no security fixes, and nothing newer is needed: the SDK uses generics, `atomic.Pointer`,
  `context.WithTimeoutCause` and the `min`/`max` builtins, all older.
- **Toolchain** (`mise.toml`): `go` 1.27.4, `golangci-lint` 2.15.0 and `zig` 0.17.1. `bin/` scripts run under
  `mise exec --` when mise is present.
- **Build and test:** the `go` tool and the standard `testing` package (the current mainstream, no
  dependency). **Lint and format:** `go vet`, `gofmt -l` (must print nothing) and `golangci-lint` with an
  explicit rule set in `.golangci.yml`.
- **Integer accessor.** `Int64Variation` uses `int64`, with contract 4's rules: a fractional part or a value
  outside signed 64 bits is `TYPE_MISMATCH`, and `2.0` is `2`. The test service advertises `integer-accessor`.

## 7. Package layout

```
sdks/go/
  go.mod  mise.toml  .golangci.yml  .gitignore  README.md  DESIGN.md  RUN.md
  toggleslash.go   package doc, New, Client, Ready, Close, Status
  options.go       functional options and the unexported test seams
  types.go         Detail, Reason, Status, EvaluationEvent, the closed sets, errors
  context.go       Context and its constructors
  evaluate.go      Variation and friends, the result rules of contract 4
  sync.go          the first load, the loop, the backoff, Retry-After
  stream.go        opening the stream, the watchdog
  sse.go           the SSE parser
  usage.go         counts, the flush
  *_test.go        native tests (package toggleslash), the loopback fake server
  jsonvalue/       the public JSON value, reader and writer
  internal/wtf8/   WTF-8 and UTF-16 helpers
  internal/evaluator/  the port of docs/evaluation.md (no network code); TestVectors, TestBucketPins
  test-service/    the conformance test service (package main), README.md
  bin/lint  bin/test  bin/test-service
```

`internal/evaluator` imports only `jsonvalue`, `internal/wtf8` and the standard library. `bin/test` builds the
test service last into `sdks/go/.build/test-service` (ignored), and `bin/test-service` runs that binary and builds it
only when it is missing.
