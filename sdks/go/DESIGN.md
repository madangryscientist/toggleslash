# ToggleSlash Go server SDK: design

This is the design the SDK in this directory was built to (phase 1 of
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md)). The contract is
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), evaluation is
[`docs/evaluation.md`](../../docs/evaluation.md) and the harness protocol is
[`docs/sdk-test-service.md`](../../docs/sdk-test-service.md). Section numbers below are the contract's.

The module path is `github.com/madangryscientist/toggleslash/sdks/go` and the package is `toggleslash`.
Nothing is published under that path.

## 1. The mapping table (contract 2)

Durations are `time.Duration`. `Create` is a package function that returns a `*toggleslash.Client` and
takes functional options. Go names are exported `CamelCase`.

| Name | Go | Default and meaning |
| --- | --- | --- |
| `create` | `toggleslash.Create(url, sdkKey string, opts ...Option) *Client` | Starts two goroutines and returns at once; it does not wait for the network and has no error return (a bad URL is a `network` last error, contract 6.1). |
| `ready` | `client.Ready() bool` | Blocks until the first ruleset attempt has settled, bounded by `requestTimeout` (the first load is one request with that timeout). True if a ruleset is held. Fixed at the first settle; false after `Close` if it never settled. |
| `variation` | `client.BoolVariation`, `StringVariation`, `NumberVariation` (`float64`), `IntVariation` (`int64`) and `JSONVariation` (`toggleslash.Value`), each `(flagKey string, ctx Context, fallback T) T` | One method per accessor kind. `JSONVariation` reads the expected kind off its fallback (`Null()` accepts any kind). |
| `variationDetail` | The same names with `Detail` appended, each returning `Detail[T]` | `Detail[T]` is `{Value T; VariationIndex int; Reason Reason}`. |
| `status` | `client.Status() Status` | One snapshot: `ETag`, `LastSyncAt`, `Stream`, `LastError`. |
| `close` | `client.Close() error` | Stops, abandons in-flight ruleset and stream requests, waits, sends the final usage flush. Idempotent, safe from many goroutines, returns within about twice `requestTimeout`. The error is always nil (it lets `Client` satisfy `io.Closer`). |
| `url` | `Create`'s first argument | The server's base URL. Trailing `/` are removed. |
| `sdkKey` | `Create`'s second argument | A server key. |
| `stream` | `toggleslash.WithStream(bool)`; default on | Off means poll only. |
| `pollInterval` | `toggleslash.WithPollInterval(time.Duration)`; default 30 s | A non-positive value is the default. |
| `flushInterval` | `toggleslash.WithFlushInterval(time.Duration)`; default 30 s | A non-positive value is the default. |
| `requestTimeout` | `toggleslash.WithRequestTimeout(time.Duration)`; default 10 s | The whole request, body included. A non-positive value is the default. |
| `hooks.afterEvaluation` | `toggleslash.WithAfterEvaluation(func(EvaluationEvent))`; default none | `EvaluationEvent` has `FlagKey`, `Context`, `Value`, `VariationIndex`, `Reason`, `ETag` and `FlagVersion`. Runs on the caller's goroutine, synchronously, once per evaluation. |
| `onError` | `toggleslash.WithOnError(func(error))`; default ignore | Receives an `error` whose text carries no URL, key or context. |
| `accessor shape` | Five typed methods (`Bool`, `String`, `Number`, `Int`, `JSON`), each with a `Detail` twin | The harness `any` accessor and `json` both map to `JSONVariationDetail`; the `number` accessor is `NumberVariationDetail` (a float64 with no integer rule); `int` is `IntVariationDetail` (contract 4: fractional or outside int64 is `TYPE_MISMATCH`). |
| `onError thread` | The SDK's own goroutine for what it did (a failed ruleset or stream attempt on the sync goroutine, a failed usage request on the flush goroutine); the caller's goroutine for a hook that panicked and for `EVALUATION_FAILED`; the goroutine that called `Close` for a request in the final flush and for counts lost at close. | No SDK lock is held while it runs. |

**The closed sets keep their spelling.** `ReasonKind`, `ErrorCode` and `StreamState` are `string` types whose
constants are the canonical text: `toggleslash.ReasonRuleMatch == "RULE_MATCH"`, `toggleslash.ErrNotReady ==
"NOT_READY"`, `toggleslash.StreamConnected == "connected"`. `Reason` is
`{Kind ReasonKind; RuleIndex int; PrerequisiteKey string; ErrorCode ErrorCode}`, with `RuleIndex` `-1` when the
kind carries none and the other fields empty when they do not apply. `Status.LastError` is a plain string
(`"http_503"`, `"timeout"`, `"bad_ruleset"`, `"network"`), with constants `LastErrorTimeout`,
`LastErrorBadRuleset` and `LastErrorNetwork`; `http_<status>` is built.

**"Not given" versus false or zero.** A Go option is a function that sets a field on an options struct, so an
option that is not passed leaves the default. `WithStream(false)` sets the field to false, which is "off".
A zero or negative duration passed to an interval option is ignored (the default stays), as contract 2 asks.
`VariationIndex` uses `-1` (the constant `toggleslash.NoVariation`) for "no index", because `0` is a real
index. `ETag` empty is "none" and `LastSyncAt` zero (`time.Time.IsZero`) is "never"; a real ETag is never
empty and a real sync time is never zero.

**The value type.** A context and a JSON fallback are `toggleslash.Value`, an alias of the SDK's own JSON
value. It exists because `encoding/json` cannot carry what the spec requires: `1e999` is an error there, and a
lone surrogate is replaced. `ParseJSON([]byte) (Value, error)` reads JSON the spec's way, and the constructors
`Null`, `Bool`, `Number`, `String`, `Array`, `Object` and `FromAny` (which takes `nil`, `bool`, any integer or
float, `string`, `[]any`, `map[string]any` and `Value`) build one in code. `Context` is another alias of
`Value`. A context that is not valid (`ERROR INVALID_CONTEXT`) is just a `Value` of the wrong shape.

## 2. Concurrency (contract 6)

- **The ruleset and its ETag** live in one immutable `view` (ruleset, ETag, last sync time, stream state, last
  error) reached through one `atomic.Pointer[view]`. A writer takes `writeMu`, copies the view, changes it and
  stores the pointer; `writeMu` covers a copy and never an I/O call or a callback. `Variation*` and `Status`
  are one atomic load: wait-free, no lock, no I/O. A hook's `ETag` and `FlagVersion` come from the same load as
  the evaluation, so they belong to one ruleset.
- **Goroutines the SDK starts**: the sync goroutine runs the first load and then the loop of 7.3; the flush
  goroutine runs the usage flush on `flushInterval`. A goroutine blocked in a read does not keep the process
  alive (Go exits when `main` returns), so a never-closed client holds nothing (contract 6.6).
- **Locks.** `writeMu` (above); `usageMu` (the counts map: one add or one swap of the map, never during I/O);
  `flushMu` (serialises flushes; taken by the flush goroutine and by `Close`, never by an evaluation).
  An evaluation takes `usageMu` for one map add and no other lock.
- **Callbacks** (the hook and `onError`) run with no SDK lock held, each inside a `recover`. A panic in a hook
  goes to `onError` as raised (it is the application's own value); a panic inside the SDK's own work (the
  evaluator, a sync or flush goroutine) goes to `onError` in fixed words with no value, because the value
  could carry a context; a panic in `onError` is swallowed. `Ready` called from the SDK's own goroutine (the
  `onError` of the first load's failure) returns at once with what is held: the SDK reads each of its two
  goroutine ids from `runtime.Stack` once, when the goroutine starts, and `Ready` compares the caller's id with
  them. A caller on any other goroutine waits on a channel the sync goroutine closes, never on a lock.
- **Close.** The first call wins a compare-and-swap on `closeStarted` and runs the shutdown; every other
  call waits on a `closeDone` channel, so a second or concurrent call returns when the first has finished. A
  `Close` called again from inside a callback the shutdown itself is running (same goroutine id as the
  closer) returns at once instead of waiting on itself. The shutdown marks the view closed (a late result
  never moves `Status`), cancels the root `context.Context` (which aborts the ruleset and stream requests in
  flight, with no `onError` for them), stops counting, waits for the sync goroutine and for a flush already
  running (not for a goroutine it is itself running on: a `Close` from an `onError` callback skips that wait),
  settles an unsettled `Ready` as false, runs the final flush with the request timeout, and reports its
  errors with no lock held. Usage requests use their own context, not the root, so Close does not cancel a
  flush; each is bounded by `requestTimeout`.
- **`FlagVersion`** is a `*float64`: nil when the flag is absent or malformed, because a version is a JSON
  number and `0` is a real version.

## 3. HTTP and SSE

- **HTTP client.** `net/http` with its own `http.Transport` (a clone of the default, no proxy from the
  environment, so a test on loopback is never routed), TLS certificates verified by the system roots, and
  `CheckRedirect` returning `http.ErrUseLastResponse` so a 3xx is an `http_3xx` error and `Authorization` is
  never replayed. The headers `net/http` adds on its own are `User-Agent: Go-http-client/<n>`,
  `Accept-Encoding: gzip`, `Host` and `Content-Length`; `RUN.md` lists each with why it names no one. The
  stream request sets `Accept-Encoding: identity` so events are not held back by a compressor.
- **`requestTimeout` bounds the whole request, body included.** Each ruleset and usage request runs under
  `context.WithTimeoutCause(root, requestTimeout, errTimeout)` and the body is read before the cancel, so a
  body that dribbles is a `timeout`. A failure is classified by the context's cause and the error type
  (`errors.As` for `net.Error`, never the text), then reported as the fixed words `network` or `timeout`; the
  `*url.Error` text is discarded because it carries the URL.
- **The stream** opens under a timer for `requestTimeout` that is stopped when the response headers arrive;
  after that a 60 second watchdog (`time.AfterFunc`) is `Reset` on every read that returns bytes (a
  `: keep-alive` line included) and cancels the request with a timeout cause when it fires. The watchdog
  length is a seam (`stream watchdog` on the unexported options). A status other than 200 is `http_<status>`;
  a 429 reads `Retry-After`.
- **SSE.** A short hand-written parser (`sse.go`) to the WHATWG rules the contract names: it ends a line on LF,
  CRLF or a lone CR (a CR at the end of a read waits for the next byte), ignores a line starting with `:`,
  reads `event` and `data` (one leading space removed, several `data` lines joined with LF), names an event
  `message` when none was given, drops an event with no `data` line, and discards an event still incomplete at
  the end of the stream. A UTF-8 byte order mark at the start is skipped. The SDK acts on `connected` and
  `changed`.
- **Seams** (unexported options, used by the native tests): the HTTP client, the clock (`now`), the sleep
  function the loops wait on, the jitter source and the watchdog length. They default to
  the real thing and are not part of the public API.

## 4. Dependencies

| Dependency | Kind | Why |
| --- | --- | --- |
| none | runtime | The standard library covers it: `net/http`, `crypto/sha256`, `sync`, `context`, `time`, `strconv`. No third-party module, so `go.sum` is absent. |
| `golangci-lint` 2.15.0 | lint | Pinned in `mise.toml`; `bin/lint` also runs `go vet` and `gofmt -l`. |
| `go test` | test | The standard library test runner, with `-race`. |
| `go` 1.27.3 | toolchain | Pinned in `mise.toml` for the checks; the module supports 1.26 and later. |

## 5. JSON

`encoding/json` is not used on the evaluation path or for the ruleset. `internal/jsonv` is a hand-written reader
and writer:

- **Reader.** A strict parser: a leading byte order mark is skipped, a raw control character in a string is an
  error, nesting beyond 512 is an error (the spec asks for no limit; this protects the stack), `NaN` and
  `Infinity` are errors. A number is read with `strconv.ParseFloat`; an overflow (`1e999`) is `+Inf` and
  `-0` is negative zero, because the spec reads every number as binary64. An integer literal beyond 2^53 rounds
  to the nearest binary64, like a literal of thousands of digits, with no big-integer step. A duplicate key
  keeps the last value (the spec says last wins for JSON, first wins only for the duplicate flag and segment
  keys, which the loader handles).
- **Strings are WTF-8.** A `\ud800` escape that is not followed by a low surrogate is held as the three-byte
  encoding of that lone surrogate; a valid pair is the one four-byte code point. A byte sequence in the input
  that is not UTF-8 becomes U+FFFD. Equality of two strings is byte equality; order and `contains` go by UTF-16
  code units through the helpers in `internal/wtf8`; hashing converts to UTF-8 with U+FFFD for each lone
  surrogate; the `matches` input is decoded to code points with a lone surrogate as itself.
- **Writer.** Numbers print the way ECMAScript prints them (`strconv.FormatFloat` in `'g'`-free form: shortest
  round trip, no exponent below 1e21 and above 1e-7, `-0` as `0`); `+Inf` and `-Inf` have no JSON form, so the
  test service writes them as `null`. A lone surrogate is written as `\ud800`.

## 6. Minimum runtime and toolchain

- **Minimum Go 1.26.** Go supports the two newest majors (1.27 and 1.26 on 2026-11-11), so 1.26 is the oldest
  with security fixes. Go has no long-term-support line, so there is no LTS to pin; the checks pin the newest
  release `mise` can install, 1.27.3 (`mise ls-remote go` does not list the two newest tags, so this is the
  newest it showed).
- **Lint and format: `golangci-lint` with `go vet` and `gofmt -l`** (the Go mainstream; one config in
  `.golangci.yml`). **Test framework: `go test`.** **Build tool: `go build ./...`**, which `bin/test` runs
  before the tests so the test service compiles.
- **The integer accessor** returns `int64`: a served number that is not finite, has a fractional part, or lies
  outside -2^63 to 2^63-1 is `TYPE_MISMATCH`; `2.0` is `2`. The largest servable number is 2^63 - 1024.
- **Capability** `integer-accessor` is advertised.

## 7. The package layout

```
sdks/go/
  go.mod  mise.toml  .gitignore  .golangci.yml  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  toggleslash.go   package doc, Create, Client, Close
  options.go       Option and the defaults
  types.go         Reason, Detail, Status, EvaluationEvent, the closed sets
  value.go         Value, Context and the constructors (aliases of internal/jsonv)
  access.go        the typed accessors and the result order of contract 4
  sync.go          the first load, the loop, backoff, Retry-After holds
  stream.go        the stream connection and the watchdog
  sse.go           the SSE parser
  usage.go         the counts and the batches
  *_test.go        native tests, with a loopback server
  internal/jsonv/      the JSON value, reader and writer
  internal/wtf8/       string helpers for WTF-8
  internal/evaluator/  the ruleset, the evaluator, the pattern engine, semver, instants, buckets
  test-service/        main.go and README.md (package main; `go build ./...` compiles it)
```

`bin/test` runs `go build ./...`, `go vet`-free tests with `-race -count=1` over every package, and builds the
test service binary last (it is what `bin/test-service` runs).

## 8. Evaluator decisions (phase 2)

- **Typed at load.** `NewRuleset` reads the root `Value` once and builds typed structs (flags, segments,
  clauses, rollouts) with each flag's status (`ready`, `broken` or `malformed`) decided then, as the TypeScript
  engine does. It never fails: anything that is not a ruleset is the caller's `bad_ruleset`, checked before.
  A `matches` pattern, a semver clause value and an instant clause value are parsed at load, once.
- **Strings** are WTF-8 (section 5). Kind names are checked with a byte walk, never a regular expression.
- **`matches`** is the spec's parser and Pike VM, over code points, in `internal/evaluator/pattern*.go`. No
  `regexp` import appears under `internal/evaluator`; a source search is in `RUN.md`.
- **Attribute reads** use own keys of an object only; a `bool` is tested before a number everywhere (Go has no
  such confusion, but the order is kept for parity).
- **Prerequisites** are evaluated once per call with a memo table and a depth counter (limit 32), as the spec
  says.

### Disagreements with other implementations

None found so far; any that turns up is recorded here with both readings and the file and line of each.
