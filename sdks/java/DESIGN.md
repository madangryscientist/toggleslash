# ToggleSlash Java server SDK: design

This page is the design the code follows. The rules are in [`docs/sdk-contract.md`](../../docs/sdk-contract.md)
(the contract) and [`docs/evaluation.md`](../../docs/evaluation.md) (the spec). Nothing here overrides either.

## Inputs and what was chosen

The brief gave the language (Java) and nothing else, so each of these is a decision, also listed in `RUN.md`.

| Input | Chosen | Why |
| --- | --- | --- |
| runtime | the JVM (Temurin) | the mainstream runtime; GraalVM and others are not needed. |
| development JDK | Temurin 25.0.5+8, the newest LTS | `mise.toml` pins it; a feature release leaves support after six months. |
| minimum version | Java 17 (`--release 17`) | the oldest LTS still in security support that has records, `HttpClient` and everything used here. The pom compiles with `--release 17`, so nothing newer than 17 can slip in. |
| build tool | Maven 3.10.0 | the mainstream build tool, with the reverse-domain coordinates this repository's conventions ask for. |

## 1. Mapping table (contract section 2)

Everything lives in the package `com.github.madangryscientist.toggleslash.sdks.java` unless a row says otherwise.

| Name | In Java |
| --- | --- |
| `create` | `Client.create(Options)` returns a `Client` and starts its two daemon threads. It does one ruleset request at once and never waits for the network. `Options.builder(url, sdkKey)...build()` builds the options. It never throws: a bad option falls back to its default. |
| `ready` | `boolean Client.ready()` blocks until the first load has finished and answers whether a ruleset is held. Bounded by `requestTimeout`. `Client.ready(Duration)` is the same with an extra caller-side bound. Fixed once the first attempt settles. |
| `variation` | typed `booleanVariation`, `stringVariation`, `longVariation`, `doubleVariation`, `jsonVariation`, and the generic `<T> T variation(String flagKey, Object context, T fallback)`. Never throws, never blocks. |
| `variationDetail` | `booleanVariationDetail`, `stringVariationDetail`, `longVariationDetail`, `doubleVariationDetail`, `jsonVariationDetail` and the generic `<T> Detail<T> variationDetail(String flagKey, Object context, T fallback)`. A `Detail<T>` is a record `(T value, Integer variationIndex, Reason reason)`. |
| `status` | `Status Client.status()`: an immutable record `(String etag, Instant lastSyncAt, StreamState stream, String lastError)` taken from one snapshot. `etag`, `lastSyncAt` and `lastError` are `null` when none. |
| `close` | `Client.close()` (the client is `AutoCloseable`, and `close` throws no checked exception). Idempotent and safe to race. |
| `url` | `Options.Builder.url` (the first argument of `Options.builder`). Required. Trailing `/` characters are removed. |
| `sdkKey` | `Options.Builder.sdkKey` (the second argument of `Options.builder`). Required. A null or blank key still builds a client; every request is then refused by the server and surfaces as `http_401`. |
| `stream` | `Options.Builder.stream(boolean)`. Default on. A builder field of type `Boolean` that is `null` until set tells "not given" from `false`. |
| `pollInterval` | `Options.Builder.pollInterval(Duration)`. Default 30 s. A `null` field means "not given". A zero or negative duration falls back to the default. |
| `flushInterval` | `Options.Builder.flushInterval(Duration)`. Default 30 s. Same rule. |
| `requestTimeout` | `Options.Builder.requestTimeout(Duration)`. Default 10 s. Same rule. |
| `hooks.afterEvaluation` | `Options.Builder.afterEvaluation(AfterEvaluation)`, where `AfterEvaluation` is a functional interface `void accept(EvaluationEvent)`. Default none. The `EvaluationEvent` is a record `(String flagKey, Object context, Object value, Integer variationIndex, Reason reason, String etag, Long flagVersion)`. |
| `onError` | `Options.Builder.onError(Consumer<Throwable>)`. Default: ignore. The SDK's own errors are `ToggleSlashException` (unchecked) values with no context in them. |
| `accessor shape` | both: the typed methods listed above (`boolean`, `string`, `long` for the integer accessor, `double`, `json`, each with a `Detail` twin) and the generic `variation` and `variationDetail` over the fallback's runtime class. |
| `onError thread` | ruleset and stream failures: the SDK's sync thread. Usage failures: the SDK's flush thread. A hook that raised, an evaluation that raised and the counts lost at `close`: the thread that called `variation` or `close`. No SDK lock is held at any call. |

### The closed sets keep their spelling

- **Reason kinds** are the enum `ReasonKind { OFF, PREREQUISITE_FAILED, TARGET_MATCH, RULE_MATCH, FALLTHROUGH,
  ERROR }`, constants written exactly as the contract writes them. `Reason` is a record `(ReasonKind kind, Integer
  ruleIndex, String flagKey, ErrorCode errorCode)`, with only the fields its kind uses set.
- **Error codes** are the enum `ErrorCode`, constants spelled as in the contract: `INVALID_CONTEXT`,
  `FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`, `MALFORMED_FLAG`, `MALFORMED_SEGMENT`, `PREREQUISITE_CYCLE`,
  `PREREQUISITE_DEPTH_EXCEEDED`, then the SDK's own `NOT_READY`, `TYPE_MISMATCH` and `EVALUATION_FAILED`.
- **Stream states** are the enum `StreamState`. The constants are upper case (`OFF`, `CONNECTING`, `CONNECTED`,
  `DISCONNECTED`, `CLOSED`, the Java norm) and each one's `toString()` and `wire()` return the contract's lower case
  spelling (`off`, `connecting`, `connected`, `disconnected`, `closed`).
- **`lastError` codes** are plain strings, exactly `http_<status>`, `timeout`, `bad_ruleset` and `network`, with the
  three fixed ones also published as constants on `Status` (`Status.TIMEOUT`, `Status.BAD_RULESET`,
  `Status.NETWORK`).

### How an option tells "not given" from false or zero

`Options.Builder` stores `stream` as a `Boolean` and the three durations as `Duration` references. Each starts
`null`, which means "not given"; `build()` replaces a `null` with the default (`stream` true, 30 s, 30 s, 10 s).
`stream(false)` is therefore a `false` that stays false. A duration that is zero or negative is treated as not
given as well (contract section 2 says an SDK SHOULD do that), and `Duration.ZERO` is never a poll interval.

## 2. Concurrency

- **State.** One `AtomicReference<State>` holds an immutable `State` record: the compiled `Ruleset`, its ETag, the
  `lastSyncAt`, the `lastError` and the stream state. A reader takes one `get()` and has a ruleset and the ETag
  that names it, and `status` is a single snapshot of the same record. Every change is a compare-and-set loop
  that builds a new record, so the ruleset and its ETag are replaced by one atomic swap. No evaluation, `status`
  or `variation` call takes a lock; they read the reference and run.
- **Threads.** Two daemon threads, started by `create`: the **sync thread** (the first load, then the stream and
  poll loop) and the **flush thread** (the usage flush loop). Being daemon threads, a client nobody closed does not
  keep the process alive. Every wait of both is on a `Closing` monitor (not `Thread.sleep`), so `close` wakes
  them at once.
- **Locks.** Three, none held while a request runs or a callback is called: the usage map's own lock (a brief
  add or swap), the `Closing` monitor (flag and notify only), and the close latch. `afterEvaluation` and `onError`
  run on a thread that holds none of them, so a callback that calls `variation`, `status`, `ready` or `close` cannot
  deadlock.
- **`ready` and `close` from the SDK's own thread.** A thread-local mark tells `ready` and `close` that the caller
  is one of the SDK's threads. `ready` then returns at once with the state held (true if a ruleset is held and the
  first attempt has settled, false otherwise), and `close` stops the rest and returns without joining the thread
  it is running on.
- **`close`.** Marks the client closed (so the state reads `CLOSED` and nothing is counted), wakes the sleeping
  threads, cancels the in-flight ruleset and stream futures, interrupts nothing else, joins the sync thread and the
  flush thread (each bounded), then sends the final usage flush. Two `close` calls share one `CountDownLatch`: the
  first does the work and every call waits for the latch. The final flush does not wait out a usage hold; the
  counts pending are lost and `onError` hears "closed with N usage counts unsent: the server asked to wait".
  `close` returns within about twice `requestTimeout`: the join waits at most `requestTimeout`, and the final flush's
  requests are each bounded by `requestTimeout`.
- **Time.** All waits and timestamps go through a package-private `Clock` seam (`wallMillis`, `monoMillis`,
  `sleep(ms, Closing)`), so the tests step a fake clock and count its sleepers.
- **Seams.** The package-private `Options` setters take the clock, a jitter `DoubleSupplier`, the watchdog
  `Duration` (default 60 s), the `HttpClient` and an evaluator that raises. Nothing reads a global or an environment
  variable.

## 3. HTTP and SSE

- **Client.** The JDK's `java.net.http.HttpClient` with `Version.HTTP_1_1` (the server speaks HTTP/1.1 and an
  upgrade attempt over cleartext adds a header the audit would list) and `connectTimeout(requestTimeout)`.
- **Bounding a whole request.** A request is `sendAsync(request, BodyHandlers.ofByteArray())` and the caller waits
  with `future.get(requestTimeout)`; on timeout the future is cancelled and the result is `timeout`. The request
  also has `HttpRequest.timeout(requestTimeout)`, but that ends at the response headers, so the future's bound is
  what makes the body count. The future is kept so `close` can cancel it.
- **The stream.** `BodyHandlers.ofPublisher()`, which answers at the response headers. The open is bounded by
  `requestTimeout` from the request to the headers. The body is read by a `Flow.Subscriber<List<ByteBuffer>>` that
  puts each chunk on a `LinkedBlockingQueue` and asks for the next one, with an end marker and an error marker. The
  sync thread polls the queue with the 60 s watchdog as its timeout, so every chunk (a keep-alive included) restarts
  it. When the queue wait times out, the stream is dropped with `timeout`. `close` cancels the future and the
  subscription.
- **SSE.** A hand-written parser (`SseParser`) over the bytes: it decodes UTF-8 with the SDK's own decoder across
  reads, ends a line on LF, CRLF or a lone CR (a CR at the end of a chunk waits for a following LF), treats a line
  starting with `:` as a comment, reads `event` and `data`, joins several `data` lines with LF, drops an event with
  no `data` line, names an event `message` when none was given, and discards an event still incomplete when the
  stream ends. It has no cap on a line or an event.
- **Retry-After.** Read only on a `429`. Whole decimal digits only (surrounding spaces ignored), capped at 300 s.
  The hold is a time (`holdUntil`, only ever raised) shared by the ruleset and stream requests; the events route
  has its own.
- **Bodies.** The ruleset body is decoded with the SDK's WHATWG UTF-8 decoder, a leading byte order mark is
  skipped, and the text is parsed by the SDK's own JSON parser. The usage body is written by hand with every
  character outside printable ASCII as a `\uXXXX` escape.
- **Headers sent.** `Authorization: Bearer <key>` on every request, `Accept: text/event-stream` on the stream,
  `Accept: application/json` on the ruleset and events requests, `If-None-Match` only when a ruleset and an ETag are
  both held, and `Content-Type: application/json` on events. The JDK adds `Host`, `User-Agent: Java-http-client/<version>`,
  `Content-Length` and `Connection` details. `RUN.md` tabulates one captured request per route.

## 4. Dependencies

- **Runtime: none.** The JDK has HTTP, SHA-256 and enough concurrency. JSON is hand-written because the standard
  library has none, and the spec's JSON rules (binary64 numbers, lone surrogates, `-0.0`, duplicate keys, the
  nesting limit) need a parser that is ours.
- **Test: JUnit 6.1.3** (`junit-jupiter`) with `maven-surefire-plugin`, `build-helper-maven-plugin` (adds
  `test-service/src` as a test source root) and `maven-compiler-plugin`. The test service uses the JDK's
  `com.sun.net.httpserver.HttpServer`, so it adds nothing.
- **Lint: google-java-format 2.0.0** (format check) and **Checkstyle 14.4.0** (rules in `checkstyle.xml`). Both are
  installed by mise; they are not Maven dependencies.

## 5. JSON

- A hand-written parser in the public package `...sdks.java.json` (`Json`, `JsonException`, `Utf8`), `Json.parse(String)`, strict RFC 8259 grammar (the four whitespace characters, no comments,
  no trailing commas, no leading zeros, no control character inside a string).
- A parsed value is `null`, `Boolean`, `Double`, `String`, `List<Object>` or `LinkedHashMap<String, Object>`.
- **Numbers** are read with `Double.parseDouble` after the grammar check, so they are IEEE-754 binary64:
  `1e999` is `Infinity` (not an error) and `9007199254740993` reads as `9007199254740992`. `-0.0` stays negative
  zero.
- **Lone surrogates.** A Java `String` is a sequence of UTF-16 units and holds a lone surrogate as it is. A `\ud800`
  escape produces that unit and a valid escaped pair produces the pair. The decoder never produces one from bytes.
- **Duplicate keys** keep the last value (the map is overwritten in place, keeping the first key's position).
- **Nesting.** The parser refuses a document nested deeper than 1024 levels as `bad_ruleset` (the contract needs at
  least 512). It is an explicit counter, so a deep document cannot overflow the stack.
- **UTF-8.** The SDK writes its own WHATWG-conformant decoder (one U+FFFD per maximal invalid subpart, a BOM
  skipped by the caller) and encoder (a lone surrogate becomes U+FFFD, not `?`). The JDK's `CharsetDecoder` with
  `REPLACE` and `String.getBytes` differ on both, and the evaluator needs the encoder to hash a key that holds a
  lone surrogate.
- **The context in the public API.** A context argument is an `Object`: a `Context` built with its typed builder,
  or any JSON value tree made of `Map`, `List`, `String`, `Boolean`, `null` and any `Number` (a `Number` of any
  type is read as the binary64 of the same value). The test service passes the parsed JSON through unchanged, so
  `1e999` and a lone surrogate reach the evaluator untouched. `Json` is public (in the `json` package) so a caller can build a context from
  raw JSON text with `Json.parse`.

## 6. Minimum runtime and toolchain

- **Minimum: Java 17.** It is the oldest LTS in security support. The pom sets `maven.compiler.release` to 17, the
  API check `--release` gives, so the SDK cannot use anything newer. The code uses records, `switch` on strings and
  `var`, and `java.net.http`; no preview feature.
- **Development: Temurin 25 LTS.** The newest LTS on the run date; checked with `mise ls-remote java`.
- **Build: Maven.** `mvn -B -q` through `mise exec`. The first run needs the network to fetch plugins.
- **Tests: JUnit 6.1.3**, the mainstream Java framework, with surefire. The vector test is one test that loops over every vector.
- **Format: google-java-format 2.0.0** (the repository's formatter for Java is not set; this one is the mainstream
  choice, and 2.0.0 is the first release that runs on JDK 25). **Lint: Checkstyle 14.4.0**, with public-API Javadoc
  rules on `src/main/java` only.
- **Integer accessor.** `long` (the 64-bit signed one), boxed as `Long` where the generic method returns it.
  The test service's `int` accessor calls `longVariation`. A fallback of `Integer`, `Short` or `Byte` is an integer
  accessor too, bounded to that type's range.
- **Hook event `value`.** The event carries the boxed value the caller receives: `Boolean`, `String`, `Long` for an
  integer accessor, `Double` for the number accessor, and the JSON value tree for the JSON accessor. An integer above
  2^53 is a `Long`, exact, so the nearest-double allowance is not used.

## 7. Package layout

```
sdks/java/
  mise.toml  pom.xml  checkstyle.xml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/
      Client, Options, Status, StreamState, Detail, EvaluationEvent, AfterEvaluation, Context,
      ToggleSlashException, and the package-private Sync, Usage, Http, SseParser, RetryAfter, Reporter, Clock, Closing
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/json/
      Json, JsonException, Utf8 (public: the evaluator and the client both use them)
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/evaluation/
      Ruleset, Evaluator, Reason, ReasonKind, ErrorCode, Result, and the internals: Validate, Clauses, Reference,
      Semver, Dates, Bucket, PatternParser, Regex (a Pike VM), Contexts, Model
  src/test/java/.../            unit and native tests, the vector test and the loopback fake server
  test-service/README.md        the accessor mapping
  test-service/src/.../         the test service (a test source root, see pom.xml)
```

- **Names.** Maven group `com.github.madangryscientist.toggleslash`, artifact `toggleslash-sdk-java`, root package
  `com.github.madangryscientist.toggleslash.sdks.java`: the reverse-domain form of this repository's
  `github.com/madangryscientist/toggleslash/sdks/java`. `java` is not a keyword in a package-name position
  (it is a legal identifier), so the repository's path is kept as is. Nothing is registered under it.
- **The evaluator** is the sub-package `...sdks.java.evaluation`, with no network code. The client package depends on
  it; it depends on nothing in the client package except what it exports itself (`Reason`, `ReasonKind`,
  `ErrorCode`, which the public API reuses).
- **The test service** is `test-service/src/.../service/Main.java` in the same Maven module, added as a
  test source root by `build-helper-maven-plugin`, so `mvn test` compiles it with the tests and `bin/test-service`
  runs `java -cp target/classes:target/test-classes`. `bin/test` builds it last.
- **Every package has a `package-info.java`** (a Checkstyle rule).
