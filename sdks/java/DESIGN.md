# Design: the ToggleSlash Java server SDK

This is written before the code (PROMPT.md, phase 1) and kept true afterwards. It follows
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) ("the contract") and
[`docs/evaluation.md`](../../docs/evaluation.md) ("the spec").

## 1. The mapping table

The root package is `com.github.madangryscientist.toggleslash.sdks.java`; `Client`, `Options`, `Context`,
`Detail`, `EvaluationEvent`, `Status` and `StreamState` live there. The value type `Json` is in
`...sdks.java.json`, and the evaluator's `Reason`, `ReasonKind` and `ErrorCode` in `...sdks.java.evaluation`.

| Canonical | In Java |
| --- | --- |
| `create` | `Client.create(Options)`: builds the client, starts two daemon threads, does not wait for the network, never throws (a null or broken `Options` gives a client whose every request is a `network` error). |
| `ready` | `boolean Client.ready()`: blocks until the first ruleset load has finished (bounded by `requestTimeout`) and answers whether a ruleset is held. `CompletableFuture<Boolean> Client.readyFuture()` is the same answer as a future that never completes exceptionally. Fixed at the first attempt; false after `close` if it had not settled. |
| `variation` | The typed accessors below, for example `boolean boolVariation(String flagKey, Context context, boolean fallback)`. |
| `variationDetail` | `Detail<T>` versions of each accessor, for example `Detail<Boolean> boolVariationDetail(...)`. `Detail` is a record `(T value, Integer variationIndex, Reason reason)`; `variationIndex` is null whenever `value` is the fallback. |
| `status` | `Status Client.status()`: an immutable record `(String etag, Instant lastSyncAt, StreamState stream, String lastError)`, read from one consistent snapshot. `etag`, `lastSyncAt` and `lastError` are null for none. |
| `close` | `void Client.close()`; the class implements `AutoCloseable`. Idempotent and safe to call from many threads; every caller returns after the same shutdown, which is bounded by about twice `requestTimeout`. |
| `url` | `Options.url(String)`. Required. Trailing `/` characters are removed. |
| `sdkKey` | `Options.sdkKey(String)`. Required. |
| `stream` | `Options.stream(boolean)`. Default on. |
| `pollInterval` | `Options.pollInterval(Duration)`. Default 30 s. |
| `flushInterval` | `Options.flushInterval(Duration)`. Default 30 s. |
| `requestTimeout` | `Options.requestTimeout(Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `Options.afterEvaluation(Consumer<EvaluationEvent>)`. Default none. Called synchronously on the caller's thread. |
| `onError` | `Options.onError(Consumer<Throwable>)`. Default ignore. |
| `accessor shape` | Typed methods, plus one untyped method over `Json`. Each of `boolVariation`, `stringVariation`, `doubleVariation`, `longVariation` and `jsonVariation` (the last takes and returns `Json`; a `Json.NULL` fallback accepts any served kind) has a `...Detail` twin. `longVariation` is the integer accessor and uses `long`: a served fraction or a number outside signed 64 bits is `TYPE_MISMATCH`, `2.0` is `2`. The test service's `any` and `json` accessors both map to `jsonVariation`. |
| `onError thread` | The thread where the failure happened: the sync thread (`toggleslash-sync`) for ruleset and stream failures, the flush thread (`toggleslash-flush`) or the closing thread for usage failures, and the caller's own thread for a hook that raised and for `EVALUATION_FAILED`. No SDK lock is held while it runs. |

Defaults: "not given" is told apart from false or zero because the `Options` fields are boxed (`Boolean`,
`Duration`) and null means not given. A zero or negative duration falls back to the default, as the contract
suggests.

The closed sets keep their spelling. Reason kinds are the enum `ReasonKind` (`OFF`, `PREREQUISITE_FAILED`,
`TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`), error codes the enum `ErrorCode` (the seven of the spec plus
`NOT_READY`, `TYPE_MISMATCH` and `EVALUATION_FAILED`), the stream states the enum `StreamState` whose `toString()`
and `value()` are the canonical lower case word (`off`, `connecting`, `connected`, `disconnected`, `closed`), and
`lastError` is a `String` holding exactly `http_<status>`, `timeout`, `bad_ruleset` or `network`.

## 2. Concurrency

- **State.** One immutable `State` record holds the ruleset, the ETag, `lastSyncAt`, the stream state and
  `lastError`. It sits in an `AtomicReference` and every change is a compare-and-set loop (`updateAndGet`). So the
  ruleset and its ETag swap together in one step, an evaluation reads the reference once and uses that ruleset and
  that ETag for the whole call, and `status()` is one consistent snapshot. Evaluation takes no lock at all.
- **Threads.** The SDK starts two daemon threads: `toggleslash-sync` (first load, then the loop of contract 7.3) and
  `toggleslash-flush` (usage). `close` may start a third short daemon thread, only when it is called from an SDK
  thread, to run the shutdown there (see below). Daemon threads do not keep the JVM alive. The JDK
  `HttpClient`'s own selector and worker threads are daemon threads too.
- **Waiting.** Every sleep is `CountDownLatch.await(timeout)` on the client's close latch, so `close` wakes a sleeper at
  once. No `synchronized` block exists in the SDK. The only lock is a `ReentrantLock` that serialises usage flushes
  (contract 9); evaluation records counts into a `ConcurrentHashMap` and never touches it, so a hung flush cannot slow
  an evaluation.
- **Usage counts.** `ConcurrentHashMap<UsageKey, Long>` with `merge` to add. A flush removes each key (`remove` returns
  the whole count atomically), and puts back what a failed request kept with `merge`.
- **Callbacks.** `afterEvaluation` and `onError` run with no SDK lock held and no CAS in progress. A callback that calls
  `status`, `variation` or `ready` is safe. `ready` on an SDK thread (checked by `Thread.currentThread()` being one of
  the client's own thread class) returns at once with whether a ruleset is held. `close` on an SDK thread starts the
  shutdown on a new daemon thread and returns without waiting.
- **Close.** The first `close` caller runs the shutdown (an `AtomicBoolean` decides which); the others wait on a
  `done` latch. The order is the contract's: set `closing` (state becomes `closed`, no more counts, no more `onError`
  for abandoned requests), cancel the in-flight ruleset and stream futures and wake the sleepers, join the sync and
  flush threads (bounded), then the final flush, which does not wait out a `Retry-After` and reports lost counts.
  `ready` then settles false if it had not settled.
- **Holds.** `Retry-After` holds are `volatile long` deadlines on the monotonic clock (`System.nanoTime`), one for
  the ruleset and the stream together, one for events. A later 429 can only lengthen a hold.

## 3. HTTP and SSE

- **Client.** `java.net.http.HttpClient`, forced to HTTP/1.1 (the default HTTP/2 upgrade adds `Upgrade`,
  `HTTP2-Settings` and a second `Connection` value on plain `http`, headers this SDK does not want), redirects never
  followed (a 3xx is `http_<status>`), `connectTimeout` set to `requestTimeout`.
- **A whole request, body included (7.1).** The ruleset and events requests use `sendAsync` with
  `BodyHandlers.ofByteArray()`, whose future completes only when the body has been read, and the SDK waits with
  `future.get(requestTimeout)`. On a timeout it cancels the future, which aborts the exchange, and the attempt is
  `timeout`. `HttpRequest.timeout` is not used: it ends at the response headers.
- **No closable client before 21.** The in-flight ruleset and stream futures are kept in a set and cancelled by `close`.
  Usage requests are not abandoned by `close`: contract 11 sends the final flush, and a flush already running is
  waited for.
- **The stream (7.2).** `sendAsync` with `BodyHandlers.ofInputStream()`, whose future completes at the response
  headers. The open is bounded by `future.get(requestTimeout)`. After that the sync thread reads the `InputStream`
  itself, and a single daemon `ScheduledExecutorService` thread holds the 60 s watchdog: it is armed before each read
  and re-armed after every read that returns bytes, and when it fires it sets a `timedOut` flag and closes the stream.
  Closing the JDK's stream wakes a blocked reader with end of stream, so the code checks the flag after every read
  and calls it `timeout` (never a clean end). The watchdog length is a test seam on the package-internal config.
  `BodyHandlers.fromSubscriber` is not used: it answers only when the body ends (the Java language notes).
- **SSE.** A hand-written parser (`SseParser`, about 80 lines) to the WHATWG rules the contract names: bytes go
  through the same streaming WHATWG UTF-8 decoder as JSON (a sequence split across reads is kept), CR, LF and CRLF all
  end a line (a CR at the end of a chunk waits for the next byte to see whether an LF follows), a leading `:` is a
  comment, one optional space after the colon is dropped, `data` lines join with LF, an event with no data is dropped,
  an event left incomplete at the end is discarded, and the event name defaults to `message`.
- **Retry-After (7.4).** Only on a 429, only whole decimal ASCII digits (spaces around are ignored), capped at 300 s.
  Digit strings longer than 9 characters clamp to the cap without overflow.

## 4. Dependencies

Runtime dependencies: **none**. The JDK has HTTP (`java.net.http`), SHA-256 (`MessageDigest`), time (`java.time`) and
concurrency. JSON is a hand-written parser (section 5) because no JDK JSON API exists, and a library would not keep the
numbers, lone surrogates and BOM rules the spec and contract name.

Test and build dependencies, each with its purpose:

| Tool | Purpose |
| --- | --- |
| JUnit Jupiter | the native tests and the vector runner |
| Maven (via mise) with `maven-compiler-plugin`, `maven-surefire-plugin`, `build-helper-maven-plugin` | build, run tests, add `test-service/src` as a test source root |
| Checkstyle (the mise tool, a bare JAR) | lint rules, run by `bin/lint` |
| google-java-format (the mise tool) | format check, run by `bin/lint` |

## 5. JSON

`Json` is a sealed interface with `Json.NULL`, `Json.Bool`, `Json.Num(double)`, `Json.Str(String)`, `Json.Arr(List)` and
`Json.Obj(Map)`. `Json.parse(byte[])` and `Json.parse(String)` read it with a hand-written recursive descent parser:

- Numbers are read with `Double.parseDouble` after the token is checked against the JSON grammar, so they are binary64,
  `1e999` is Infinity (not an error), `-0.0` is negative zero and `9007199254740993` reads as `9007199254740992`.
- A Java `String` is a sequence of UTF-16 code units and holds a lone surrogate as it is, so `"\ud800"` is kept and is
  equal only to the same lone surrogate. A valid escaped pair (`"😀"`) is the same two units as the literal
  emoji, which is what a Java string would do anyway.
- A byte body is decoded by `Utf8.decode`, a hand-written WHATWG UTF-8 decoder (one U+FFFD per maximal invalid
  subpart; a leading BOM is skipped). The JDK's `CharsetDecoder` with `REPLACE` and `String.getBytes(UTF_8)` differ
  from the WHATWG rules, so neither is used. `Utf8.encode` writes the hashing bytes, turning a lone surrogate into
  U+FFFD (EF BF BD), never `?`. Both are tested against the Encoding standard's examples.
- A duplicate key in an object keeps the last value. Nesting deeper than 1000 levels is refused as a parse error
  (`bad_ruleset`); the contract needs at least 512.
- The public API takes a context as `Context`, which wraps a `Json` object, so a context can carry `1e999` and a lone
  surrogate (the vectors' contexts do). `Context.user(key)`, `Context.of(kind, key, attributes)`, `Context.multi(...)`
  and `Context.of(Json)` build one.

## 6. Minimum runtime and toolchain

- **Minimum runtime: Java 17** (`maven.compiler.release` 17). It is the oldest long-term-support release that still has
  `java.net.http.HttpClient` with `BodyHandlers.ofInputStream`, records, sealed interfaces and pattern matching for
  `instanceof`, which keep the code short. The SDK uses nothing newer; it is built and tested on the pinned JDK.
- **Pinned toolchain** (`mise.toml`): Temurin 25.0.5+8, the newest long-term-support release on the run date, Maven
  3.10.0, Checkstyle 14.4.0 and google-java-format 2.0.0 (1.37.0 cannot run on JDK 25). The versions came from
  `mise ls-remote`.
- **Test framework:** JUnit Jupiter. **Build tool:** Maven, the mainstream one. **Lint:** Checkstyle plus
  google-java-format, plus `javac -Xlint:all -Werror`.
- **Integer accessor:** `long`, the 64-bit signed type.

## 7. Package layout

```text
sdks/java/
  pom.xml  mise.toml  checkstyle.xml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/            Client, Options, Context, Detail, ...
  src/main/java/.../sdks/java/json/                                            Json, Utf8
  src/main/java/.../sdks/java/evaluation/                                      the evaluator, no network code
  src/test/java/...                                                            native tests (the same packages)
  test-service/src/...                                                         the conformance test service
  test-service/README.md
```

The evaluator package imports nothing from the client and nothing from `java.net`. The client package imports the
evaluator. Maven group `com.github.madangryscientist.toggleslash`, artifact `toggleslash-sdk-java`. Nothing is
registered under these names.
