# ToggleSlash Python SDK: design

This is the design for `sdks/python`, written before the code, to
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) (the contract) and
[`docs/evaluation.md`](../../docs/evaluation.md) (the spec). It is the Python run of
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md), done cold: nothing was read from an earlier
Python SDK in this repository.

## 1. The mapping table

Python spelling is `snake_case`. Durations are seconds (a `float`, an `int` or a `datetime.timedelta`), not
milliseconds. Everything is keyword-only on `toggleslash.create`.

| Name | In Python |
| --- | --- |
| `create` | `toggleslash.create(*, url, sdk_key, stream=None, poll_interval=None, flush_interval=None, request_timeout=None, hooks=None, on_error=None) -> Client`. Starts two daemon threads and returns at once. `Client(...)` takes the same arguments. |
| `ready` | `Client.ready() -> bool`. Blocks the calling thread until the first load has finished (bounded by `request_timeout`), then answers whether a ruleset was held at that moment. The answer is fixed. Called from an SDK thread (inside `on_error`) it returns the state held at once. |
| `variation` | `Client.variation(flag_key, context, fallback)`. The generic method: the fallback's JSON kind is the expected kind (a `None` fallback accepts any kind). |
| `variationDetail` | `Client.variation_detail(flag_key, context, fallback) -> Detail`, a frozen dataclass with `value`, `variation_index` (`int` or `None`) and `reason`. |
| `status` | `Client.status() -> Status`, a frozen dataclass with `etag`, `last_sync_at` (epoch milliseconds as an `int`, or `None`), `stream` (a `StreamState`) and `last_error` (a `str` or `None`). |
| `close` | `Client.close() -> None`. Safe to call twice and from several threads. `Client` is also a context manager (`with toggleslash.create(...) as client:`), and `close` runs on exit. |
| `url` | `url=`, required. Trailing `/` characters are removed. |
| `sdkKey` | `sdk_key=`, required. |
| `stream` | `stream=None`. `None` means on; only `False` turns it off. |
| `pollInterval` | `poll_interval=None`. `None` means 30 s. |
| `flushInterval` | `flush_interval=None`. `None` means 30 s. |
| `requestTimeout` | `request_timeout=None`. `None` means 10 s. |
| `hooks.afterEvaluation` | `hooks=Hooks(after_evaluation=fn)`. `fn(event: EvaluationEvent)`; its return value is ignored. |
| `onError` | `on_error=fn`. `fn(error: BaseException)`; `None` ignores. |
| `accessor shape` | Both. The generic `variation` and `variation_detail`, and typed methods `bool_variation`, `string_variation`, `number_variation`, `int_variation` and `json_variation`, each with a `_detail` twin. The typed ones fix the expected kind (`int_variation` adds the contract 4 integer rule over signed 64 bits) and ignore the fallback's kind; `json_variation` accepts any JSON kind and has the same kind rule as `variation` with a `None` fallback. |
| `onError thread` | The SDK's own thread that met the failure: `toggleslash-sync` for a ruleset or stream failure, `toggleslash-flush` for a usage failure, and the caller's thread for a hook that raised, an evaluation that raised, and the counts lost at `close`. No SDK lock is held when it is called. |

**Closed sets.** `ReasonKind` (`OFF`, `PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`,
`ERROR`), `ErrorCode` (the evaluator's seven codes plus `NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`) and
`StreamState` (`off`, `connecting`, `connected`, `disconnected`, `closed`) are `enum.StrEnum` classes whose member
values are the canonical spelling, so `Reason.kind == "RULE_MATCH"` and `ReasonKind.RULE_MATCH == "RULE_MATCH"`
both hold and the JSON a service writes is the canonical text. `Reason` is a frozen dataclass with `kind`,
`rule_index`, `flag_key` and `code`, the last three `None` unless the kind carries them. `last_error` is a plain
`str`: `http_<status>`, `timeout`, `bad_ruleset` or `network`, with the four spellings also exported as
`toggleslash.LastError` constants (`TIMEOUT`, `BAD_RULESET`, `NETWORK`, and `http(status)`).

**"Not given" against false or zero.** Every option whose default is on or non-zero defaults to `None` in the
signature, which is the sentinel for "not given". `stream=False` is off, `stream=None` (or anything that is not a
`bool`) is on. A duration that is `None`, not a number (`bool` is not a number), not finite or not above zero is
the default, as contract 2 suggests; `0` is therefore the default and not "immediately".

## 2. Concurrency

- **One immutable snapshot.** The ruleset, its ETag, `last_sync_at`, the stream state and `last_error` live in one
  frozen `_Snapshot` held in one attribute. A writer builds the next snapshot with `dataclasses.replace` under
  `_state_lock` and assigns it; a reader (`variation`, `status`, the hook event) reads the attribute once and has a
  consistent view. So the ruleset swap is atomic and the ETag a hook reports is the one of the ruleset the
  evaluation used. A reader takes no lock at all.
- **Threads.** `create` starts two daemon threads: `toggleslash-sync` (the first load, then the loop of contract
  7.3) and `toggleslash-flush` (the usage flush loop). Waits are `Event.wait` on a stop event, through the clock seam
  (below), so `close` wakes them at once. Both are daemons, so a client that is never closed does not keep the
  interpreter alive.
- **Locks.** `_state_lock` (writers of the snapshot; held for a dict-replace and nothing else), `_usage_lock` (the
  pending-count dict; a flush swaps the dict out under it and sends after releasing it), `_flush_lock` (serial
  flushes; held across a usage request, but only ever taken by the flush loop, `close` and the optional `flush`,
  never by an evaluation) and `_close_lock` (which caller runs the shutdown). `variation` takes `_usage_lock` for an
  integer add and nothing else, and a usage flush never holds it across I/O, so a hung server cannot slow an
  evaluation down. `afterEvaluation` and `on_error` run with no SDK lock held.
- **`close`.** The first caller wins `_close_lock`, sets `closing`, marks the snapshot `closed`, sets the stop
  event, aborts the sync side's in-flight sockets (the stream and the ruleset request; the usage requests are a
  separate group and are not aborted), joins the sync and flush threads (each join is bounded by
  `request_timeout`, and skipped for the calling thread itself), settles `ready` false if unsettled, then runs the
  final flush (each request bounded by `request_timeout`, so `close` returns within about twice `request_timeout`).
  Later and concurrent callers wait on a `done` event for the same shutdown.
- **`ready` from an SDK thread.** A thread-identity check (`threading.current_thread()` is the sync or flush
  thread) makes it return `ruleset is not None` at once, so `on_error(lambda e: client.ready())` cannot deadlock.
- **No race detector.** CPython has none. The tests run under `python -X dev -W error` and stress the client with a
  `threading.Barrier` and `Thread.join(timeout)` (a deadlock fails the test instead of hanging it).

## 3. HTTP and SSE

- **Client.** `http.client` over a socket the SDK opens itself (`socket.create_connection`, wrapped by `ssl` for
  `https`), one connection per request, closed after it. `Connection` handling, redirects (not followed: a `3xx` is
  `http_<status>`) and compression (`Accept-Encoding: identity` is what `http.client` sends) are the defaults of
  the library. The base URL is split with `urllib.parse.urlsplit` only to find the host, port and a path prefix.
- **`request_timeout` covers the whole request, body included.** Each request registers its socket with a
  `threading.Timer` guard that, on expiry, shuts the socket down (`shutdown(SHUT_RDWR)`) from another thread, which
  wakes any blocked `recv` in the loop thread; the loop thread then sees the guard's `timed_out` flag and raises the
  `timeout` error. That bounds connect (the socket's own connect timeout is the remaining time), the headers, and
  every read of the body, and a server that trickles one byte a second cannot outlast it. The same registry is how
  `close` abandons the sync side's in-flight requests: it shuts those sockets down, and the thread sees `aborted`
  and reports nothing. A name lookup (`getaddrinfo`) cannot be interrupted in Python; a hung resolver is the one
  thing `request_timeout` does not bound, and `close` and `ready` stay bounded anyway because they join with a
  timeout (the threads are daemons).
- **The stream.** Opening it is guarded by `request_timeout` from the request to the response headers; the guard is
  then cancelled. After that the SDK keeps **its own reference to the socket** (`http.client` sets
  `connection.sock` to `None` after `getresponse()` for an answer that ends with the connection, so a later
  `settimeout` through the connection goes nowhere) and calls `sock.settimeout(watchdog)` with the watchdog length
  (60 s, a `create` seam for tests). Every `recv` restarts that timeout, so every read, a keep-alive comment
  included, restarts the 60 s, and a `TimeoutError` out of a read is the `timeout` error.
- **SSE.** A hand-written incremental parser (`_sse.py`) over text decoded by an incremental UTF-8 decoder (so a
  multi-byte character split across reads is whole), after a leading byte order mark is skipped. It holds a trailing
  CR (it may be the first half of CRLF), turns CRLF and a lone CR into LF, and ends a block at a blank line. Within a
  block a line starting with `:` is a comment, `event` and `data` are read (one leading space of the value removed,
  several `data` lines joined with LF), other fields are ignored, a block with no `data` line is dropped, an empty
  event name means `message`, and a block still open when the stream ends is discarded. The loop acts only on
  `connected` and `changed`.

## 4. Dependencies

Runtime dependencies: **none**. `http.client`, `ssl`, `socket`, `threading`, `json`, `hashlib`, `dataclasses`
and `enum` are the standard library. Lint and format are `ruff` (pinned in `mise.toml`); the tests are
`unittest` from the standard library, so nothing else is installed. The test service is the standard library's
`http.server`.

## 5. JSON

- **Parser.** The standard `json` module with three hooks (`_json.py`): `parse_int` turns an integer literal
  beyond plus or minus 2^53 into a `float` (so `9007199254740993` reads as `9007199254740992.0`, and the interpreter's
  integer-digit limit, which makes `int()` raise on a literal of thousands of digits, is never reached because a long
  literal goes to `float()`); `parse_float` is `float` (so `1e999` is `inf`, not an error, and `-0.0` stays negative
  zero); `parse_constant` raises, because `NaN` and `Infinity` are not JSON. A duplicate key keeps the last value
  (what a `dict` does), and a leading byte order mark is skipped. A body that is not valid UTF-8, not JSON, or too
  deep for the interpreter is `bad_ruleset`.
- **Strings.** A Python `str` is a sequence of code points and holds a lone surrogate (`"\ud800"`) as it is; the
  `json` module keeps a lone `\ud800` escape and joins a valid pair of escapes into one code point. The evaluator
  normalises a string that is not ASCII by a UTF-16 round trip with `surrogatepass`, so a high and a low surrogate
  that sit side by side as two code points become the one code point they spell. UTF-16 order, `contains`,
  `startsWith` and `endsWith` work on the UTF-16-BE bytes (an ASCII pair uses the native operations), and hashing
  replaces a lone surrogate with U+FFFD before the UTF-8 encoding.
- **The context.** A context is a plain `dict` (any `Mapping`) of JSON values: `str`, `int`, `float` (infinite
  allowed), `bool`, `None`, `list` and `dict`. That form keeps `1e999` and lone surrogates, so the public API needs
  no raw-JSON companion. A `bool` is tested before a number everywhere, since `True` is an `int`. Numbers are
  converted to `float` before any comparison, because Python compares an `int` and a `float` exactly, which is not
  binary64.

## 6. Minimum runtime, toolchain

- **Runtime:** CPython. Nothing here is CPython only, but the run was on CPython.
- **Pinned for development (`mise.toml`):** Python 3.15.2, the newest stable release at the run date (the version
  list of `mise ls-remote python` and endoflife.date agree), and ruff 0.17.4. Python has no LTS line.
- **Minimum:** Python 3.11 (`requires-python = ">=3.11"`). 3.11 is the oldest release still in security support
  at the run date (end of life 2027-10-31); 3.10 ended in October 2026. 3.11 has what the SDK uses: `enum.StrEnum`,
  `Thread.join(timeout)` and `Event.wait` returning the flag, `dataclass(slots=True, frozen=True)`, and `TimeoutError`
  as the one name for `socket.timeout`.
- **Package manager / build tool:** none required to use it from a checkout (`pip install ./sdks/python` or putting
  `sdks/python/src` on `PYTHONPATH`); `pyproject.toml` declares the package with `setuptools` as the build backend
  (the most common one, and in the standard toolchain). Nothing is built or published by the scripts.
- **Lint and format:** `ruff check` and `ruff format --check`, because ruff is the current mainstream tool for both
  and one pinned binary does the job of three. No type checker is run (not required by the prompt); the code is
  annotated anyway and ships `py.typed`.
- **Tests:** `unittest` (`python -m unittest discover -s tests -t tests`), under `-X dev -W error`. The vectors
  test reads `fixtures/evaluation` from the repository.
- **Build step of `bin/test`:** `python -m compileall -q -f` over `src`, `tests` and `test-service` into a
  `__pycache__` that `.gitignore` lists, so a syntax error in a file no test imports fails.
- **The integer accessor** uses Python's `int`, bounded to the signed 64-bit range `-2**63 <= n <= 2**63 - 1`
  (`2.0` is the integer `2`, a fraction is `TYPE_MISMATCH`). The test service advertises `integer-accessor`.

## 7. Package layout

The package is named `toggleslash` (a Python package name has no host). The repository path
`github.com/madangryscientist/toggleslash/sdks/python` is the project URL in `pyproject.toml`. Nothing is
registered under any name.

```
sdks/python/
  DESIGN.md  README.md  RUN.md  mise.toml  pyproject.toml  .gitignore
  bin/lint  bin/test  bin/test-service
  src/toggleslash/
    __init__.py     the public names
    _types.py       Reason, ReasonKind, ErrorCode, StreamState, Detail, Status, Hooks, EvaluationEvent
    _json.py        the JSON loader and the encoder the tests share
    _text.py        code point and UTF-16 helpers, UTF-8 for hashing
    _context.py     context validation and normalising
    _validate.py    the load-time checks of flags and segments
    _ruleset.py     ruleset construction (first key wins, entries without a key dropped)
    _evaluate.py    the evaluator: order of evaluation, prerequisites, targets, rules, rollouts
    _clause.py      attribute reading and the ops
    _reference.py   the `/` references
    _semver.py      the hand-written semantic version walk
    _dates.py       the hand-written instant parser
    _pattern.py     the pattern parser and the Pike VM (no `re`)
    _bucket.py      SHA-256 bucketing
    _clock.py       the clock seam
    _http.py        sockets, guards, the response reader
    _sse.py         the SSE parser
    _sync.py        the ruleset sync (first load, stream, loop, Retry-After)
    _usage.py       the usage counts and their flush
    _client.py      the client
  tests/            unittest: vectors, pins, SSE, sync, usage, concurrency, TLS, watchdog, privacy
  test-service/     main.py and README.md: the conformance test service
```

The evaluator modules (`_context` to `_bucket`, with `_text`) import nothing from the client modules and no network
module.

## 8. Seams

`create` takes a private keyword, `_seams=Seams(...)`, with: `clock` (monotonic time, epoch milliseconds and a
`wait(stop_event, seconds)` that a test can fake), `jitter` (a callable returning a float in `[0, 1)`), `watchdog`
(seconds, default 60) `evaluator` (a callable replacing the evaluator, to make it raise) and `ssl_context` (the trust for an `https`
URL, so a test can trust a local certificate). The 300 s
`Retry-After` cap is not a seam: it is tested at its real length on the fake clock. No global and no
environment variable is read.

## 9. Choices the prompt left open

- **Time types:** seconds as numbers, because every Python timeout API (`Event.wait`, `socket.settimeout`) is
  seconds; a `timedelta` is accepted too.
- **Names:** `snake_case`, `sdk_key`, `on_error`, with the canonical operation names in the table above.
- **Redirects:** not followed.
- **Callbacks:** a hook or `on_error` that raises an `Exception` is ignored (or sent to `on_error`); `BaseException`
  (`KeyboardInterrupt`, `SystemExit`) is never swallowed.
- **Futures-returning hooks:** a hook is a plain function; a hook that returns an awaitable is not awaited
  (marked `n/a: no futures` in the checklist).
