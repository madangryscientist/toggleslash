# Go SDK: design

This is the design of the ToggleSlash server SDK for Go, written before the code, to
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) (the contract) and
[`docs/evaluation.md`](../../docs/evaluation.md) (the spec). It is built from
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md); the run report is [`RUN.md`](RUN.md).

Inputs: language Go. Runtime, minimum version and build tool were not given, so each is a decision (see
"The minimum runtime and the toolchain").

## The mapping table

The module is `github.com/madangryscientist/toggleslash/sdks/go` and its one public package is named
`toggleslash` (`go` is a keyword). Everything below is in package `toggleslash` unless it says otherwise.
Durations are `time.Duration`.

| Name | In Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client`. It starts two goroutines and returns without waiting for the network. It never panics and never returns an error: a URL that cannot be used shows up as a `network` failure in `Status` and `onError`, like any other unreachable server. |
| `ready` | `(*Client).Ready() bool`. Blocks the calling goroutine until the first load has finished (bounded by `requestTimeout`), then answers whether a ruleset is held. Any number of calls from any goroutine; the answer is fixed when the first attempt settles; after `Close` it answers `false` if nothing had loaded. |
| `variation` | Typed methods, no generic method: `BoolVariation(flagKey, ctx, fallback bool) bool`, `StringVariation(...) string`, `IntVariation(...) int64`, `Float64Variation(...) float64` and `JSONVariation(flagKey, ctx, fallback Value) Value`. A `Null()` fallback to `JSONVariation` accepts any served value. |
| `variationDetail` | One `...Detail` twin per typed method: `BoolVariationDetail`, `StringVariationDetail`, `IntVariationDetail`, `Float64VariationDetail` and `JSONVariationDetail`, each returning `Detail[T]`, a struct of `Value T`, `VariationIndex int` (`NoVariation`, which is `-1`, when the value is the fallback) and `Reason Reason`. |
| `status` | `(*Client).Status() Status`, a struct of `ETag *string`, `LastSyncAt time.Time` (the zero time means never), `Stream StreamState` and `LastError SyncErrorCode` (`""` means none). One consistent snapshot, read from memory under one small lock. |
| `close` | `(*Client).Close()`. Idempotent and safe from any goroutine and from a callback; every call returns when the same shutdown is done. |
| `url` | the first argument of `New`. Trailing `/` characters are removed; nothing else is validated. |
| `sdkKey` | the second argument of `New`. |
| `stream` | `WithStream(bool)`. The default is on. A functional option is only applied when given, so "not given" and `false` differ with no sentinel. |
| `pollInterval` | `WithPollInterval(d)`; default 30 s; a zero or negative `d` is the default. |
| `flushInterval` | `WithFlushInterval(d)`; default 30 s; a zero or negative `d` is the default. |
| `requestTimeout` | `WithRequestTimeout(d)`; default 10 s; a zero or negative `d` is the default. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(EvaluationEvent))`. A nil function is ignored. The event holds `FlagKey`, `Context`, `Value`, `VariationIndex`, `Reason`, `ETag *string` and `FlagVersion *float64`. |
| `onError` | `WithOnError(func(error))`; default none (ignore). The errors the SDK builds are `*toggleslash.Error` values with an `Op` and a `Code`; a hook's panic arrives as an `*Error` that wraps what it panicked with. |
| `accessor shape` | Typed methods only, because a Go method cannot take type parameters and a generic function (`toggleslash.Variation[T](c, ...)`) would add a second spelling of the same five. The accessors are `bool`, `string`, `int` (an `int64`), `float64` and `json` (a `Value`, whose `Null()` fallback is the `any` accessor). Each has a `Detail` twin. A typed method needs the served value to be of its own kind, whatever the caller's fallback is. `IntVariation` is the integer accessor (below). |
| `onError thread` | The goroutine that met the failure: the SDK's sync goroutine for a failed ruleset or stream attempt, its flush goroutine for a failed usage request, the goroutine that called `Close` for the final flush and for the counts lost at close, and the caller's own goroutine for an evaluation that panicked and for a hook that panicked. No SDK lock is held during the call. |

**The closed sets** keep their spelling as string types with constants, so `toggleslash.ReasonOff` is the
string `"OFF"` and it prints and marshals as the canonical text: `ReasonKind` (`OFF`,
`PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`), `ErrorCode` (the
evaluator's seven and the SDK's `NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`) and `StreamState`
(`off`, `connecting`, `connected`, `disconnected`, `closed`). `Reason` is a struct `(Kind, RuleIndex,
FlagKey, Code)`; `RuleIndex` means something only for `RULE_MATCH`, `FlagKey` only for
`PREREQUISITE_FAILED` and `Code` only for `ERROR`. `SyncErrorCode` is a string type holding
`http_<status>`, `timeout`, `bad_ruleset` or `network`; the constants `ErrTimeout`, `ErrBadRuleset` and
`ErrNetwork` hold the three fixed ones.

**An option whose default is on or non-zero.** The options are functional options applied over a struct
that starts with the defaults, so an option that is not passed keeps its default and `WithStream(false)`
is `false`. A duration that is zero or negative is replaced by the default, as contract section 2
suggests.

**The integer accessor.** `IntVariation` returns an `int64`. A served number that has a fractional part,
is not finite, or is outside the signed 64-bit range is `ERROR TYPE_MISMATCH` and the caller gets the
fallback; `2.0` is the integer `2`. The test service advertises the `integer-accessor` capability.

## Concurrency

Go runs goroutines on several cores, so everything is written for the race detector, and `bin/test`
runs the tests with `-race`.

- **The held ruleset.** One immutable `snapshot{ruleset, etag}` held in an `atomic.Pointer[snapshot]`.
  Evaluation loads the pointer once into a local and uses only that, so a hook's `ETag` and
  `FlagVersion` always belong to the ruleset the evaluation used. A load builds a new `snapshot` and
  stores it: one pointer store, which a reader sees whole or not at all. `variation` takes no lock to read it.
- **Locks.** Four, each held for a handful of in-memory operations and never across I/O or a callback:
  1. `statusMu` guards `lastSyncAt`, `lastError`, the stream state and the "closed" flag, and the store
     of the new snapshot (so `Status` sees the ETag, the sync time and the error from one moment).
     `variation` never takes it. `Status` takes it for a few field reads.
  2. `usageMu` guards the pending-count map and the "recording stopped" flag. `variation` takes it for one
     map add. A flush takes it only to swap the map out and to merge kept counts back in.
  3. `flushMu` makes flushes serial and guards the `Retry-After` hold of the events route. Only the flush
     goroutine, a `Flush` call and `Close` take it, and a flush holds it across its requests; evaluation
     never touches it.
  4. the one-shot `closing` flag is an `atomic.Bool`, not a lock: the first `Close` to flip it shuts down
     and every other call waits on the `done` channel.
  An evaluation takes only `usageMu`, which a request never holds, so a hung server or a hung flush
  cannot slow `variation` down.
- **Callbacks.** `afterEvaluation` and `onError` are called with no lock held. Code that meets an error
  collects it, releases its locks and then reports it, so a callback may call `Status`, `variation` and
  even `Close` on the same client without a deadlock. `Close` is the one place that needs care: a call
  from inside `onError` on the sync goroutine would wait for that goroutine to finish. Each loop goroutine
  counts the callbacks it is inside, and `Close` does not wait for a loop that is inside one (the loop
  has already been told to stop and starts nothing more). The errors `Close` itself collects during the
  final flush are delivered after the `done` channel is closed, so a `Close` made from that callback
  returns at once.
- **Goroutines.** Two, started by `New`: the sync goroutine (the first load, then the stream and poll
  loop) and the flush goroutine. Neither keeps a process alive: a Go process exits when `main`
  returns, whatever goroutines are running. Both stop on `Close`.
- **Waits.** Every wait is `select` on a timer (through the clock seam) and a context's `Done`, so
  `Close` cancelling the context wakes the sync loop, the flush loop and a `Retry-After` hold at once.
- **Close.** `Close` flips `closing`, sets the stream state to `closed`, stops recording usage counts,
  cancels the sync context (which aborts a ruleset request or stream in flight) and the flush context,
  waits for both goroutines, settles `Ready` (false when nothing loaded), sends the final flush under
  `flushMu`, closes `done`, and then delivers the errors it collected. A usage request already in flight
  in the flush goroutine is left to finish, bounded by `requestTimeout`, as the contract allows.
- **The tests the harness cannot run** are in `*_test.go` and run under `-race`: many goroutines calling
  every operation, `Close` racing `variation` and itself, a ruleset request and a usage flush that
  hang, swaps under readers (a hook's `ETag` against its `FlagVersion`), and callbacks that call back
  into the client.

## HTTP and SSE

**Client.** `net/http`. One `*http.Client` per SDK client, with a `Transport` cloned from
`http.DefaultTransport` (so proxy environment variables and HTTP/2 behave as a Go program expects) and
no `Client.Timeout`. A test seam can replace the `*http.Client`. The headers the SDK sets are exactly
`Authorization`, `Accept` (stream only), `If-None-Match` (ruleset only, when a ruleset and an ETag are
held) and `Content-Type` (events only); `net/http` adds `Host`, `User-Agent`, `Accept-Encoding: gzip`
(and decodes it) and `Content-Length`.

**The whole-request bound (7.1).** A ruleset request and a usage request each get a
`context.WithTimeoutCause(parent, requestTimeout, errTimeout)`, passed to `http.NewRequestWithContext`
and held until the body has been read. `net/http` ends a body read when the request's context ends, so
a server that sends headers and then trickles or stalls the body is a `timeout`, and the connect, the
headers and the body share the one limit. The parent is the client's sync context, so `Close` aborts a
ruleset or stream request at once (and a request aborted that way is never reported).

**The stream (7.2).** A `context.WithCancelCause` per stream attempt. A `time.AfterFunc` of
`requestTimeout` cancels it with `errTimeout` and is stopped when the headers arrive: that bounds the
open and nothing after it. Then the read loop owns a second `time.AfterFunc`, the watchdog (60 s,
a seam for tests), which is `Reset` after every `Read` that returns bytes, a keep-alive comment included,
and cancels the attempt with `errIdle` if it fires. Cancelling makes the blocked `Read` return, and the
cause says `timeout`. A status other than 200 is `http_<status>` (a 429 is also read for
`Retry-After`). A body that ends cleanly is not an error. A body that is empty is simply a stream that
ends with no `connected`, which counts as never connected.

**Server-Sent Events.** A hand-written parser in `sse.go`, about eighty lines, to the WHATWG rules the
contract names. It works on bytes, which is enough: a UTF-8 sequence never contains a CR or LF byte, so
a character split across reads survives because a line is only decoded when it is complete. LF, CRLF
and a lone CR end a line, and a CR at the end of one read is remembered so the LF that starts the next
read is not a second line end; a line that starts with `:` is a comment; a field with no `:` has an
empty value; one leading space of a value is dropped; a leading BOM is dropped; several `data` lines are
joined with LF; an event with no `data` line is dropped; the event name is `message` when none was given;
and an event incomplete when the stream ends is never delivered. Only `event` and `data` are read. No SSE
library is used.

## Dependencies

Runtime: none. The standard library has HTTP (`net/http`), SHA-256 (`crypto/sha256`), time and
synchronisation. The JSON reader is hand-written (see "JSON") because `encoding/json` cannot hold what
the spec requires.

Test and lint: the standard `testing` package for the tests (no third-party assertion library), the
race detector (`go test -race`, which needs cgo and a C compiler), and `golangci-lint` (pinned in
`mise.toml`) together with `gofmt` and `go vet` for lint and format. The test service uses `net/http`.

## JSON

`encoding/json` cannot be the parser, for two reasons the spec pins: it returns an error for `1e999`
instead of reading Infinity, and it turns a lone surrogate (`"\ud800"`) into U+FFFD. So the SDK has its
own reader and its own value type, in `internal/jsonvalue`, exposed in the public API as `toggleslash.Value`
(a type alias) with constructors (`Null`, `Bool`, `Number`, `String`, `Array`, `Object`), accessors,
`ParseJSON`, `MarshalJSON` and `AppendJSON`.

- **Numbers** are `float64`, read with `strconv.ParseFloat` after a grammar check of RFC 8259, and a range
  error is accepted as the infinity it returns: `1e999` is `+Inf`, `9007199254740993` is
  `9007199254740992`, `-0.0` is kept as negative zero. Writing a value back out spells `+Inf` as `1e999`,
  `-Inf` as `-1e999` and negative zero as `-0`.
- **Strings** are Go strings holding WTF-8: the UTF-8 encoding in which a lone surrogate is the three bytes
  that UTF-8 forbids for it (`ED A0..BF xx`). A valid escaped pair (`😀`) is combined into the real
  four-byte sequence, so one string has one byte form, which makes byte equality the same as UTF-16
  code unit equality. Raw bytes that are not valid UTF-8 in the input are read as U+FFFD, as `Response.json()`
  does in the TypeScript SDK. Writing a value back out escapes a lone surrogate as `\udXXX`.
- **Objects** keep their members in order with an index, and a duplicate key keeps the last value, as
  `JSON.parse` does. Nesting deeper than 1000 levels is an error (so a ruleset that deep is `bad_ruleset`).
- **Contexts.** The public `Context` wraps a `Value`, so it can hold `1e999` and lone surrogates. It is built
  from typed builders (`UserContext`, `NewContext`, `MultiContext`, with `WithAttribute`) or from raw
  JSON with `ContextFromJSON(raw []byte) Context`, which never fails: text that is not JSON gives a context
  the evaluator answers with `INVALID_CONTEXT`, like any other invalid one.

**Strings in the evaluator.** The spec compares by UTF-16 code unit and matches patterns by code point.
Equality and the substring ops are byte operations on canonical WTF-8 except when the needle contains a
lone surrogate, which could match half of a pair in the haystack: then they run over UTF-16 units. Order
compares the first differing code point by its UTF-16 form (a high surrogate for anything above U+FFFF).
Patterns read both the pattern and the candidate as code points, a lone surrogate being the code point
with that value. Hashing replaces a lone surrogate with U+FFFD, then hashes the UTF-8.

## The minimum runtime and the toolchain

- **Runtime.** The `gc` toolchain, the current mainstream one (`gccgo` and TinyGo are not tested).
- **Minimum version: Go 1.26.** Go supports the two most recent releases, and with 1.27 current that is
  1.27 and 1.26; the SDK uses nothing newer than 1.26 (it needs `atomic.Pointer`, `context.WithTimeoutCause`,
  the `min` and `max` built-ins and `for range n`, all older). Declared as `go 1.26` in `go.mod`.
- **Developed and tested on** the version `mise.toml` pins (Go 1.27.3). CI runs that one only.
- **Build tool.** `go build` and modules, no wrapper: the mainstream default and enough for a library
  with no dependencies.
- **Lint and format: `golangci-lint` v2** (pinned by mise), with `gofmt` and `go vet`: the mainstream
  aggregator, running `vet`, `staticcheck` and `errcheck` among its defaults, plus a short list of extra
  linters in `.golangci.yml`.
- **Tests: the standard `testing` package** with table tests and subtests; the vector test reads
  `fixtures/` at test time. Test seams (a clock, a jitter source, the watchdog length, an `*http.Client`, an
  evaluator that panics) are unexported options of `New`, used by tests inside the package.
- **Build output.** `bin/test` builds the test service into `.build/` (ignored by git) every time it runs.
- **The race detector** is on in `bin/test`.
- **The integer accessor** is `IntVariation`, an `int64`, bounded to the signed 64-bit range.

## The package layout

```
sdks/go/
  go.mod                  module github.com/madangryscientist/toggleslash/sdks/go, go 1.26, no requires
  mise.toml               go and golangci-lint pins
  .golangci.yml           the lint configuration
  bin/lint  bin/test  bin/test-service
  toggleslash.go          package documentation, New and the options
  client.go               Client, the typed accessors and the result builder
  context.go  types.go    Context, Reason, Detail, Status, Error and the closed sets
  sync.go  sse.go         the ruleset sync, the stream, the loop and the SSE parser
  usage.go                usage counts and the flush
  *_test.go               the native tests (package toggleslash)
  internal/wtf8/          strings as WTF-8: lone surrogates kept, UTF-16 code unit order and length
  internal/jsonvalue/     the JSON value type, reader and writer (no dependencies)
  internal/eval/          the evaluator: context, ruleset loading, clauses, semver, dates, the pattern
                          matcher, bucketing; no network code. vectors_test.go runs fixtures/evaluation
  test-service/           main.go (package main, the conformance test service) and README.md
```
