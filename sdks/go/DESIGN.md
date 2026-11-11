# Go SDK: design

This page is written before the code (PROMPT.md, phase 1) and kept true after it. The module is
`github.com/madangryscientist/toggleslash/sdks/go` and the package is `toggleslash` (`go` is a keyword, so
the package cannot carry the directory's name). Nothing is registered under that path.

## The mapping table

Contract section 2. Every name has its own row. `Client` is `*toggleslash.Client`.

| Name | In Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client`. It returns a client and no error: contract 5.1 forbids failing, so a bad URL makes every request a `network` error. It starts two goroutines and returns without waiting for the network. |
| `ready` | `(*Client).Ready() bool`. Blocks until the first load has settled (bounded by `requestTimeout`) and answers whether a ruleset is held. The answer is fixed when the first attempt settles. |
| `variation` | Typed methods `BoolVariation`, `StringVariation`, `IntVariation` (`int64`), `FloatVariation` (`float64`) and `JSONVariation` (`toggleslash.Value`), each `(flagKey string, ctx Context, fallback T) T`, plus the generic function `toggleslash.Variation[T](c *Client, flagKey string, ctx Context, fallback T) T`. |
| `variationDetail` | `BoolVariationDetail`, `StringVariationDetail`, `IntVariationDetail`, `FloatVariationDetail` and `JSONVariationDetail`, each returning `Detail[T]`, plus the generic function `toggleslash.VariationDetail[T]`. `Detail[T]` has `Value T`, `VariationIndex int` (`-1` is none), `Reason Reason`. |
| `status` | `(*Client).Status() Status`: `ETag string` (empty is none), `LastSyncAt time.Time` (the zero time is never), `Stream StreamState`, `LastError LastError` (empty is none). One immutable snapshot. |
| `close` | `(*Client).Close()`. Returns nothing, never fails, safe to call twice or concurrently. |
| `url` | The first parameter of `New`. Required. Trailing `/` characters are removed. |
| `sdkKey` | The second parameter of `New`. Required. |
| `stream` | `WithStream(bool)`. Default `true`. |
| `pollInterval` | `WithPollInterval(time.Duration)`. Default `30 * time.Second`. |
| `flushInterval` | `WithFlushInterval(time.Duration)`. Default `30 * time.Second`. |
| `requestTimeout` | `WithRequestTimeout(time.Duration)`. Default `10 * time.Second`. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(EvaluationEvent))`. Default none. |
| `onError` | `WithOnError(func(error))`. Default ignore. |
| `accessor shape` | Both. Typed methods for the five kinds (`Bool`, `String`, `Int`, `Float`, `JSON`), each with a `...Detail` twin, and the generic `Variation[T]` and `VariationDetail[T]` over the constraint `bool \| string \| int64 \| float64 \| Value`. The integer accessor takes and returns `int64`; there is no `int` form. |
| `onError thread` | A ruleset or stream failure: the sync goroutine the SDK started. A failed usage request: the flush goroutine, or the goroutine that called `Close` for the final flush. An evaluator panic or a hook panic: the goroutine that called the accessor. The counts lost at `Close`: the goroutine that called `Close`. No SDK lock is held for any of them. |

### Closed sets

Each keeps its canonical spelling as the value of a named string type, so `string(r.Kind)` is the canonical
text and a `switch` over the constants reads naturally.

- Reason kinds: `ReasonKind` with `ReasonOff = "OFF"`, `ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"`,
  `ReasonTargetMatch = "TARGET_MATCH"`, `ReasonRuleMatch = "RULE_MATCH"`, `ReasonFallthrough = "FALLTHROUGH"`
  and `ReasonError = "ERROR"`. `Reason` also carries `RuleIndex` (for `RULE_MATCH`), `FlagKey` (for
  `PREREQUISITE_FAILED`) and `Code` (for `ERROR`).
- Error codes: `ErrorCode` with `INVALID_CONTEXT`, `FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`, `MALFORMED_FLAG`,
  `MALFORMED_SEGMENT`, `PREREQUISITE_CYCLE`, `PREREQUISITE_DEPTH_EXCEEDED`, `NOT_READY`, `TYPE_MISMATCH` and
  `EVALUATION_FAILED`, each constant named `Err<Code in CamelCase>` (`ErrNotReady`) with the canonical text as
  its value.
- Stream states: `StreamState` with `StreamOff = "off"`, `StreamConnecting`, `StreamConnected`,
  `StreamDisconnected`, `StreamClosed`.
- `lastError` codes: `LastError` is a string type holding `timeout`, `bad_ruleset`, `network` or
  `http_<status>`; the first three are the constants `LastErrorTimeout`, `LastErrorBadRuleset`,
  `LastErrorNetwork`, and `http_<status>` is built by the SDK and compared as text.

### Options that are on or non-zero by default

`New` fills a `config` with the defaults first, then applies the options in order, so a missing option is the
default by construction. `WithStream(false)` writes `false` over a `true`. A duration option that is zero or
negative is ignored (contract 2: "SHOULD fall back to the default"), which is the default again.

### Context and values

The evaluator reads a context as a JSON value: the vectors' contexts carry `1e999` and lone surrogates, and
they reach the SDK through the test service, so the public API accepts a context in a form that keeps both.
`toggleslash.Value` is the SDK's own JSON value (a type alias of `internal/jsonvalue.Value`, with its methods
exported), `toggleslash.ParseValue([]byte) (Value, error)` reads JSON into it, `toggleslash.ValueFrom(any)`
converts Go data, and `toggleslash.Context` wraps a `Value`. `Reason`, `ReasonKind` and `ErrorCode` are type
aliases of the evaluator's types too, so the evaluator's result reaches the caller without a copy and a
`switch` over the public constants reads the same as one inside the package. The constructors are `NewContext(key string, attributes map[string]any)`
(a user context; the attribute map takes Go values: `nil`, `bool`, `string`, every integer and float type,
`[]any`, `map[string]any`, `[]string`, `Value`), `NewKindContext(kind, key string, attributes map[string]any)`,
`ContextFromValue(Value)` and `ParseContext([]byte) Context`. A multi context is built by `ContextFromValue` or
`ParseContext`. The zero `Context` is not valid, so it gets `ERROR INVALID_CONTEXT`.

An integer accessor returns `int64`. The hook event carries the served value as a `Value`, and the flag's
version as `FlagVersion *float64` (nil when the flag is not in the ruleset or did not load; a `float64` because
the ruleset's numbers are binary64, so a version above 2^53 is the nearest double). A `Value` holds a
binary64, so an integer above 2^53 reaches the hook as the nearest double (contract 10 allows this); the
caller of `IntVariation` still gets the exact `int64` when the served number is exact. (A number is read as
binary64, so the largest an `IntVariation` can ever serve is `2^63 - 1024`.)

## Concurrency

Contract section 6.

- **The held state is one immutable value behind one `atomic.Pointer`.** `state` holds the ruleset
  (`*evaluator.Ruleset`, itself immutable), its ETag, `lastSyncAt`, `lastError` and the stream state. Every
  change is copy, edit, store under `Client.mu`, a mutex that guards only that three-line sequence and is never
  held across I/O or a callback. A reader does `state.Load()` and sees a ruleset and the ETag that names it
  together (contract 6.3), and `Status()` is that same load (6.5), so it is one consistent snapshot.
- **Evaluation takes no lock a request holds.** `variation` does one atomic load, runs the evaluator on the
  immutable ruleset, then adds to the usage map under `usageMu`. `usageMu` guards an in-memory map add, a swap
  and a merge, and nothing else, so the flush swaps the map out before it sends and merges the unsent counts
  back after. The only lock held across a network call is `flushMu`, which serialises flushes (contract 9) and
  which evaluation never takes. Close sets its `closed` flag under `usageMu`, so a count either lands before the
  final flush swaps the map (and goes out) or is not counted at all.
- **Goroutines.** `New` starts the sync goroutine (the first load, then the loop of 7.3, the stream and the
  polls) and the flush goroutine (a clock timer every `flushInterval`). Both are plain goroutines, so a client
  that is never closed does not keep a process alive (6.6).
- **`Close`.** The first caller does the shutdown, in this order: settle `Ready` false if it has not settled,
  mark the client closed (counts stop, `Status().Stream` reads `closed`), cancel the root context (every ruleset
  and stream request in flight is abandoned, no new one starts), wait for the sync and flush goroutines, then run
  the final usage flush on the caller's own goroutine, then close idle connections. A second or concurrent
  `Close` waits for the same shutdown. A request's context carries a cause, so an abandoned request is told from
  a timeout and from a network failure, and an abandoned request sets no `lastError` and calls no `onError`.
  The wait is bounded: an in-flight flush request is bounded by `requestTimeout` and the final flush stops at its
  first failed request, so `Close` returns within about `2 * requestTimeout` plus the time the successful requests
  take (the bound this SDK keeps).
- **No SDK lock is held while `afterEvaluation` or `onError` runs.** `onError` calls are collected while
  `flushMu` is held and made after it is released. `Client.mu` and `usageMu` are never held across a call out.
  The evaluator panics are recovered and reported after the evaluation, outside every lock.
- **A callback that calls back into the client** (contract 6.4). Go has no goroutine identity, so a goroutine
  the SDK starts marks itself (a per-worker counter) while it runs an application callback. `Close` does not wait
  for a worker so marked: it is doing nothing more until the callback returns, so `Close` from `onError` on the
  sync or flush goroutine stops the rest and returns. The goroutine that is running `Close` marks itself the same
  way while it calls `onError`, so a second `Close` from that callback also returns. The marker is `task`
  (`task.go`): each worker has one, `task.callback(f)` runs an application callback with the worker marked
  busy, and `task.wait()` returns when the worker has finished or is inside a callback. A callback that starts
  (`woke` is closed and replaced) wakes a `Close` that was already waiting, so a callback that begins after
  `Close` started waiting is not waited on either. `Ready` settles before the
  first load's `onError` is called, so a `Ready` from that callback returns at once with the state held. A
  goroutine the application's callback starts is the application's, and its `Ready` and `Close` wait like any
  other caller's.

## HTTP and SSE

- **The client** is `net/http`, with an SDK-owned `http.Client` whose `CheckRedirect` returns
  `http.ErrUseLastResponse`, so a `3xx` is an `http_3xx` error and `Authorization` is never resent to another
  place. The default transport is a clone of `http.DefaultTransport` with `DisableCompression` set, so no
  `Accept-Encoding` is sent and the stream is never held back by a decompressor. `WithTransport` replaces it.
- **`requestTimeout` bounds a whole request, body included.** Each ruleset and usage request is made with
  `context.WithTimeoutCause(root, requestTimeout, errTimeout)`; the body is read before the cancel function
  runs, so the same deadline covers connect, headers and the body read (a server that sends headers and then
  stalls is `timeout`). The root context also carries `Close`'s cancel, so one `context.Cause` tells timeout
  from abandonment from a network failure.
- **The stream** is opened with a context whose cancel is armed by a `time.AfterFunc(requestTimeout)` until
  `Do` returns the response headers, which bounds only the open. After that a second `time.AfterFunc(60 s)` is
  the watchdog: it cancels the stream's context with a `timeout` cause, and every `Read` that returns bytes
  resets it (a keep-alive comment included). Cancelling the context aborts the pending `Read` on the real socket,
  so the timeout reaches the connection. The watchdog length is a test seam on the config
  (`watchdog`, default 60 s). The watchdog is paused while the SDK fetches the ruleset an event asked for.
- **A body that is not UTF-8** is decoded as the WHATWG decoder does, one U+FFFD per maximal invalid subpart
  (`internal/jsonvalue.DecodeLossy`, not `strings.ToValidUTF8` and not Go's per-byte conversion), then a leading
  byte order mark is skipped, then the text is parsed.
- **Server-Sent Events** are parsed by `internal/sse`, about 100 lines written to the WHATWG rules: a line ends
  at LF, CRLF or a lone CR (a CR at the end of a read waits for a possible LF by remembering it), a line starting
  with `:` is a comment, `field: value` drops one leading space, several `data` lines are joined with LF, an
  event with no `data` is dropped, the name is `message` when none was given, a leading byte order mark is
  skipped, and bytes are buffered across reads so a split UTF-8 sequence or a split CRLF is whole by the time it
  is read. An event incomplete at the end of the stream is discarded. There is no cap on a line or an event.
- **Headers.** The SDK sets `Authorization`, `If-None-Match` (only when a ruleset and a non-empty ETag are held),
  `Accept: text/event-stream` (stream only) and `Content-Type: application/json` (events only). `net/http` adds
  `User-Agent`, `Host` and, for the events `POST`, `Content-Length`; `RUN.md` has the audit.

## Dependencies

Runtime: none. The module imports the standard library only (`net/http`, `crypto/sha256`, `sync`,
`sync/atomic`, `context`, `time`, `math/rand/v2`, `strconv`, `unicode/utf8`, `unicode/utf16`).

Test and lint: the standard `testing` package (run by `go test`, with `-race`), `net/http/httptest` for the
fake servers, `golangci-lint` for lint, and `zig cc` as the C compiler `-race` needs (see the toolchain). None
is imported by the SDK.

## JSON

`encoding/json` cannot be used: it reports an error for `1e999` and replaces a lone surrogate with U+FFFD, and
the spec requires both to survive (evaluation.md, "Reading JSON" and "Bucketing"). `internal/jsonvalue` is a
strict RFC 8259 reader and writer of its own.

- **Numbers** are `float64`, read with `strconv.ParseFloat` after the grammar is checked by hand (no leading
  zero, no `+`, no bare `.`). An overflow (`1e999`) is `+Inf`, not an error; `9007199254740993` reads as
  `9007199254740992`; `-0.0` is a negative zero and equals `0` by IEEE comparison.
- **Strings** are held as WTF-8: ordinary UTF-8, except that a lone surrogate (JSON `"\ud800"`) is its
  three-byte encoding. An escaped pair (`😀`) is combined into the one four-byte code point, so
  the encoding is canonical and byte equality is code unit equality. A Go `string` holds this without any change
  to the type. Raw invalid UTF-8 in a body never gets here: the decoder above has already replaced it.
- **Operations that differ between WTF-8 bytes and UTF-16 code units** are written over code units where they
  would differ: ordering (`lt`, `gt`) and the three substring operations take a byte fast path only when
  neither string has a byte at or above `0xED`, and otherwise walk UTF-16 code units, so `"😀"` sorts before
  `"～"` and a lone high surrogate is found inside a pair. `matches` walks code points. Hashing replaces each
  lone surrogate with U+FFFD, as the spec says.
- **Objects** keep the last value of a duplicate key. **Depth** is limited to 4096 levels (contract 7.1 allows
  a limit of at least 512); a body deeper than that is `bad_ruleset`.
- **The writer** (used by the test service) writes a lone surrogate back as a `\u` escape, writes a non-finite
  number as `null` and a negative zero as `0`, as `JSON.stringify` does.

## The minimum runtime, and the toolchain

- **Minimum: Go 1.26.** Go supports the two newest major releases; the newest is 1.27 (released August 2026), so
  1.26 is the oldest in support on the run date, and 1.25 is not. The SDK needs `context.WithTimeoutCause`
  (1.21), `atomic.Pointer` (1.19), `math/rand/v2` (1.22), ranging over an integer (1.22) and the timer
  semantics of 1.23, all of which 1.26 has. `go.mod` says `go 1.26`.
- **Development toolchain: Go 1.27.4**, the newest stable release (go.dev/dl and `mise ls-remote go` agree),
  pinned in `mise.toml`. Go has no long-term-support line, so the newest release is the pin.
- **Lint and format: `golangci-lint` 2.15.0** (the mainstream aggregator; the config `.golangci.yml` names its
  linters and gives a reason beside every one it leaves out) plus `go vet` and `gofmt -s -l`, all read only.
- **Test framework: the standard `testing` package under `go test -race`.** `-race` needs cgo and so a C
  compiler, and the CI runner is not assumed to have one, so `mise.toml` pins **zig 0.17.1** and `bin/test` sets
  `CC` to `<zig>/zig cc` by path (a `zig` elsewhere on `PATH` could shadow the pinned one).
- **Build tool: the `go` command.** `bin/test` builds everything (`go build ./...`, `go vet ./...`), runs the
  tests, then builds the test service into `.build/test-service`, which `bin/test-service` runs.
- **The integer accessor** uses `int64`.

## The package layout

```
sdks/go/
  go.mod                         module github.com/madangryscientist/toggleslash/sdks/go
  mise.toml                      go, golangci-lint, zig
  .golangci.yml                  the lint rule set
  toggleslash.go                 package doc, Client, New, Ready, Close
  options.go                     config, Option and the With... options
  types.go                       Reason, ErrorCode, Status, Detail, EvaluationEvent
  context.go                     Context, Value, the constructors
  evaluate.go                    the accessors and the result rules of contract 4
  task.go                        the worker marker that lets Close skip a goroutine inside a callback
  sync.go                        the first load, the loop, the stream, the backoff and holds
  usage.go                       the counts, the flush and the day
  clock.go                       the clock seam and the real clock
  *_test.go                      the SDK's native tests (package toggleslash)
  internal/jsonvalue/            the JSON value, reader, writer, WHATWG UTF-8 decoder
  internal/evaluator/            docs/evaluation.md: ruleset, context, clauses, pattern, semver, date, bucket
  internal/sse/                  the Server-Sent Events parser
  test-service/                  the conformance test service (package main, compiled with the module)
  bin/lint, bin/test, bin/test-service
  DESIGN.md, README.md, RUN.md
```

The evaluator is `internal/evaluator`, with no network code and no import of `net/http`. Its tests include
`TestVectors` and `TestBucketPins`, which read `fixtures/evaluation/` by the relative path `../../../../fixtures/evaluation`.
The client is the root package. The test service is `test-service/main.go`, part of the same module, so
`go build ./...` and `go vet ./...` compile it with the tests.

## Disagreements with the reference implementations

None found between `docs/evaluation.md`, `docs/sdk-contract.md` and the fixtures while porting. Two places where
this port picked one of two readings the text allows:

- A carriage return at the very end of a read waits for a possible line feed (`internal/sse/sse.go`), so an event
  ended by a lone CR is dispatched when the next bytes arrive, not at once. That is what the WHATWG rules require, since a CR may begin a CRLF.
- `ValueFrom(math.NaN())` is accepted and held as a number (`internal/jsonvalue/fromgo.go`); the writer emits a
  non-finite number as `null`, as `JSON.stringify` does. Go data cannot arrive from JSON as NaN, so only a caller
  who builds an attribute from a computed float can meet it.
