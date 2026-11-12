# Java SDK: design

The Java server SDK for ToggleSlash. It keeps one environment's ruleset current and evaluates flags locally,
to [`docs/evaluation.md`](../../docs/evaluation.md) and [`docs/sdk-contract.md`](../../docs/sdk-contract.md).
It is private and unpublished. Written before any code, to
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md).

## 1. The mapping table

Package `com.github.madangryscientist.toggleslash.sdks.java` (the root package, `Client` and friends). Types
the evaluator shares with the public API (`ReasonKind`, `ErrorCode`, `Reason`) live in
`...sdks.java.evaluation`; the JSON value model and parser in `...sdks.java.json`.

| Contract name | In Java |
| --- | --- |
| `create` | `Client.create(Options)`: builds the client, starts its two daemon threads and returns at once. |
| `ready` | `boolean Client.ready()`: blocks until the first load has settled (bounded by `requestTimeout`) and answers true if a ruleset is held. The answer is fixed when the first attempt settles. Called on one of the SDK's own threads (from `onError`) it returns at once with whether a ruleset is held. |
| `variation` | One typed method per kind, no generic one: `boolean boolVariation(String, Context, boolean)`, `String stringVariation(String, Context, String)`, `long longVariation(String, Context, long)`, `double numberVariation(String, Context, double)` and `Object jsonVariation(String, Context, Object)`. |
| `variationDetail` | The same five with a `Detail` suffix, returning `Detail<T>` (`boolVariationDetail`, `stringVariationDetail`, `longVariationDetail`, `numberVariationDetail`, `jsonVariationDetail`). `Detail<T>` is a record `(T value, Integer variationIndex, Reason reason)`; `variationIndex` is `null` for a fallback. |
| `status` | `Status Client.status()`: an immutable record `(String etag, Instant lastSyncAt, StreamState stream, String lastError)`; `etag`, `lastSyncAt` and `lastError` are `null` when there is none. |
| `close` | `void Client.close()`: blocks until the shutdown is done; safe to call twice or from many threads; `Client` is also `AutoCloseable`. It declares no checked exception. |
| `url` | `Options.Builder.url(String)`. Required. Trailing `/` are removed. |
| `sdkKey` | `Options.Builder.sdkKey(String)`. Required. |
| `stream` | `Options.Builder.stream(boolean)`. Default on. |
| `pollInterval` | `Options.Builder.pollInterval(Duration)`. Default 30 s. |
| `flushInterval` | `Options.Builder.flushInterval(Duration)`. Default 30 s. |
| `requestTimeout` | `Options.Builder.requestTimeout(Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `Options.Builder.afterEvaluation(AfterEvaluation)`, a functional interface taking an `EvaluationEvent` record `(String flagKey, Context context, Object value, Integer variationIndex, Reason reason, String etag, Long flagVersion)`. Default none. |
| `onError` | `Options.Builder.onError(Consumer<Throwable>)`. Default ignore. |
| `accessor shape` | Typed methods only: `bool`, `string`, `long` (the integer accessor, Java `long`), `number` (`double`) and `json` (any kind, the fallback may be `null`). No generic method, because Java erases `T` and a `null` fallback or a `Long` versus `Double` fallback could not tell the kinds apart. Each has a `Detail` twin. |
| `onError thread` | The thread that hit the failure: the SDK's sync thread (ruleset and stream failures), its flush thread (usage failures), the caller's thread (a hook that raised, an evaluation that raised) and the closing thread (counts lost at `close`). Never called while an SDK lock is held. |

Beyond the contract (section 13): `Context` helpers (`Context.of`, `Context.parse`, `Context.user`),
`Client.flush()` (sends the pending counts now, under section 9's rules) and the test seams `Options.Builder.clock`
(`Clock`), `random` (`DoubleSupplier`, the backoff jitter) and `streamIdleTimeout` (the 60 s watchdog).

**Closed sets.** `ReasonKind` is an enum whose constants are the canonical spelling (`OFF`,
`PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`). `ErrorCode` is an enum holding all
ten codes in their spelling (`INVALID_CONTEXT`, `FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`, `MALFORMED_FLAG`,
`MALFORMED_SEGMENT`, `PREREQUISITE_CYCLE`, `PREREQUISITE_DEPTH_EXCEEDED`, `NOT_READY`, `TYPE_MISMATCH`,
`EVALUATION_FAILED`); the last three are the SDK's own and the evaluator never produces them. `Reason` is a
record `(ReasonKind kind, ErrorCode errorCode, Integer ruleIndex, String flagKey)`: `errorCode` is non-null
exactly when `kind` is `ERROR`, `ruleIndex` only for `RULE_MATCH` and `flagKey` only for `PREREQUISITE_FAILED`.
`StreamState` is an enum (`OFF`, `CONNECTING`, `CONNECTED`, `DISCONNECTED`, `CLOSED`) with `wire()` returning the
lowercase spelling of the contract (`"off"`...). `lastError` is a `String` because `http_<status>` is not a closed
set: it is `http_401`, `"timeout"`, `"bad_ruleset"` or `"network"`, in their spelling, or `null`.

**Not given versus false or zero.** The builder holds `Boolean stream` and `Duration` fields as `null` until set;
`build()` substitutes the default for a `null`, and for a zero or negative duration (not specified by the
contract, which says an SDK SHOULD fall back to the default). So `stream(false)` is "off" and not given is "on".

## 2. Concurrency

- **The ruleset and its ETag** are one immutable object (`Snapshot(Ruleset ruleset, String etag)`) in a
  `volatile` field. A load builds the new `Snapshot` and assigns the field once: a reader sees the old pair or the
  new pair. `variation` reads the field once and uses that pair for the evaluation and for the `etag` it hands a hook.
- **`status`** is one immutable `Status` in a second `volatile` field, replaced whole under a small
  `synchronized (stateLock)` that only does memory work (a copy of four fields). `status()` reads the volatile
  and takes no lock.
- **Threads.** Two daemon threads, started by `create`: `toggleslash-sync` (the first load, then the loop of
  section 7.3) and `toggleslash-flush` (every `flushInterval`, then a flush). Daemon threads mean a client that
  is never closed does not keep the JVM alive. Both use blocking `HttpClient` calls through
  `CompletableFuture.get(timeout)`.
- **Locks.** `stateLock` (the status record, memory only), `usageLock` (the map of pending counts: `record` adds
  under it, a flush swaps the map under it, memory only) and `flushLock` (see below). None is held across I/O, a
  callback or an evaluation, so an evaluation never waits on a request. `variation` takes only `usageLock` for the
  map add, and a flush holds it for a map swap only.
- **Callbacks hold no lock.** `afterEvaluation` and `onError` run after every SDK lock has been released.
- **`close`.** The first call does the shutdown, every later or concurrent call waits for the same
  `CompletableFuture` and returns when it completes. Order: set `closing`; set the status stream to `closed`;
  interrupt the sync thread and cancel its in-flight HTTP future (abandoning the request, silently); wake the
  flush thread out of its sleep (not out of a request: an interrupt is delivered only while it sleeps, under
  `flushLock`, so a running flush is waited for, bounded by `requestTimeout` per request); join both threads
  (skipping the one that is the current thread, so a `close` or `ready` from `onError` on an SDK thread does
  not wait for itself); settle `ready` false if it had not settled; then the final flush, which does not wait
  out a `Retry-After` hold and tells `onError` how many entries were lost. The bound is about
  `2 * requestTimeout`: an abandoned request returns at once, a running usage request is bounded by
  `requestTimeout` and the final flush by another.
- **No new SDK thread per call.** The caller's thread does the evaluation and the hook; the SDK never runs a
  hook on its own threads.
- **Two `HttpClient`s.** JDK 17 has no `close` on `HttpClient` (it came in 21), so the ruleset and stream client
  is separate from the usage client; closing aborts requests by cancelling the futures, and the usage client is
  left alone so the final flush can still go out.

## 3. HTTP and SSE

- **Client.** `java.net.http.HttpClient`, forced to HTTP/1.1 (the JDK would otherwise send `Connection: Upgrade,
  HTTP2-Settings` on plain `http`, headers the harness's protocol check does not allow), redirects not followed,
  `connectTimeout` = `requestTimeout`.
- **A request bounded body included.** `sendAsync` with `BodyHandlers.ofByteArray()` returns a future that
  completes only when the whole body has been read; the SDK waits with `future.get(requestTimeout)`, so one
  deadline covers connect, headers and body. On expiry it cancels the future and the error is `timeout`.
  (`HttpRequest.timeout` alone ends at the response headers, which is why it is not used for the bound.)
- **The stream.** A hand-written `BodySubscriber<Void>` whose `getBody()` completes at once, so the response
  future completes when the headers arrive (`BodyHandlers.fromSubscriber` completes only when the body ends and
  would time out every open). `future.get(requestTimeout)` bounds the open. Body buffers go onto a
  `LinkedBlockingQueue`; the sync thread polls it with `streamIdleTimeout` (60 s unless a test sets it), and a
  poll that returns nothing is the watchdog: `timeout`. Each buffer received restarts the 60 s, keep-alive
  comments included. The subscriber requests one buffer list at a time, so the queue never grows.
- **SSE.** A hand-written `SseParser` over text already decoded by the streaming UTF-8 decoder (a sequence split
  across reads is held until it completes). It follows the WHATWG rules: LF, CRLF and a lone CR end a line (a CR
  at the end of a read waits for a possible LF), a line starting with `:` is a comment, `field: value` strips one
  leading space, `event` and `data` are the only fields read, several `data` lines join with LF, an event with
  no data is dropped, the event name defaults to `message`, an event incomplete at the end of the stream is
  discarded, and no line or event size is capped.

## 4. Dependencies

Runtime: **none**. The JDK has an HTTP client (`java.net.http`), SHA-256 (`java.security.MessageDigest`) and
`java.time`; the JSON parser, the UTF-8 decoder and encoder, the regex engine for `matches`, the date and semver
parsers are written for this SDK (`json`, `evaluation`), because the spec's rules differ from the JDK's (below).

Test: **JUnit Jupiter 6.1.3** (the native test framework; run by `maven-surefire-plugin`). Build and lint tools,
none of them a runtime dependency: Maven 3.10.0, `maven-compiler-plugin`, `maven-surefire-plugin`,
`maven-resources-plugin`, `maven-clean-plugin`, `build-helper-maven-plugin` (adds `test-service/src` as a test
source root), Checkstyle 14.4.0 and google-java-format 2.0.0 (both installed by mise and run by `bin/lint`).

## 5. JSON

The SDK's own value model is plain Java objects: `null`, `Boolean`, `Double`, `String`, `List<Object>` and
`Map<String, Object>` (insertion ordered). `Double` is IEEE-754 binary64, so `1e999` is `Infinity` and
`9007199254740993` is `9007199254740992.0`, and `-0.0` stays `-0.0`. A Java `String` is a sequence of UTF-16
code units and can hold a lone surrogate, so `"\ud800"` is kept as the single unit `\ud800`.

The parser is hand-written (`json.Json.parse`) because `java.net.http` ships no JSON parser and the JDK has none.
It is strict to the JSON grammar the TypeScript `JSON.parse` accepts (no trailing comma, no leading zero, no
unescaped control character, no comment, NaN or Infinity literal), reads a number by validating the grammar and
calling `Double.parseDouble` on the text (correctly rounded), keeps the last value of a duplicate key, and
refuses nesting past 1024 levels as an error (`bad_ruleset` for a ruleset; the minimum is 512).

The context reaches the SDK as a `Context`, which wraps such a value: `Context.parse(String json)` for raw JSON
(it keeps `1e999` and a lone surrogate) and `Context.of(Map)` for a native map. `Context.of` accepts the model
above and also any `Number` and `CharSequence`, which the evaluator reads through the same normalisation (an
`Integer`, `Long` or `BigDecimal` becomes the nearest `double`). Anything else is an invalid context, answered
as `ERROR INVALID_CONTEXT` like any other.

**UTF-8.** The ruleset body (and the stream) is decoded with a hand-written decoder to the WHATWG Encoding
rules, because a JDK `CharsetDecoder` with `REPLACE` and `String.getBytes` differ from them (one U+FFFD per
maximal invalid subpart; a lone surrogate must not become `?`). A leading byte order mark is skipped. The same
codec's encoder turns a String to bytes for hashing and for the test service; a lone surrogate is encoded as
U+FFFD (the spec says which in "Bucketing"; DESIGN.md section 8 records the reading).

## 6. Minimum runtime, toolchain

- **Minimum Java 17** (`maven.compiler.release` 17, `--release 17`), the oldest long-term-support release still
  in widespread use that has records, sealed types, text blocks and `instanceof` patterns, and the
  `java.net.http` client (11). The code uses no API newer than 17 (`--release 17` enforces it). The language
  note's toolchain fact is taken as given.
- **Developed on the newest LTS: Temurin 25.0.5+8.** `mise ls-remote java` lists `temurin-25.0.5+8.0.LTS` as the
  newest 25 build and Adoptium's release page names 25 as the most recent LTS (27 is a feature release). The
  run date is 2026-11-12.
- **Build tool: Maven 3.10.0** (the latest 3.x on maven.apache.org that day), the mainstream Java build.
- **Test framework: JUnit Jupiter 6.1.3**, via `maven-surefire-plugin 3.6.1`.
- **Format: google-java-format 2.0.0**, pinned by mise as a native binary (versions before 1.38 fail on a JDK
  25 javac). `bin/lint` runs it in check mode.
- **Lint: Checkstyle 14.4.0** with `checkstyle.xml` in this directory, severity `error`, plus `javac
  -Xlint:all -Werror` in the build. Checkstyle under mise is a bare JAR, run as `java -jar`.
- **Integer accessor.** `longVariation` / `longVariationDetail`, Java's signed 64-bit `long`.

## 7. Package layout

```
sdks/java/
  pom.xml  mise.toml  checkstyle.xml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/
      Client, Options, Context, Detail, Status, StreamState, AfterEvaluation, EvaluationEvent, Clock
      (package private: Sync, Usage, HttpCalls, StreamBody, SseParser, RetryAfter, Snapshot, SyncException)
  src/main/java/.../json/         Json (parser and writer), JsonException
  src/main/java/.../evaluation/   Ruleset, Evaluator, Result, Reason, ReasonKind, ErrorCode, Pattern (Pike VM),
                                  Semver, Dates, Bucket, Clauses, Contexts, References, Utf8 (decoder and encoder)
  src/test/java/...               JUnit tests: VectorsTest, Utf8Test, JsonTest, SseParserTest, RetryAfterTest,
                                  ClientTest; helpers FakeServer (a loopback server that records headers),
                                  TestClock and Support
  test-service/src/.../testservice/Main.java
  test-service/README.md
```

The evaluator package has no network code and no dependency on the client. The client, the tests and the test
service compile in one Maven module; `build-helper-maven-plugin` adds `test-service/src` as a test source root so
`mvn test` compiles it with the tests, and `bin/test-service` runs `java -cp target/classes:target/test-classes`.

Maven coordinates: group `com.github.madangryscientist.toggleslash`, artifact `toggleslash-sdk-java`, root package
`com.github.madangryscientist.toggleslash.sdks.java`. Nothing is registered under them.

## 8. Notes on the evaluator

Recorded as the port was written.

- **Numbers.** Every number is a `double`. `flagVersion` in a hook event is a `Long`: `null` when the flag is
  not in the ruleset, is malformed, or has a version that is not a whole number below 2^63.
- **Served collections.** A served array or object (a `jsonVariation` value) is a deep unmodifiable copy, so a
  caller cannot change the ruleset through the value it was given.
- **Evaluator failures.** The evaluator catches nothing it can avoid; `Client` wraps each evaluation, so a
  defect becomes one `onError` call and `EVALUATION_FAILED` with the fallback.

Disagreements between the spec and a reference implementation: none found. Where the SSE parser differs from the
TypeScript SDK's blank-line splitter it follows the WHATWG rules, which the contract names.
