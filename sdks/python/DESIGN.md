# Python SDK: design

This is the design of the ToggleSlash server SDK for Python, written before the code, to
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) (the contract) and
[`docs/evaluation.md`](../../docs/evaluation.md) (the spec). It is built from
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md); the run report is [`RUN.md`](RUN.md).

Inputs: language Python. Runtime, minimum version and package manager were not given, so each is a
decision (see "The minimum runtime and the toolchain").

## The mapping table

The distribution and the import package are both `toggleslash`. Everything below is in
`toggleslash` unless it says otherwise. Durations are seconds, as an `int`, a `float` or a
`datetime.timedelta`.

| Name | In Python |
| --- | --- |
| `create` | `toggleslash.create_client(url, sdk_key, *, stream=True, poll_interval=None, flush_interval=None, request_timeout=None, hooks=None, on_error=None) -> Client`. It starts two daemon threads and returns without waiting for the network. It never raises. |
| `ready` | `Client.ready() -> bool`. Blocks the calling thread until the first load has finished (bounded by `request_timeout`), then answers whether a ruleset is held. Any number of calls, from any thread; after `close` it answers `False` if nothing had loaded. |
| `variation` | `Client.variation(flag_key, context, fallback)`: the generic method, with the fallback's JSON kind doing the work (a `None` fallback accepts any served value). Typed methods beside it: `bool_variation`, `string_variation`, `int_variation`, `float_variation`, `json_variation`. |
| `variationDetail` | `Client.variation_detail(flag_key, context, fallback) -> VariationDetail`, a frozen dataclass `(value, variation_index, reason)`; `variation_index` is `None` when the value is the fallback. Every typed method has a `_detail` twin (`bool_variation_detail` and so on). |
| `status` | `Client.status() -> ClientStatus`, a frozen dataclass `(etag, last_sync_at, stream, last_error)`. `last_sync_at` is a timezone-aware UTC `datetime.datetime` or `None`. One consistent snapshot, read from memory. |
| `close` | `Client.close() -> None`. Idempotent and safe from any thread and from a callback. `Client` is also a context manager that calls it. |
| `url` | required argument `url: str`. Trailing `/` characters are removed; nothing else is validated. |
| `sdkKey` | required argument `sdk_key: str`. |
| `stream` | keyword `stream: bool = True`. `False` is polling only. The default is `True` in the signature, so `False` and "not given" differ with no sentinel. |
| `pollInterval` | keyword `poll_interval=None`, meaning 30 s. A `None`, zero, negative or non-numeric value is the default. |
| `flushInterval` | keyword `flush_interval=None`, meaning 30 s, with the same fallback. |
| `requestTimeout` | keyword `request_timeout=None`, meaning 10 s, with the same fallback. |
| `hooks.afterEvaluation` | `hooks=Hooks(after_evaluation=callable)`; the callable gets one `EvaluationEvent`. A `Hooks` whose `after_evaluation` is not callable is ignored. |
| `onError` | keyword `on_error=callable`, default none (ignore). It gets one `Exception` instance. |
| `accessor shape` | Both. One generic method (`variation`, `variation_detail`) over the fallback's kind, and typed methods: `bool_variation`, `string_variation`, `int_variation`, `float_variation`, `json_variation`, each with a `_detail` twin. A typed method also needs the served value to be of its own kind, whatever the fallback is: `bool_variation` never answers a non-bool. `int_variation` is the integer accessor (below). |
| `onError thread` | The thread that met the failure: the SDK's sync thread for a failed ruleset or stream attempt, its flush thread for a failed usage request (the caller of `close` for the final flush), and the caller's own thread for an evaluation that raised and for a hook that raised. No SDK lock is held during the call. |

**The closed sets** keep their spelling as `enum.StrEnum` members, so `ReasonKind.OFF == "OFF"` and
they serialise as the canonical text: `ReasonKind` (`OFF`, `PREREQUISITE_FAILED`, `TARGET_MATCH`,
`RULE_MATCH`, `FALLTHROUGH`, `ERROR`), `ErrorCode` (the evaluator's seven and the SDK's `NOT_READY`,
`TYPE_MISMATCH`, `EVALUATION_FAILED`) and `StreamState` (`off`, `connecting`, `connected`,
`disconnected`, `closed`). `Reason` is a frozen dataclass `(kind, rule_index, flag_key, code)` with
the fields that do not apply `None`. `last_error` is a plain `str`, one of `http_<status>`,
`timeout`, `bad_ruleset`, `network` (or `None`); the module constants `TIMEOUT`, `BAD_RULESET` and
`NETWORK` hold the three fixed ones.

**The integer accessor.** `int_variation` returns a Python `int`. A served number that has a
fractional part, is not finite, or is outside the signed 64-bit range is `ERROR TYPE_MISMATCH` and
the caller gets the fallback; `2.0` is the integer `2`. Python's `int` is unbounded, so the contract
and the prompt would leave the range rule unchecked; the harness's scenario "an integer accessor
refuses a served fraction with TYPE_MISMATCH" serves `1e20` and expects the mismatch, so the SDK
bounds its integer accessor to the 64-bit signed range, as the Go, Java and C# SDKs do.
`bool` is never an integer here (`True` is not `1`), in the served value or in the fallback.

## Concurrency

Python threads, and a GIL that a free-threaded build may not have, so nothing relies on the GIL.

- **The held ruleset.** One immutable `Snapshot(ruleset, etag)` object. Evaluation reads the
  `self._held` attribute once into a local and uses only that, so the ruleset and the ETag a hook
  reports always belong together. A load replaces the attribute with one new `Snapshot`: one reference
  store, which a reader sees whole or not at all.
- **Locks.** There are four, each held for a handful of in-memory operations and never across I/O or a
  callback:
  1. `Sync._state_lock` guards the mutable status (`stream`, `last_sync_at`, `last_error`) and the
     replacement of `_held`, so `status()` is one consistent snapshot. `variation` never takes it;
     `status()` takes it for a few field reads.
  2. `Usage._lock` guards the pending-count dictionary and the "recording stopped" flag. `variation`
     takes it for one dictionary add. A flush takes it only to swap the dictionary out and to merge
     kept counts back in.
  3. `Usage._flush_lock` makes flushes serial, and guards the `Retry-After` hold of the events route.
     Only the flush thread, `flush()` and `close` take it, and a flush holds it across its requests;
     evaluation never touches it.
  4. `Client._close_lock` makes `close` run once and lets every other caller wait for the same
     shutdown. A call to `close` made on the thread that is already inside `close` (from a callback)
     returns at once instead of waiting for itself.
  Plus `Sync._inflight_lock`, a tiny lock around the set of sync requests `close` aborts.
  An evaluation takes only the usage lock, which is never held by a request, so a hung server or a
  hung flush cannot slow `variation` down.
- **Callbacks.** `afterEvaluation` and `onError` are called with no lock held. Code that raises an
  error collects it, releases its locks and then reports it, so a callback may call `status`,
  `variation` and even `close` on the same client without deadlock.
- **Threads.** Two, both `daemon=True`, started by `create`: `toggleslash-sync` (the first load, then
  the stream and poll loop) and `toggleslash-flush`. Neither keeps a process alive. A request in
  progress is on one of these threads or, for the final flush, on the thread that called `close`.
- **Waits.** Every wait is a `threading.Event.wait(timeout)` on one `_stop` event, so `close` wakes
  the sync loop, the flush loop and a `Retry-After` hold at once.
- **Close.** `close` takes `_close_lock` (so a second or concurrent call blocks until the first is
  done, then returns), sets the closing flag, sets the stream state to `closed`, stops recording usage
  counts, sets `_stop`, aborts the sync requests in flight (shutting their sockets down, which makes
  the blocked read return), joins the sync and flush threads (skipping the calling thread, so `close`
  from a callback on one of them does not join itself), settles `ready` (false when nothing loaded),
  and sends the final flush under `_flush_lock`. A `close` that reaches the lock after the first has
  finished returns at once.
- **Abort rule.** Only sync requests (ruleset and stream) are registered for abort. A usage request
  already in flight is left to finish, bounded by `request_timeout`, as the contract allows.
- **The tests the harness cannot run** are in `tests/`: many threads calling every operation, `close`
  racing `variation` and itself, a ruleset request and a usage flush that hang, swaps under readers
  (a hook's `etag` against its `flag_version`), and hooks that call back into the client.

## HTTP and SSE

**Client.** `http.client` (`HTTPConnection` or `HTTPSConnection` by the scheme of `url`), one
connection per request, closed after it. The URL is split with `urllib.parse.urlsplit` only to find the
host, port and any path prefix; a URL that cannot be used is a `network` failure, never an exception.
The headers set by the SDK are exactly `Authorization`, `Accept` (stream only), `If-None-Match`
(ruleset only, when an ETag is held) and `Content-Type` (events only); `http.client` adds `Host`,
`Accept-Encoding: identity` and `Content-Length`.

**The whole-request bound (7.1).** `http.client` bounds each socket operation, not the request, so the
SDK does not use its timeout for that. Each request starts a `threading.Timer` for `request_timeout`
whose callback marks the request timed out and shuts the connection's socket down; that unblocks
whatever the thread is reading (headers, a body that trickles, a body that never ends), which then
fails, and the SDK reports `timeout` because the flag is set. The connect is bounded by `request_timeout`
through the socket timeout. The timer is cancelled when the request ends. The body is read in the same window: a server that
sends headers and never the body is a `timeout`. A hostname lookup and a TCP connect are the one thing the socket
shutdown cannot interrupt (there is no socket to shut down yet): `close` waits for them, bounded by the
platform's resolver and by `request_timeout` for the connect.

**The stream (7.2).** The same timer bounds the open, from the request until the response headers
arrive; it is cancelled then. After that the socket's timeout is the watchdog (60 s, a seam for the
tests; the reader keeps its own reference to the socket, because `http.client` lets go of
`connection.sock` for an answer that ends with the connection): each `read1` call that gets no byte within it raises `TimeoutError`, which is `timeout`, and
every read that does get bytes (a keep-alive comment included) starts the 60 s again. A status other
than 200 is `http_<status>` (a 429 is also read for `Retry-After`). A clean end of the stream is not an
error.

**Server-Sent Events.** A hand-written parser in `_sse.py`, about sixty lines, to the WHATWG rules the
contract names: bytes go through an incremental UTF-8 decoder (so a character split across reads
survives); LF, CRLF and a lone CR end a line, with a CR at the end of one read remembered so its LF at
the start of the next is not a second line end; a line that starts with `:` is a comment; a field with
no `:` has an empty value; one leading space of a value is dropped; a leading U+FEFF is dropped;
several `data` lines are joined with LF; an event with no `data` line is dropped; the event name is
`message` when none was given; and an event incomplete when the stream ends is never delivered. Only
`event` and `data` are read. No SSE library is used.

## Dependencies

Runtime: none. The standard library has HTTP (`http.client`), JSON (`json`), SHA-256 (`hashlib`),
threads (`threading`) and time.

Test and lint: `unittest` and `unittest.mock` (standard library) for the tests, and `ruff` (pinned in
`mise.toml`) for lint and format. The test service uses `http.server` from the standard library.

## JSON

The parser is `json.loads` from the standard library, with two arguments:

- `parse_int` returns a Python `int` for a literal of at most 18 characters whose value is within
  plus or minus 2^53 (every such integer is exact in binary64), and a `float` otherwise. The 18
  characters only keep `int()` away from a long literal; the 2^53 test decides. `9007199254740993`
  therefore reads as `9007199254740992.0`, and a literal with thousands of digits is `inf`, not an error,
  and does not meet Python's integer-digit limit (which raises `ValueError` from `int()`). So a served
  `9223372036854775807` is the binary64 `2^63`, which is outside the integer accessor's range.
- `parse_constant` raises, so `NaN`, `Infinity` and `-Infinity` (which are not JSON) are errors.

`json.loads` reads `1e999` as `inf` and `-0.0` as `-0.0`, and keeps a lone surrogate (`"\ud800"`) as a
one-character `str` holding it (`str` is a sequence of code points and may hold a surrogate), so the
vectors' contexts and rulesets keep what the spec requires; a valid pair written as two `\u` escapes is
combined into one code point by the parser. A body is decoded as UTF-8 first; text that is not UTF-8,
or a document that is too deep, is `bad_ruleset`.

Everything that compares or tests a number goes through one helper that turns an `int` into a `float`
(an `int` too big for a `float` is `inf`, which never matches), so a context built in Python with
`9007199254740993` compares as the binary64 the spec says, and `bool` is checked before `int` everywhere,
since `True` is an `int` in Python. The public API takes a context as any `collections.abc.Mapping`
with string keys (a `dict`, whether built by hand or by `json.loads`); there is no wrapper type, because
the standard types already hold `1e999` and lone surrogates.

**Strings.** The spec compares by UTF-16 code unit and matches patterns by code point. The evaluator
converts a string to its UTF-16 form where it matters (every character above U+FFFF becomes two
surrogate characters, which are code units in a Python `str`), so `in`, `contains`, `startsWith`,
`endsWith` and the orderings are the same operations on the same units as JavaScript, and the
surrogate-pair examples of the spec hold. Patterns read both the pattern and the candidate as code
points, merging a valid pair into one and keeping a lone surrogate as itself. Hashing replaces a lone
surrogate with U+FFFD, then encodes UTF-8.

## The minimum runtime and the toolchain

- **Runtime.** CPython, the current mainstream one (PyPy is not tested).
- **Minimum version: Python 3.11**, the oldest version in upstream security support (3.10 reached end of
  life in October 2026) and the first with `enum.StrEnum`, which carries the closed sets above. Declared
  as `requires-python = ">=3.11"` in `pyproject.toml`.
- **Developed and tested on** the version `mise.toml` pins (3.15.2). CI runs that one only; the SDK uses no
  feature newer than 3.11, which a test of the minimum would confirm and this run does not have a runner for.
- **Package manager and build backend.** `pip` with `setuptools` and a `src` layout, declared in
  `pyproject.toml`: the mainstream default and a pure-Python package with no dependencies.
- **Lint and format: `ruff`** (`ruff check` and `ruff format --check`), the current mainstream choice, one
  fast tool for both, installable by mise.
- **Tests: `unittest`** from the standard library, over pytest, so the SDK needs no dependency to be
  tested and `bin/test` needs nothing mise does not pin. The vector test is a `unittest` that reads
  `fixtures/` at test time.
- **Build.** Python has no compile step; `bin/test` runs `python -m compileall -f` over the sources
  first, which fails on a syntax error, and then runs the tests under `python -X dev -W error`, which
  turns on the development checks and makes every warning an error. Python has no race detector, so
  the concurrency tests carry the weight (see above).
- **No type checker** is run: a second pinned tool and a second set of opinions about the same code. The
  code is fully annotated and ships a `py.typed` marker.
- **The integer accessor** is `int_variation`, bounded to signed 64 bits (see above).

## The package layout

```
sdks/python/
  pyproject.toml            package metadata, requires-python, ruff settings
  mise.toml                 python and ruff pins
  DESIGN.md README.md RUN.md .gitignore
  bin/lint bin/test bin/test-service
  src/toggleslash/
    __init__.py             the public API: create_client, Client, the types
    py.typed
    _types.py               ReasonKind, ErrorCode, StreamState, Reason, VariationDetail, ClientStatus, ...
    _text.py                UTF-16 form, code points, UTF-8 for hashing
    _bucket.py              SHA-256 bucketing
    _pattern.py             the `matches` parser and Pike VM (no `re`)
    _semver.py  _dates.py  _reference.py
    _validate.py            the shape checks of a ruleset, flag and segment
    _ruleset.py             compile a ruleset: flag entries, segments, the target index
    _clause.py              attribute clauses
    _context.py             context validation and normalising
    _evaluate.py            the evaluator: evaluate(ruleset, flag_key, context)
    _json.py                the JSON reader
    _http.py                one request with a whole-request bound, and the stream reader
    _sse.py                 the SSE parser
    _retry.py               reading a Retry-After
    _usage.py               usage counts, batches, the events hold
    _seams.py               the test seams (clock, wait, jitter, watchdog, transport, evaluator)
    _sync.py                the sync loop: first load, stream, backoff, Retry-After, hold
    _client.py              Client: evaluation, hooks, status, close
  tests/                    unittest: vectors, SSE, Retry-After, usage, sync, client, concurrency
  test-service/
    main.py README.md       the conformance test service and its accessor mapping
```

The evaluator modules (`_text` to `_evaluate`) import nothing from the client side and no network
module, and use no regular expression. `README.md` carries the quickstart and links to the mapping
table above. The distribution is named `toggleslash` and nothing is registered under it.

## Test seams

`Client` takes a private keyword `_seams`, a `Seams` dataclass (`_seams.py`) of the things a test
replaces: the transport (a callable returning a reply object), the monotonic and wall clocks, the wait
(so a test moves time instead of sleeping), the jitter, the stream watchdog and the evaluator. The
public `create_client` does not accept it, and nothing is read from the environment.
