# ToggleSlash Python server SDK: design

This is the design the SDK in this directory was built to (phase 1 of
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md)). The contract is
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), evaluation is
[`docs/evaluation.md`](../../docs/evaluation.md) and the harness protocol is
[`docs/sdk-test-service.md`](../../docs/sdk-test-service.md). Section numbers below are the contract's.

The distribution and import name is `toggleslash` (`import toggleslash`). Python's convention for a package in
a repository is the bare name, with no host in it. Nothing is registered under it.

## 1. The mapping table (contract 2)

Durations are `int` or `float` seconds. `create` is a module function, `toggleslash.create(...)`, that returns a
`toggleslash.Client`. Python names are `snake_case`, and every argument is a keyword-only argument.

| Name | Python | Default and meaning |
| --- | --- | --- |
| `create` | `toggleslash.create(*, url, sdk_key, stream=True, poll_interval=30, flush_interval=30, request_timeout=10, hooks=None, on_error=None)` returns a `Client`. | Starts two daemon threads and returns at once; it does not wait for the network. It never raises: a wrong argument type falls back to the default or makes each request a `network` error. |
| `ready` | `client.ready() -> bool` | Blocks until the first ruleset attempt has settled, bounded by `request_timeout` plus a 1 s grace for work the SDK does not control (a name lookup). True if a ruleset is held. The answer is fixed at the first settle. |
| `variation` | `client.variation(flag_key, context, fallback)` | Generic: the expected kind is read off the type of `fallback` (`bool`, `int` or `float`, `str`, `list` or `tuple`, `dict`; `None` accepts any kind). |
| `variationDetail` | `client.variation_detail(flag_key, context, fallback) -> Detail` | `Detail` is a frozen dataclass: `value`, `variation_index` (`int` or `None`) and `reason`. Also `client.int_variation_detail`. |
| `status` | `client.status() -> Status` | A frozen dataclass: `etag` (`str` or `None`), `last_sync_at` (a timezone-aware UTC `datetime` or `None`), `stream` (a `StreamState`), `last_error` (a `LastError` string or `None`). One snapshot. |
| `close` | `client.close()` | Stops, joins (bounded), sends the final usage flush. Safe twice and from many threads. Returns within about twice `request_timeout`. The client is also a context manager (`with toggleslash.create(...) as client:`). |
| `url` | `url=` (required, `str`) | The server's base URL. Trailing `/` are removed. |
| `sdkKey` | `sdk_key=` (required, `str`) | A server key. |
| `stream` | `stream=True` | `True` or `False`. Off means poll only. |
| `pollInterval` | `poll_interval=30` | Seconds. |
| `flushInterval` | `flush_interval=30` | Seconds. |
| `requestTimeout` | `request_timeout=10` | Seconds, the whole request, body included. |
| `hooks.afterEvaluation` | `hooks=toggleslash.Hooks(after_evaluation=callable)`; default `None` | `callable(event)` with an `EvaluationEvent`, on the caller's thread. The return value is ignored, but a future it returns (anything with `add_done_callback`, such as a `concurrent.futures.Future` or an `asyncio.Future`) that later fails sends its exception to `on_error`, and changes neither the answer nor the count; a coroutine it returns is closed unawaited (the SDK runs no event loop); an object that is not callable is ignored. A `dict` with an `"after_evaluation"` key is accepted too. |
| `onError` | `on_error=callable`; default `None` (ignore) | `callable(error)` with a `toggleslash.ToggleSlashError` (an `Exception` with a `code`) or the exception a hook or the evaluator raised. |
| `accessor shape` | Both: the generic `variation` and `variation_detail` (the kind comes from the type of the fallback), and `int_variation` and `int_variation_detail` for the integer accessor. | There is no `bool_variation`, `string_variation` or `float_variation`: the generic method already compares the fallback's kind. An `int` or `float` fallback to the generic method is the `number` kind with no integer rule (the harness `number` accessor), while `int_variation` applies contract 4's fractional and signed 64-bit rules and returns an `int`. The `json` accessor is the generic method with a `list` or `dict` fallback. A `None` fallback accepts any kind. A fallback of any other type (a `bytes`, a `datetime`, an arbitrary object) never matches a served value and answers `ERROR TYPE_MISMATCH`; it is still returned unchanged. |
| `onError thread` | The SDK's own thread for what that thread did (a failed ruleset or stream attempt on `toggleslash-sync`, a failed usage request on `toggleslash-flush`); the caller's thread for a hook that raised and for `EVALUATION_FAILED`; the thread that called `close` for a usage request in the final flush and for counts lost at close. | No SDK lock is held while it runs. |

**The closed sets keep their spelling.** `toggleslash.ReasonKind`, `toggleslash.ErrorCode`,
`toggleslash.StreamState` and `toggleslash.LastError` are `enum.StrEnum` classes whose member names and values
are the canonical spelling (`ReasonKind.RULE_MATCH == "RULE_MATCH"`, `ErrorCode.NOT_READY == "NOT_READY"`,
`StreamState.CONNECTED == "connected"`, `LastError.TIMEOUT == "timeout"`). A `StrEnum` member is a `str`, so it
compares equal to the spelling in the contract and serialises as it. `Reason` is a frozen dataclass with `kind`,
`code` (the `ErrorCode` for an `ERROR`, `None` otherwise), `rule_index` and `flag_key`. `http_<status>` cannot
be an enum member, so `Status.last_error` is a plain `str` for it (`"http_503"`) and a `LastError` member for
the other three (`"timeout"`, `"bad_ruleset"`, `"network"`); every value is a `str` equal to the contract's code.

**"Not given" versus false or zero.** Python keyword defaults carry the defaults themselves (`stream=True`,
`poll_interval=30`), so "not given" is the default. A caller who passes `stream=False` gets off, and passing
`None` for an interval also gets the default. A non-positive, non-numeric or non-finite duration (`True` and
`False` included, as they are not durations) falls back to the default (contract 2, SHOULD). Only the exact
value `False` turns the stream off; `None` means the default (on).

## 2. Concurrency (contract 6)

- **The ruleset and its ETag** live in one immutable `Held` object (`ruleset`, `etag`, a `__slots__` class that is
  never mutated) referenced by one attribute of the client. A swap is one assignment of that reference, which is
  atomic under the global interpreter lock and under a free-threaded build alike (a single attribute store).
  An evaluation reads the attribute once, so it sees the old pair or the new pair, and the `etag` and
  `flag_version` a hook reports come from that same read.
- **The status** is one immutable `_State` (`last_sync_at`, `stream`, `last_error`) replaced under a short
  `_state_lock` (a `threading.Lock`). The lock covers a few assignments and never an I/O call or a callback.
  `status()` reads the held pair and the state together under that lock, so it is one snapshot, and a stream
  failure sets `last_error` and the stream state in one replacement, so a reader never sees the failure's
  `last_error` beside `connected` (contract 6.5).
- **Threads the SDK starts** (contract 6.6): `toggleslash-sync` runs the first load and then the loop of 7.3,
  `toggleslash-flush` runs the usage flush on `flush_interval`. Both are `daemon=True`, so a client that is
  never closed does not keep the process alive.
- **Locks.** `_state_lock` (above), `_usage_lock` (the counts dict, held for an add or a swap of the dict and
  never during I/O), `_flush_lock` (serialises flushes; only the flush thread and `close` take it, never an
  evaluation), `_close_lock` (decides who performs the shutdown). Evaluation takes `_usage_lock` for the length
  of one dict update and no other lock. A request in flight holds none of them, so a hung server cannot slow an
  evaluation down.
- **Callbacks.** The hook and `on_error` run with no SDK lock held. `ready()` called from the SDK's own thread
  (`threading.current_thread()` is the sync or flush thread, as an `on_error` callback is) returns at once with
  the state held, and `close()` from an SDK thread skips joining itself.
- **Close.** `close` sets the closing flag and the stream state to `closed` under `_state_lock`, sets a
  `threading.Event` that both loops wait on (so no timer outlives it), writes to the client's wake socket (below)
  so a ruleset, stream or usage request in flight is abandoned at once, joins the sync and flush threads with a
  bound of `request_timeout`, takes `_flush_lock` (waiting for a flush already running, bounded) and runs the final
  flush, then settles an unsettled `ready` as false. A second or concurrent caller waits for the same shutdown on
  an `Event`, bounded by four times `request_timeout`, and returns. A thread stuck in a name lookup cannot be
  interrupted; `close` does not wait for it past its bound, and as that thread is a daemon it does not keep the
  process alive.
- **The global interpreter lock.** CPython has one (3.13 and later can be built without it), so the ruleset swap
  and the counts would be safe on assignment alone; the locks above are for the free-threaded build and for the
  multi-step updates. Python has no race detector, so the tests run under `python -X dev -W error`, and a stress
  test (`tests/test_concurrency.py`) races `close`, `variation` and `status` with a `threading.Barrier` and joins
  every thread with a timeout so a deadlock fails instead of hanging.

## 3. HTTP and SSE

- **HTTP client.** A small HTTP/1.1 client over `socket` (and `ssl` for `https`, with
  `ssl.create_default_context()`: the system certificate store, peer verification and host name checking),
  written in `src/toggleslash/_http.py`. No size is capped (contract 7.1, "there is no size cap"): the
  whole-request deadline bounds a head, a line and a body. A port outside 1 to 65535 is refused.
  `http.client` is not used because it adds an `Accept-Encoding` header, its timeouts are per socket operation and
  not per request, and it sets its socket to `None` after `getresponse()` for an answer that ends with the
  connection, which takes the stream watchdog's timeout away. One new connection per request,
  `Connection: close`, no redirects, no proxy lookup. The headers it sends are `Host`, `Connection`,
  `Content-Length` (a POST), `Authorization`, `Accept`, `If-None-Match` and `Content-Type`, and nothing else.
- **`request_timeout` bounds the whole request, body included.** Every step (connect, the TLS handshake, the write,
  each read of the head and of the body) is non-blocking and waits with `select.select` for at most the time left
  against one monotonic deadline set when the request starts, so a server that trickles a body runs out of time
  and the request is a `timeout`. Every wait also watches the client's wake socket (one `socket.socketpair()`
  per client): `close` writes a byte to it, the `select` wakes, and the request is turned into "aborted".
  (Closing a socket from another thread does not reliably wake a thread blocked in `select`, so it is not used.)
  The name lookup (`socket.getaddrinfo`) cannot be interrupted, so it runs on a helper daemon thread and is
  waited for against the deadline and the wake socket.
- **The stream** is opened with the same deadline for the connect and the head. Once the head has arrived the
  watchdog takes over: every read waits at most `stream_watchdog` (60 s; a seam shortens it) for a byte, and each
  read that returns bytes (a `: keep-alive` included) restarts it. A read that runs out is `timeout`. A 200 whose
  body ends with zero bytes read is a stream the server ends cleanly, not an error. A status other than 200 is
  `http_<status>`; a 429 reads `Retry-After`. The body is decoded as `chunked`, `Content-Length` or
  read-until-close, as the head says.
- **SSE.** A short hand-written parser, `src/toggleslash/_sse.py`, to the WHATWG rules the contract names: it
  decodes UTF-8 incrementally (`codecs.getincrementaldecoder("utf-8")("replace")`, so a multi-byte character split
  across reads is held back and an invalid byte becomes U+FFFD), ends a line on LF, CRLF or a lone CR (a CR at the
  end of a chunk waits for the next byte to see whether an LF follows), ignores a line that starts with `:`, reads
  `event` and `data` (one leading space removed from a value, several `data` lines joined with LF), drops an event
  with no `data` line, names an event `message` when no name was given, and discards an event still incomplete at
  the end of the stream. The SDK acts on `connected` and `changed`.

## 4. Dependencies

| Dependency | Kind | Why |
| --- | --- | --- |
| none | runtime | `socket`, `ssl`, `select`, `threading`, `hashlib` (SHA-256) and `json` ship with Python and cover everything. |
| `ruff` 0.17.4 | lint | One tool for lint and for the format check, pinned in `mise.toml`. |
| `unittest` | test | Ships with Python, so the tests need nothing installed. |
| `python` 3.15.2 | toolchain | Pinned in `mise.toml` for the checks; the package itself supports 3.11 and later. |

## 5. JSON

The standard `json` module is enough once its hooks are set, and `src/toggleslash/_json.py` wraps it:

- **Numbers** are binary64. `parse_float=float` makes `1e999` an infinity (`float("1e999")` is `inf`, no error and
  no warning). `parse_int` reads every integer literal through `float(text)` first, which rounds correctly, and
  keeps an `int` only when the double is within plus or minus 2^53 (so `9007199254740993` is
  `9007199254740992.0`, and a literal of 5000 digits never reaches `int()` and its 4300-digit limit; it is
  `inf`). `-0` and `-0.0` are `-0.0` or `0`, which are equal. `parse_constant` raises, so `NaN`, `Infinity` and
  `-Infinity` are parse errors, and so are a leading `+` and a leading zero (the module refuses them).
- **Strings** are `str`. Python's `str` holds a lone surrogate (`"\ud800"`) as the code point U+D800, and the
  `json` module keeps a lone surrogate escape as that code point and combines an escaped valid pair
  (`"😀"`) into one character. The evaluator reads a string by UTF-16 code unit where the spec says so
  (section 8). Bytes that are not valid UTF-8 are decoded with `bytes.decode("utf-8", "replace")`, which replaces
  each maximal invalid subpart by one U+FFFD as the WHATWG decoder does (checked in `tests/test_text_json.py`), and a
  leading U+FEFF is skipped.
- **A duplicate key** in an object keeps the last value (the `dict` default). Nesting deeper than the parser's
  recursion allows is a `RecursionError`, caught and turned into `bad_ruleset`; a test reads a body nested 512
  levels deep and requires it to parse.
- **Output** to the harness is `json.dumps` with `ensure_ascii=True`, so every code unit above U+007F leaves as
  `\uXXXX` and a lone surrogate leaves as `\ud800`. An infinity or a NaN is written as `null` (the harness
  protocol's rule for a non-finite number).

The public API takes a context as a `dict` (string keys) whose values are `None`, `bool`, `int`, `float`, `str`,
`list`, `tuple` and `dict`; the vectors' `1e999` and lone surrogates reach the SDK through the same reader in the
test service, so they are kept. A number that is not a finite binary64 never matches (the spec). An `int` is read
as the nearest double (an `int` beyond 2^53 is converted with `float()`, and one beyond the double range is an
infinity); a `decimal.Decimal` and a `fractions.Fraction` are read through `float()` as numbers. Any other object
(a `bytes`, a `datetime`, a `set`) is a present attribute of no JSON kind: it matches nothing. A key written as two
lone-surrogate escapes in a hand-built dict is not the same Python key as the one character it stands for; the
JSON reader combines them, so only a hand-built dict can differ, and the SDK does not rewrite a caller's keys.

## 6. Minimum runtime and toolchain

- **Runtime: CPython**, the mainstream implementation. The code is plain Python with no C extension, so it is
  expected to run on PyPy, which this run does not test.
- **Minimum Python 3.11.** On the run date (2026-11-12) Python 3.10 is end of life (October 2026), 3.11 is in
  security-only maintenance until October 2027, and 3.12 to 3.15 are current
  (<https://devguide.python.org/versions/>, read on the run date). 3.11 has `enum.StrEnum`, `typing.Self` and
  `datetime.UTC`, which the code uses. The checks run on 3.15.2, the latest stable release
  (`mise ls-remote python`), and the full suite is also run once on 3.11.
- **Lint and format: ruff** (`ruff check` and `ruff format --check`), the mainstream Python tool for both, one
  config in `pyproject.toml`. **Test framework: `unittest`**, which ships with Python, so `bin/test` needs nothing
  installed. **Build tool: none for the checks**: `bin/test` compiles every source, test and test-service file in memory under `-W error` as its build step, so a syntax error or a warning in a file no test imports still fails, and a failure counts as a failure. A `pyproject.toml` with a
  `setuptools` build backend makes `pip install ./sdks/python` work from a checkout.
- **Warnings are errors in the tests.** `bin/test` runs `python -X dev -W error`, so a `ResourceWarning`, a
  `DeprecationWarning` or an unraisable exception from the SDK fails the run. A duration the caller gives is
  capped at 31,536,000 s (a year), and a non-finite one falls back to the default.
- **The integer accessor** uses Python's `int`, bounded to signed 64 bits by the SDK (an `int` is unbounded): a
  served number that is not finite, has a fractional part, or lies outside -2^63 to 2^63-1 is `TYPE_MISMATCH`;
  `2.0` is the integer `2`. The `value` of an `EvaluationEvent` for the integer accessor is that `int`, exact.
- **No type checker is run.** The public API carries type annotations and ships `py.typed`.

## 7. The package layout

```
sdks/python/
  mise.toml  pyproject.toml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/toggleslash/
    __init__.py    the public API: create and the types
    _types.py      Reason, Detail, Status, EvaluationEvent, Hooks, ToggleSlashError, the closed sets
    _client.py     Client: options, evaluation, the loops, usage, close
    _http.py       the HTTP/1.1 client: whole-request deadline, stream open and read, wake socket
    _sse.py        the SSE parser
    _usage.py      the counts and the batches
    _json.py       the JSON reader and writer
    _text.py       UTF-16 code units and code points of a str, hashing text
    _ruleset.py _evaluate.py _clause.py _pattern.py _semver.py _dates.py _bucket.py _context.py _reference.py
                   the evaluator (no network, no threads)
  tests/           unittest: vectors, evaluator units, source searches, client, sync, usage, sse, http, tls and
                   concurrency tests against a loopback fake server
  test-service/    main.py and README.md (byte-compiled by bin/test with the tests)
```

`bin/test` runs `python -X dev -W error -m unittest discover -s tests -t tests` with `src` on `PYTHONPATH`.

## 8. Evaluator decisions (phase 2)

- **Strings.** A Python `str` is a sequence of code points. The spec compares in UTF-16 code units for equality,
  `contains`, `startsWith`, `endsWith` and order, and in code points for patterns. Two ASCII strings are compared
  directly. Anything else goes through `_text.units`, which encodes a string as UTF-16 (big endian, with the
  `surrogatepass` handler so a lone surrogate is its one unit) and compares those bytes, so order is by code unit
  and a pair of lone surrogates equals the character it spells. `contains` searches the unit bytes and accepts a
  match only at an even offset. For patterns, `str` is already a sequence of code points, with an adjacent high and
  low surrogate pair joined first (`_text.code_points`); a lone surrogate stays.
- **Hashing.** The string is encoded as UTF-16 with `surrogatepass`, decoded again with `replace` (a lone surrogate
  becomes U+FFFD, a valid pair becomes one character), then encoded as UTF-8.
- **Kind names** are checked with a character walk, never a regular expression.
- **`matches`** is the spec's Pike VM in `_pattern.py`. The `re` module is not imported anywhere under
  `src/toggleslash/`: not for a pattern, a semver, a date, a kind or a header.
- **Attribute reads** use own keys of a `dict` only. `bool` is tested before number everywhere.
- **Numbers.** An `int` attribute within 2^53 is kept; beyond it, it goes through `float()` (which rounds to nearest,
  and which raises `OverflowError` for an `int` past the double range, caught and turned into an infinity).

### Disagreements with other implementations

None found so far; any that turns up is recorded here with both readings and the file and line of each.
