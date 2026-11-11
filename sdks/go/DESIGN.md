# Go SDK: design

The Go binding of [`docs/sdk-contract.md`](../../docs/sdk-contract.md). It evaluates locally with its own port of
[`docs/evaluation.md`](../../docs/evaluation.md). Module `github.com/madangryscientist/toggleslash/sdks/go`; the root package is named
`toggleslash` (the directory is `go`, a keyword).

## 1. Mapping table (contract section 2)

`import "github.com/madangryscientist/toggleslash/sdks/go"` gives package `toggleslash`; `jsonvalue` is the sibling package that holds the JSON
type contexts, fallbacks and values are written in.

| Name | Go |
| --- | --- |
| `create` | `toggleslash.New(url, sdkKey string, opts ...Option) *Client`. It never returns an error and never panics: a bad URL or key gives a client whose first load fails. It starts the sync and flush goroutines and returns at once. |
| `ready` | `(*Client).Ready() bool`. Blocks until the first load attempt settles, then always answers the same. `Close` settles it `false` if still open. Safe to call from any goroutine, any number of times. |
| `variation` | `(*Client).Variation(flagKey string, ctx jsonvalue.Value, fallback jsonvalue.Value) jsonvalue.Value`, and the typed forms `BoolVariation`, `StringVariation`, `FloatVariation`, `IntVariation`, `JSONVariation` (below). |
| `variationDetail` | `(*Client).VariationDetail(...) Detail[jsonvalue.Value]`, and `BoolVariationDetail`, `StringVariationDetail`, `FloatVariationDetail`, `IntVariationDetail`, `JSONVariationDetail`, each returning `Detail[T]{Value T; VariationIndex int; Reason Reason}`. `VariationIndex` is `-1` for none. |
| `status` | `(*Client).Status() Status` with `ETag string` (`""` is none), `LastSyncAt int64` (whole epoch milliseconds, `-1` is none), `Stream string`, `LastError string` (`""` is none). One consistent snapshot, no I/O. |
| `close` | `(*Client).Close()`. Idempotent and safe from many goroutines (every caller returns after the same shutdown). From a callback running on an SDK goroutine it starts the shutdown and returns without waiting. |
| `url` | `toggleslash.New`'s first argument, a plain string. Trailing `/` characters are stripped. No default; required. |
| `sdkKey` | `toggleslash.New`'s second argument, a plain string. Sent only as `Authorization: Bearer <key>`. No default; required. |
| `stream` | `WithStream(bool) Option`. Default `true`. An unexported `*bool` is nil until the option is applied, which is how "not given" differs from `false`. |
| `pollInterval` | `WithPollInterval(time.Duration) Option`. Default 30 s. A non-positive value is the same as not given. |
| `flushInterval` | `WithFlushInterval(time.Duration) Option`. Default 30 s. A non-positive value is the same as not given. |
| `requestTimeout` | `WithRequestTimeout(time.Duration) Option`. Default 10 s. A non-positive value is the same as not given. |
| `hooks.afterEvaluation` | `WithAfterEvaluation(func(EvaluationEvent)) Option`. Default none. Runs on the caller's goroutine, synchronously, after the answer is known and the usage count taken. It returns nothing; a panic in it is recovered and goes to `onError`. |
| `onError` | `WithOnError(func(error)) Option`. Default none. The value is an `*toggleslash.Error` with `Op` and `Code` and fixed text (never a URL or a context). A panic in it is recovered and ignored. |
| `accessor shape` | Both. One method per JSON kind, with the JSON kind in the name: `Variation` (any `jsonvalue.Value`, the fallback decides nothing but a `null` accepts any kind), `BoolVariation`, `StringVariation`, `FloatVariation` (float64), `IntVariation` (int64, the integer accessor, so the SDK advertises `integer-accessor`), `JSONVariation` (array or object kinds only). Each has a `...Detail` form. Go generics cannot put a type parameter on a method, so the typed forms are methods with fixed types and `Detail[T]` is the one generic result type. |
| `onError thread` | The goroutine that hit the failure: the sync goroutine for ruleset and stream failures, the flush goroutine (or the closing goroutine, for the final flush) for usage failures, the caller's goroutine for a hook failure or an evaluation failure raised during `Variation`. No SDK lock is held while it runs. |

### Closed sets and "not given"

The reason kinds (`OFF`, `FALLTHROUGH`, `TARGET_MATCH`, `RULE_MATCH`, `PREREQUISITE_FAILED`, `ERROR`), the error codes (`NOT_READY`,
`INVALID_CONTEXT`, `FLAG_NOT_FOUND`, `MALFORMED_FLAG`, `EVALUATION_FAILED`, `TYPE_MISMATCH`, and the spec's others), the stream states (`off`, `connecting`,
`connected`, `disconnected`, `closed`) and the `lastError` codes (`http_<status>`, `timeout`, `bad_ruleset`, `network`) are Go strings with the contract's
exact spelling. The first three sets are also exported as untyped string constants (`ReasonOff`, `ErrorFlagNotFound`, `StreamConnected`, ...) so a caller can
compare without typing the literal. `Reason` is `struct{ Kind string; Code string; RuleIndex int; FlagKey string }` with `Code` set only for `ERROR`, `RuleIndex`
`-1` when none and `FlagKey` set only for `PREREQUISITE_FAILED`.

Options are applied to an unexported config struct whose `stream` field is a `*bool` and whose durations are `time.Duration`. A duration of zero is "not
given" because zero and negative both fall back to the default (contract section 2), so no pointer is needed there; `stream` needs one because `false` is meaningful.

## 2. Concurrency (contract section 6)

- **The ruleset and its ETag** live in one immutable `snapshot{ruleset *eval.Ruleset; etag string}` behind an `atomic.Pointer[snapshot]`. A sync builds a
  new snapshot, then one `Store` replaces both, so an evaluation that loads the pointer once sees a ruleset and the ETag that belongs to it. `variation`
  loads the pointer once and uses that snapshot for the whole call, hook event included.
- **Goroutines.** `New` starts two: the sync loop (the first load, then the stream or the poll, with the backoff) and the flush loop (a timer every
  `flushInterval`). `Close` closes a `done` channel, cancels the context every sync request derives from, and a single shutdown goroutine waits for both loops and for
  a flush in progress, then does the final flush and closes `closedCh`. The first caller wins a `CompareAndSwap` on `closing` (not a `sync.Once`: a second caller
  must wait for the same shutdown, not run it). Every other caller waits on `closedCh`.
- **Locks.** `statusMu` guards the status fields (etag, last sync, stream state, last error, ready state) and is held only for field reads and writes, so
  `Status()` is one snapshot; the snapshot pointer is swapped while holding it so a `Status()` never reports an ETag the evaluator does not have. `usageMu`
  guards the pending-count map and is held only to add a count or to drain the map. `flushMu` serialises flushes and is held across a network call, but nothing
  on the evaluation path takes it. `variation` takes `usageMu` for one map increment and nothing else, so no evaluation waits for a request.
- **No lock across callbacks.** The hook runs on the caller's goroutine after `variation` released `usageMu`; `onError` is called after the failing code
  released `statusMu`, `usageMu` and `flushMu` (the failure text is copied out first).
- **Re-entrancy.** A callback on an SDK goroutine (`onError` from the sync or flush goroutine) that calls `Close` or `Ready` would deadlock if it waited for
  the loop it runs on. The SDK goroutines register their goroutine id (read once from `runtime.Stack`) in a small set; `Close` and `Ready` check the caller's id
  against it and, when it matches, `Close` starts the shutdown and returns, and `Ready` returns the current answer without blocking. The id is read only in
  `Close` and `Ready`, never on the evaluation path.
- **Process exit.** The SDK's goroutines do not keep a Go program alive (Go exits when `main` returns), so a client never closed does not block exit.
  Tests hold this with a subprocess that creates a client and returns.

## 3. HTTP and SSE

- **Client.** `net/http` with its own `http.Client` per SDK, `CheckRedirect` returning `http.ErrUseLastResponse` (a 3xx is `http_3xx`, and `Authorization`
  is never resent to a redirect target). No cookie jar. The transport is a clone of the default, with `DisableCompression` left off (so `net/http` adds
  `Accept-Encoding: gzip` itself; `RUN.md` tabulates the headers it adds).
- **Bounding a whole request.** Each ruleset and usage request runs under `context.WithTimeoutCause(ctx, requestTimeout, errTimeout)`. The context stays
  live until the body is fully read (`io.ReadAll` before the cancel), so a server that sends headers and then stalls is cut off, as is one that trickles. A
  request cancelled by `Close` is reported as nothing.
- **Stream open.** The open is bounded by `requestTimeout` through a timer that cancels the request context with a cause until the response headers arrive; the
  timer is stopped at the headers. After that a watchdog (`time.AfterFunc`, 60 s by default, a test seam shortens it) is reset on every read that returned data
  (a keep-alive comment is data); if it fires it cancels the request. A stalled read ends the stream and counts as not connected.
- **SSE parser.** Hand-written, a byte state machine over the chunks as they arrive: strips one leading BOM, treats LF, CRLF and a lone CR as line ends
  (a CR at the end of a chunk waits for the next byte to decide), ignores comment lines (`:`), splits a field at the first colon and strips one leading space of
  the value, reads only `event` and `data`, joins several `data` lines with LF, drops an event with no `data` lines, names an event with no `event` field
  `message`, and discards an incomplete event at the end of the stream. A stream line that is not valid UTF-8 is decoded lossily by the same decoder JSON uses.
- **Retry-After.** Whole decimal seconds only, capped at 300 s, held separately for the ruleset routes and for usage reports.

## 4. Dependencies

Runtime: none. `net/http`, `crypto/sha256`, `sync`, `sync/atomic`, `time`, `strconv`, `unicode/utf16` and `math` are enough. Test: none beyond `testing`, and the loopback
fake server is a plain `net.Listener` with `http.Server`, recording each request's headers. Lint: `go vet`, `golangci-lint` and `gofmt`
(listed in section 6).

## 5. JSON

`encoding/json` cannot be used: it errors on `1e999` and replaces a lone surrogate with U+FFFD, and the spec forbids both (language note `go.md`). So `jsonvalue`
is a small reader and writer of its own.

- **Strings are WTF-8** held in a Go `string`: a code point that is not a surrogate is ordinary UTF-8, a lone surrogate is its generalised three-byte form
  (`ED A0..BF xx`), and a valid `😀` pair is combined into the four-byte form. Ordering and hashing convert to UTF-16 code units or code points where
  the spec asks; the bucket hash writes a lone surrogate as U+FFFD (spec "Bucketing").
- **Numbers** are read with `strconv.ParseFloat(..., 64)`, ignoring `ErrRange` so `1e999` is `+Inf`; `9007199254740993` reads as `...992`; `-0` stays `-0`.
- **Bytes from the wire** (a ruleset body, a stream tag) are first decoded as UTF-8 with the WHATWG replacement of each maximal invalid subpart by U+FFFD (the
  decoder is written by hand: `strings.ToValidUTF8` replaces a whole run with one). A leading BOM is skipped. A duplicate object key keeps the last value
  (position of the first). Depth is limited to 1000 (the contract asks for at least 512); beyond it the document is rejected and counts as `bad_ruleset`.
- **The public value type** is `jsonvalue.Value` (kind, bool, float64, string, array, object). Contexts, fallbacks, served values and hook events use it, so
  `1e999` and a lone surrogate survive the whole way. `jsonvalue.Parse([]byte)` reads raw JSON, `jsonvalue.From(any)` builds one from native Go values
  (`nil`, `bool`, numbers, `string`, `[]any`, `map[string]any`), and constructors (`Null`, `Bool`, `Float`, `String`, `Array`, `Object`) build one directly.
  A native Go string with invalid UTF-8 given to `From` gets U+FFFD in place of each bad subpart; the SDK never panics on one.

## 6. Minimum runtime and toolchain

- **Minimum**: Go 1.26 (`go 1.26` in `go.mod`). It is the oldest release still supported next to 1.27 (Go supports the two newest), so a consumer on a
  supported toolchain can use the module. The module uses generics, `atomic.Pointer` and `context.WithTimeoutCause`, all older than that.
- **Pinned toolchain** (`mise.toml`): `go 1.27.3` (the newest `mise ls-remote go` lists; it hides two newer releases through its `minimum_release_age`, per the
  language note), `zig 0.17.1` (the C compiler `-race` needs, `CC="zig cc"`), `golangci-lint 2.15.0`.
- **Test framework**: `testing` from the standard library (the language's own, no assertion library). **Build tool**: `go build` and `go test`. **Lint and
  format**: `go vet`, `golangci-lint` with `.golangci.yml`, and `gofmt -l`; `usestdlibvars` is dropped (language note).
- **Integer accessor**: `int64`, with the fractional and range rules of contract section 4 (a fraction, or a value outside int64, is `TYPE_MISMATCH`; `2.0` is 2).

## 7. Package layout

```
sdks/go/
  go.mod                      module github.com/madangryscientist/toggleslash/sdks/go, go 1.26
  jsonvalue/                  the public JSON value, reader, writer, lossy UTF-8 and WTF-8 helpers
  internal/eval/              the evaluator: validate/load, evaluate, clauses, segments, bucketing, semver, dates, the pattern parser and Pike VM
  *.go                        package toggleslash: client, options, status, sync loop, SSE parser, HTTP helpers, usage reporter, errors, goroutine id
  *_test.go                   the vectors test, the fake loopback server and the native tests (next to the code they test)
  test-service/               main.go and README.md: a `package main` the conformance harness drives
  bin/{lint,test,test-service}
  mise.toml  .golangci.yml  .gitignore  README.md  DESIGN.md  RUN.md
```

The evaluator is `internal/eval` and imports only `jsonvalue`; it has no network code. The test service is a `main` package inside the module, so `go build ./...`
and `go vet ./...` compile it with the tests; `bin/test` builds it last, to `.build/test-service`, and `bin/test-service` runs that binary and rebuilds only when it is missing.

## 8. Disagreements recorded

None so far. If a reference implementation under `packages/engine` or `apps/server/ToggleSlash.Evaluation` disagrees with the spec, it is listed here with both readings.
