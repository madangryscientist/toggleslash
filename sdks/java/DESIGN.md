# ToggleSlash server SDK for Java: design

This is the design of `sdks/java`, written before any code. It follows `docs/sdk-contract.md` (the
contract) and `docs/evaluation.md` (the spec). Nothing here is published and no name is registered.

Package root: `com.github.madangryscientist.toggleslash.sdks.java` (Maven group
`com.github.madangryscientist.toggleslash`, artifact `toggleslash-sdk-java`). The code below uses `P` for it.

## 1. The mapping table (contract section 2)

| Contract name | Java |
| --- | --- |
| `create` | `Client.create(Options)`, a static factory. It starts two daemon threads and returns at once; it never throws (a bad option falls back to its default, a URL the HTTP client cannot use makes each request a `network` error). `Options` is built with `Options.builder()...build()`. |
| `ready` | `boolean Client.ready()`. Blocks until the first load attempt has ended (bounded by `requestTimeout`, the load's own bound) and answers whether a ruleset is held; the answer is fixed at that moment. Called again it answers the same. After `close` it answers `false` if it had not settled. Called on one of the SDK's own threads (from `onError`, which the sync thread calls) it never waits: it answers the settled answer, or `false` if unsettled. |
| `variation` | `<T> T Client.variation(String flagKey, Object context, T fallback)`, the generic method, plus the typed accessors below. Never throws, never does I/O. |
| `variationDetail` | `<T> Detail<T> Client.variationDetail(String flagKey, Object context, T fallback)`, and a `...Detail` for each typed accessor. `Detail<T>` is a record `(T value, Integer variationIndex, Reason reason)`; `variationIndex` is `null` for a fallback. |
| `status` | `Status Client.status()`, one immutable snapshot record `(String etag, Long lastSyncAt, StreamState stream, String lastError)`. `etag`, `lastSyncAt` (epoch milliseconds) and `lastError` are `null` for "none". |
| `close` | `void Client.close()`. Also `AutoCloseable`, but declares no exception. Idempotent; concurrent calls wait for the one shutdown. Returns within about two times `requestTimeout` (section 2). |
| `url` | `Options.Builder.url(String)`. Required. Trailing `/` characters are removed. |
| `sdkKey` | `Options.Builder.sdkKey(String)`. Required. |
| `stream` | `Options.Builder.stream(boolean)`. Default `true`. The builder holds a `Boolean`, `null` meaning "not given", so `false` is told from "absent". |
| `pollInterval` | `Options.Builder.pollInterval(Duration)`. Default 30 seconds. `null` means "not given"; a zero or negative duration falls back to the default (contract 2 SHOULD). |
| `flushInterval` | `Options.Builder.flushInterval(Duration)`. Default 30 seconds, same handling. |
| `requestTimeout` | `Options.Builder.requestTimeout(Duration)`. Default 10 seconds, same handling. |
| `hooks.afterEvaluation` | `Options.Builder.afterEvaluation(Consumer<EvaluationEvent>)`. Default none. `EvaluationEvent` is a record `(flagKey, context, value, variationIndex, reason, etag, flagVersion)`, with `Integer variationIndex`, `String etag` and `Long flagVersion` nullable. The hook runs on the caller's thread, synchronously; its return value does not exist (a `Consumer`). |
| `onError` | `Options.Builder.onError(Consumer<Throwable>)`. Default: ignore. Values are `ToggleSlashException` (a `RuntimeException` with a `code()`) for failures the SDK builds, or the exception a hook or the evaluator raised, as raised. |
| `accessor shape` | Both: one generic method and typed methods. Generic: `variation` and `variationDetail`, `T` being the fallback's type (`Boolean`, `String`, any `Number`, `List`, `Map`, or `null` for "any kind"). Typed: `boolVariation` (`boolean`), `stringVariation` (`String`), `numberVariation` (`double`), `intVariation` (`long`, the signed 64-bit integer), `jsonVariation` (`Object`, any JSON kind, a `null` fallback accepted), each with a `...Detail` twin. Every one applies contract 4's order and mismatch rule. `intVariation` also answers `TYPE_MISMATCH` for a served number with a fractional part or outside int64 (`2.0` is `2`). |
| `onError thread` | The thread that met the failure: the SDK's sync thread for a failed ruleset or stream attempt, the SDK's flush thread (or the thread that called `flush` or `close`) for a usage failure and for counts lost at `close`, and the caller's own thread for a hook that raised or an evaluation that raised. No SDK lock is held when it is called. An `onError` that throws is caught and ignored. |

Reasons, error codes, stream states and `lastError` keep their canonical spelling:

- `ReasonKind` is an enum `OFF, PREREQUISITE_FAILED, TARGET_MATCH, RULE_MATCH, FALLTHROUGH, ERROR`. `Reason` is a
  record `(ReasonKind kind, Integer ruleIndex, String flagKey, ErrorCode code)`, with the fields that do not
  apply `null`.
- `ErrorCode` is an enum with the constants `INVALID_CONTEXT, FLAG_NOT_FOUND, SEGMENT_NOT_FOUND, MALFORMED_FLAG,
  MALFORMED_SEGMENT, PREREQUISITE_CYCLE, PREREQUISITE_DEPTH_EXCEEDED, NOT_READY, TYPE_MISMATCH,
  EVALUATION_FAILED`; `toString()` is the constant's name.
- `StreamState` is an enum `OFF, CONNECTING, CONNECTED, DISCONNECTED, CLOSED`. The contract's spelling is lower
  case (`off`, `connecting`, ...), which Java style forbids for a constant, so `value()` and `toString()` answer
  the lower-case spelling and the constants are the usual upper-case names.
- `lastError` is a `String`: `http_<status>` is open-ended, so it is not an enum. The other three are the
  constants `Status.TIMEOUT`, `Status.BAD_RULESET`, `Status.NETWORK` (`"timeout"`, `"bad_ruleset"`, `"network"`).

How "not given" is told from false or zero: `Options.Builder` stores `Boolean stream`, `Duration pollInterval`,
`Duration flushInterval` and `Duration requestTimeout` as boxed or reference types that start `null`; `build()` applies
the default only for `null` (and for a non-positive duration). `stream(false)` is therefore kept.

A context is passed as `Object`: the JSON value, as `Map<String,Object>` (`"key"`, `"attributes"`, `"kind"`,
`"contexts"`), with `String`, `Boolean`, `Double` or any `Number`, `List`, `Map` and `null` inside. A `Number` is read as
its `double` value. Anything else (a `Date`, an array) is an invalid attribute type and is simply never equal to a
clause value. `Context.single(key)`/`Context.builder()` helpers build the map for callers who want them
(contract 13). A context that is not valid gets `ERROR INVALID_CONTEXT`.

## 2. Concurrency (contract 6)

- **The state.** `Sync` holds one immutable `View(ruleset, etag, lastSyncAt, stream, lastError)` in a `volatile`
  field. A reader (`variation`, `status`) reads the field once and uses that object, so it sees one ruleset with the
  ETag that names it and one consistent status (contract 6.3, 6.5). The sync thread replaces the whole record in
  one assignment; the code that builds the new record does no callback. A ruleset and its ETag swap together.
- **Threads.** Two daemon threads per client, both `SdkThread` (a `Thread` subclass the client can recognise):
  `toggleslash-sync` (the first load, then the loop of contract 7.3) and `toggleslash-flush` (sleep
  `flushInterval`, flush, repeat). Both are daemon, so a client that is never closed does not hold the process
  (contract 6.6). A `flush` called by the application runs on the application's thread.
- **Waiting without interrupts.** Sleeps go through `Wake`, a small monitor with a woken flag, so `close` wakes a
  sleeper by setting the flag and never interrupts a thread. A blocked request is ended by cancelling its
  `CompletableFuture`.
- **Locks.** The flush lock serialises flushes and is held across usage I/O; evaluation never takes it. Usage counts
  live in a `ConcurrentHashMap`: recording is `merge`, a flush drains with `remove` per key and re-adds what a
  failed request kept. An evaluation therefore takes no lock a network call holds and waits for none (contract
  6.2). Shutdown happens once, guarded by an `AtomicBoolean` and a `CountDownLatch`.
- **Callbacks.** `afterEvaluation` and `onError` are called with no SDK lock held, so a callback may call `status`,
  `variation`, `ready` or `close` without a deadlock.
- **`ready`** waits on a `CountDownLatch` counted down at the end of the first attempt (or by `close`); a `ready`
  on an `SdkThread` of this client does not wait.
- **`close`** order (contract 11): mark closing (so no count is added after this); stop following (the sync thread is
  woken and its in-flight future is cancelled); stop the flush loop (its wake is set); join both threads, each for at
  most `requestTimeout` (a thread never joins itself, so a `close` made on an `SdkThread` skips its own join); send
  the final flush; release waiters. Every other caller waits on the same completion latch.
  The bound: the joins take at most two `requestTimeout` (one per thread) in the worst case and in practice
  return at once, because the in-flight request is cancelled; the final flush is serial requests, each bounded by
  `requestTimeout`, and a flush of more than 500 entries is more than one request. A server that never answers
  therefore ends `close` after about `requestTimeout` for the one flush request plus the joins.
- **Time.** Waits use `System.nanoTime`. Tests reach the clock, the jitter, the stream idle timeout and the evaluation
  through package-private seams on `Options.Builder`.

## 3. HTTP and SSE (contract 7)

- **The client.** The JDK's `java.net.http.HttpClient` (no dependency), forced to HTTP/1.1 (no `Upgrade: h2c`
  header on cleartext, no `HTTP2-Settings`), redirects off (a `3xx` is `http_<status>`; the contract leaves
  redirects open), one `HttpClient` for all three routes. Requests carry `Authorization: Bearer <key>`;
  the ruleset request adds `If-None-Match` only with a ruleset and an ETag held; the stream adds `Accept:
  text/event-stream`; the events request adds `Content-Type: application/json`. The JDK adds `User-Agent`,
  `Host`, `Content-Length` (POST) and `Connection`/`Accept-Encoding` where it does; `RUN.md` lists what a
  loopback listener saw.
- **Whole-request bound.** `HttpRequest.timeout` ends at the response headers, so it is not enough. Every request
  except the stream is `sendAsync`; the caller thread waits on the `CompletableFuture` for the remaining time of
  `requestTimeout` measured from just before the call (headers and body read inside the bound), and on expiry
  cancels the future (which aborts the exchange) and reports `timeout`. The ruleset body is read with
  `BodySubscribers.ofByteArray()`.
- **The stream.** `StreamBody` is a hand-written `BodySubscriber<StreamBody>` that hands itself over the moment the
  headers arrive (the language notes: `BodyHandlers.fromSubscriber` answers only at the end of the body). It
  queues each chunk in a `LinkedBlockingQueue` and requests one more only after the reader took one, so memory is
  bounded. The open is bounded by `requestTimeout` (`future.get(timeout)`), and the 60 second watchdog is simply
  `queue.poll(watchdog)`: every chunk, a keep-alive comment included, restarts it, and a `null` from `poll` is
  `timeout`. The watchdog length is a seam (`Options.streamIdleTimeout`), and its test reads a real loopback
  socket that goes quiet. `close` cancels the subscription and the future and wakes the reader.
- **No in-flight request survives close.** The JDK before 21 has no closable `HttpClient`, so the sync thread keeps
  the `CompletableFuture` of its current ruleset or stream request in a field and `close` cancels it.
- **Errors.** `Errors` maps a failure to the `lastError` spelling (`http_<status>`, `timeout`, `bad_ruleset`,
  `network`); an `HttpTimeoutException`, a cancelled-by-timeout future and a stalled stream are all `timeout`.
- **SSE.** A hand-written incremental parser (`SseParser`) over decoded text: it feeds on `String` chunks from a
  streaming UTF-8 decoder (below), holds a trailing CR until the next character (a CRLF split across reads), ends
  a line on LF, CRLF or CR, ignores a line starting with `:`, splits `field:value` at the first colon and removes
  one leading space from the value, joins several `data` lines with LF, drops an event with no `data` line, names an
  event `message` when none is given, and discards an event still incomplete at the end of the stream. It acts on
  `connected` and `changed` only. A line may be any length: no cap.
- **UTF-8.** `Utf8` is a hand-written WHATWG decoder (one U+FFFD per maximal invalid subpart; incremental for the
  stream, with the partial sequence held across chunks) and encoder (a lone surrogate encodes as U+FFFD, used
  only when hashing). The JDK's `CharsetDecoder` and `String.getBytes` differ from the WHATWG rules the contract
  names, so they are not used for the spec's cases.
- **Retry-After.** Read on a `429` only, decimal digits only after trimming, capped at 300 s, held as a monotonic
  deadline (`AtomicLong`, only lengthens) for the ruleset and stream together; the usage hold is a second one.

## 4. Dependencies

Runtime dependencies: none. The JDK has `java.net.http`, `java.security.MessageDigest` (SHA-256) and `java.time`;
JSON, UTF-8, the SSE parser and the pattern matcher are written in this package because the spec requires
behaviour the platform's versions do not have (section 5 and the language notes).

Build and test: `junit-jupiter` (JUnit 6, the test framework), Maven with `maven-compiler-plugin`,
`maven-surefire-plugin`, `maven-resources-plugin`, `maven-jar-plugin` and `build-helper-maven-plugin` (adds
`test-service/src` as a test source root). Lint: Checkstyle 14.4.0 and google-java-format 2.0.0, both pinned in
`mise.toml`.

## 5. JSON

- `P.json.Json.parse(String)` is a hand-written recursive-descent parser to plain Java values: `Map<String,Object>`
  (a `LinkedHashMap`), `List<Object>`, `Double` for every number, `String`, `Boolean` and `null`. Numbers go
  through `Double.parseDouble`, which rounds correctly, so `1e999` is `Infinity` (not an error),
  `9007199254740993` is `9007199254740992` and `-0` is `-0.0`. The grammar is checked by the parser, not by
  `parseDouble`, so `NaN`, `0x10` and `+1` are rejected.
- A duplicate key keeps the last value; a leading byte order mark is skipped; there is no size cap. The depth limit
  is 1000 levels (the contract requires at least 512), as `bad_ruleset`.
- A Java `String` is UTF-16, so a lone surrogate (`"\ud800"`) is held as itself, as the spec wants; it becomes
  U+FFFD only when `Bucket` encodes the hash input. Bodies are decoded with `Utf8.decode` (WHATWG).
- The public API accepts a context as `Map`/`List`/`Double`/`String`/`Boolean`/`null`, which keeps `1e999` and lone
  surrogates; `Json.parse` gives the test service exactly that.

## 6. Minimum runtime and toolchain

- **Pinned toolchain:** Temurin 25 LTS (newest LTS on the run date). `mise.toml` pins `java`, `maven`,
  `checkstyle`, `google-java-format`.
- **Minimum:** Java 17, set with `maven.compiler.release` 17. It is the oldest LTS Temurin still supports and has
  records, `instanceof` patterns, text blocks and the `java.net.http` client; the code uses nothing newer. Java 21
  would give a closable `HttpClient`, which the in-flight future cancel replaces.
- **Test framework:** JUnit Jupiter, the mainstream one. **Build tool:** Maven. **Lint:** Checkstyle with a
  rule set written in `checkstyle.xml` plus a google-java-format check (`--set-exit-if-changed`).
- **Evaluator limits:** `matches` uses the spec's Pike VM (`P.evaluation.Pattern`), not `java.util.regex`.

## 7. Package layout

```
sdks/java/
  mise.toml  pom.xml  checkstyle.xml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/
    Client, Options, Status, StreamState, Detail, Reason, ReasonKind, ErrorCode, EvaluationEvent,
    ToggleSlashException, Context        public API
    Sync, Usage, Http, StreamBody, SseParser, Hold, Wake, Errors   internals of the client
    json/        Json, Utf8, JsonException
    evaluation/  Evaluator, Ruleset, Pattern (Pike VM), PatternParser, Semver, Dates, Bucket, ...
  src/test/java/.../java/   VectorTest, evaluator and JSON tests, FakeServer, client, sync, TLS, stress and source tests
  test-service/src/.../     the conformance test service, README.md maps the accessors
```

`evaluation` has no network code and no dependency on the client, except for the three small root types it returns
(`Reason`, `ReasonKind`, `ErrorCode`), which are part of the public API and so live in the root package. That is a
package cycle by one direction of use only (the root uses `evaluation`, `evaluation` uses these three enums and
records); it is deliberate and the alternative, duplicating the types, was worse.
The test service is compiled with the tests (build-helper adds `test-service/src`); `bin/test-service` runs
`java -cp target/classes:target/test-classes ...testservice.Main`, building first only when the classes are missing.

## 8. Spec disagreements between implementations

None found so far; any found while porting is recorded here with both readings.
