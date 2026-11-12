# ToggleSlash Java server SDK: design

This is the design written before the code (phase 1 of `docs/sdk-authoring/PROMPT.md`). It follows
[`docs/sdk-contract.md`](../../docs/sdk-contract.md), evaluates to
[`docs/evaluation.md`](../../docs/evaluation.md) and is driven by the harness in
[`docs/sdk-test-service.md`](../../docs/sdk-test-service.md). Input was `language: Java` and nothing else, so
the runtime is the JVM (Temurin) and the build tool is Maven.

## Minimum runtime and toolchain

- **Toolchain pinned to the newest LTS:** Temurin 25.0.5+8 (Java 25 LTS) and Maven 3.10.0, in `mise.toml`.
  The newest LTS is pinned and not a feature release, because a feature release leaves support after six
  months. Checkstyle 14.4.0 is pinned there too (it ships as a bare JAR).
- **Minimum runtime: Java 17**, set with `--release 17`, so the compiler rejects any API newer than 17. Java 17 is
  the oldest LTS that is still widely deployed and it has records, text blocks, `var`, sealed types and
  `java.net.http.HttpClient`, which is all this SDK needs. Java 11 would work for the client but would lose
  records, and 8 has no `HttpClient`. The cost of 17: `HttpClient` is not `AutoCloseable` (that came in 21), so
  `close` cannot close it and every request is aborted on its own instead (below).
- Maven coordinates: group `com.github.madangryscientist.toggleslash`, artifact `toggleslash-sdk-java`,
  root package `com.github.madangryscientist.toggleslash.sdks.java`.

## Dependencies

- **Runtime: none.** The SDK uses the JDK only: `java.net.http.HttpClient`, `java.security.MessageDigest`
  (SHA-256 for bucketing), `java.util.concurrent`. No JSON library (see JSON), no regular expression engine
  (`evaluation.md` forbids a platform one), no logging library.
- **Test:** JUnit Jupiter 6.1.3 (the newest release at the time of writing; it needs Java 17, matching the
  minimum). Surefire 3.6.1 runs it. JUnit is the decision because it is what every Maven user expects, it needs
  no second runner, and a test class is plain Java.
- **Build tooling:** maven-compiler-plugin 3.16.0 with `<release>17</release>`, maven-surefire-plugin 3.6.1,
  build-helper-maven-plugin 3.8.0 (adds `test-service/src` as a test source root), Spotless 3.11.0 with
  google-java-format 2.0.0 (1.37.0 fails on JDK 25), and Checkstyle 14.4.0 run as a JAR from `bin/lint`.
  Stable (non-beta) releases only. The first `mvn` run is online (it caches plugins); later runs work offline.

## Mapping table

| Canonical | In Java |
| --- | --- |
| `create` | `Client.create(Options)`, a static factory. It starts two daemon threads and returns at once. It never throws: anything it cannot use makes each request a `network` error. |
| `ready` | `boolean Client.ready()`. Blocks until the first ruleset attempt settles (bounded by `requestTimeout`), answers whether a ruleset is held, and gives the same answer afterwards. Called on one of the SDK's own threads it returns the state held without waiting. After `close` it answers false if it had not settled. |
| `variation` | Typed `boolVariation(key, context, boolean)`, `stringVariation(key, context, String)`, `floatVariation(key, context, double)` (the contract's "number"), `intVariation(key, context, long)` and the generic `variation(key, context, Object fallback)`, which also serves the harness's `json` and `any` accessors. `context` is an `Object`: a `Map<String, Object>` in the JSON shape of `evaluation.md` (single or multi), and anything else is `ERROR INVALID_CONTEXT`. |
| `variationDetail` | One per accessor: `boolVariationDetail`, `stringVariationDetail`, `floatVariationDetail`, `intVariationDetail`, `variationDetail`. Each returns a `Detail<T>` record `(value, variationIndex, reason)`. |
| `status` | `Status Client.status()`, a record `(etag, lastSyncAt, stream, lastError)` read from one immutable snapshot. `etag` and `lastError` are `String` or null, `lastSyncAt` is an `Instant` or null, `stream` is a `StreamState`. |
| `close` | `void Client.close()`. `Client` implements `AutoCloseable` and `close()` declares no checked exception. Idempotent and safe to race. |
| `url` | `Options.builder(url, sdkKey)`, first argument. Trailing `/` removed. |
| `sdkKey` | `Options.builder(url, sdkKey)`, second argument. |
| `stream` | `Options.Builder.stream(boolean)`. Not given is on: the builder holds a `Boolean` that is null until set, so "not given" differs from `false`. |
| `pollInterval` | `Options.Builder.pollInterval(Duration)`. Not given (null), zero or negative falls back to 30 s. |
| `flushInterval` | `Options.Builder.flushInterval(Duration)`. Same rule, 30 s. |
| `requestTimeout` | `Options.Builder.requestTimeout(Duration)`. Same rule, 10 s. |
| `hooks.afterEvaluation` | `Options.Builder.afterEvaluation(Consumer<EvaluationEvent>)`. `EvaluationEvent` is a record `(flagKey, context, value, variationIndex, reason, etag, flagVersion)`. The int accessor's `value` is a `Long`, so there is no precision loss above 2^53. `flagVersion` is a `Long`, null when the flag is missing or malformed or the version is above 2^63 minus 1. |
| `onError` | `Options.Builder.onError(Consumer<Throwable>)`. Not given ignores. |
| `accessor shape` | Typed methods plus one generic method over the fallback's runtime kind, each with a `...Detail` form (see `variation`). `intVariation` answers `TYPE_MISMATCH` for a fractional or out-of-int64 number; `2.0` is the integer 2; the largest servable integer is 2^63 minus 1024. A null fallback is possible only on the generic method (a primitive cannot be null) and accepts any kind. |
| `onError thread` | `onError` runs on the thread that met the failure: the SDK's sync thread for a ruleset or stream failure, its flush thread for a usage failure, the caller's thread for a hook that threw or an `EVALUATION_FAILED`, and the closing thread (or the close helper thread) for counts lost at `close`. It is never called while the SDK holds a lock. |

**Closed sets.** `ReasonKind` (`OFF`, `PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`,
`ERROR`) and `ErrorCode` (the seven evaluator codes plus `NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`)
are enums whose constant names are the canonical spelling. `StreamState` is an enum whose constants are the
upper-case words (`OFF`, `CONNECTING`, `CONNECTED`, `DISCONNECTED`, `CLOSED`) and whose `value()` is the
canonical lower-case spelling. `lastError` is an open set (`http_<n>`), so it is a `String` and the fixed codes
are constants in `LastError` (`timeout`, `bad_ruleset`, `network`). A `Reason` record carries the kind plus the
optional `errorCode`, `ruleIndex` and `flagKey`.

**Options not given.** A builder field is a boxed type (`Boolean`, `Duration`, `Consumer`) that is null until set,
so every default is applied in one place and `stream(false)` is different from not calling `stream`.

**Extra options for tests** (the contract allows them; they are package private on the builder so the public
API stays the canonical eight): a time source, a jitter source, a pacer that sleeps, and the stream watchdog
length. `Client.flush()` is a public extra that sends the pending counts now under the rules of section 9.

## Concurrency

- **One immutable state in one `AtomicReference`.** `State(ruleset, etag, lastSyncAt, stream, lastError)`.
  Every change is a compare-and-set of a new state built from the old (`updateAndGet` with a pure function).
  The ruleset and its ETag therefore swap together, a reader of `variation` sees the old pair or the new pair,
  and `status` is one snapshot and a hook's `etag` is the one of the ruleset the evaluation used.
- **`variation` and `status` take no lock and do no I/O.** They read the reference, run the evaluator and
  record one usage count. The only lock they touch is the usage map's own (`Usage.lock`), which is held for one
  hash-map add or a swap and never during a request.
- **Two daemon threads.** `toggleslash-sync` does the first load, the stream and the polls. `toggleslash-flush`
  waits `flushInterval` and flushes. Both are daemon threads, so an unclosed client does not keep the JVM alive.
  A third daemon thread (`toggleslash-close`) exists only when `close` is called from one of the SDK's own
  threads, to run the shutdown without making that thread wait for itself.
- **Waiting.** Sleeps are `CountDownLatch.await(timeout)` on a `closed` latch, so `close` wakes them at once.
  Holds (Retry-After) are absolute monotonic times (`System.nanoTime`), not sleeps.
- **Callbacks outside locks.** `afterEvaluation` runs on the caller's thread before the call returns, after the
  result is built. `onError` is called with no lock held. A callback that calls `variation`, `status`, `ready` or
  `close` cannot deadlock.
- **`ready`.** A `CompletableFuture<Boolean>` completed once, when the first attempt ends, with "a ruleset is
  held". `close` completes it with false if it is not complete, and `complete` keeps the first value, so the
  answer never changes. Called from `toggleslash-sync` or `toggleslash-flush` it uses `getNow` and does not wait.
- **`close`.** The first caller wins a compare-and-set on a `closing` flag and does, in order: sets the state to
  `CLOSED`, stops usage counting (inside the usage lock), counts down the `closed` latch, aborts the exchange in
  flight on the sync thread, joins the sync thread, joins the flush thread (bounded by `requestTimeout`, since a
  flush in flight is bounded by it), then sends the final flush ignoring the usage hold and reports lost counts
  to `onError`, then counts down a `done` latch. Every other caller waits on `done`. A caller that is itself an
  SDK thread does not wait and does not join itself: the first such caller hands the shutdown to the helper
  thread and returns. `close` returns within about twice `requestTimeout` when a request hangs (one join
  bounded by `requestTimeout`, one final-flush request bounded by it).

## HTTP and SSE

- **One `java.net.http.HttpClient`**, HTTP/1.1 only (so no h2c upgrade headers on cleartext), no redirect
  following (a 3xx is `http_<n>`), no proxy, no cookie handler, a connect timeout of `requestTimeout`.
  Notes said to keep two clients because none is closable before 21. The need behind it, aborting ruleset and
  stream work at `close` while the final usage flush still goes out, is met here by aborting each exchange
  itself, so one client is enough; this is recorded as a decision in `RUN.md`.
- **The body is a hand-over `BodySubscriber`.** `HttpRequest.timeout` ends at the response headers and
  `BodyHandlers.fromSubscriber` answers only when the body ends, so the stream open would time out. The handler
  returns a subscriber whose `getBody()` is already complete, so the response future completes at the headers,
  and the subscriber puts each chunk (copied to a `byte[]`) on a queue and asks for the next one only after the
  reader took the last (demand of one, so memory stays bounded). The reader (`Exchange`) polls the queue with a
  deadline.
- **`Exchange`** wraps one request: `status()`, a header lookup, `read(timeoutNanos)` returning the next chunk or
  an end marker or throwing `TimeoutException` or `IOException`, and `abort()` (cancels the subscription and the
  request, wakes the reader). The request deadline for ruleset and events requests is one absolute time, taken
  when the request starts, that covers the headers and the whole body. The stream's open uses `requestTimeout`
  until the headers, then each `read` uses the 60 s watchdog, which every chunk restarts.
- **The 200 ruleset body** is collected, skipped over a leading BOM, decoded with the SDK's own WHATWG UTF-8
  decoder and parsed with the SDK's own JSON parser.
- **SSE** is parsed by `SseParser`, a byte-fed state machine: it decodes UTF-8 incrementally with the same
  WHATWG decoder (a character split across reads is kept), ends a line on LF, CRLF or a lone CR (a CR at the end
  of a chunk is held until the next byte says whether an LF follows), skips comment lines, strips a leading BOM
  once, reads `event` and `data`, joins `data` lines with LF, drops an event with no `data` line, names an
  event `message` when none was given, and discards an event still incomplete at the end.
- **Headers sent:** `Authorization: Bearer <key>` on all three routes; `Accept: text/event-stream` on the
  stream; `If-None-Match` on a ruleset request only when a ruleset and a non-empty ETag are held;
  `Content-Type: application/json` on events. The JDK client adds `Host`, `User-Agent: Java-http-client/<ver>`
  and, on a body, `Content-Length`. No other header. A header-capture test prints the table (see `RUN.md`).
- **Retry-After:** counts only on a 429, only as one or more ASCII digits (spaces around ignored), capped at 300 s.
  A hold for ruleset plus stream is one field, the usage hold is another; both only ever move later.

## JSON

- **Own parser** (`evaluation/Json`), because the JDK has none and the contract wants specific behaviour:
  a duplicate key keeps the last value, a leading BOM is skipped, numbers are binary64 (`Double.parseDouble` on
  text the parser has already checked against the JSON grammar; `1e999` becomes infinity, as `evaluation.md`
  needs), strings keep a lone surrogate written as an escape, and nesting deeper than 1000 levels is refused
  as `bad_ruleset` (the contract needs at least 512; the parser is recursive and 1000 is well inside the default
  stack).
- **Value model:** `null`, `Boolean`, `Double`, `String`, `List<Object>` (`ArrayList`) and `Map<String, Object>`
  (`LinkedHashMap`). A number is always a `Double`; a caller may pass any `Number` in a context and it is read as
  a `double`.
- **UTF-8:** `evaluation/Utf8` holds a WHATWG decoder (one U+FFFD per maximal invalid subpart) and an encoder
  (a lone surrogate becomes U+FFFD, bytes `EF BF BD`). The JDK's `CharsetDecoder` and `getBytes` are not used for
  the spec. Both are tested against the examples in the Encoding standard.
- **Evaluation** is a port of the structure of `docs/evaluation.md`: validate at load (`Ruleset`), then
  `Evaluator` in the order of the spec, `Clauses` for the ops, `Semver`, `Dates`, and `Patterns` (a parser to a
  small program run by a Pike VM over code points, with the size and length limits). Bucketing hashes the
  spec's string with `MessageDigest` SHA-256 over bytes from `Utf8.encode`.

## Package layout

```
sdks/java/
  mise.toml  pom.xml  checkstyle.xml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/
    Client, Options, Detail, StreamState, Status, LastError, EvaluationEvent, ToggleSlashException            (public API)
    Core, State, SyncLoop, Exchange, Http, SseParser, Usage, Pacer, Hold, SdkThread                          (package private)
    evaluation/  Reason, ReasonKind, ErrorCode (public: they appear in Detail and EvaluationEvent), Json, Utf8,
                 Ruleset, Evaluator, Result, Contexts, Semver, Dates, Patterns, Bucket (public), Clauses, Shape (package private)
  src/test/java/.../  JUnit tests: vectors, units, fake server, stress
  test-service/src/.../service/Main.java   (the harness's test service; a test source root)
  test-service/README.md                   (the accessor mapping)
```

The evaluation package is an implementation detail: an application uses it only through `Client`. Its types are
public where `Detail`, `EvaluationEvent` or the tests need them, and package private otherwise.
