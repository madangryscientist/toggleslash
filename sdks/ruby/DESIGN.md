# ToggleSlash Ruby server SDK: design

This is the design the SDK in this directory was built to (phase 1 of
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md)). The contract is
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), evaluation is
[`docs/evaluation.md`](../../docs/evaluation.md) and the harness protocol is
[`docs/sdk-test-service.md`](../../docs/sdk-test-service.md). Section numbers below are the contract's.

The gem is `toggleslash` and the namespace is `Toggleslash` (`require "toggleslash"`). Nothing is registered
under that name.

## 1. The mapping table (contract 2)

Durations are `Numeric` seconds (an `Integer` or a `Float`). `create` is a module function,
`Toggleslash.create(...)`, that returns a `Toggleslash::Client`. Ruby names are `snake_case`, and every
argument is a keyword.

| Name | Ruby | Default and meaning |
| --- | --- | --- |
| `create` | `Toggleslash.create(url:, sdk_key:, stream: true, poll_interval: 30, flush_interval: 30, request_timeout: 10, hooks: nil, on_error: nil)` returns a `Client`. | Starts two threads and returns at once; it does not wait for the network. |
| `ready` | `client.ready -> true or false` | Blocks until the first ruleset attempt has settled, bounded by `request_timeout` (plus a 1 s grace for work the SDK does not control, such as a name lookup). True if a ruleset is held. The answer is fixed at the first settle. |
| `variation` | `client.variation(flag_key, context, fallback)` | Generic: the expected kind is read off the class of `fallback` (`true` or `false`, `Integer` or `Float`, `String`, `Array`, `Hash`; `nil` accepts any kind). |
| `variationDetail` | `client.variation_detail(flag_key, context, fallback) -> Detail` | `Detail` has `value`, `variation_index` (`Integer` or `nil`) and `reason`. Also `client.int_variation_detail`. |
| `status` | `client.status -> Status` | A frozen `Data`: `etag`, `last_sync_at` (a UTC `Time` or `nil`), `stream` (a `StreamState` string), `last_error` (a `String` or `nil`). One snapshot. |
| `close` | `client.close` | Stops, joins (bounded), sends the final usage flush. Safe twice and from many threads. Returns within about twice `request_timeout`. |
| `url` | `url:` (required, `String`) | The server's base URL. Trailing `/` are removed. |
| `sdkKey` | `sdk_key:` (required, `String`) | A server key. |
| `stream` | `stream: true` | `true` or `false`. Off means poll only. |
| `pollInterval` | `poll_interval: 30` | Seconds. |
| `flushInterval` | `flush_interval: 30` | Seconds. |
| `requestTimeout` | `request_timeout: 10` | Seconds, the whole request, body included. |
| `hooks.afterEvaluation` | `hooks: Toggleslash::Hooks.new(after_evaluation: callable)`; default `nil` | `callable.call(event)` with an `EvaluationEvent`, on the caller's thread. The return value is ignored (a Thread or a future it returns is not awaited); an object that does not respond to `call` is ignored. A `Hash` with an `:after_evaluation` key is accepted too. |
| `onError` | `on_error: callable`; default `nil` (ignore) | `callable.call(error)` with a `Toggleslash::Error` (a `StandardError` with a `code`). |
| `accessor shape` | Both: the generic `variation` and `variation_detail` (the kind comes from the class of the fallback), and `int_variation` and `int_variation_detail` for the integer accessor. | There is no `bool_variation`, `string_variation` or `float_variation`: the generic method already compares the fallback's kind. An `Integer` or `Float` fallback to the generic method is the `number` kind with no integer rule (the harness `number` accessor), while `int_variation` applies contract 4's fractional and signed 64-bit rules. The `json` accessor is the generic method with an `Array` or `Hash` fallback. A `nil` fallback accepts any kind. |
| `onError thread` | The SDK's own thread for what that thread did (a failed ruleset or stream attempt on `toggleslash-sync`, a failed usage request on `toggleslash-flush`); the caller's thread for a hook that raised and for `EVALUATION_FAILED`; the thread that called `close` for a usage request in the final flush and for counts lost at close, except a `close` made on the SDK's own thread (from `on_error`), whose final flush runs on a `toggleslash-close` thread, which reports those there. | No SDK lock is held while it runs. |

A fallback whose class is not a JSON value (an arbitrary object, a `Symbol`, a `Time`) never matches a served
value and answers `ERROR TYPE_MISMATCH`: it is the caller's fallback, so it is still returned unchanged.

**The closed sets keep their spelling.** `Toggleslash::ReasonKind`, `Toggleslash::ErrorCode`,
`Toggleslash::StreamState` and `Toggleslash::LastError` are modules of frozen string constants whose names
and values are the canonical spelling (`ReasonKind::RULE_MATCH == "RULE_MATCH"`, `ErrorCode::NOT_READY ==
"NOT_READY"`, `StreamState::CONNECTED == "connected"`, `LastError::TIMEOUT == "timeout"`). A reason or a status
carries the plain string, so it compares equal to the spelling in the contract. `Reason` is a `Data` with
`kind`, `code` (the `ErrorCode` for an `ERROR`, `nil` otherwise), `rule_index` and `flag_key`. `Status#last_error`
is a plain `String` (`"http_503"`, `"timeout"`, `"bad_ruleset"`, `"network"`); `http_<status>` is built.

**"Not given" versus false or zero.** Ruby keyword defaults carry the defaults themselves
(`stream: true`, `poll_interval: 30`), so "not given" is the default. A caller who passes `stream: false` gets
off, and passing `nil` for an interval also gets the default. A non-positive, non-numeric or non-finite
duration (`true` and `false` included) falls back to the default (contract 2, SHOULD). Only the exact value
`false` turns the stream off.

## 2. Concurrency (contract 6)

- **The ruleset and its ETag** live in one frozen `Held` (`ruleset`, `etag`) referenced by one instance
  variable. A swap is one assignment of that reference (atomic in MRI, JRuby and TruffleRuby alike), and an
  evaluation reads the variable once, so it sees the old pair or the new pair. The `etag` and `flagVersion` a
  hook reports come from that same read.
- **The status** is one frozen `Held`-like `State` (`last_sync_at`, `stream`, `last_error`) replaced under a
  short `@state_lock` (a `Mutex`). The lock covers a few assignments and never an I/O call or a callback.
  `status` reads the held pair and the state together under that lock, so it is a snapshot, and a stream
  failure sets `last_error` and the stream state in one replacement, so a reader never sees the failure's
  `last_error` beside `connected` (contract 6.5).
- **Threads the SDK starts** (contract 6.6): `toggleslash-sync` runs the first load and then the loop of 7.3,
  `toggleslash-flush` runs the usage flush on `flush_interval`. Ruby kills every other thread when the main
  thread exits, so a client that is never closed does not keep the process alive. Both threads set
  `report_on_exception = false`.
- **Locks.** `@state_lock` (above), `@usage_lock` (the counts hash, held for an add or a swap of the hash and
  never during I/O), `@flush_lock` (serialises flushes; only the flush thread and `close` take it, never an
  evaluation), `@close_lock` (decides who performs the shutdown). Evaluation takes `@usage_lock` for the length
  of one hash update and no other lock. A request in flight holds none of them, so a hung server cannot slow
  an evaluation down.
- **Callbacks.** The hook and `onError` run with no SDK lock held. `ready` called from the SDK's own thread
  (`Thread.current` is the sync or flush thread) returns at once with the state held. `close` from an SDK
  thread does not wait for that thread's work (contract 6.4): it sets the closing flag and the `closed` state and
  aborts the requests in flight, hands the joins and the final flush to a thread named `toggleslash-close`, which
  ends when shutdown ends, and returns at once. A second `close` on an SDK thread returns at once too.
- **Close.** `close` sets the closing flag and the stream state to `closed` under `@state_lock`, sets an
  `Event` (a mutex and a condition variable) both loops wait on, so no timer outlives it, closes the sockets of
  the in-flight ruleset and stream requests (below), joins the sync and flush threads with a bound of
  `request_timeout`, takes `@flush_lock` (waiting for a flush already running, bounded) and runs the final
  flush, then settles an unsettled `ready` as false. A second or concurrent caller waits for the same shutdown
  on an `Event`, bounded by four times `request_timeout`, and returns. A thread stuck in a name lookup cannot
  be interrupted; `close` does not wait for it past its bound.
- **The global interpreter lock.** MRI has one, so the ruleset swap and the counts would be safe on assignment
  alone; the locks above are for the other runtimes and for correctness of the multi-step updates. The stress
  test (`test/concurrency_test.rb`) stands in for a race detector, which Ruby has none of.

## 3. HTTP and SSE

- **HTTP client.** A small HTTP/1.1 client over `Socket` (and `OpenSSL::SSL::SSLSocket` for `https`, with
  the default certificate store, peer verification and a `post_connection_check` of the host), written in
  `lib/toggleslash/http.rb`. No size is capped (contract 7.1, "there is no size cap"): the whole-request deadline bounds a head, a line and a body. A port outside 1 to 65535 is refused.
  `Net::HTTP` is not used because it adds a `User-Agent`, an `Accept-Encoding` and follows nothing the contract
  asks it to, and its timeouts are per read, not per request. One new connection per request, `Connection:
  close`, no redirects, no proxy lookup. The headers it sends are `Host`, `Connection`, `Content-Length` (a
  POST), `Authorization`, `Accept`, `If-None-Match` and `Content-Type`, and nothing else.
- **`request_timeout` bounds the whole request, body included.** Every step (connect, the TLS handshake, the
  write, each read of the head and of the body) is non-blocking and waits with `IO.select` for at most the time
  left against one monotonic deadline set when the request starts, so a server that trickles a body runs out of
  time and the request is a `timeout`. Every wait also watches a self-pipe: `close` writes to the pipe of
  every in-flight ruleset or stream request, the `IO.select` wakes, and the client turns it into "aborted".
  (Closing a socket from another thread does not reliably wake a thread blocked in `IO.select`, so it is not
  used.)
- **The stream** is opened with the same deadline for the connect and the head. Once the head has arrived the
  watchdog takes over: every read waits at most `stream_watchdog` (60 s; a seam shortens it) for a byte, and
  each read that returns bytes (a `: keep-alive` included) restarts it. A read that runs out is `timeout`. A
  `Content-Length: 0` answer on the stream is `http_200`; a 200 that ends cleanly with no bytes leaves
  `last_error` alone. A
  status other than 200 is `http_<status>`; a 429 reads `Retry-After`. The body is decoded as `chunked`,
  `Content-Length` or read-until-close, as the head says.
- **SSE.** A short hand-written parser, `lib/toggleslash/sse.rb`, to the WHATWG rules the contract names: it
  decodes UTF-8 incrementally (a multi-byte character split across reads is held back, an invalid byte becomes
  U+FFFD), ends a line on LF, CRLF or a lone CR (a CR at the end of a chunk waits for the next byte to see
  whether an LF follows), ignores a line that starts with `:`, reads `event` and `data` (one leading space
  removed from a value, several `data` lines joined with LF), drops an event with no `data` line, names an event
  `message` when no name was given, and discards an event still incomplete at the end of the stream. The SDK
  acts on `connected` and `changed`.

## 4. Dependencies

| Dependency | Kind | Why |
| --- | --- | --- |
| none | runtime | `socket`, `openssl` (SHA-256 and TLS), `uri` and `thread` ship with Ruby and cover everything. The JSON reader is the SDK's own (section 5). |
| `rubocop` 1.92.2 | lint | One tool for lint and format check, pinned in `mise.toml` (`gem:rubocop`). |
| `minitest` | test | Ships with Ruby (a bundled gem), so the tests need nothing installed. |
| `ruby` 4.0.9 | toolchain | Pinned in `mise.toml` for the checks; the gem itself supports 3.3 and later. |

## 5. JSON

The standard `json` gem cannot hold what the spec requires (a lone surrogate in a `String`, a literal such as
`1e999` as an infinity without a warning, a leading byte order mark, the WHATWG replacement of invalid bytes),
so `lib/toggleslash/json.rb` is a small hand-written reader and writer:

- **Numbers** are binary64. An integer literal of 15 digits or fewer is an `Integer`; a longer one, and any
  literal with a fraction or an exponent, is converted through `Rational(...).to_f`, which rounds correctly
  and, unlike `Float("1e999")`, does not print a warning under `ruby -w`. A value whose magnitude is beyond the
  range is decided from its exponent without building the number. `1e999` is `Float::INFINITY`;
  `9007199254740993` is `9007199254740992.0`; an integer literal whose double is below 2^53 in magnitude stays
  an `Integer`; `-0` is `-0.0`. `NaN`, `Infinity` and a leading `+` or zero are errors.
- **Strings** are UTF-8 `String`s. A lone surrogate escape (`"\ud800"`) is kept by writing its three-byte
  generalised UTF-8 form (WTF-8) into the string; Ruby holds those bytes, `valid_encoding?` is false for it, and
  the evaluator reads it as the one UTF-16 code unit it stands for. A valid escaped pair (`"😀"`) is
  combined into one character. Invalid bytes in the input are replaced by `String#scrub`, whose output matches
  the WHATWG UTF-8 decoder (checked in `test/json_test.rb`), and a leading byte order mark is skipped.
- **A duplicate key** in an object keeps the last value. Nesting is read to a depth of 1000 and deeper is a
  parse error (never a crash: a `SystemStackError` is caught too).
- **Output** to the harness is `Toggleslash::Json.generate`, with `ensure_ascii` behaviour: every code unit above
  U+007F leaves as `\uXXXX`, so a lone surrogate leaves as `\ud800`. An infinity is written as `1e999` (valid
  JSON that reads back as an infinity) and a NaN as `null`.

The public API takes a context as a `Hash` (string keys, as the JSON reader returns, or `Symbol` keys, which
are read as the string of the same name) whose values are `nil`, `true`, `false`, `Integer`, `Float`, `String`,
`Array` and `Hash`. The vectors' `1e999` and lone surrogates reach the SDK through the same reader in the test
service, so they are kept. A number that is not a finite binary64 never matches (the spec), an `Integer` is
read as the nearest double, and a `Rational` or a `BigDecimal` (any real `Numeric`) is read through `to_f` as a
number. Any other object (a `Symbol`, a `Complex`, a `Time`) is a present attribute of no JSON kind: it
matches nothing.

## 6. Minimum runtime and toolchain

- **Runtime: MRI** (CRuby), the mainstream implementation. The code is plain Ruby and uses no C extension, so
  it is expected to run on JRuby and TruffleRuby, which this run does not test.
- **Minimum Ruby 3.3.** On the run date (2026-11-12) Ruby 3.3 is in security maintenance (end of life expected
  2027-03-31) and 3.2 is end of life; the 4.0 branch is in normal maintenance
  (<https://www.ruby-lang.org/en/downloads/branches/>, read on the run date). 3.3 has `Data` and `Queue#pop` with a
  timeout, which the code uses (the name lookup runs on a helper thread and is waited for with that timeout). The checks run on 4.0.9,
  the latest stable release (`mise latest ruby`), and the full suite is also run once on 3.3.
- **Lint and format: RuboCop** (the mainstream Ruby tool for both, one config). **Test framework: Minitest**,
  which ships with Ruby, so `bin/test` needs nothing installed. **Build tool: none for the checks**: `bin/test`
  compiles every source, test and test-service file under `ruby -w` as its build step, with a warning counting
  as a failure.
  A `toggleslash.gemspec` makes `gem build` and a Bundler `path:` entry work from a checkout.
- **Warnings are errors in the tests.** `bin/test` runs `ruby -w` and a test helper prepends a module to
  `Warning` that raises, so a warning from the SDK fails the run. A duration the caller gives is capped at
  31,536,000 s (a year), and a non-finite one falls back to the default.
- **The integer accessor** uses Ruby's `Integer`, bounded to signed 64 bits by the SDK (an `Integer` is
  unbounded): a served number that is not finite, has a fractional part, or lies outside -2^63 to 2^63-1 is
  `TYPE_MISMATCH`; `2.0` is the integer `2`.
- **No type checker is run.** The public methods carry YARD-style comments only.

## 7. The package layout

```
sdks/ruby/
  mise.toml  .rubocop.yml  .gitignore  toggleslash.gemspec  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  lib/toggleslash.rb             requires, create
  lib/toggleslash/
    types.rb       Reason, Detail, Status, EvaluationEvent, Hooks, Error, the closed sets
    client.rb      Client: options, evaluation, the loops, usage, close
    http.rb        the HTTP/1.1 client: whole-request deadline, stream open and read, abort self-pipe
    sse.rb         the SSE parser
    usage.rb       the counts and the batches
    json.rb        the JSON reader and writer
    text.rb        UTF-16 code units and code points of a String, WHATWG decoding
    event.rb       a settable flag with a timed wait, and the Clock the tests replace
    eval/          the evaluator (no network, no threads)
      ruleset.rb  evaluator.rb  pattern.rb  semver.rb  dates.rb  bucket.rb  context.rb  reference.rb  num.rb
  test/            Minitest: vectors, evaluator units, client, sync, usage, http, tls and concurrency tests
                   against a loopback fake server
  test-service/    main.rb and README.md (syntax-checked by bin/test with the tests)
```

`bin/test` runs `test/run_all.rb` under `ruby -w` with `lib` on the load path.

## 8. Evaluator decisions (phase 2)

- **Strings.** A Ruby `String` is a sequence of bytes with an encoding. The spec compares in UTF-16 code units
  for equality, `contains`, `startsWith`, `endsWith` and order, and in code points for patterns. For two valid
  strings the equality, containment and prefix tests are done on bytes (UTF-8 is self-synchronising, so the
  result is the same as by units), and order is done on bytes when both are ASCII. Everything else goes through
  `Text.units`, which decodes a string into an array of UTF-16 code units (a character above U+FFFF becomes its
  pair, a WTF-8 surrogate its one unit, an invalid byte U+FFFD by the WHATWG rules). For patterns,
  `Text.code_points` joins an adjacent high and low surrogate into one code point; a lone surrogate stays.
- **Hashing.** The string is hashed as UTF-8 bytes where a lone surrogate (three WTF-8 bytes) is replaced by the
  bytes of U+FFFD.
- **Kind names** are checked with a character walk, never a regular expression.
- **`matches`** is the spec's Pike VM in `eval/pattern.rb`. No `Regexp` and no `String#match`, `=~`, `scan`,
  `gsub` or `sub` with a pattern appears under `lib/toggleslash/eval/`.
- **Attribute reads** use own keys of a `Hash` only. `true` and `false` are tested before a number everywhere.
- **Numbers.** `Integer` attributes are converted to binary64 with `Rational#to_f` beyond 2^53 (it rounds to
  nearest and does not warn); `Integer#to_f` of a huge integer warns under `-w`.

### Disagreements with other implementations

None found so far; any that turns up is recorded here with both readings and the file and line of each.
