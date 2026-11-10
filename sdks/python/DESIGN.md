# ToggleSlash Python server SDK: design

This is the design the SDK in this directory was built to (phase 1 of
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md)). The contract is
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), evaluation is
[`docs/evaluation.md`](../../docs/evaluation.md) and the harness protocol is
[`docs/sdk-test-service.md`](../../docs/sdk-test-service.md). Section numbers below are the contract's.

The distribution and the import name are both `toggleslash` (`import toggleslash`). Nothing is registered
under that name.

## 1. The mapping table (contract 2)

Durations are `float` seconds (an `int` is accepted). `create` is a module function, `toggleslash.create(...)`,
that returns a `toggleslash.Client`. Python names are `snake_case`.

| Name | Python | Default and meaning |
| --- | --- | --- |
| `create` | `toggleslash.create(url=..., sdk_key=..., ...)` returns a `Client`. All arguments after the first two are keyword-only. | Starts two daemon threads and returns at once; it does not wait for the network. |
| `ready` | `client.ready() -> bool` | Blocks until the first ruleset attempt has settled, bounded by `request_timeout` (plus a 1 s grace for work the SDK does not control, such as a name lookup). True if a ruleset is held. The answer is fixed at the first settle. |
| `variation` | `client.variation(flag_key, context, fallback)` | Generic: the expected kind is read off the type of `fallback` (`bool`, `int` or `float`, `str`, `list` or `tuple`, `dict`; `None` accepts any kind). |
| `variationDetail` | `client.variation_detail(flag_key, context, fallback) -> Detail` | `Detail` has `value`, `variation_index` (`int` or `None`) and `reason`. Also `client.int_variation_detail`. |
| `status` | `client.status() -> Status` | A frozen dataclass: `etag`, `last_sync_at` (aware UTC `datetime` or `None`), `stream` (`StreamState`), `last_error` (`str` or `None`). One snapshot. |
| `close` | `client.close()` | Stops, joins (bounded), sends the final usage flush. Safe twice and from many threads. Returns within about twice `request_timeout`. |
| `url` | `url=` (required, `str`) | The server's base URL. Trailing `/` are removed. |
| `sdkKey` | `sdk_key=` (required, `str`) | A server key. |
| `stream` | `stream=True` | `bool`. Off means poll only. |
| `pollInterval` | `poll_interval=30.0` | Seconds. |
| `flushInterval` | `flush_interval=30.0` | Seconds. |
| `requestTimeout` | `request_timeout=10.0` | Seconds, the whole request, body included. |
| `hooks.afterEvaluation` | `hooks=Hooks(after_evaluation=fn)`; default `None` | `fn(event: EvaluationEvent) -> None`, called on the caller's thread. The return value is ignored; a non-callable is ignored. |
| `onError` | `on_error=fn`; default `None` (ignore) | `fn(error: BaseException) -> None`. |
| `accessor shape` | Both: the generic `variation` and `variation_detail` (kind from the fallback's type), and `int_variation` and `int_variation_detail` for the integer accessor. | There is no `bool_variation`, `string_variation` or `float_variation`: the generic method already compares the fallback's kind, and an `int` fallback to the generic method is the `number` kind with no integer rule (the harness `number` accessor), while `int_variation` applies contract 4's fractional and signed 64-bit rules. The `json` accessor is the generic method with a `list` or `dict` fallback. |
| `onError thread` | The SDK's own thread for what that thread did (a failed ruleset or stream attempt on `toggleslash-sync`, a failed usage request on `toggleslash-flush`); the caller's thread for a hook that raised and for `EVALUATION_FAILED`; the thread that called `close` for a usage request in the final flush and for counts lost at close. | No SDK lock is held while it runs. |

A fallback whose type is not a JSON value (an arbitrary object, a `datetime`) never matches a served value
and answers `ERROR TYPE_MISMATCH`: it is the caller's fallback, so it is still returned unchanged.

**The closed sets keep their spelling.** `toggleslash.ReasonKind`, `toggleslash.ErrorCode` and
`toggleslash.StreamState` are `enum.StrEnum`s whose member names and values are the canonical spelling
(`ReasonKind.RULE_MATCH == "RULE_MATCH"`, `ErrorCode.NOT_READY == "NOT_READY"`, `StreamState.CONNECTED ==
"connected"`). They compare equal to the plain string. `reason` is a frozen dataclass `Reason(kind, code)`, with
`code` the `ErrorCode` for an `ERROR` and `None` otherwise. `Status.last_error` is a plain `str`
(`"http_503"`, `"timeout"`, `"bad_ruleset"`, `"network"`), with the four constants in `toggleslash.LastError`
(`TIMEOUT`, `BAD_RULESET`, `NETWORK`; `http_<status>` is built).

**"Not given" versus false or zero.** Python keyword defaults carry the defaults themselves
(`stream=True`, `poll_interval=30.0`), so "not given" is the default. A caller who passes `stream=False` gets
off, and passing `None` for an interval also gets the default. A non-positive or non-numeric duration (a
`bool` included) falls back to the default (contract 2, SHOULD).

## 2. Concurrency (contract 6)

- **The ruleset and its ETag** live in one immutable object, `_Held(ruleset, etag)`, referenced by one attribute.
  A swap is one attribute assignment (atomic under the GIL and in free-threaded builds for a single reference),
  and an evaluation reads the attribute once, so it sees the old pair or the new pair. The `etag` and
  `flagVersion` a hook reports come from that same read.
- **The status** is one immutable `_State` (`last_sync_at`, `stream`, `last_error`) replaced under a short
  `_state_lock`. The lock covers a few assignments and never an I/O call or a callback. `status()` reads the
  reference once (with the held ETag from one read of `_held`), so it is a snapshot.
- **Threads the SDK starts**, both daemon (contract 6.6): `toggleslash-sync` runs the first load and then the loop
  of 7.3, `toggleslash-flush` runs the usage flush on `flush_interval`. A hook, `onError` or any callback never
  runs on an SDK thread except `onError` (see the table).
- **Locks.** `_state_lock` (above), `_usage_lock` (the counts dictionary, held for an add or a swap of the dictionary and
  never during I/O), `_flush_lock` (serialises flushes; only the flush thread and `close` take it, never an
  evaluation), `_close_lock` (decides who performs the shutdown). Evaluation takes `_usage_lock` for the length of
  one dictionary add and no other lock. A request in flight holds none of them, so a hung server cannot slow an
  evaluation down.
- **Callbacks.** The hook and `onError` run with no SDK lock held (errors are gathered under a lock and raised to
  the callback after it is released). `ready` called from the SDK's own thread (`threading.current_thread()` is
  the sync or flush thread) returns at once with the state held, and `close` from an SDK thread skips joining
  itself.
- **Close.** `close` sets the closing flag and the stream state to `closed` under `_state_lock`, sets a
  `threading.Event` both loops wait on (so no timer outlives it), aborts the in-flight ruleset and stream
  sockets (below), joins the sync and flush threads with a bound of `request_timeout`, takes `_flush_lock`
  (waiting for a flush already running, bounded) and runs the final flush, then settles an unsettled `ready` as
  false. A second or concurrent caller waits for the same shutdown on an `Event`, bounded by four times
  `request_timeout`, and returns. A daemon thread stuck in a name lookup cannot be interrupted; `close` does not
  wait for it past its bound.

## 3. HTTP and SSE

- **HTTP client.** `http.client` (`HTTPConnection` and `HTTPSConnection`, the default TLS context, certificates
  verified), one new connection per request, no redirects, no proxy lookup. `urllib` is not used because it
  adds a `User-Agent` and follows redirects, and the contract wants neither decision made for it.
- **`request_timeout` bounds the whole request, body included.** `http.client`'s own `timeout` is per socket
  operation. So each request starts a `threading.Timer` daemon for `request_timeout` that marks the request
  timed out and calls `shutdown` on the **socket the SDK itself holds**: `fetch` calls `conn.connect()` first
  and keeps `sock = conn.sock`, because `http.client` sets `conn.sock` to `None` after `getresponse()` for an
  answer that ends with the connection. The timer is cancelled when the body has been read, and a request
  whose timer fired is a `timeout` even if the read looked complete (a shutdown makes a read-until-close body
  look finished). The connect step also carries the socket timeout. The same hold on the socket lets `close`
  abort an in-flight ruleset or stream request (`shutdown` of every registered socket).
- **The stream** is opened with the same timer, which is cancelled when the response headers arrive; the
  watchdog then takes over: `sock.settimeout(60)` on the held socket, and the body is read with
  `HTTPResponse.read1`, so each read that returns bytes (a `: keep-alive` included) restarts the 60 s, and a
  read that raises `TimeoutError` is `timeout`. The watchdog length is a seam (`stream_watchdog`). A status
  other than 200 is `http_<status>`; a 429 reads `Retry-After`.
- **SSE.** A short hand-written parser, `_sse.py`, to the WHATWG rules the contract names: it decodes UTF-8
  incrementally (a multi-byte character split across reads is held back), ends a line on LF, CRLF or a lone CR
  (a CR at the end of a chunk waits for the next byte to see whether an LF follows), ignores a line that
  starts with `:`, reads `event` and `data` (one leading space removed from a value, several `data` lines
  joined with LF), drops an event with no `data` line, names an event `message` when no name was given, and
  discards an event still incomplete at the end of the stream. The SDK acts on `connected` and `changed`.

## 4. Dependencies

| Dependency | Kind | Why |
| --- | --- | --- |
| none | runtime | `http.client`, `threading`, `hashlib` (SHA-256), `json` and `socket` cover everything. |
| `ruff` 0.17.4 | lint | One tool for lint and format check, pinned in `mise.toml`. |
| `unittest` | test | In the standard library, so the tests need nothing installed. |
| `python` 3.15.2 | toolchain | Pinned in `mise.toml` for the checks; the package itself supports 3.11 and later. |

## 5. JSON

`json.loads` with two hooks, through `toggleslash._json.loads`:

- `parse_constant` raises, so `NaN`, `Infinity` and `-Infinity` (which Python's parser accepts and JSON does
  not) are a parse error.
- `parse_int` returns a `float` for an integer literal beyond plus or minus 2^53 (the spec reads every number
  as binary64: `9007199254740993` is `9007199254740992.0`, and a literal of thousands of digits becomes a
  float without ever calling `int()` on it, which keeps the interpreter's integer-digit limit away), the
  `int` otherwise, and `-0.0` for the literal `-0`.
- `1e999` is `float('inf')` naturally (Python's `float` does not raise on overflow of a literal).
- A lone surrogate (`"\ud800"`) is kept: a Python `str` is a sequence of code points and holds U+D800 on its
  own. A valid escaped pair (`"😀"`) the parser combines into one code point.
- A duplicate key in an object keeps the last value (the `json` default); a leading byte order mark is
  skipped by decoding the body as `utf-8-sig`.
- Output to the harness uses `json.dumps(ensure_ascii=True)`, so a lone surrogate leaves as `\ud800`.

The public API takes a context as a `dict` of what `json.loads` returns (`None`, `bool`, `int`, `float`,
`str`, `list`, `dict`), or any `Mapping`. The vectors' `1e999` and lone surrogates reach the SDK through
the same parser in the test service, so they are kept. A `Mapping` other than `dict`, and a `tuple`, are read as
the object and array they stand for.

## 6. Minimum runtime and toolchain

- **Minimum Python 3.11.** It has `enum.StrEnum` and `datetime.UTC`, and 3.10 reached end of life on
  2026-10-01. The checks run on 3.15.2, the latest stable release on 2026-11-10 (`mise ls-remote python`).
- **Lint and format: `ruff`** (current mainstream, one tool, fast). **Test framework: `unittest`** from the
  standard library, so `bin/test` needs nothing installed. **Build tool: none for the checks** (`bin/test` byte-compiles every
  source, test and service file as its build step); `pyproject.toml` uses the `setuptools` backend so that
  `pip install ./sdks/python` works from a checkout.
- **The integer accessor** uses Python's `int`, bounded to signed 64 bits by the SDK (an `int` is unbounded):
  a served number that is not finite, has a fractional part, or lies outside -2^63 to 2^63-1 is
  `TYPE_MISMATCH`; `2.0` is the integer `2`.
- **No type checker is run.** The code is annotated; `py.typed` is shipped.

## 7. The package layout

```
sdks/python/
  mise.toml  pyproject.toml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/toggleslash/
    __init__.py     the public names
    _types.py       Reason, Detail, Status, EvaluationEvent, Hooks, the closed sets
    _client.py      create, Client, the loops, usage, close
    _http.py        fetch with the whole-request timer, stream open and read
    _sse.py         the SSE parser
    _usage.py       the counts and the batches
    _json.py        the JSON reader
    _evaluate.py    the ruleset and the evaluator (no network, no threads)
    _pattern.py     the `matches` parser and Pike VM
    _semver.py  _dates.py  _bucket.py  _context.py  _reference.py
  tests/            unittest: vectors, evaluator units, client tests with a loopback server
  test-service/     main.py and README.md (compiled by bin/test with the tests)
```

`bin/test` runs the unittest discovery in `tests/` under `python -X dev -W error` with `src` on the path.

## 8. Evaluator decisions (phase 2)

- **Strings.** A Python `str` is a sequence of code points. The spec compares in UTF-16 code units for
  equality, `contains`, `startsWith`, `endsWith` and order, and in code points for patterns. The evaluator
  turns a string into code units with a `_units()` helper (a code point above U+FFFF becomes its surrogate pair,
  each half held in one `str` character) and compares those, so a non-BMP character sorts as its pair does.
  For patterns, `code_points()` joins an adjacent high and low surrogate back into one code point first; a lone
  surrogate stays as it is.
- **Hashing.** The string is encoded as UTF-8 and, where it holds a lone surrogate, as U+FFFD for that surrogate.
- **Kind names** are checked with a character walk, never a regular expression.
- **`matches`** is the spec's Pike VM in `_pattern.py`. No `re` module appears in the evaluator.
- **Attribute reads** use own keys of a `dict` only. A `bool` is tested before a number everywhere.

### Disagreements with other implementations

None found so far; any that turns up is recorded here with both readings and the file and line of each.
