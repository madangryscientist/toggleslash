# ToggleSlash server SDK for Ruby: design

This is phase 1 of `docs/sdk-authoring/PROMPT.md`, written before any code. The contract is
`docs/sdk-contract.md`; the evaluator is `docs/evaluation.md`.

## Mapping table (contract section 2)

Durations are `Numeric` seconds (an `Integer` or a `Float`), the usual Ruby idiom, never milliseconds.
Options are keyword arguments to `ToggleSlash.create`. A closed set is a `Symbol` that keeps the canonical
spelling (`:RULE_MATCH`, `:connected`, `:http_503`).

| Name | In Ruby |
| --- | --- |
| `create` | `ToggleSlash.create(url:, sdk_key:, stream: true, poll_interval: 30, flush_interval: 30, request_timeout: 10, hooks: nil, on_error: nil)` returns a `ToggleSlash::Client` and starts its two threads. It does not wait for the network and it never raises: an option that is missing or of the wrong type takes its default (a missing `url` makes every attempt a `network` error). `ToggleSlash::Client.new` takes the same keywords. |
| `ready` | `client.ready` blocks the calling thread until the first load has finished and returns `true` or `false`. The answer is fixed when the first attempt settles. After `close` it is `false` if it had not settled. It is bounded by `request_timeout`. |
| `variation` | `client.variation(flag_key, context, fallback)`: the generic accessor. The served value must be the same JSON kind as `fallback` (a `nil` fallback accepts any kind). |
| `variationDetail` | `client.variation_detail(flag_key, context, fallback)` returns a `ToggleSlash::Detail` (`value`, `variation_index`, `reason`). |
| `status` | `client.status` returns a frozen `ToggleSlash::Status` (`etag`, `last_sync_at`, `stream`, `last_error`), one consistent snapshot. `etag` is a `String` or `nil`, `last_sync_at` is a `Time` or `nil`, `stream` is one of `:off :connecting :connected :disconnected :closed`, `last_error` is `nil` or one of `:timeout :bad_ruleset :network` or `:http_<status>`. |
| `close` | `client.close` stops both threads, joins them, sends the final usage flush and returns `nil`. Safe to call twice and from many threads at once. |
| `url` | keyword `url:`, required (a `String`). Trailing `/` characters are removed. Only `http://` and `https://` are understood. |
| `sdkKey` | keyword `sdk_key:`, required (a `String`). |
| `stream` | keyword `stream:`, default `true`. Only `false` turns the stream off (`nil` means the default), so "not given" and "false" differ. |
| `pollInterval` | keyword `poll_interval:`, default `30` seconds. A `nil`, non-numeric or non-positive value takes the default. |
| `flushInterval` | keyword `flush_interval:`, default `30` seconds. Same rule. |
| `requestTimeout` | keyword `request_timeout:`, default `10` seconds. Same rule. Bounds a whole request, body included. |
| `hooks.afterEvaluation` | keyword `hooks:`, a `Hash` with `:after_evaluation` (or any object that responds to `after_evaluation`), a callable taking one `ToggleSlash::EvaluationEvent` (`flag_key`, `context`, `value`, `variation_index`, `reason`, `etag`, `flag_version`). Default none. |
| `onError` | keyword `on_error:`, a callable taking one `Exception`. Default: ignore. |
| `accessor shape` | Both. Generic `variation` and `variation_detail` (kind taken from the fallback) and typed `bool_variation`, `string_variation`, `number_variation`, `int_variation` and `json_variation`, each with a `_detail` twin. The typed ones check the served kind against their own kind, so a `nil` fallback is allowed. `int_variation` applies the integer rule of contract section 4: a fraction or a number outside signed 64 bits is `:TYPE_MISMATCH`, and `2.0` is `2`. `json_variation` takes a served array or object (the kind of its fallback, or either when the fallback is `nil`). |
| `onError thread` | The thread that met the failure. Ruleset and stream failures: the SDK's sync thread. A failed usage request: the SDK's flush thread, or the thread that called `close` for the final flush. A hook that raised, or an evaluation that raised: the caller's thread, inside `variation`. No SDK lock is held when it is called. |

The reason is a `ToggleSlash::Reason` (`kind`, and `rule_index`, `flag_key` or `code` when the kind carries
them, `nil` otherwise). `kind` is `:OFF :PREREQUISITE_FAILED :TARGET_MATCH :RULE_MATCH :FALLTHROUGH :ERROR`.
For `:ERROR`, `code` is `:INVALID_CONTEXT :FLAG_NOT_FOUND :SEGMENT_NOT_FOUND :MALFORMED_FLAG
:MALFORMED_SEGMENT :PREREQUISITE_CYCLE :PREREQUISITE_DEPTH_EXCEEDED :NOT_READY :TYPE_MISMATCH
:EVALUATION_FAILED`. Symbols keep the spelling exactly.

A context is a `Hash` (string or symbol keys) in either shape of `docs/evaluation.md`. The test service
passes what `ToggleSlash::Json.parse` returns, which keeps `1e999` and a lone surrogate. See "JSON". The
SDK adds no context builder.

Defaults and "not given": a Ruby keyword argument has a default, so an option the caller leaves out is
the default, and passing `false` for `stream:` is different from leaving it out. A `nil` is read as "not
given" for every option. An interval of `0`, a negative number or a non-number is read as "not given"
(contract section 2: not specified, SHOULD fall back).

## Concurrency (contract section 6)

- **State.** One frozen `State` value (`ruleset`, `etag`, `last_sync_at`, `stream`, `last_error`) held in
  one instance variable. A ruleset swap builds a new `State` and replaces the variable inside a tiny
  `Mutex` (`@state_lock`) that guards only the reference read and the reference write, never a network
  call. So the ruleset and the ETag that names it swap together, a reader sees the old pair or the new
  one, and `status` is one snapshot.
- **Threads.** Two daemon-style threads (Ruby kills non-main threads when the process exits, so a client
  that is never closed does not keep it alive, contract 6.6): the **sync thread** (first load, stream,
  poll, backoff) and the **flush thread** (usage counts every `flush_interval`). Both wait on a
  `ToggleSlash::Cancel`: a `Mutex` and `ConditionVariable` for timed waits, and a lazily made self-pipe
  so a thread blocked in `IO.select` on a socket wakes the moment `close` cancels.
- **Close.** The first `close` caller is the leader: it cancels the lifetime `Cancel`s (which abandons a
  ruleset or stream request but not a usage flush that is running: that finishes, bounded by
  `request_timeout`, as contract 11 step 3 says), joins the sync and flush threads (never joining itself),
  settles `ready` with `false`, then sends the final usage flush. Other callers (and a second call later)
  wait on a condition variable until the leader is done. A `close` from the leader's own thread or from
  either SDK thread (an `on_error` that calls `close`) returns at once, since it cannot wait for a shutdown
  that is waiting for it. Evaluation keeps answering from the ruleset held.
- **Locks.** `@state_lock` (reference swap), `@usage_lock` (add to or swap out the pending map: an
  in-memory hash operation), `@flush_lock` (serialises flushes, held across the usage request, taken only
  by the flush and close paths), `@close_lock`. `variation` takes `@state_lock` and `@usage_lock` for a
  hash operation each and never takes `@flush_lock`, so a usage flush that hangs cannot delay it. No lock
  is held while `after_evaluation` or `on_error` runs.
- **GIL.** MRI's global lock is not relied on for correctness: every shared write is under one of the locks
  above, so the design is the same on a runtime with parallel threads.

## HTTP and SSE

- **HTTP client.** A small HTTP/1.1 client over `Socket` (and `OpenSSL::SSL::SSLSocket` for `https`),
  written because `Net::HTTP` bounds each read and not the whole request (contract 7.1). It makes
  non-blocking connect, write and read calls and waits with `IO.select` against one monotonic deadline, so
  `request_timeout` covers connect, TLS, the request, the headers **and the body**. Each request uses
  `Connection: close` (one request per connection, no pool). The response body is read lazily:
  a `304` or a refused status never reads it, a `200` reads it under the deadline.
- **Headers sent.** Only `Host`, `Connection: close`, `Authorization`, `Accept` (stream), `If-None-Match`,
  `Content-Type` and `Content-Length` (events). No `User-Agent` and no `Accept-Encoding`.
- **Stream.** Opened with the request deadline until the response headers arrive. After that each read waits
  at most the **idle timeout** (60 s; a seam shortens it for the loopback-socket test) for the next byte
  from the socket, so a keep-alive restarts it, and a silent stream is dropped as `timeout`. Chunked
  framing is decoded incrementally.
- **SSE.** A hand-written incremental parser over bytes (line endings never occur inside a UTF-8
  sequence, so splitting bytes is safe and a multi-byte character cut by a read is whole by the time its line
  is complete). It follows WHATWG: LF, CRLF and a lone CR end a line (a trailing CR waits for the next
  read), `:` lines are comments, a leading byte order mark is skipped, `data` lines join with LF, an event
  with no `data` is dropped, the name defaults to `message`, and an unfinished event at the end of the
  stream is discarded.

## Dependencies

Runtime dependencies: **none**. The SDK uses only the Ruby standard library: `socket`, `openssl` (TLS for
`https`; required when the HTTP client loads), `ipaddr` (to tell an IP literal from a name), `digest`
(SHA-256, the standard library has it), `strscan` (the JSON tokenizer), `uri` (to split a URL). Not `json`: see below.

Test and lint dependencies: `minitest` (the test framework, a bundled gem of Ruby), `rubocop` (lint and
format, installed by mise as a gem so its version is pinned in `mise.toml`).

## JSON

The standard `json` gem cannot be used: it rejects a lone surrogate (`"\ud800"` raises `incomplete
surrogate pair`) and the vectors need it kept. The SDK has its own parser and generator
(`lib/toggleslash/json.rb`).

- **Numbers** are binary64. An integer literal of at most 15 digits is an `Integer`; any other number is
  read with `Float(text)`, so `9007199254740993` is `9007199254740992.0`, `1e999` is `Infinity` (not an
  error) and `-0.0` and `-0` are `-0.0`. `1.0` stays a `Float` and equals `1`. The evaluator compares
  numbers as binary64 whatever their Ruby class.
- **Strings** are Ruby `String`s in UTF-8, in the **WTF-8** form: a lone surrogate is kept as its own
  three bytes (`ED A0 80` to `ED BF BF`), so the string is not `valid_encoding?` but is byte-exact. A
  `😀` pair becomes the one four-byte code point. Equality is `String#==` (bytes), which is
  the same as UTF-16 equality for WTF-8. Order, `contains`, `startsWith` and `endsWith` are by UTF-16 code
  unit, as the spec says, so they run over a UTF-16BE copy (code point order would differ for a code point
  above U+FFFF against U+E000 to U+FFFF). Hashing replaces a lone surrogate with U+FFFD as the spec says.
- **Contexts** reach the SDK as a `Hash`. A caller's own `Hash` with symbol or string keys and plain
  strings, integers and floats works. A context that carries `1e999` or a lone surrogate (the vectors,
  through the test service) is built by `ToggleSlash::Json.parse`, whose values the evaluator reads as
  they are. That is the "form that keeps them": the SDK's own parser, whose output is plain Ruby values.
- **Generation** (`Json.generate`) writes a lone surrogate as `\ud800`, `Infinity` as `1e999`, and keeps
  the rest as UTF-8, so the test service round-trips every vector.

## Minimum runtime and toolchain

- **Runtime:** MRI (CRuby), the mainstream runtime. **Developed on 4.0.9**, the current stable release
  (`mise ls-remote ruby`, ruby-lang.org release page, 2026-11-10). **Minimum 3.3**: the oldest release still in
  support (3.3 is in security maintenance until 2027-03-31; 3.2 reached end of life on 2026-04-01).
  3.3 has `Data.define` and `Data#with`, `Hash#except`, `Time.at(in:)` and the non-blocking socket
  calls this SDK uses. `toggleslash.gemspec` sets `required_ruby_version = ">= 3.3"`.
- **Package manager:** RubyGems with a `toggleslash.gemspec` (the language's mainstream choice); from a
  checkout use `-I sdks/ruby/lib` or `gem build`. Nothing is published.
- **Test framework:** Minitest (the default that ships with Ruby, no extra dependency).
- **Lint and format:** RuboCop (the mainstream Ruby linter and formatter, one tool for both), configured in
  `.rubocop.yml`. Warnings are errors. The tests also fail on any Ruby warning (`Warning.warn` is
  patched in the test helper), Ruby's analogue of `-W error`.
- **Build tool:** none (an interpreted language). `bin/test` runs the tests and the vectors straight from
  `test/`. There is no race detector in Ruby; the stress test is `test_operations_called_from_many_threads_with_close_racing` in `test/client_test.rb`.
- **Integer accessor:** `int_variation` uses Ruby's `Integer`, bounded to signed 64 bits (an unbounded
  type, so the bound is explicit), as the `huge` scenario expects.
- **Pin:** `mise.toml` pins `ruby` and the `rubocop` gem to exact versions.

## Package layout

```
sdks/ruby/
  mise.toml  .rubocop.yml  .gitignore  toggleslash.gemspec  DESIGN.md  README.md  RUN.md
  bin/lint  bin/test  bin/test-service
  lib/toggleslash.rb                 requires everything
  lib/toggleslash/json.rb            parser and generator
  lib/toggleslash/eval/              the evaluator: no network, no regular expression, no JSON
    text.rb  bucket.rb  reference.rb  semver.rb  dates.rb  pattern_parser.rb  pattern.rb
    context.rb  validate.rb  ruleset.rb  clause.rb  evaluator.rb  result.rb
  lib/toggleslash/client.rb          the client, hooks, status
  lib/toggleslash/sync.rb            ruleset sync, stream, backoff, Retry-After
  lib/toggleslash/usage.rb           usage counts
  lib/toggleslash/http.rb            the HTTP client and Response
  lib/toggleslash/sse.rb             the SSE parser
  lib/toggleslash/cancel.rb  clock.rb
  test/                              Minitest: vectors, native tests
  test-service/main.rb  README.md    the conformance test service
```

The module is `ToggleSlash`; the gem is `toggleslash`, required as `require "toggleslash"`. The gemspec sets
no `homepage` and no registry: nothing is published and no name is reserved. If it is ever published, the
repository path the prompt gives is `github.com/madangryscientist/toggleslash/sdks/ruby`.
