# Java SDK: design

The ToggleSlash server SDK for Java. It meets [`docs/sdk-contract.md`](../../docs/sdk-contract.md) and
evaluates to [`docs/evaluation.md`](../../docs/evaluation.md). This page is the design that was written
before the code; `RUN.md` says where the code departed from it.

## Inputs and decisions about them

The brief named only the language, so every other input is a decision.

- **Runtime: the JVM (HotSpot, Eclipse Temurin).** The mainstream runtime; GraalVM native image is not a
  target.
- **Toolchain pin: Temurin 25.0.5 (the newest LTS) and Maven 3.10.0.** `mise ls-remote java` lists
  Temurin 25 as the newest long-term-support line (27 is the newest feature release and 26 left support in
  September 2026, per the Adoptium support page), so the pin is the newest 25 build. Maven 3.10.0 is the
  release the Maven download page calls "Current".
- **Minimum version: Java 17.** It is the oldest LTS the SDK needs: it has `java.net.http.HttpClient`
  (11), records, `instanceof` patterns and text blocks (16 to 17) and sealed types, all of which keep the
  code short. Java 11 and 8 are still in security support but have none of those. The SDK compiles with
  `--release 17` (so the build cannot call an API newer than 17), and the pin (25) runs the tests. `java.net.http`
  is not closable before 21, which is why `close` cancels requests one by one instead of closing the client.
- **Build tool: Maven.** The mainstream choice for a library that other builds depend on, and mise installs it.
- **Lint and format: javac `-Xlint:all -Werror`, Checkstyle and Spotless with google-java-format**, all run
  through Maven plugins (no extra mise tool; a Checkstyle JAR under mise has no launcher). See `checkstyle.xml`
  for the rule set and the reason beside each omission.
- **Test framework: JUnit Jupiter 6** (the Maven Surefire provider), the mainstream one.
- **Coordinates.** Group `com.github.madangryscientist.toggleslash` (the reverse-domain form of
  `github.com/madangryscientist/toggleslash`), artifact `toggleslash-sdk-java`, root package
  `com.github.madangryscientist.toggleslash.sdks.java`. Nothing is registered under it.

## 1. The mapping table

The names below are the canonical ones of contract section 2. In code the packages are
`...sdks.java` (the client; written `client` below), `...sdks.java.evaluation` (the evaluator and the JSON
parser; `evaluation`) and, for tests only, `...sdks.java.testservice`.

| Name | In Java |
| --- | --- |
| `create` | `ToggleSlashClient.create(ClientOptions)`: builds the client and starts its two daemon threads; returns at once, never waits for the network. |
| `ready` | `boolean ToggleSlashClient.ready()`: blocks the caller until the first ruleset attempt settles (bounded by `requestTimeout`), true if a ruleset is held. May be called again with the same answer. |
| `variation` | The typed accessors `boolVariation(flagKey, context, boolean)`, `stringVariation(flagKey, context, String)`, `intVariation(flagKey, context, long)`, `doubleVariation(flagKey, context, double)` and the generic `jsonVariation(flagKey, context, Object)`, each returning the value (or the fallback). |
| `variationDetail` | `boolVariationDetail`, `stringVariationDetail`, `intVariationDetail`, `doubleVariationDetail`, `jsonVariationDetail`, each returning `Evaluation<T>` (`value()`, `variationIndex()` as an `OptionalInt`, `reason()`). |
| `status` | `Status ToggleSlashClient.status()`: a record of `etag` (`Optional<String>`), `lastSyncAt` (`Optional<Instant>`), `stream` (`StreamState`) and `lastError` (`Optional<String>`). |
| `close` | `void ToggleSlashClient.close()` (the client is `AutoCloseable`, and `close()` declares no checked exception): safe to call twice or from many threads. |
| `url` | `ClientOptions.builder(url, sdkKey)`: first argument, required. Trailing `/` characters are removed. |
| `sdkKey` | `ClientOptions.builder(url, sdkKey)`: second argument, required. |
| `stream` | `ClientOptions.Builder.stream(boolean)`. Default on. |
| `pollInterval` | `ClientOptions.Builder.pollInterval(Duration)`. Default 30 s. |
| `flushInterval` | `ClientOptions.Builder.flushInterval(Duration)`. Default 30 s. |
| `requestTimeout` | `ClientOptions.Builder.requestTimeout(Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `ClientOptions.Builder.afterEvaluation(Consumer<EvaluationEvent>)`. Default none. |
| `onError` | `ClientOptions.Builder.onError(Consumer<Throwable>)`. Default: ignore. |
| `accessor shape` | Both. Typed methods for boolean, string, 64-bit integer (`long`) and double, and one generic `jsonVariation(flagKey, context, Object)` for any JSON value (the fallback may be null). Every one has a `...Detail` twin. The integer accessor is `intVariation`, so the harness's `integer-accessor` capability is advertised. |
| `onError thread` | A failed ruleset or stream attempt: the SDK's sync thread (`toggleslash-sync`). A failed usage request: the flush thread (`toggleslash-flush`), or the thread that called `close` for the final flush, which is also the thread told about counts lost at close. An evaluation that raised and a hook that raised: the caller's thread. |

**Closed sets keep their spelling.** `ReasonKind` is an enum with `OFF`, `PREREQUISITE_FAILED`,
`TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH` and `ERROR`. `ErrorCode` is an enum with the seven evaluator
codes and the SDK's `NOT_READY`, `TYPE_MISMATCH` and `EVALUATION_FAILED`. `StreamState` is an enum
with `OFF`, `CONNECTING`, `CONNECTED`, `DISCONNECTED`, `CLOSED`; Java constants are upper case, which is the
language's convention and the only change of spelling (`OFF` reads as `off`; the test service writes the
canonical lower case). `lastError` is a plain string holding exactly `http_<status>`, `timeout`,
`bad_ruleset` or `network`, because the set is open (`http_<status>`).

**"Not given" versus false or zero.** `ClientOptions.Builder` holds the default in the field's initialiser
(`stream = true`, `pollInterval = 30 s`, and so on), so a setter that is never called leaves the default and
an explicit `stream(false)` is a different value. A `Duration` that is null, zero or negative falls back to the
default (contract section 2, "SHOULD").

## 2. Concurrency

- **One state object.** `State` is an immutable record: the `Ruleset`, its ETag, `lastSyncAt`, the stream
  state and `lastError`. It lives in one `AtomicReference<State>`, and every change is `updateAndGet` with a
  pure function. So the ruleset and the ETag swap in one step (contract 6.3), `status()` is one consistent
  snapshot (6.5), and an evaluation reads the reference once and uses that ruleset and that ETag for the
  result, the hook's `etag` and `flagVersion`.
- **Threads.** Two daemon threads, started by `create`: `toggleslash-sync` (the first load, then the loop of
  contract 7.3, the stream included) and `toggleslash-flush` (the usage flush every `flushInterval`). Daemon, so
  a client that is never closed does not keep the JVM alive (6.6). The HTTP client has its own daemon threads.
  A third piece is a single daemon `ScheduledExecutorService` thread (`toggleslash-timer`) that runs the stream
  watchdog; it starts lazily and ends with `close`.
- **`close`.** The first caller wins a compare-and-set on `closing` and does the shutdown in order (contract
  11): it sets the stream state to `closed` and settles `ready` false if unsettled, cancels the in-flight
  ruleset and stream requests (which are tracked in a concurrent set), wakes the sleepers, joins the sync
  thread and the flush thread (a thread never joins itself), then sends the final flush, then completes a
  `CompletableFuture` that every later or concurrent `close` waits on. A `close` called on the SDK's own
  thread (inside `onError`) stops the rest and returns without waiting for itself; the flush thread, when it was
  the caller, does the final flush when its callback returns.
- **Locks.** There are no locks held across I/O or a callback. The only monitors are two small ones: the
  `Sleeper`'s monitor, which a loop waits on (`Object.wait`, which releases it) and `close` notifies, and the
  flush gate, a flag guarded by a monitor that is held only to set or clear the flag, so a flush that starts
  while another runs waits on it without holding it. `variation` takes neither: it reads the
  `AtomicReference`, runs the evaluator and does one `ConcurrentHashMap.merge` for the usage count. A hung
  ruleset request or usage flush therefore cannot slow an evaluation (5.2, 6.2).
- **Callbacks.** `afterEvaluation` runs on the caller's thread after the result is built, and `onError` is
  called after the state change it reports; neither is called while any SDK monitor or `compute` function is
  running. A callback that calls `status`, `variation` or `ready` cannot deadlock. `ready()` on an SDK thread
  returns at once with the state held (6.4).
- **Time.** Every wait goes through a `Clock` (wall clock for the UTC day and `lastSyncAt`, monotonic for
  waits and Retry-After holds) whose `awaitUntil(monitor, deadline)` is `Object.wait` in production and a
  monitor the test steps in the fake. The jitter is a `DoubleSupplier`, the watchdog length a `Duration`.
  These and the SSL context are package-private fields of `ClientConfig` that the package's tests set; no
  global, no environment variable.

## 3. HTTP and SSE

- **Client.** `java.net.http.HttpClient`, forced to HTTP/1.1 (the default h2c upgrade would add `Upgrade`
  and `HTTP2-Settings` headers), no redirects (a `3xx` is `http_<status>`), and one instance per SDK client.
- **`requestTimeout` bounds the whole request, body included.** `HttpRequest.timeout` stops at the response
  headers, so it is only a second guard. The real bound is `sendAsync(...).get(requestTimeout)` with a body
  handler that collects the whole body (`BodyHandlers.ofByteArray`): that future completes when the body ends,
  so one deadline covers connect, headers and body. On expiry the future is cancelled (which closes the
  connection) and the result is `timeout`. The same call serves the ruleset and the events routes.
- **The stream.** The request is sent with `BodyHandlers.ofInputStream()`, whose future completes at the
  response headers; the deadline on that future is the open bound (`requestTimeout`, then `timeout`). The sync
  thread then reads the body with `InputStream.read(byte[])`. The watchdog (60 s, a package-private option
  for tests) is a task on the timer thread that closes the `InputStream` when no byte has been read for the
  whole window and sets a flag so the read loop reports `timeout`; every read, a keep-alive included,
  restarts the clock. Closing an `HttpClient` response stream unblocks a pending `read` with an `IOException`.
- **SSE.** A hand-written parser (`SseParser`): a streaming UTF-8 decoder (`CharsetDecoder` with
  `REPLACE`, so a multi-byte character split across reads works), a line splitter for LF, CRLF and a lone CR
  (a CR at the end of a chunk waits to see whether an LF follows), a leading byte order mark skipped, comment
  lines dropped, `event` and `data` fields only, several `data` lines joined by LF, no event without `data`,
  the name `message` by default, and an event unfinished at the end of the stream discarded. No length limit.
- **Retry-After.** Read on a `429` only; whole decimal digits only (spaces around are ignored), capped at 300 s.

## 4. Dependencies

- **Runtime: none.** The JDK has HTTP (`java.net.http`), SHA-256 (`MessageDigest`) and everything else;
  JSON is a hand-written parser (see 5), because `java.util` has none.
- **Test and lint:** JUnit Jupiter (tests), the JDK's `com.sun.net.httpserver` (the fake server and the
  test service, no dependency), Spotless with google-java-format (format check), Checkstyle (lint),
  `build-helper-maven-plugin` (adds `test-service/src` as a test source root, so the service compiles with
  the tests).

## 5. JSON

The parser is `evaluation/Json.java`, a recursive-descent parser over a `String`, producing plain Java
objects: `null`, `Boolean`, `Double`, `String`, `List<Object>` and `LinkedHashMap<String, Object>`.

- Numbers go through `Double.parseDouble` on a grammar-checked token, so they are binary64: `1e999` is
  Infinity (no error), `-0.0` keeps its sign, `9007199254740993` reads as `9007199254740992`.
- A Java `String` is UTF-16, so a lone surrogate written as `"\ud800"` stays one `char`, and a valid pair
  written as two escapes is the pair. Raw bytes are decoded as UTF-8 with U+FFFD for invalid input, one per
  maximal invalid subpart, which `CharsetDecoder` with `REPLACE` does (checked against the WHATWG examples in
  a test); a leading byte order mark is skipped.
- A duplicate key keeps the last value. Nesting is refused beyond 1000 levels as `bad_ruleset` (the contract's
  minimum is 512).
- **The context** is a `Map<?, ?>` of these same kinds. A caller can build it by hand
  (`Double.POSITIVE_INFINITY` for `1e999`, any Java `String`), or from JSON text with `Json.parse`. Any
  `Number` is read as its `double` value. That keeps every case in the vectors and needs no second type.
- **The serialiser** for the events body writes only the four fields; a flag key with a lone surrogate is
  written as a `\uXXXX` escape so the body stays valid JSON.

## 6. Minimum runtime, toolchain

See the first section. In short: `--release 17`; JUnit, Spotless and Checkstyle through Maven. The integer
accessor `intVariation` uses `long`, the 64-bit signed type: a served number with a fractional part, or outside
`[-2^63, 2^63)`, is `TYPE_MISMATCH`, and `2.0` is `2`. The hook's `value` for it is the exact `Long`, so no
precision is lost (the event type is `Object`, not `double`).

## 7. Package layout

```
sdks/java/
  pom.xml                       the build (compiler flags, plugins, test source root)
  mise.toml                     Temurin and Maven pins
  checkstyle.xml                the lint rule set
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/
      ToggleSlashClient.java    the client: options, loops, sync, usage, status, close
      ClientOptions.java        the options and the builder
      ...                       Status, StreamState, Evaluation, EvaluationEvent, Clock, Sleeper, SseParser, etc.
      evaluation/               Json, Ruleset, Flag, Segment, Evaluator, Pattern (Pike VM), Semver, Dates, Reason,
                                ReasonKind, ErrorCode, EvaluationResult
  src/test/java/...             unit tests, the vector runner, the loopback fake server, the race tests
  test-service/src/.../testservice/   the conformance test service (a test source root)
  test-service/README.md
```

The evaluator package has no network code and no `java.util.regex`; the client imports it, never the reverse.
