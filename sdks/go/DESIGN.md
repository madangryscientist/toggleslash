# ToggleSlash Go server SDK: design

This is the design the SDK in this directory was built to (phase 1 of
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md)). The contract is
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), evaluation is
[`docs/evaluation.md`](../../docs/evaluation.md) and the harness protocol is
[`docs/sdk-test-service.md`](../../docs/sdk-test-service.md). Section numbers below are the contract's.

The module is `github.com/madangryscientist/toggleslash/sdks/go` and the package is `toggleslash` (Go's `go` is a
keyword, so the package cannot be named after it). Nothing is registered or published under the path.

## 1. The mapping table (contract 2)

Durations are `time.Duration`. `create` is `toggleslash.New(options)`, which returns a `*toggleslash.Client` at
once and starts its goroutines. Every method is safe from many goroutines and none panics.

| Name | Go | Default and meaning |
| --- | --- | --- |
| `create` | `toggleslash.New(toggleslash.Options{...}) *toggleslash.Client` | Starts two goroutines and returns at once; it does not wait for the network and it never fails. A URL or key it cannot use makes each request a `network` error. |
| `ready` | `client.Ready() bool` | Blocks until the first ruleset attempt has settled, bounded by the request timeout plus a second of grace. True if a ruleset is held. The answer is fixed when the first attempt settles. |
| `variation` | `client.Variation(flagKey string, context Context, fallback any) any`, and the typed accessors below | The generic accessor: the expected kind is read off the fallback's type, and a nil fallback accepts any kind. |
| `variationDetail` | `client.VariationDetail(...) Detail`, and the typed detail methods below | `Detail` holds `Value`, `VariationIndex` (`*int`, nil for a fallback) and `Reason`. |
| `status` | `client.Status() Status` | `ETag` (empty when none), `LastSyncAt` (the zero `time.Time` when never), `Stream` and `LastError` (empty when none). One snapshot, no request. |
| `close` | `client.Close()` | Stops following the server, stops the flush loop, waits for the loops, sends the final flush. Safe twice and from many goroutines; returns within about twice the request timeout. |
| `url` | `Options.URL` (required, `string`) | The server's base URL. Trailing `/` characters are removed. |
| `sdkKey` | `Options.SDKKey` (required, `string`) | A server key. |
| `stream` | `Options.Stream *bool` | nil means on; a pointer to false means poll only. A pointer is how "not given" differs from false. |
| `pollInterval` | `Options.PollInterval time.Duration` | Zero (or negative) means 30 s. |
| `flushInterval` | `Options.FlushInterval time.Duration` | Zero (or negative) means 30 s. |
| `requestTimeout` | `Options.RequestTimeout time.Duration` | Zero (or negative) means 10 s; the whole request, body included. |
| `hooks.afterEvaluation` | `Options.Hooks.AfterEvaluation func(EvaluationEvent)` | nil means none. Called on the caller's goroutine, before the call returns; its return value is ignored and a panic in it goes to `OnError`. |
| `onError` | `Options.OnError func(error)` | nil means ignore. Receives a `*toggleslash.Error` whose `Code` names the failure (`timeout`, `bad_ruleset`, `network`, `http_<status>`) or another error value. |
| `accessor shape` | Both: the generic `Variation` and `VariationDetail` over `any`, and typed methods `BoolVariation`, `StringVariation`, `Float64Variation`, `Int64Variation` (each with a `...Detail` form). | `Int64Variation` is the integer accessor: a served fraction or a number outside signed 64 bits is the fallback with `TYPE_MISMATCH`, and a served `2.0` is the integer `2`. `Float64Variation` is the number accessor and has no integer rule. `Variation` with a nil fallback accepts any kind; with a non-nil fallback it compares JSON kinds, and a fallback of no JSON kind (a `[]string`, say) matches nothing. |
| `onError thread` | The SDK's own goroutine that made the failing request (the sync goroutine for a ruleset or stream attempt, the flush goroutine for a usage request); the caller's goroutine for a hook that panicked and for `EVALUATION_FAILED`; the goroutine that called `Close` for the counts lost at close. | No SDK lock is held while it runs, and a panic in it is ignored. |

**The closed sets keep their spelling.** `ReasonKind`, `ErrorCode`, `StreamState` and `LastError` are string types
whose constants carry the contract's spelling: `toggleslash.ReasonRuleMatch == "RULE_MATCH"`,
`toggleslash.CodeTypeMismatch == "TYPE_MISMATCH"`, `toggleslash.StreamConnected == "connected"`,
`toggleslash.LastErrorTimeout == "timeout"`. `http_<status>` is built as a `LastError` from the number.

**"Not given" versus false or zero.** `Stream` is a `*bool`, so nil is "not given" and a pointer to false turns
the stream off. A duration is a `time.Duration`, where zero is "not given"; a negative duration falls back to
the default too (contract 2's SHOULD). A duration longer than a year is capped at a year, so a timer cannot
misbehave.

## 2. Concurrency (contract 6)

- **One snapshot for the ruleset, its ETag and the status.** Everything `Status` reads and every loop writes lives
  in one `clientSnapshot` value stored behind an `atomic.Pointer`: the held ruleset and ETag, the sync time, the
  stream state, `lastError` and the closing flag. A writer builds a new value and stores it; a reader loads it
  once. So a swap replaces the ruleset and the ETag together, `status` is one moment rather than four reads, and
  a stream failure sets `lastError` and `disconnected` in one store, so no reader sees a failure beside
  `connected` (6.5).
- **No lock on the evaluation path.** `Variation` and its typed forms load the snapshot pointer and run the
  evaluator; they take no mutex, do no I/O and never wait (5.2, 6.2). The only lock an evaluation takes is the
  usage counter's, for one map increment.
- **The writers' lock** (`stateMu`) serialises snapshot updates and is held for a struct copy and a store, never
  across I/O or a callback.
- **The goroutines the SDK starts:** `runSync` (the first load and the loop of 7.3) and `runFlush` (the usage
  flush). Go goroutines are not daemon threads and never keep the process alive, so a client that is never closed
  does not stop a program from exiting (6.6): `newClient` is the function that starts them.
- **Callbacks.** `afterEvaluation` and `OnError` run with no SDK lock held. Go has no goroutine identity, so the
  SDK cannot ask "am I on my own goroutine"; instead each goroutine marks itself in an atomic counter while it
  runs a callback (`reportFromLoop`, `callbacks`). `Ready` and `Close` read that counter: inside such a callback
  they return at once rather than waiting for the work that goroutine is doing (6.4). The unsettled-`ready` case
  cannot arise anyway, because a failed first load settles `ready` before it calls `onError` (client.go's `fail`).
- **Close** marks the snapshot closed, stops counting, closes the stop channel, cancels the sync context (so a
  ruleset or stream request in flight is abandoned at once), then waits for the two goroutines with a bound of
  one request timeout, runs the final flush (bounded per request), settles an unsettled `ready` as false and
  closes the shutdown channel that a second caller waits on. A close from inside a callback on an SDK goroutine
  skips the join, since that goroutine cannot finish until the callback returns.
- **The flush context is separate** from the sync context, so the final flush still reaches the server after the
  sync side has been cancelled.

## 3. HTTP and SSE

- **Client.** `net/http` from the standard library, with `CheckRedirect` returning `http.ErrUseLastResponse`, so a
  3xx is an `http_<status>` error and an `Authorization` header is never replayed. Each request is one
  `http.Client` shared by the SDK; connections are pooled by the transport and closed when the client is closed.
- **`requestTimeout` bounds the whole request, body included.** The request carries a context built with
  `context.WithTimeoutCause`, and the body is read under that same context, so a slow or endless body runs out of
  time (7.1). The cause distinguishes a timeout (`errTimeout`) from a close (`context.Canceled`), and every error
  a caller sees is one of three fixed values, so a `*url.Error` never leaks a URL into `onError`.
- **The stream** is opened with a goroutine and a timer, so the head is bounded by `requestTimeout` alone and a
  body that never arrives cannot hang the loop. Once the head is in, a reader goroutine pumps the body to the
  stream loop, which runs the 60 s watchdog itself: a timer reset on every chunk, a keep-alive comment included,
  and a timeout that cancels the request and reports `timeout` (7.2). A body that ends with `io.EOF` is a stream
  the server ended cleanly and is not an error; any other read failure is `network`. The watchdog length is a
  config field the tests shorten.
- **SSE** is `sse.go`, a hand-written parser to the WHATWG rules the contract names: bytes are held until a line
  ends, so a character or a CRLF split across reads is fine; LF, CRLF and a lone CR end a line; `:` is a comment;
  one leading space of a value is dropped; `data` lines are joined with LF; an event with no data line is dropped;
  an event still incomplete when the stream ends is discarded.
- **`Retry-After`** is read only on a 429: whole decimal seconds after trimming spaces and tabs, capped at 300 s.
  A hold is a time each group of routes checks, and the loop's wait is at least until it has passed.

## 4. Dependencies

| Dependency | Kind | Why |
| --- | --- | --- |
| none | runtime | `net/http`, `crypto/sha256` and `context` cover the HTTP, the hash and the cancellation; there is no module requirement at all (`go.mod` has no `require` block). |
| `golangci-lint` 2.15.0 | lint | `go vet` and `gofmt -l` ship with Go; golangci-lint adds the checks named in `.golangci.yml`. |
| `testing` | test | Ships with Go, so the tests need nothing installed. |
| `go` 1.27.4 | toolchain | Pinned in `mise.toml` for the checks; the module declares 1.26 as its minimum. |
| `zig` 0.17.1 | toolchain | Go's race detector needs cgo and a C compiler. `bin/test` names this zig by path (`CC="$(mise where zig)/zig cc"`), so one file pins every tool. |

## 5. JSON

`internal/jsonx` is the SDK's reader and writer, because `encoding/json` cannot be used: it refuses `1e999`
(errors) and replaces a lone surrogate escape with U+FFFD, where the spec keeps the surrogate.

- **Numbers are binary64.** `strconv.ParseFloat` gives Infinity for `1e999` and zero for `1e-999`, and rounds
  `9007199254740993` to `9007199254740992`. `-0.0` keeps its sign. `NaN`, `Infinity` and a leading zero are
  refused, as the text is not JSON.
- **Strings are WTF-8.** A Go string is bytes, so the reader writes ordinary UTF-8 for everything except a lone
  surrogate, which gets its own three-byte sequence; a valid escaped pair becomes one code point. `text.go`
  reads code units and code points from that, so the spec's UTF-16 ordering and a lone surrogate comparing only
  to itself both hold. Hashing (`hashText`) writes U+FFFD in a lone surrogate's place.
- **A body** is decoded first as the WHATWG UTF-8 decoder does: one U+FFFD per maximal invalid subpart, a leading
  byte order mark skipped, and the body kept and parsed. Nesting deeper than 2048 levels is refused as
  `bad_ruleset`; the contract's floor is 512 and a test reads 512 levels.
- **A duplicate key** keeps the last value. The writer makes compact JSON, writes a non-finite number as `null`
  and writes a lone surrogate back as `\uXXXX`.
- **The public API takes a context as `Context` (`map[string]any`)** whose values may be nil, bool, string, any of
  Go's numeric types, `[]any` or `map[string]any`. The vectors' `1e999` and lone surrogates reach the SDK through
  the same reader in the test service, so they are kept as they were written.

## 6. Minimum runtime and toolchain

- **Minimum Go 1.26.** On the run date (2026-11-12) Go 1.26 was released 2026-02-10 and is still supported, while
  1.25 reached end of life on 2026-08-19 (<https://endoflife.date/api/go.json>, read on the run date). Nothing
  the SDK uses is newer than 1.26; `go.mod` says `go 1.26.0`.
- **The pin is Go 1.27.4**, the newest stable release on the run date (`https://go.dev/dl/?mode=json` and
  `mise ls-remote go` agree). The checks run on it.
- **Lint and format: `go vet`, `golangci-lint` and `gofmt -l`**, the language's standard tools. The rule set is
  `default: standard` plus `errorlint`, `misspell`, `nilerr`, `unconvert` and `unparam`, each named in
  `.golangci.yml`; the only exclusion is the test service's path, whose panics are the protocol's 500 answers.
  **Tests: `testing`**, which ships with Go, so the test run needs nothing installed beyond the toolchain. **Build:
  `go build`**, which also compiles the test service.
- **The integer accessor** uses `int64`, the signed 64-bit type. A served number is binary64, so `2.0` is `2`,
  `1.5` is `TYPE_MISMATCH` and `2^63` is out of range; the largest integer a test can serve is `2^63 - 1024`.
- **The race detector** runs every test (`go test -race`); it needs cgo and the pinned zig as `CC`.

## 7. The package layout

```
sdks/go/
  mise.toml  go.mod  .gitignore  .golangci.yml  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  types.go        the public values: Reason, Detail, Status, EvaluationEvent, Hooks, Options, Error
  client.go       New, the public methods, the sync loop, close
  transport.go    the HTTP client: whole-request deadline, stream open, Retry-After, the hold
  sse.go          the Server-Sent Events parser
  usage.go        the counts, the batches and the flush
  context.go      contexts, kinds, numeric types, bucketing by kind
  ruleset.go      a ruleset validated and indexed at load
  evaluate.go     the order docs/evaluation.md fixes
  clause.go       attribute clauses and the ops
  pattern.go      the `matches` parser and Pike VM
  semver.go       versions, by hand
  dates.go        instants, by hand
  reference.go    "/" attribute references
  text.go         WTF-8, code units, code points, hashing
  internal/jsonx/ the JSON reader and writer (the test service reads its requests with it too)
  test-service/   main.go and README.md
  *_test.go       the native tests, beside the code they test
```

`bin/test` runs `go build ./...`, then `go test -race -count=1 ./...`, then the vector and pin counts, then builds
`.build/test-service`. `internal/` is where Go puts code only this module may import, which is exactly what the
JSON reader is.

## 8. Evaluator decisions (phase 2)

- **Strings.** Comparisons go through `text.go`: pure ASCII compares directly, anything else as UTF-16 big-endian
  bytes, which is the code unit order the spec names.
- **Bucketing** uses `crypto/sha256` over the WTF-8 bytes with a lone surrogate written as U+FFFD, big-endian, then
  `% 100000`.
- **Patterns** are parsed into a tree and compiled to a small program run by a Pike VM; the `regexp` package is
  imported nowhere in the SDK, and a test holds that.
- **No pattern cache.** The TypeScript engine caches compiled patterns in a map; this SDK compiles on each clause
  instead, because a cache shared by goroutines needs a lock on the evaluation path and the patterns it sees are
  short.
- **Attribute reads** use own keys of a `map[string]any` only, and a bool is never a number.
- **Numbers from a caller** go through `numericValue`, which reads every integer and float width as binary64 and
  uses reflection only for a named type.

### Disagreements with other implementations

None found. `docs/evaluation.md`, `packages/engine/src` and `apps/server/ToggleSlash.Evaluation` agreed on every
vector, which the SDK passes unmodified.
