# Go SDK: design

The Go server SDK for ToggleSlash. It follows [`docs/sdk-contract.md`](../../docs/sdk-contract.md) (the
contract) and evaluates to [`docs/evaluation.md`](../../docs/evaluation.md) (the spec). This page was written
before the code and corrected when the code taught something.

The module is `github.com/madangryscientist/toggleslash/sdks/go` and the package is `toggleslash` (the
last path element, `go`, is a keyword, so it cannot be the package name).

## Inputs and decisions made because an input was absent

| Input | Value | Why |
| --- | --- | --- |
| language | Go | given |
| runtime | the `gc` toolchain on the standard library | the mainstream runtime; nothing else is in play |
| minimum version | Go 1.26 | the oldest release in security support on the run date (2026-11-12): Go supports the two newest major releases, which are 1.27 and 1.26. It has everything used: generics, `context.WithTimeoutCause`, `atomic.Pointer`, `min`, `for range` over integers |
| build tool | the `go` command and `go.mod` | the language's own |
| pinned toolchain | Go 1.27.4 | the newest stable release, from `mise ls-remote go` and go.dev/dl. Go has no LTS line, so the rule for a language with long-term-support releases does not apply |

`go.mod` declares `go 1.26`. The pinned 1.27.4 builds and tests it, and a 1.26 toolchain builds it too (no
language or library feature newer than 1.26 is used).

## 1. The mapping table (contract section 2)

| Name | In Go | Default and notes |
| --- | --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client` | never returns nil, never panics, does not wait for the network; the first ruleset request starts at once on the client's own goroutine |
| `ready` | `(*Client).Ready() bool` | blocks until the first load attempt has settled, bounded by `requestTimeout`; the answer is fixed when it settles |
| `variation` | `(*Client).BoolVariation`, `StringVariation`, `Float64Variation`, `IntVariation`, `JSONVariation`, each `(flagKey string, ctx Context, fallback T) T` | see `accessor shape` |
| `variationDetail` | `(*Client).BoolVariationDetail`, `StringVariationDetail`, `Float64VariationDetail`, `IntVariationDetail`, `JSONVariationDetail`, each returning `Detail[T]` | `Detail[T]` has `Value T`, `VariationIndex int` (`NoVariation`, which is -1, for none) and `Reason Reason` |
| `status` | `(*Client).Status() Status` | one consistent snapshot: `ETag string` (empty for none), `LastSyncAt time.Time` (zero for never), `Stream StreamState`, `LastError LastError` (empty for none) |
| `close` | `(*Client).Close()` | returns nothing; safe to call twice and concurrently |
| `url` | the first argument of `New` | required; trailing `/` removed |
| `sdkKey` | the second argument of `New` | required |
| `stream` | `WithStream(bool) Option` | on; `WithStream(false)` is how it is turned off |
| `pollInterval` | `WithPollInterval(time.Duration) Option` | 30 s |
| `flushInterval` | `WithFlushInterval(time.Duration) Option` | 30 s |
| `requestTimeout` | `WithRequestTimeout(time.Duration) Option` | 10 s |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(Event)) Option` | none |
| `onError` | `WithOnError(func(error)) Option` | ignore |
| `accessor shape` | typed methods and no generic method: `Bool`, `String`, `Float64`, `Int` (an `int64`) and `JSON` (a `Value`), each with a `Detail` variant | there is no generic method because Go methods cannot take type parameters, and a package-level generic function adds a second spelling for no gain. The test service's `any` accessor maps to `JSONVariationDetail` |
| `onError thread` | the goroutine that hit the failure | a ruleset or stream failure: the client's sync goroutine. A usage failure: the flush goroutine, or the goroutine that called `Close` for the final flush, which reports after the shutdown's channel is closed so a `Close` from the `onError` function returns at once. A failed hook or evaluation: the goroutine that called `variation`. Never a lock held while it runs |

Also in the package (contract section 13): `WithHTTPClient(*http.Client) Option`, and the context builders
`NewContext`, `NewContextKind`, `NewMultiContext`, `ParseContext`, `ValueOf` (below).

The closed sets of contract section 2 are string types whose constants carry the canonical spelling:
`ReasonKind` (`ReasonOff = "OFF"`, `ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"`, `ReasonTargetMatch`,
`ReasonRuleMatch`, `ReasonFallthrough`, `ReasonError`), `ErrorCode` (the evaluator's seven and the SDK's
`NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`), `StreamState` (`off`, `connecting`, `connected`,
`disconnected`, `closed`) and `LastError` (`timeout`, `bad_ruleset`, `network`, and `http_<status>` built from
the status). A `Reason` is a struct: `Kind ReasonKind`, then `RuleIndex int` (meaning only for `RULE_MATCH`),
`FlagKey string` (only for `PREREQUISITE_FAILED`) and `Code ErrorCode` (only for `ERROR`).

**Not given against false or zero.** Options are functional options applied over a defaults struct, so an
option that is not passed keeps its default and `WithStream(false)` is a different thing from not passing
`WithStream`. A non-positive duration is not an error: it falls back to the default (contract section 2 says
an SDK SHOULD).

**Contexts.** `Context` is an alias of `Value`, the SDK's JSON value type (section 5). `NewContext(key string,
attributes map[string]any)` builds a user context; `NewContextKind(kind, key, attributes)` a single context of
another kind; `NewMultiContext(map[string]Context)` a multi context from single contexts (it takes each
entry's key and attributes); `ParseContext([]byte)` reads raw JSON, which is the form that keeps `1e999` and a
lone surrogate. `ValueOf(any)` turns Go values into a `Value`: `nil`, `bool`, every integer width, `float32`,
`float64`, `string`, `[]any`, `[]string`, `map[string]any`, `Value`, `json.Number`. Every number becomes the
binary64 nearest its value, as a served number does. A value `ValueOf` does not know becomes null. None of
these can fail: an invalid context is the evaluator's `ERROR INVALID_CONTEXT`.

## 2. Concurrency (contract section 6)

- **The ruleset and its ETag.** One `atomic.Pointer[snapshot]`, where a `snapshot` is an immutable struct
  holding the compiled ruleset, the ETag and the flag index. A sync builds a new snapshot and stores the
  pointer. An evaluation loads the pointer once and uses that snapshot throughout, so the ruleset, the ETag
  and `flagVersion` it reports always belong together, and a reader sees the old snapshot or the new one.
- **Goroutines.** `New` starts two: the **sync goroutine** (first load, then the loop of 7.3, the stream
  included) and the **flush goroutine** (the usage timer). Nothing else is started; a request runs on its
  goroutine. `Close` cancels one `context.Context` that every request carries (which abandons the requests in
  flight), then waits for both goroutines to finish, then makes the final flush on its own goroutine.
- **Close.** The first call to `Close` runs the shutdown; every call, concurrent or later, waits on one
  channel the shutdown closes. A goroutine the SDK started marks itself while it runs `onError`: `Close`
  does not wait for a marked goroutine (it is doing nothing until the callback returns, and a `Close` from
  that callback must not wait for its own goroutine). A goroutine the application's callback starts is the
  application's own, so its `Close` and `Ready` wait like any other caller's. The wait is on a small state
  (a mutex and a channel replaced on every change) rather than a poll, so a goroutine that enters a callback
  while `Close` waits releases it.
- **Locks.** Each is held for a few field reads or writes and never across a network call, a callback or a
  wait: the status mutex (the `Status` snapshot, `lastError`, the stream state), the usage mutex (the counts
  map), the hold mutex (two `Retry-After` deadlines) and the loop-state mutexes above. One more, the flush
  mutex, serialises flushes and is held across the usage requests; nothing on the evaluation path takes it,
  and `Close` takes it only after cancelling, so it waits for a flush already running, bounded by
  `requestTimeout`. `variation` takes the usage mutex only to add one count, and the status mutex never.
- **Callbacks.** `afterEvaluation` runs on the caller's goroutine after the evaluation with no SDK lock held;
  `onError` is called with none held either. A panic in either is recovered; a panic in `onError` is dropped,
  and a panic in the hook is passed to `onError` as an error that carries the panic value.
- **`ready` from a callback.** The sync goroutine settles `ready` before it calls `onError` for the first
  load's failure, so a `Ready` inside any callback that goroutine runs returns at once. The flush goroutine's
  callbacks cannot wait on it for longer than the instants between storing a ruleset and settling.
- **A client never closed** keeps no process alive: goroutines do not count when the program exits.
- **The status snapshot.** A stream attempt's end writes `lastError` and `stream` in one critical section.
- **Time.** All waits of the loops (the poll and backoff waits, the hold, the flush timer) and the day of a
  count go through a small `clock` interface, which the tests replace with a fake that fires on demand. The
  watchdog and the request timeout use real timers: a real socket needs a real clock.

## 3. HTTP and SSE

- **Client.** `net/http`. The default client is a clone of the default transport with `DisableCompression`
  set (the SDK never asks for `gzip`, so a stream is never decoded) and redirects not followed
  (`CheckRedirect` returns `http.ErrUseLastResponse`, so a `3xx` is an `http_3xx` error and `Authorization`
  is never resent to another place). `WithHTTPClient` replaces it.
- **`requestTimeout` covers the body.** Every ruleset and usage request has one `context.WithTimeoutCause`
  that starts before the request and is cancelled after the body has been read to the end, so it bounds the
  connect, the headers and the body. The cause tells a timeout from `Close`'s cancel.
- **The stream.** Its open is bounded by `requestTimeout`, by a timer that stops the moment the response
  headers arrive (the request context itself carries no deadline). After that, a watchdog timer (60 s, a
  seam in tests) is reset after every read that returns bytes and cancels the request with its own cause when
  it fires, which turns the pending `Read` into an error classified `timeout`.
- **SSE.** A hand-written parser (`sse.go`) over the raw bytes: it splits lines on LF, CRLF and a lone CR
  (a CR ends the line at once, and an LF that directly follows it, even in the next read, is skipped), ignores comment lines, reads `event` and
  `data` (several `data` lines joined with LF, a value's one leading space removed), drops an event with no
  `data` line, names an event with none `message`, ignores the other fields, and discards an event still
  incomplete when the stream ends. A UTF-8 sequence cut between reads is no problem because it works on
  bytes and decodes a finished field once, with the WHATWG decoder.
- **Usage requests** run on their own context, built from `context.Background()` with the request timeout, not
  on the one `Close` cancels: the final flush `Close` makes must not be abandoned (contract section 11), and a
  flush that was already running when `Close` began finishes within the request timeout.
- **No limits of its own.** No cap on a line, an event or a body (contract 7.1, 7.2).

## 4. Dependencies

Runtime: none. The standard library has HTTP, SHA-256, time and sync. Test: the standard `testing` and
`net/http/httptest` packages only. Lint and format: `golangci-lint` (several linters in one run, configured
in `.golangci.yml`), `go vet` and `gofmt`. `zig` is a build tool only: `-race` needs cgo, cgo needs a C
compiler, and the CI runner is not assumed to have one, so `mise.toml` pins `zig` and `bin/test` names it by
path as `CC`.

## 5. JSON

`encoding/json` cannot be used: it fails on `1e999`, and it replaces a lone surrogate with U+FFFD, both of
which the spec forbids. `internal/jsonvalue` is a hand-written reader and writer.

- **The value type** is `Value`, a struct of a kind and one of: a `bool`, a `float64`, a `string`, a slice or
  a map. Null is the zero `Value`. The public package exposes it as `toggleslash.Value`, so a context, a
  served JSON value and a hook event's value all use it.
- **Numbers** are read with `strconv.ParseFloat` after the grammar of RFC 8259 has been checked by hand, so
  they are the correctly rounded binary64: `1e999` is `+Inf` and not an error, `9007199254740993` is
  `9007199254740992`, and `-0.0` is `-0`.
- **Strings are WTF-8** held in a Go `string`. A UTF-8 sequence is kept as it is, and a surrogate (U+D800 to
  U+DFFF) that is alone is stored as the 3-byte sequence `ED A0..BF 80..BF`, which valid UTF-8 forbids and the
  `internal/wtf8` package reads. The reader joins a `😀` pair into one 4-byte code point, so the
  stored form is always the canonical one: equal code-unit sequences are equal byte sequences. The functions
  that need code units (ordering with `<`, `contains`, `startsWith`, `endsWith` when the needle starts with a
  lone low surrogate or ends with a lone high one) work on UTF-16 units; the others use bytes.
- **The body.** The bytes of a `200` are decoded with the WHATWG UTF-8 decoder (one U+FFFD per maximal
  invalid subpart, which `utf8.DecodeRune`'s one per byte is not), a leading byte order mark is skipped, and
  the result is parsed. A duplicate object key keeps the last value. Nesting deeper than 10000 levels is
  `bad_ruleset` (contract 7.1 allows a limit of at least 512; Go cannot recover from stack exhaustion, so a
  limit is needed to keep a hostile body from ending the process).
- **The writer** (used by the test service and the tests) writes a non-finite number as `null`, as
  `JSON.stringify` does, and escapes a lone surrogate as `\uD800` so the output is valid JSON.

## 6. Minimum runtime and toolchain

- **Minimum Go 1.26**, for the reason in the table at the top.
- **Lint and format:** `go vet`, `gofmt -l` and `golangci-lint` v2 with an explicit list of linters. Each
  is the mainstream tool for Go; `golangci-lint` runs the others under one configuration.
- **Test framework:** the standard `testing` package. It has subtests, `t.TempDir`, the race detector and
  `httptest`, which is what the native tests need.
- **Build tool:** the `go` command. `bin/test` runs `go build ./...` and `go vet` first, then
  `go test -race`, then builds the test service binary into `.build/`.
- **The integer accessor** is `IntVariation`, using `int64`.

## 7. The package layout

```
sdks/go/
  go.mod                    module github.com/madangryscientist/toggleslash/sdks/go, go 1.26
  toggleslash.go            package doc, Client, New, options
  types.go                  Reason, Detail, Status, Event, the closed sets, errors
  context.go                Context builders and ValueOf
  client.go                 the evaluation path: accessors, the mismatch rule, hooks, counting
  sync.go                   the first load, the loop, backoff, Retry-After, the stream
  sse.go                    the Server-Sent Events parser
  usage.go                  the counts, flushes, batches, the usage hold
  clock.go                  the clock seam and its real implementation
  *_test.go                 native tests (package toggleslash)
  internal/evaluator/       the spec: ruleset, evaluation, clauses, pattern VM, semver, dates, bucketing
  internal/jsonvalue/       the JSON value, reader and writer
  internal/wtf8/            WTF-8 decoding, UTF-16 helpers and the WHATWG decoder
  test-service/main.go      the conformance test service (package main; built by bin/test)
  bin/lint  bin/test  bin/test-service
  mise.toml  .golangci.yml  .gitignore  README.md  DESIGN.md  RUN.md
```

The evaluator imports no HTTP package and the client imports the evaluator through its `Ruleset` type; the
vectors test lives in `internal/evaluator` and reads `fixtures/evaluation` by a path relative to the test.
