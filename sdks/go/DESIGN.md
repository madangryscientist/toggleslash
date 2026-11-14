# Go SDK design

Written in phase 1, before any code. It answers `PROMPT.md` "Phase 1: design" items 1 to 7 against
`docs/sdk-contract.md` sections 2, 4, 6, 7.1, 7.2, 8, 10 and 12, `docs/sdk.md`'s mapping table and
`docs/evaluation.md` "Reading JSON". Where the contract and this file disagree, the contract wins.

## 1. The mapping table

The module root is package `toggleslash` (Go's `go` is a keyword, so the package takes the product name).
`Context`, `Event`, `Detail`, `Status` and the closed sets are all types of that package.

| Name | Go | Notes |
| --- | --- | --- |
| `create` | `New(opts Options) (*Client, error)` | Builds the client and starts its goroutines. It waits for no network. The only error it returns is a missing `URL` or `SDKKey`. |
| `ready` | `(*Client) Ready(ctx context.Context) bool` | Waits for the first load to end. `ctx` bounds the wait only; the load runs on. A malformed or absent ruleset is `false`. |
| `variation` | `(*Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool`, and the siblings `StringVariation`, `IntVariation`, `FloatVariation`, `JSONVariation` | One method per JSON kind, the fallback fixing the accessor's kind. `IntVariation` is the integer accessor and takes and returns `int64`. No error return: a failure is a reason on the Detail form and an `onError` call, and the caller gets the fallback (contract 4). |
| `variationDetail` | `(*Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool]`, and the sibling `StringVariationDetail`, `IntVariationDetail`, `FloatVariationDetail`, `JSONVariationDetail` | `Detail[T]` is `{ Value T; VariationIndex *int; Reason Reason }`; a nil index is "none". |
| `status` | `(*Client) Status() Status` | Reads memory only, no request, never fails (contract 8). |
| `close` | `(*Client) Close()` | Stops every loop and sends the pending counts. Idempotent; two racing calls are safe. |
| `url` | `Options.URL string` | Required. Trailing `/` characters are trimmed once at `New`. |
| `sdkKey` | `Options.SDKKey string` | Required. Never logged, never in `Status`, never in an error text. |
| `stream` | `Options.Stream *bool` | Nil means not given, which is on. `false` polls only. A pointer, because `false` is a real value and the zero `bool` cannot say "not given". |
| `pollInterval` | `Options.PollInterval time.Duration` | Zero means not given, which is 30 s. It is also the longest reconnect wait. Negative also falls back to the default (contract 2, last paragraph: not specified, not tested). |
| `flushInterval` | `Options.FlushInterval time.Duration` | Zero means not given, which is 30 s. |
| `requestTimeout` | `Options.RequestTimeout time.Duration` | Zero means not given, which is 10 s. Bounds a whole request, body included (section 3). |
| `hooks.afterEvaluation` | `Options.Hooks.AfterEvaluation func(Event)` | A nil field means none. Called on the caller's goroutine, synchronously, before the call returns. |
| `onError` | `Options.OnError func(error)` | A nil field means ignore. A `func(error)`, because contract 10 says the language's own error value. |
| `accessor shape` | Typed methods: the five accessors above, each with a `...Detail` sibling. Not one generic method. | Go's type parameters cannot select the JSON kind comparison from the fallback's static type, and contract 4 makes that comparison per accessor. `JSONVariation` takes a `jsonval.Value` fallback, which holds every kind, so it accepts any served value only when that fallback is null. |
| `onError thread` | The client's own notify goroutine: never the caller's goroutine, never the sync or flush goroutine. | The one failure that is delivered on the caller's goroutine is none: even an evaluation or hook failure is queued (section 2). It is for logging and metrics (contract 10), so it does not need to be synchronous, and routing it through one goroutine is what makes `Close` from inside `onError` safe. |

**The closed sets.** Each is a named string type with constants whose value is the contract's spelling
exactly, so `fmt` and a test's string comparison see `TARGET_MATCH`, not `TargetMatch`:

- `ReasonKind` (`Reason`) with `ReasonOff = "OFF"`, `ReasonPrerequisiteFailed`, `ReasonTargetMatch`,
  `ReasonRuleMatch`, `ReasonFallthrough`, `ReasonError = "ERROR"`. `Reason` is the struct
  `{ Kind ReasonKind; RuleIndex *int; FlagKey string; Code ErrorCode }`; `RuleIndex` is nil unless `Kind`
  is `ReasonRuleMatch`, `FlagKey` names the flag that failed (`ReasonPrerequisiteFailed` names the
  prerequisite), and `Code` is empty unless `Kind` is `ReasonError`.
- `ErrorCode`: the evaluator's `INVALID_CONTEXT`, `FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`,
  `MALFORMED_FLAG`, `MALFORMED_SEGMENT`, `PREREQUISITE_CYCLE`, `PREREQUISITE_DEPTH_EXCEEDED`, and the
  SDK's own `NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`.
- `StreamState`: `StreamOff = "off"`, `StreamConnecting = "connecting"`, `StreamConnected = "connected"`,
  `StreamDisconnected = "disconnected"`, `StreamClosed = "closed"`.
- `LastError` (a string): the constants are the fixed codes `timeout`, `bad_ruleset` and `network`, and
  `HTTPLastError(status int) LastError` builds `http_<status>`, because that code is open ended. `Status`
  carries `LastError *LastError` and nil is "none".

**"Not given" versus false or zero.** Only three options can mean two things in their zero value, and each
says so:

- `Stream *bool`: nil is not given (on), `false` is off.
- The three durations are `time.Duration`, whose zero is not a valid interval, so zero means not given and
  the default applies. `New` replaces a zero or negative duration with the default before anything starts.
- Everything else is a value whose zero is exactly what the caller asked for: `URL` and `SDKKey` are
  required, `Hooks.AfterEvaluation` nil is no hook, `OnError` nil is ignore.

`Options` is a plain struct, not functional options: the harness and the test service fill it in one
literal, and a struct makes "not given" visible and testable.

## 2. Concurrency

Go has goroutines and threads, so contract 6's first list applies to every line.

**Held state, no lock on the hot path.** The ruleset and its ETag live in one immutable snapshot,
`atomic.Pointer[snapshot]` where `snapshot` is `{ ruleset *ruleset; etag string }`. A successful load
builds a whole new snapshot and one `Store` swaps ruleset and ETag together, so a reader sees the old pair
or the new pair and never a mix, and the `etag` a hook reports is the one the evaluation used (6.3).
`Status` is the same shape: one immutable `statusSnapshot` behind `atomic.Pointer`, written whole by the
goroutine that made the change, so a read is one consistent snapshot and a stream failure sets `lastError`
and `stream` in one store (6.5). Two further fields are plain atomics: the client's `closed` flag and the
last error the loop is about to publish.

**Locks that exist, and why an evaluation never waits on one.** There are three, none of them held across
I/O, a channel send, a timer or a callback:

1. `usageMu sync.Mutex` around the in-memory usage counts. `variation` takes it for one map increment
   after the value is decided and releases it before the hook runs. The flush goroutine takes it to swap
   the map for a fresh one and releases it before the request goes out. No request ever holds it.
2. `queueMu sync.Mutex` plus a `sync.Cond` around the notify queue. Only the producer (whoever met the
   failure) and the notify goroutine take it, and only to move an error into or out of the queue. The
   callback itself runs after it is released, so nothing is held while `onError` runs.
3. `closeOnce sync.Once` and a `wg sync.WaitGroup`. Neither is touched by an evaluation.

An evaluation therefore takes one short mutex (the counts) and one atomic load. It makes no request, takes
no lock a request holds, and waits for no network.

**Goroutines the SDK starts.** Three, all started by `New`:

- the **sync** goroutine: the first ruleset request, the poll timer, opening the stream, the stream read
  loop, and the ruleset fetch a `connected` or `changed` event causes. One goroutine owns the sync state
  machine, so a fetch and a stream attempt cannot overtake each other.
- the **flush** goroutine: the usage timer, each send, and the final send at close.
- the **notify** goroutine: the only caller of `onError`.

**How `Close` stops and joins them.** `Close` is `closeOnce`: it sets `closed`, cancels the client's
context, and then waits (`wg.Wait`) for the sync goroutine and the flush goroutine. The sync goroutine
returns on cancellation, closing the stream body; the flush goroutine does one last send with the counts
it holds, without the canceled context as its bound, and returns. Both publish `StreamClosed` before they
go, and a request aborted by close sets no `lastError` and calls no `onError` (contract 8). The notify
goroutine is told the queue is finished and is **not** joined: it is the only goroutine a user callback
runs on, so a `close` called from inside `onError` cannot wait for the goroutine that is running it
(6.4). A `close` called from inside `afterEvaluation` waits for sync and flush, which are not the caller's
goroutine, so that is safe too.

Callbacks hold no SDK lock: `afterEvaluation` runs after the counts lock is released, and `onError` runs
on the notify goroutine with nothing held. A `ready` called from inside a callback returns the state held
at once, because the callback is never on the sync goroutine. A callback that blocks blocks only the
goroutine it runs on (the caller for a hook, the notify goroutine for an error), which contract 10 allows.

**A client never closed** does not keep a process alive: Go ends a program when `main` returns, whatever
goroutines are still running, so 6.4 is met without a daemon flag.

## 3. HTTP and SSE

**The client.** One `*http.Client` per `*Client` value (not per request), with the default transport
cloned and `MaxIdleConnsPerHost` raised, and with `Timeout` left at zero: the bounds are per request and
per attempt, below. The key travels in the header the admin API defines, added on every request. No
`Accept` header on the ruleset request; `Accept: text/event-stream` on the stream; `Content-Type:
application/json` on events. Nothing identifies a user or a machine beyond what `net/http` adds itself
(contract 12).

**`requestTimeout` bounds the whole request, body included.** Each ruleset and events request is made
with `context.WithTimeout(ctx, requestTimeout)` and its body is read to the end inside that deadline. The
deadline is live while the body is read, because Go's transport aborts a body read when the request's
context is done, so a server that sends headers and then stalls is a `timeout` and not a hang. Nothing
cancels the read early: the body is read to EOF, then closed. A deadline that fires is `timeout`; a
connection error is `network`; a status other than the expected one is `http_<status>`, with `429` read
for `Retry-After` first (7.4, read in phase 3); `304` with no ruleset held is `http_304`.

**The stream's two bounds.** The open is bounded by `requestTimeout` and the reads after it are not:

1. Open: `ctx, cancel := context.WithCancel(parent)` and a `time.AfterFunc(requestTimeout, cancel)`. The
   request is sent with that context, so connecting and waiting for the response headers are bounded.
   Once `Do` returns the headers, the timer is stopped. A stopped timer leaves no deadline on the body,
   which is exactly 7.2's rule: only the open is bounded by `requestTimeout`.
2. Read: a 60 s watchdog on the same `cancel`, as `time.AfterFunc(60*time.Second, cancel)`, reset on
   every read that returns at least one byte, a keep-alive comment included. A watchdog that fires makes
   the next read fail with the cancellation, which the read loop turns into `timeout` and a dropped
   stream. That one cancel is also what ends the attempt when the loop decides to reconnect.

A status other than `200`, or a response with no body, is `http_<status>`; a `200` whose body reaches EOF
with nothing read is a clean end, not an error, and leaves `lastError` alone.

**The SSE parser is hand written**, in the same file as the read loop: no dependency keeps 7.2's rules,
and the rules are few. It takes a `bufio.Reader` of 4 KiB and scans bytes, not lines, so a line ending
split across reads or across buffer boundaries is still one ending:

- `\n`, `\r\n` and a lone `\r` end a line, whichever the read boundaries were; a `\r` at the end of a
  buffer waits for the next byte to learn whether an `\n` follows.
- A line starting with `:` is a comment, which includes `: keep-alive`; it still counts as a byte read
  for the watchdog.
- `event:` and `data:` set the current event's name and append one data line; a second data line appends
  `\n` plus its text. Every other field is collected and dropped.
- An empty line dispatches the event: no `data` line, drop it; no `event` line, the name is `message`;
  otherwise hand `(name, data)` to the read loop, which acts on `connected` and `changed` and ignores
  every other name.
- At EOF, or when the attempt is cancelled, an event still under construction is discarded.
- No cap on a line or an event: the buffer grows, and the watchdog and `requestTimeout` are the bounds,
  as 7.2 says.

UTF-8 inside an event is bytes, so a comment or a tag that is not valid UTF-8 passes through; the ETag is
compared as an exact byte string.

**Redirects** are not followed (7.5): the client's `CheckRedirect` returns `http.ErrUseLastResponse`, so a
redirect (a `3xx` but the `304`) comes back as the answer and is `http_<status>` (`http_302`) like any other status, and no request
goes to the `Location`.

**Reading a `200` body** follows 7.1: the bytes are handed to the JSON reader of section 5, which skips a
leading byte order mark, replaces invalid UTF-8 with U+FFFD the way WHATWG's decoder does, keeps the last
of duplicate keys, and refuses a body nested deeper than 1024 levels as `bad_ruleset` (7.1's floor is
512). There is no size cap: `requestTimeout` is the bound.

## 4. Dependencies

**Runtime: none.** `net/http` and `crypto/tls` do HTTP, the JSON reader and writer are ours (section 5),
and `sync`, `sync/atomic`, `time` and `context` are the standard library. Nothing here justifies a
third-party module: the SSE grammar is 60 lines, and the JSON rules of section 5 rule out every stdlib or
external decoder anyway, so a dependency would buy nothing.

**Tests: the standard `testing` package only**, run by `go test -race`. The vectors are read with the
SDK's own reader, since the point of the test is that reader. `net/http/httptest` fakes the server. No
test framework, no assertion library: `net/http/httptest` and `testing` are what Go projects use.

**Lint and format: `gofmt -l` and `golangci-lint` 2.15.0**, both pinned in `sdks/go/mise.toml` and called
through `mise exec --` by `bin/lint`. `golangci-lint` gets a `sdks/go/.golangci.yml` with the default
linters plus `errcheck`, `govet`, `staticcheck`, `ineffassign`, `unused`; the file is committed so the lint
is the same here and in CI. No `go vet` of its own: `golangci-lint` runs `govet`.

**The race detector: `zig` 0.17.1 as the C compiler**, because `-race` needs cgo and the CI runner is not
assumed to have a compiler (`docs/sdk-authoring/language-notes/go.md`, "`-race` needs cgo"). `bin/test`
sets `CC="$(mise exec -- mise where zig)/zig cc"` and calls the tool by path.

## 5. JSON

**A reader of our own**, in `jsonval`, because the standard decoder cannot meet the spec: `encoding/json`
replaces a lone surrogate escape with U+FFFD, so `"\ud800"` would not survive, and it rejects `1e999` as
an out of range number instead of reading it as Infinity. A hand written recursive descent reader over
the bytes is smaller than working around it.

The value type is a sealed interface with five cases, so a caller and the evaluator switch on it:

```go
type Value interface{ isValue() }
type Null struct{}          // nil is not used: a typed nil in an interface is a footgun
type Bool bool
type Number float64
type String string          // UTF-8, or WTF-8 for a lone surrogate (below)
type Array []Value
type Object map[string]Value // duplicate keys keep the last value
```

- **Numbers are IEEE-754 binary64**: `strconv.ParseFloat(token, 64)`, keeping its value even when it
  reports `strconv.ErrRange`, so `1e999` is `+Inf` and `-1e999` is `-Inf`. `9007199254740993` reads as
  `9007199254740992` by rounding, and `1.0` and `1` are the same `Number(1)`. `-0.0` keeps its sign,
  because `ParseFloat` does.
- **Lone surrogates are kept as they are.** `\uD800` with no valid low surrogate after it is encoded into
  the string as its three byte WTF-8 form (`ED A0 80`), and a valid high and low pair is combined into the
  one code point's UTF-8. A string therefore holds UTF-8 and, where the input had a lone surrogate, WTF-8;
  Go's `string` is a byte slice, so it holds either without a wrapper. A string that arrives as raw bytes
  and is not valid UTF-8 (a stream or a body) is decoded with WHATWG's replacement: one U+FFFD per maximal
  invalid subpart, not one per byte.
- `isValue()` has a pointer receiver on nothing and value receivers above; `Number`, `String` and `Bool`
  are defined types, so a caller writes `jsonval.Number(1)`, `jsonval.String("on")`.
- `Read(r io.Reader, maxDepth int) (Value, error)`: skips one leading byte order mark, refuses more than
  `maxDepth` open containers (the client passes 1024), and returns `*json.SyntaxError`-shaped errors with
  byte offsets, which the client turns into `bad_ruleset`.
- `Write(w io.Writer, v Value) error`: the companion encoder, so the events body is written with the same
  string escaping (a lone surrogate is written back as its `\uXXXX` escape, not U+FFFD). A non-finite
  number is written as `1e999` or `-1e999`, which is the spelling the spec's vectors use for Infinity and
  the only JSON text that reads back as the same value.
- **The context is a `jsonval.Value`**: `type Context = jsonval.Value` in package `toggleslash`. The
  vectors' contexts carry `1e999` and lone surrogates, and the test service receives them as JSON, so a
  native `map[string]any` would normalise both away. A `Context` that is not an object is
  `INVALID_CONTEXT` at evaluation, the evaluator's business and not the reader's.
- The evaluator reports `Object` and `Array` distinctly, and `Number` carries no integer flag, so the
  kind comparison of contract 4 ("no integer versus float distinction") is the evaluator's kind test and
  not a type of this package.

## 6. Minimum runtime and toolchain

**Minimum: Go 1.26**, written in `go.mod`'s `go` directive. It is the oldest release still in security
support: upstream supports each major Go release until two newer ones exist, and 1.27 is current
(`sdks/go/RUN.md`, Decisions 1 and 2). Nothing in this design needs a feature newer than 1.19's
`atomic.Pointer`; 1.26 is chosen for support, and 1.27.4 is what builds it.

**Toolchain choices**, each the mainstream one:

| Job | Tool | Why |
| --- | --- | --- |
| Build and test | the `go` command with modules (`go build`, `go test -race`) | Go's own toolchain and its mainstream choice; `mise` pins 1.27.4 |
| Lint | `golangci-lint` 2.15.0 | the mainstream Go linter runner; it runs `govet`, `staticcheck`, `errcheck` and more |
| Format | `gofmt -l` | the formatter every Go project uses; `gofmt` and not `gofumpt`, because the gate should want what the toolchain ships |
| Test framework | `testing` with `net/http/httptest` | the standard one; a third-party assertion library would be a dependency for nothing |
| C compiler | `zig` 0.17.1 | cgo for `-race` on a runner with no compiler |

**The integer accessor uses `int64`**, Go's signed 64-bit type, so a served number with a fractional part
or outside `[-2^63, 2^63-1]` is the caller's fallback with `TYPE_MISMATCH` (contract 4), and the largest
integer a test can serve exactly is `2^63 - 1024`. `IntVariation` therefore has no `int32` or `int` twin.

## 7. Package layout

One module, `github.com/madangryscientist/toggleslash/sdks/go`, in `sdks/go/`:

```
sdks/go/
  go.mod  go.sum  mise.toml  .golangci.yml  .gitignore
                        the module, the pins and the lint config
  api.go  options.go    package toggleslash, the module root: New, Options, the five
                        accessors and their Detail forms, Ready, Status, Close, Event,
                        Reason, ErrorCode, StreamState, LastError
  ruleset.go            the ruleset types and the 7.1 ruleset reader, same package
  client.go             the sync goroutine, the poll timer, the status snapshot
  stream.go             the stream attempt and the SSE parser
  events.go             the flush goroutine and the usage counts
  context.go            Context, the jsonval.Value alias, and the evaluator's entry
  *_test.go             the package's tests, beside it
  jsonval/              package jsonval: Value, Read, Write and its tests
  eval/                 package eval: the evaluator of docs/evaluation.md, its tests,
                        and vector_test.go, which walks fixtures/evaluation/*.json
  test-service/         package main: the conformance harness's test service
  bin/lint  bin/test  bin/test-service   the three entry points
  build/               bin/test's output, ignored by .gitignore
  DESIGN.md  RUN.md  README.md
```

Why these three packages and no `internal`:

- The root `toggleslash` is the public API, and its package name is `toggleslash` rather than `go`,
  because `go` is a keyword; the import path ends in `/sdks/go`, which keeps the repository's convention
  of `github.com/madangryscientist/toggleslash/sdks/<lang>` (item 7). Nothing is registered under it.
- `jsonval` is separate because the public API's `Context` is its `Value`: an `internal` package cannot
  appear in a public signature, so it is a normal subpackage.
- `eval` is separate because the evaluator is a self-contained function of a ruleset, a flag key and a
  context, and the vectors test it directly. It imports `jsonval` and nothing else of the SDK, which
  keeps it out of the client's import cycle.
- `test-service/` is `package main` so `bin/test` builds it into `build/testservice` as its last step, and
  `bin/test-service` runs that binary. The directory keeps the `test-service` spelling that `PROMPT.md` and
  the protocol page use.

Tests live beside the package they test (`go test ./...` from `sdks/go`). The vectors test reads the
repository's fixtures by walking up from the package directory to `fixtures/evaluation`, and reads only
the files it finds there, so nothing is copied into `sdks/go`.
