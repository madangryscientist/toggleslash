# Python SDK: design

The Python server SDK for ToggleSlash. It keeps [`docs/sdk-contract.md`](../../docs/sdk-contract.md) (the
contract) and evaluates to [`docs/evaluation.md`](../../docs/evaluation.md) (the spec). Standard library
only at run time.

## Inputs and decisions taken

The brief named only the language, so every other input was absent and is a decision:

- **Runtime:** CPython. It is the mainstream runtime; PyPy is not tested.
- **Pinned toolchain:** CPython 3.15.2 and ruff 0.17.4 in `mise.toml`. 3.15 is the newest feature release
  (released 2026-10-09, supported until 2031-10-31, per endoflife.date on the run date, 2026-11-10) and Python
  has no long-term-support line, so the newest stable release is the pin.
- **Minimum version:** Python 3.11 (`requires-python = ">=3.11"`). 3.10 left security support on
  2026-10-01, before the run date, so 3.11 (supported until 2027-10-31) is the oldest release in support.
  The SDK needs `enum.StrEnum` and `datetime.UTC` (both 3.11) and nothing newer.
- **Package manager and build tool:** `pyproject.toml` with the setuptools backend, which every pip and uv
  can build; the SDK has no dependency, so no resolver choice matters at run time. `uv` is not pinned
  because nothing in `bin/` needs it.
- **Test framework:** `unittest` from the standard library, not pytest. The `bin/` scripts may need nothing
  beyond what `mise.toml` pins and a stock runner has, and pytest would need a network install into a
  virtualenv. `unittest` runs under `python -X dev -W error`, which is the nearest thing Python has to a
  race detector (it turns every unraisable exception, unclosed resource and thread exception into a failure).
- **Lint and format:** ruff (`ruff check` and `ruff format --check`), the mainstream single tool, pinned
  through mise. Type annotations are written throughout; no separate type checker is pinned (mypy and pyright
  are not in the mise registry or need a pip install, and `ty` is still a beta).
- **Build step:** Python has nothing to compile, so `bin/test` byte-compiles the package, the tests and the
  test service with `python -m compileall -f` under `-W error` first. A syntax error anywhere, in a file no
  test imports, fails the build.

## The mapping table

Names are the canonical names of contract section 2, written in the first column. `from toggleslash import
create_client` is the entry point; `Client` is the class it returns.

| Name | In Python |
| --- | --- |
| `create` | `toggleslash.create_client(*, url, sdk_key, ...)` returns a `Client`; `Client(...)` takes the same arguments. Keyword-only. It starts two daemon threads and returns at once. It never raises. |
| `ready` | `Client.ready() -> bool`. Blocks the calling thread until the first load settles (bounded by `request_timeout`); the answer is fixed once. |
| `variation` | `Client.variation(flag_key, context, fallback)`: the generic accessor, applying the mismatch rule by the fallback's JSON kind. Typed accessors: `bool_variation`, `string_variation`, `float_variation`, `int_variation`, `json_variation`. |
| `variationDetail` | `Client.variation_detail(flag_key, context, fallback) -> Detail`, with `bool_variation_detail`, `string_variation_detail`, `float_variation_detail`, `int_variation_detail`, `json_variation_detail`. `Detail` is a frozen dataclass: `value`, `variation_index` (`None` for the fallback), `reason`. |
| `status` | `Client.status() -> Status`, a frozen dataclass: `etag`, `last_sync_at` (an aware UTC `datetime`, or `None`), `stream` (a `StreamState`), `last_error` (a `str` code or `None`). |
| `close` | `Client.close()`. Idempotent; concurrent calls all return once the one shutdown is done. `with create_client(...) as client:` calls it on exit. |
| `url` | `url`, required, a `str`. Trailing `/` removed. |
| `sdkKey` | `sdk_key`, required, a `str`. |
| `stream` | `stream: bool = True`. |
| `pollInterval` | `poll_interval: float \| timedelta = 30.0` (seconds). |
| `flushInterval` | `flush_interval: float \| timedelta = 30.0` (seconds). |
| `requestTimeout` | `request_timeout: float \| timedelta = 10.0` (seconds). |
| `hooks.afterEvaluation` | `hooks=Hooks(after_evaluation=callable)`, default `None`. The callable gets one `EvaluationEvent` (`flag_key`, `context`, `value`, `variation_index`, `reason`, `etag`, `flag_version`). |
| `onError` | `on_error: Callable[[Exception], object] \| None = None`, default ignore. Every absorbed failure arrives as an `Exception`; the SDK's own are `ToggleSlashError` with a `code`. |
| `accessor shape` | Both: one generic `variation` / `variation_detail` over the fallback's JSON kind, and typed methods for bool, string, float, int and json. The integer accessor (`int_variation`) applies the fractional-part and signed 64-bit rules of contract section 4. |
| `onError thread` | The thread that saw the failure: the SDK's sync thread for ruleset and stream failures, its flush thread (or the thread calling `close`) for usage failures and counts lost at close, and the caller's own thread for an evaluation or hook that raised. Never while an SDK lock is held. |

**Closed sets.** They are `enum.StrEnum`s whose values keep the canonical spelling, so they compare equal to
the plain string: `ReasonKind` (`OFF`, `PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`,
`ERROR`), `ErrorCode` (the evaluator's seven plus `NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`) and
`StreamState` (`off`, `connecting`, `connected`, `disconnected`, `closed`). `Reason` is a frozen dataclass
(`kind`, and `rule_index`, `flag_key` or `code` where the kind carries one). `Status.last_error` is the plain
string of contract section 8 (`http_401`, `timeout`, `bad_ruleset`, `network`), because `http_<status>` is an
open family.

**"Not given" and zero.** The defaults live in the keyword defaults (`stream=True`, `poll_interval=30.0`), so
an omitted argument is the default and `stream=False` is false. `None` is also taken as "not given" for the
three durations and for `stream`. A duration that is zero, negative, not a number, `nan` or `inf` falls
back to the default (contract section 2, "SHOULD", untested). `bool` is rejected as a duration.

**Extras** (all contract 13): the typed accessors; `Client.flush()` (send the pending counts now, under the
section 9 rules); the context manager. Contexts are plain dicts in the spec's shape; there is no builder.

## Concurrency (contract 6)

CPython has threads and, from 3.13, an optional free-threaded build, so the SDK is written for real
parallelism and does not lean on the GIL.

- **One immutable state, one reference.** The ruleset, its ETag, `last_sync_at`, the stream state and
  `last_error` live in one frozen `State` (`state.py`). Writers (the sync thread, `close`) build a new `State`
  under the `StateBox` lock and assign it to one attribute; the lock covers only that build, never I/O and
  never a callback. Readers (`variation`, `status`, the hook event) read the attribute once into a local and
  use only that, so the ruleset and its ETag are swapped together by one assignment, `status` is one
  consistent snapshot, and the hook's `etag` and `flag_version` come from the same ruleset the value did.
- **Threads.** Two daemon threads, both started by `create`: `toggleslash-sync` (first load, then the loop of
  contract 7.3, including reading the stream) and `toggleslash-flush` (waits `flush_interval`, flushes,
  repeats). Daemon threads mean a client that is never closed does not keep the interpreter alive
  (`tests/test_exit.py` runs a child process that never closes one). `close` sets the stop event, stops
  counting, aborts the sockets of the ruleset and stream requests in flight (below), joins the two threads
  with a bound of `request_timeout` + 2 s (unless the caller *is* one of them, as when `on_error` calls
  `close`: a thread cannot join itself, so it skips it), settles `ready` as false, then runs the final flush.
- **Locks, and why evaluation never waits on a request.** `StateBox` lock (above); `UsageReporter`'s counts
  lock (a dict add or the swap-out of the whole dict, never held across I/O) and flush lock (held across a
  usage request; taken only by the flush thread and `close`, never by `variation`); the client's ready lock
  (settles `ready` once) and close lock (an `RLock`, so a second `close`, or `close` from a callback running
  inside the first, does not deadlock and every concurrent call returns after the one shutdown); the guard
  set's lock in `http.py`. No lock is held while `after_evaluation` or `on_error` runs.
- **Closing the sockets.** A read blocked in `recv` is not woken by `socket.close()` from another thread on
  Linux. Every request therefore has a `Guard` that owns its socket (taken over in the connect hook, because
  `http.client` clears `connection.sock` after `getresponse()` for an answer that ends with the connection)
  and can `shutdown(SHUT_RDWR)` it from another thread. `close` aborts all live ruleset and stream guards. A
  usage request already in flight is not abandoned (contract 6: `close` sends the final flush), and is bounded
  by `request_timeout` like any other, so `close` with a hung events route takes at most about twice that.
- **Ready.** An `Event` plus the value, settled once: by the first load, or by `close` as false. Called from
  one of the SDK's own threads (an `on_error` callback) it answers at once with what is held, since waiting
  would wait for itself.

## HTTP and SSE

- **Client.** `http.client`, one connection per request, closed after it. Redirects are not followed (a `3xx`
  is `http_<status>`). The headers sent are `Host`, `Accept-Encoding: identity` and `Authorization`, plus
  `If-None-Match` on a poll that holds a ruleset, `Accept: text/event-stream` on the stream, and
  `Content-Type` and `Content-Length` on the usage POST. No `User-Agent`, no `Connection`. A test
  (`tests/test_sync.py`) and a captured request in `RUN.md` hold that.
- **`request_timeout` bounds the whole request, body included.** `http.client` bounds one socket operation, so
  a server that drips a byte a second would never time out. Each request starts a `threading.Timer` that marks
  its guard timed out and shuts the socket down; the error that follows is reported as `timeout`. Host name
  resolution runs on a helper thread and is abandoned at the deadline, because `getaddrinfo` has no timeout
  of its own.
- **Stream.** The timer bounds only the open (request until the response headers). Then the socket gets
  `settimeout(60)`: a `recv` that sees no byte for 60 s raises `TimeoutError`, which is `timeout`, and every
  read (a keep-alive comment included) restarts it. The watchdog length is a test seam, so a test reads a real
  loopback socket that goes quiet for 0.4 s.
- **SSE.** A hand-written incremental parser (`sse.py`): bytes in through an incremental UTF-8 decoder (so a
  character split across reads survives), lines ended by LF, CRLF or a lone CR (a CR at the end of a read
  waits to see whether an LF follows), a leading BOM dropped, `:` comment lines ignored, `event` and `data`
  read and every other field ignored, several `data` lines joined with LF, an event with no `data` line
  dropped, the name `message` when none was given, and an event unfinished at the end of the stream discarded.

## Dependencies

- **Runtime: none.** The standard library has HTTP (`http.client`), JSON (`json`), SHA-256 (`hashlib`),
  threads (`threading`) and time.
- **Test:** `unittest`, `socketserver`/`http.server` (loopback fake servers). Nothing installed.
- **Lint:** ruff, installed by mise.

## JSON

`json.JSONDecoder` from the standard library, with three arguments (`_json.py`):

- `parse_int` returns an `int` only when it is exactly representable as binary64 (at most 2^53 in magnitude)
  and the `float` otherwise, so `9007199254740993` reads as `9007199254740992.0`, a literal of 5000 digits
  is `inf` rather than the interpreter's integer-digit-limit `ValueError`, and `1` stays the `int` `1` for
  a caller.
- `parse_float` is `float`: `1e999` is `inf`, `-0.0` stays negative zero.
- `parse_constant` raises, because `NaN`, `Infinity` and `-Infinity` are not JSON and the module accepts them
  by default.
- A leading byte order mark is stripped by decoding the body as UTF-8 and removing one `U+FEFF`; invalid
  UTF-8 is `bad_ruleset`. A duplicate key keeps the last value (the module's behaviour). `json` combines a
  valid surrogate pair written as two escapes into one code point and keeps a lone surrogate such as
  `"\ud800"` as it is: a Python `str` is a sequence of code points and can hold a lone surrogate. Hashing and
  the UTF-16 comparisons convert deliberately (below).
- **The context** is a plain Python value: a `dict` of `str` to `str`, `int`, `float` (which holds `inf`),
  `bool`, `None`, `list` and `dict`. A `str` keeps a lone surrogate and a `float` keeps `1e999`, so the
  vectors' contexts reach the SDK intact through the test service, which parses with the same function. No
  raw-JSON form is needed. `Mapping` and `tuple` are accepted for objects and arrays.
- **Strings in the evaluator.** Equality is the `str` equality of code points. The UTF-16 rules (order,
  `contains`, `startsWith`, `endsWith`) work on a string of code units (`evaluator/text.py`: astral characters
  split into surrogate pairs), so `"😀"` sorts below `"～"` and a lone high surrogate is found inside a pair.
  Hashing encodes with the lone surrogate replaced by U+FFFD. The pattern matcher works on code points.

## Minimum runtime, toolchain

Python 3.11, for the reason in "Inputs and decisions". The integer accessor uses Python's `int`, bounded to
the signed 64-bit range by hand because Python integers are unbounded; a served number outside it, or with a
fractional part, is `TYPE_MISMATCH`, and `2.0` is the integer `2`.

## Package layout

The distribution is `toggleslash-sdk` and the import package is `toggleslash`. Python's convention is a
plain name, not a host, so the repository path of the prompt is not used in the name.

```
sdks/python/
  pyproject.toml  mise.toml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/toggleslash/
    __init__.py            the public API
    types.py               Detail, Status, EvaluationEvent, Hooks, StreamState, ToggleSlashError
    client.py              Client: options, evaluation, the integer rule, hooks, close
    state.py               State and StateBox: the ruleset and its ETag, swapped together
    sync.py                the sync thread: the loop, the ruleset request, the stream, the 429 holds
    usage.py               counts, flush, batches, the usage hold
    http.py                Guard, GuardSet, Base, request, open_stream, retry_after
    sse.py                 the SSE parser
    clock.py               the clock seam, the system clock, utc_day
    _json.py               the JSON parser
    evaluator/             no network code; imports nothing outside the standard library and itself
      __init__.py          evaluate, create_ruleset, Reason, ErrorCode, ReasonKind
      result.py  validate.py  context.py  ruleset.py  evaluate.py  clause.py  reference.py
      text.py  numbers.py  semver.py  dates.py  pattern.py  bucket.py
  tests/                   unittest; test_vectors.py reads fixtures/evaluation; fakes.py is a loopback server
  test-service/            main.py (the conformance test service) and README.md
```

The test service is byte-compiled with the tests by `bin/test`, and `bin/test-service` runs it with
`exec python`. It builds nothing, so the harness's 30 s wait for `listening` is never at risk.
