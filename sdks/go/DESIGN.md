# Go SDK: design

The ToggleSlash server SDK for Go. It keeps `docs/sdk-contract.md` (the contract) and evaluates to
`docs/evaluation.md` (the spec). Module `github.com/madangryscientist/toggleslash/sdks/go`, package
`toggleslash` (`go` is a keyword, so the package name is not the directory name). Nothing is registered or
published under that path.

## 1. The mapping table

| Canonical name | In Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client`. It returns at once and starts two goroutines; it never returns nil and never panics. `url` and `sdkKey` are positional because they are required. |
| `ready` | `(*Client).Ready() bool`. Blocks until the first load has finished, bounded by `requestTimeout`; the answer is fixed when the first attempt settles. `(*Client).ReadyContext(ctx) bool` also stops waiting when `ctx` is done (it then answers what is held). |
| `variation` | The typed accessors `BoolVariation`, `StringVariation`, `IntVariation`, `FloatVariation` and `JSONVariation`, each `(flagKey string, ctx Context, fallback T) T`. |
| `variationDetail` | `BoolVariationDetail`, `StringVariationDetail`, `IntVariationDetail`, `FloatVariationDetail` and `JSONVariationDetail`, each returning `Detail[T]` (`Value`, `VariationIndex`, `Reason`). `VariationIndex` is `-1` (`NoVariation`) when the value is the fallback. |
| `status` | `(*Client).Status() Status` with `ETag string` (empty is none), `LastSyncAt time.Time` (zero is never), `Stream StreamState` and `LastError SyncErrorCode` (empty is none). One atomic snapshot. |
| `close` | `(*Client).Close() error`. It always returns nil (the `error` is so `io.Closer` fits), is safe to call twice or concurrently, and returns within about twice `requestTimeout`. |
| `url` | the first argument of `New`. Required. Trailing `/` characters are removed. |
| `sdkKey` | the second argument of `New`. Required. |
| `stream` | `WithStream(on bool)`. Default `true`. |
| `pollInterval` | `WithPollInterval(d time.Duration)`. Default 30 s. |
| `flushInterval` | `WithFlushInterval(d time.Duration)`. Default 30 s. |
| `requestTimeout` | `WithRequestTimeout(d time.Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(Event))`. Default none. |
| `onError` | `WithOnError(func(error))`. Default ignore. |
| `accessor shape` | typed methods only: `Bool`, `String`, `Int` (`int64`), `Float` (`float64`) and `JSON` (the SDK's own `Value`). There is no generic method, because Go methods cannot have type parameters. `JSONVariation` with a `Null()` fallback accepts any served kind, and is the "any" accessor. |
| `onError thread` | a failed ruleset or stream attempt is reported on the sync goroutine, a failed usage request on the flush goroutine (or on the goroutine that called `Close`, for the final flush), and a hook that panicked or an evaluation that panicked on the goroutine that called the accessor. No SDK lock is held when it runs. |

**Closed sets.** Each is a named string type with constants whose values keep the canonical spelling:
`ReasonKind` (`OFF`, `PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`),
`ErrorCode` for the evaluator's and the SDK's error codes (`INVALID_CONTEXT`, ..., `NOT_READY`,
`TYPE_MISMATCH`, `EVALUATION_FAILED`), `StreamState` (`off`, `connecting`, `connected`, `disconnected`,
`closed`) and `SyncErrorCode` for `lastError` (`http_<status>` is built at run time from the status,
`timeout`, `bad_ruleset`, `network`; the last three are constants). A caller compares a string and never
parses a message.

**Not given versus zero.** The options are functional options applied over a defaults struct, so an option
that is not passed leaves the default. `WithStream(false)` sets false. A duration of zero or less is
ignored and the default stays (contract section 2 says an SDK should do that and does not test it). A
`nil` callback leaves none.

**Detail.** `Detail[T]` is a generic struct, not a method, so typed accessors share it. `Reason` is a
struct: `Kind ReasonKind`, `RuleIndex int` (set for `RULE_MATCH`), `FlagKey string` (set for
`PREREQUISITE_FAILED`) and `Code ErrorCode` (set for `ERROR`).

**Contexts.** `Context` wraps the SDK's own `Value`. It is built with `UserContext(key, attributes)`,
`NewContext(kind, key, attributes)`, `MultiContext(map[string]Context)`, `ContextFromMap(map[string]any)`
(native Go values) or `ContextFromJSON([]byte)`, which parses with the SDK's parser and so keeps `1e999`
and lone surrogates. Bad JSON gives a context the evaluator reports as `INVALID_CONTEXT`. Nothing here
fails.

## 2. Concurrency

- **The ruleset and its ETag** live in one immutable `*snapshot` (ruleset, ETag, last sync time, stream
  state, last error) held in an `atomic.Pointer`. A writer copies the snapshot, changes the copy and
  stores it, under a small mutex `writeMu` that is held only for that copy and never across I/O or a
  callback. A reader does one `Load`. So a swap replaces ruleset and ETag together, a hook's `etag` and
  `flagVersion` come from the snapshot the evaluation used, and `Status` is one consistent read.
- **Goroutines.** `New` starts the sync goroutine (first load, then the loop of contract 7.3) and the flush
  goroutine (a timer loop). Both are given a context derived from one lifetime context. `Close` cancels
  it, which abandons in-flight requests and wakes every sleep, then waits for both goroutines to finish.
  A goroutine that is running an application callback (`onError`) is marked with an atomic counter while
  it does, and `Close` does not wait for a goroutine so marked: it has nothing more to do until the
  callback returns. Go has no goroutine identity, so this marker is how a `Close` or `Ready` called from
  inside a callback on the SDK's own goroutine returns instead of waiting for itself. `Ready` settles the
  first load before it reports that load's error, so a `Ready` from that `onError` has its answer.
- **Locks.** `writeMu` (above), `usageMu` (the pending-count map; held for a map increment or a swap only),
  `flushMu` (makes flushes serial; held across the usage requests) and `closeMu` (held for a flag flip).
  Evaluation takes none of `flushMu` or `closeMu`; the only lock it ever takes is `usageMu` for the
  increment of a counted evaluation, and that is never held across I/O or a callback. So no evaluation
  waits on a request. `onError` calls are collected while a lock is held and made after it is released;
  an `afterEvaluation` hook runs on the caller's goroutine with no lock held.
- **`Close`** flips a closing flag (the first call only), publishes `closed` as the stream state, cancels
  the lifetime context, waits for the two goroutines (not a marked one), then does the final flush once.
  Every call waits for the same final flush through a channel, so two `Close` calls both return when
  shutdown is done. A `Ready` that had not settled settles false.
- **No leak keeps a process alive.** Go ends a program when `main` returns whatever goroutines run, so a
  client that is never closed cannot keep a process from exiting; the goroutines start in `New`
  (`startLoops`).

## 3. HTTP and SSE

- **Client.** `net/http` with its own `*http.Transport` (a clone of the default, so proxy settings from
  the environment apply and `Close` can close its idle connections) and `CheckRedirect` returning
  `http.ErrUseLastResponse`, so a `3xx` is an `http_3xx` error and `Authorization` is never resent to
  another host. HTTPS uses the platform's `crypto/tls` with the system roots.
- **`requestTimeout` bounds the whole request, body included.** Each ruleset and events request gets
  `context.WithTimeoutCause(lifetime, requestTimeout, errTimeout)`, and the context is cancelled only after
  the body has been read, so a server that sends headers and then stalls is cut off. A context cause of
  `errTimeout` is `timeout`; a cancelled lifetime is silent; anything else is `network`.
- **The stream.** Opening is bounded by a timer for `requestTimeout` that is stopped when the response
  headers arrive. After that a watchdog (`time.AfterFunc`, 60 s, `cfg.idle` in tests) cancels the request
  with cause `errTimeout` when no read returns bytes for that long, and every read that returns bytes
  resets it. Keep-alive comments are bytes, so they reset it.
- **SSE** is a hand-written byte parser (`sse.go`) to the WHATWG rules the contract names: LF, CRLF and a
  lone CR end a line (a CR at the end of a read waits for a possible LF), a line starting with `:` is a
  comment, a field is split at the first `:` and one leading space of the value is dropped, only `event`
  and `data` are read, `data` lines join with LF, an event with no `data` line is dropped, the name is
  `message` when none was given, and an event incomplete when the stream ends is discarded. A leading byte
  order mark is skipped. It works on bytes, so a UTF-8 sequence split across reads needs no care; the data
  is scrubbed like a body before it is compared with the ETag.
- **Retry-After** is read only on a `429`: one or more ASCII digits after trimming spaces, times 1000 ms,
  capped at 300 s. The ruleset and stream routes share one hold (a time, `holdUntil`, in the snapshot's
  owner), the events route has its own.

## 4. Dependencies

Runtime: none. The standard library has HTTP, TLS, SHA-256 and everything else; JSON is written by hand
(section 5). Test: the standard `testing` package only. Lint: `golangci-lint` and `go vet`, installed by
`mise`. The race detector needs cgo, so `zig` is pinned too and used as the C compiler (`zig cc`); it is a
build tool, not a dependency of the module, and there is no `go.sum`.

## 5. JSON

`encoding/json` is not used: it errors on `1e999` and replaces a lone surrogate with U+FFFD, both of which
the spec forbids. `internal/jsonvalue` is a strict RFC 8259 parser (no comments, no trailing commas, no
leading zeros, control characters in strings rejected) that produces `Value`: null, bool, number
(`float64`), string, array, object (ordered keys with a map; a duplicate key keeps the last value).

- **Numbers** are read with `strconv.ParseFloat` on the exact text: `1e999` is `+Inf` (a range error is
  not an error here), `9007199254740993` is `9007199254740992`, and `-0.0` is negative zero.
- **Strings are WTF-8.** A Go string holds bytes; a valid code point is its UTF-8, and a lone surrogate is
  the three bytes UTF-8 would use for it (`ED A0 80` to `ED BF BF`). A `😀` escape pair is joined
  into the one four-byte code point, so a string has one spelling and byte equality is code point
  equality. The spec's UTF-16 code unit order and substring rules are kept by `internal/wtf8`, which
  compares by unit and falls back to unit-by-unit search when the needle holds a lone surrogate (a lone
  surrogate is a prefix or suffix of a pair in UTF-16 and not in bytes). Hashing converts each lone
  surrogate to U+FFFD, as the spec says.
- **Bodies** are decoded first with the WHATWG UTF-8 decoder (one U+FFFD per maximal invalid subpart), a
  leading BOM is skipped, and the nesting limit is 10000 levels (the contract asks at least 512). The limit
  is there because a Go stack overflow is fatal, not recoverable.
- **Context.** A `Context` holds a `Value`, so the public API keeps `1e999` and lone surrogates (through
  `ContextFromJSON`), and `Value` is exported (`ParseValue`, `String`, `Number`, `Bool`, `Null`, `Array`,
  `Object`) for building them.

## 6. The minimum runtime, and the toolchain

- **Minimum Go: 1.26**, declared in `go.mod`. The brief gave none, so it is the oldest release in upstream
  security support on the run date (Go supports the two latest majors, 1.27 and 1.26). The SDK needs
  `atomic.Pointer`, `context.WithTimeoutCause`, `min`/`max`, `for range n` and `math/rand/v2`, all older.
- **Pinned toolchain** (`mise.toml`): Go 1.27.4 (the newest stable `mise ls-remote go` lists, checked against
  go.dev/dl), `golangci-lint` 2.15.0 and `zig` 0.17.1 (the C compiler for `-race`). Go has no LTS line.
- **Test framework:** the standard `testing` package. **Build tool:** `go build`/`go test` (modules).
  **Lint and format:** `go vet`, `gofmt -l` and `golangci-lint` v2 with a named linter set
  (`.golangci.yml`), the language's current mainstream pair; `oxfmt` formats the TOML, YAML and JSON.
- **Integer accessor:** `IntVariation` uses `int64`. A served number with a fractional part, or outside
  `[-2^63, 2^63)`, is `TYPE_MISMATCH`; `2.0` is `2`.
- **Event values** are the SDK's `Value` (binary64), so an integer above 2^53 is exact in the caller's
  `int64` and the event carries the same double the evaluator held.

## 7. The package layout

```
sdks/go
  go.mod  mise.toml  .golangci.yml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  toggleslash.go     package doc, New, options, defaults
  client.go          Client, accessors, Ready, Status, Close
  sync.go            ruleset sync, stream, the loop (contract 7)
  sse.go             SSE parser
  usage.go           counts and flushing (contract 9)
  clock.go           the clock seam (real and fake)
  types.go  context.go   public types, Context builders
  *_test.go          native tests (fake loopback server, fake clock)
  internal/wtf8        code point decoding, UTF-16 order and search, scrubbing
  internal/jsonvalue   the JSON parser, Value, the writer
  internal/evaluator   the spec port: validation, contexts, clauses, patterns, dates, semver, SHA-256 buckets
  test-service/        main.go and README.md (the conformance test service; built with the module)
```

The evaluator has no network code and no import of `regexp`. The test service is `package main` inside
the same module, so `go build ./...` and `go vet ./...` compile it with the tests, and `bin/test` builds
its binary into `.build/test-service` as its last step.
