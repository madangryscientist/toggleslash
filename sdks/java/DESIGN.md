# Java SDK: design

This is the design of the ToggleSlash server SDK for Java, written before the code, to
[`docs/sdk-contract.md`](../../docs/sdk-contract.md) (the contract) and
[`docs/evaluation.md`](../../docs/evaluation.md) (the spec). It is built from
[`docs/sdk-authoring/PROMPT.md`](../../docs/sdk-authoring/PROMPT.md); the run report is [`RUN.md`](RUN.md).

Inputs: language Java. Runtime, minimum version and build tool were not given, so each is a decision (see
"The minimum runtime and the toolchain").

## The mapping table

The Maven coordinates are `com.github.madangryscientist.toggleslash:toggleslash-sdk-java:0.1.0-SNAPSHOT`
and the root package is `com.github.madangryscientist.toggleslash.sdks.java` (the repository path
`github.com/madangryscientist/toggleslash/sdks/java` in Java's reverse-domain form). Everything below is in
that package unless it says otherwise. Durations are `java.time.Duration`.

| Name | In Java |
| --- | --- |
| `create` | `Client.create(Options options)` returns a `Client`. It starts two daemon threads and returns without waiting for the network. It never throws: a URL that cannot be used shows up as a `network` failure in `status()` and `onError`, like any unreachable server. `Options.builder(url, sdkKey)` makes the options. |
| `ready` | `boolean Client.ready()`. Blocks the calling thread until the first load has finished (bounded by `requestTimeout`), then answers whether a ruleset is held. Any number of calls from any thread; the answer is fixed when the first attempt settles; after `close()` it answers `false` if nothing had loaded. |
| `variation` | Typed methods, no generic method: `boolean boolVariation(String flagKey, Context context, boolean fallback)`, `String stringVariation(...)`, `long intVariation(...)`, `double doubleVariation(...)` and `Object jsonVariation(String flagKey, Context context, Object fallback)`. A `null` fallback to `jsonVariation` accepts any served value. |
| `variationDetail` | One `...Detail` twin per typed method: `boolVariationDetail`, `stringVariationDetail`, `intVariationDetail`, `doubleVariationDetail` and `jsonVariationDetail`, each returning `Detail<T>`, a record of `T value`, `OptionalInt variationIndex` (empty when the value is the fallback) and `Reason reason`. |
| `status` | `Status Client.status()`, a record of `Optional<String> etag`, `Optional<Instant> lastSyncAt`, `StreamState stream` and `Optional<String> lastError`. One consistent snapshot, read from memory under one small lock. |
| `close` | `Client.close()` (the client is `AutoCloseable`, and `close()` declares no checked exception). Idempotent and safe from any thread and from a callback; every call returns when the same shutdown is done. |
| `url` | the first argument of `Options.builder(url, sdkKey)`. Trailing `/` characters are removed; nothing else is validated. Required. |
| `sdkKey` | the second argument of `Options.builder(url, sdkKey)`. Required. |
| `stream` | `Options.Builder.stream(boolean)`. The default is on. |
| `pollInterval` | `Options.Builder.pollInterval(Duration)`; default 30 s; a zero, negative or null `Duration` is the default. |
| `flushInterval` | `Options.Builder.flushInterval(Duration)`; default 30 s; a zero, negative or null `Duration` is the default. |
| `requestTimeout` | `Options.Builder.requestTimeout(Duration)`; default 10 s; a zero, negative or null `Duration` is the default. |
| `hooks.afterEvaluation` | `Options.Builder.afterEvaluation(Consumer<EvaluationEvent>)`; default none. A `null` is ignored. The event is a record of `flagKey`, `context`, `value`, `variationIndex` (`OptionalInt`), `reason`, `etag` (`Optional<String>`) and `flagVersion` (`Optional<Double>`). |
| `onError` | `Options.Builder.onError(Consumer<Throwable>)`; default none (ignore). The errors the SDK builds are `ToggleSlashException` values (a `RuntimeException` that is only ever passed to `onError`, never thrown at a caller) with a fixed message and a `code()`. A hook's exception arrives as it was thrown. |
| `accessor shape` | Typed methods only, because Java has no generic method over the fallback's type that survives erasure for `long` versus `double` versus `boolean`, and a second spelling of the same five would add nothing. The accessors are `bool`, `string`, `int` (a `long`), `double` and `json` (an `Object` from the JSON tree, whose `null` fallback is the `any` accessor). Each has a `Detail` twin. A typed method needs the served value to be of its own kind, whatever the fallback is. `intVariation` is the integer accessor (below). |
| `onError thread` | The thread that met the failure: the SDK's sync thread (`toggleslash-sync`) for a failed ruleset or stream attempt, its flush thread (`toggleslash-flush`) for a failed usage request, the thread that called `close()` for the final flush and for the counts lost at close, and the caller's own thread for an evaluation that raised and for a hook that raised. No SDK lock is held during the call. |

**The closed sets** keep their spelling as enums whose constants carry the canonical text, so
`ReasonKind.OFF` has `value()` of `"OFF"` (`Reason`, `ReasonKind` and `ErrorCode` are public types of the
`evaluation` package, because the evaluator returns them and the SDK passes them through unchanged): `ReasonKind` (`OFF`, `PREREQUISITE_FAILED`,
`TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`), `ErrorCode` (the evaluator's seven and the SDK's
`NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`) and `StreamState` (`OFF("off")`, `CONNECTING("connecting")`,
`CONNECTED("connected")`, `DISCONNECTED("disconnected")`, `CLOSED("closed")`). `Reason` is a record
`(ReasonKind kind, OptionalInt ruleIndex, Optional<String> flagKey, Optional<ErrorCode> code)`; `ruleIndex`
means something only for `RULE_MATCH`, `flagKey` only for `PREREQUISITE_FAILED` and `code` only for
`ERROR`. `lastError` is an `Optional<String>` holding `http_<status>`, `timeout`, `bad_ruleset` or `network`.
The errors handed to `onError` are `ToggleSlashException` values whose `code()` is one of those four, or
`usage` for a failed usage request.

**An option whose default is on or non-zero.** `Options.Builder` starts with the defaults in its fields, so
an option that is not called keeps its default and `stream(false)` is `false`. A duration that is zero,
negative or null is replaced by the default, as contract section 2 suggests.

**The integer accessor.** `intVariation` returns a `long`. A served number that has a fractional part, is
not finite, or is outside the signed 64-bit range is `ERROR TYPE_MISMATCH` and the caller gets the
fallback; `2.0` is the integer `2`. The test service advertises the `integer-accessor` capability.

## Concurrency

The JVM runs threads on several cores, so everything is written for that. The JDK has no race detector; the
native tests stress every operation from many threads instead (`ClientTest`, see `RUN.md`).

- **The held ruleset.** One immutable `Snapshot(ruleset, etag)` in a `volatile` field of `Sync`. Evaluation
  reads the field once into a local and uses only that, so a hook's `etag` and `flagVersion` belong to
  the ruleset the evaluation used. A load builds a new `Snapshot` and stores it under the `state` monitor
  together with `lastSyncAt` and `lastError`, so `status()` sees them from one moment. `variation` takes no
  lock to read it.
- **Locks.** Each is held for a few in-memory operations and never across I/O or a callback:
  1. `Sync.state` (a monitor): the snapshot store, `lastSyncAt`, `lastError` and the stream state.
     `status()` takes it for a few field reads. `variation` never takes it.
  2. `Usage.lock` (a monitor): the pending-count map and the "recording stopped" flag. `variation` takes it
     for one map add. A flush takes it only to swap the map out and to merge kept counts back in.
  3. `Usage.flushLock` (`ReentrantLock`): makes flushes serial. Only the flush thread and `close()` take it;
     a flush holds it across its requests, and evaluation never touches it.
  4. `Hold` (a monitor on two longs): the `Retry-After` deadline of a route.
  A hung server or a hung flush therefore cannot slow `variation` down. The first `close()` flips an
  `AtomicBoolean`; every other call waits on a `CountDownLatch` that the shutdown releases.
- **Callbacks.** `afterEvaluation` and `onError` run with no SDK lock held, so a callback may call
  `status()`, `variation` and `close()` on the same client without a deadlock. A `close()` made from the
  closing thread's own callback, or from the sync or flush thread, returns at once instead of waiting for
  itself. `ClientTest` has the hook and `onError` re-entrancy test.
- **Threads.** Two, started by `create`, both daemon: `toggleslash-sync` (the first load, then the stream
  and poll loop) and `toggleslash-flush`. A client that is never closed does not keep the JVM alive
  (`ClientTest` runs a child JVM that does exactly that and expects it to exit). The JDK's own
  `HttpClient` selector thread is a daemon too.
- **Waits.** The sync loop waits on a stop `CountDownLatch` (`await` with the delay or the hold), the flush
  loop on another, so `close()` wakes both at once. A request in flight is abandoned by cancelling its
  `CompletableFuture`; for the stream, by aborting its body subscriber (below).
- **Close**, in this order: usage stops recording; the sync loop is told to stop and its requests
  (ruleset and stream) are cancelled; the flush loop is told to stop; both threads are joined; `ready()` is
  settled (`false` if nothing loaded); then the final flush runs on the caller's thread, with the usage
  HTTP client, which is not cancelled, so the last counts can go out. The final flush does not wait out a
  hold: counts it could not send because the server asked to wait go to `onError` as "closed with N usage
  counts unsent: the server asked to wait". The done latch is released last.

## HTTP and SSE

**Client.** `java.net.http.HttpClient`, forced to HTTP/1.1 (so the JDK sends no `Connection: Upgrade` or
`HTTP2-Settings` header on a cleartext request), redirects off (a `3xx` is `http_<status>`, which the
contract allows). A `Client` owns two `Http` wrappers, each over its own `HttpClient`: one for the ruleset and
the stream, which `close()` aborts, and one for usage, which it does not abort so the final flush can go out.
A JDK 17 `HttpClient` cannot be closed; its threads are daemons and it is garbage collected with the `Client`.
The headers the SDK sets are exactly `Authorization` (every request), `Accept` (stream only), `If-None-Match`
(ruleset only, when a ruleset and an ETag are held) and `Content-Type` (events only). The JDK adds `Host`,
`User-Agent` (`Java-http-client/<jdk version>`) and `Content-Length`; `RUN.md` lists them with why none
identifies a user or a machine, and `ClientTest.everyRequestCarriesOnlyTheProtocolsHeadersAndTheJdksThree`
holds it on a real loopback socket.

**The whole-request bound (7.1).** A ruleset or usage request is `HttpClient.sendAsync(request, handler)`,
and the SDK waits `future.get(requestTimeout)`. Past the limit it calls `future.cancel(true)` and the failure
is `timeout`. That bounds the connect, the headers and the whole body together, which `HttpRequest.timeout`
alone does not (it ends at the headers). The body is read only for a `200`; any other answer's body is
dropped unread.

**The stream (7.2).** The stream is its own `HttpResponse.BodySubscriber` (`Http.Stream`): it hands itself
over as soon as the headers arrive, requests one chunk at a time from the JDK (back-pressure) and puts each
on a `LinkedBlockingQueue`. The sync thread reads the queue with `poll(streamIdle)`, so the 60 s watchdog is
that one call: any chunk, a keep-alive comment included, restarts it, and an empty poll is `timeout`. The
open is bounded by `requestTimeout`; after it the stream is bounded only by the watchdog. Aborting pushes a
sentinel and cancels the subscription, so a blocked reader wakes at once. (`BodyHandlers.fromSubscriber`
would hold the answer until the body ended, which is why the subscriber is written out.) A status other than
200 is `http_<status>`; a `429` is also read for `Retry-After`. A body that ends cleanly is not an error and
counts as never connected unless a `connected` event arrived.

**Server-Sent Events.** A hand-written parser in `SseParser` to the WHATWG rules the contract names. It
decodes UTF-8 incrementally, so a character split across reads survives. LF, CRLF and a lone CR end a line
(a CR at the end of one read is remembered so the LF that starts the next is not a second line end); a line
that starts with `:` is a comment; a field with no `:` has an empty value; one leading space of a value is
dropped; a leading BOM is dropped; several `data` lines are joined with LF; an event with no `data` line is
dropped; the event name is `message` when none was given; and an event incomplete when the stream ends is
never delivered. No SSE library is used.

## Dependencies

Runtime: none. The JDK has HTTP (`java.net.http`), SHA-256 (`MessageDigest`), time and synchronisation.
The JSON reader is hand-written (see "JSON") because the JDK has no JSON parser and the spec pins number
and string behaviour no common library keeps.

Build, test and lint (none ship in the SDK): Maven and its compiler, surefire and build-helper plugins;
JUnit Jupiter for the tests (no third-party assertion library); `google-java-format` for layout; Checkstyle
for a short rule list; `javac -Xlint:all -Werror` for warnings. The test service and the fake server in the
tests use the JDK's `com.sun.net.httpserver`.

## JSON

The JDK has no JSON parser, so the SDK has its own, in the evaluator package (`evaluation.Json`). It reads
into a plain tree: `LinkedHashMap<String,Object>`, `ArrayList<Object>`, `String`, `Double`, `Boolean` and
`null`. It is also what a caller's context and a `jsonVariation` value are made of.

- **Numbers** are `Double`, read with `Double.parseDouble` after a grammar check of RFC 8259, so `1e999`
  is `Infinity` (not an error), `9007199254740993` is `9007199254740992`, and `-0.0` is kept as negative
  zero. Writing a value back out spells `Infinity` as `1e999`, `-Infinity` as `-1e999` and negative zero as
  `-0`.
- **Strings** are Java `String`s, which are UTF-16, so a lone surrogate (`"\ud800"`) is held natively and a
  valid escaped pair is the pair. Raw bytes that are not valid UTF-8 in the input are read as U+FFFD.
  Writing a value back out escapes a lone surrogate as `\udXXX`.
- **Objects** are ordered maps, and a duplicate key keeps the last value. A leading byte order mark is
  skipped. Nesting deeper than 512 levels is an error (so a ruleset that deep is `bad_ruleset`, and a
  stack overflow cannot happen).
- **Contexts.** The public `Context` wraps a tree, so it can hold `1e999` and lone surrogates. It is built
  from `Context.of(Map)`, `Context.user(key)`, `Context.kind(kind, key)`, `Context.with(attribute, value)`,
  `Context.multi(...)` or from raw JSON with `Context.fromJson(String)`, which never fails: text that is not
  JSON gives a context the evaluator answers with `INVALID_CONTEXT`, like any other invalid one.

**Strings in the evaluator.** The spec compares by UTF-16 code unit, which is `String.compareTo` and
`String.equals`, and matches patterns by code point (`String.codePointAt`; a lone surrogate is the code point
with that value). Hashing replaces a lone surrogate with U+FFFD, then hashes the UTF-8 bytes. The patterns
run on `evaluation.Pattern`, a parser plus a Pike VM, so no `java.util.regex` is used anywhere in the
evaluator.

## The minimum runtime and the toolchain

- **Runtime.** The JVM, Temurin builds (the OpenJDK distribution mise installs). GraalVM native image is not
  tested.
- **Minimum version: Java 17**, the oldest LTS that has records, pattern matching for `instanceof` and a
  `java.net.http` with cancellable `sendAsync`. Compiled with `--release 17` (`maven.compiler.release` in
  `pom.xml`), so the compiler rejects an API newer than 17.
- **Developed and tested on** the version `mise.toml` pins (Temurin 25, the latest LTS). CI runs that one only.
- **Build tool: Maven**, pinned by mise; no wrapper jar is committed.
- **Lint and format:** `google-java-format` (`--dry-run --set-exit-if-changed`), Checkstyle with
  `checkstyle.xml` (unused and star imports, braces, modifier order, missing `@Override`, a switch without
  `default`, `equals` without `hashCode`, naming, tabs, trailing whitespace, and Javadoc on every public type
  and method), and `javac -Xlint:all -Werror` through the pom. Checkstyle is a bare JAR under mise, so
  `bin/lint` runs it with `java -jar`.
- **Tests: JUnit Jupiter** run by surefire; `VectorsTest` reads `fixtures/evaluation` at test time and
  `ClientTest` is the native test set. Test seams are package-private methods of `Options.Builder`: `clock`
  (`java.time.Clock`, for the UTC day), `nanoTime` (the hold deadlines), `jitter` (the backoff), `streamIdle`
  (the watchdog's length) and `evaluator` (so a test can make an evaluation raise). None is a global or an
  environment variable.
- **Build output.** `target/` (ignored by git). `bin/test` runs `mvn clean test` every time, which also
  compiles the test service; `bin/test-service` runs the built classes and builds only if there are none.
- **The integer accessor** is `intVariation`, a `long`, bounded to the signed 64-bit range.

## The package layout

```
sdks/java/
  pom.xml                 Maven build: release 17, no runtime dependencies, JUnit for tests
  mise.toml               java, maven, google-java-format and checkstyle pins
  checkstyle.xml          the Checkstyle configuration
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/
    Client  Options  Context  Detail  Status  StreamState  EvaluationEvent  ToggleSlashException   (public)
    Sync  Usage  Http  Hold  SseParser                       (package-private: the loops, the HTTP, the parser)
    evaluation/           the evaluator, no network code: Json, Ruleset, Contexts, Clauses, Pattern and
                          PatternParser, Semver, Dates, Validate, Reference, Result, Reason, ReasonKind, ErrorCode
  src/test/java/.../      ClientTest and FakeServer (native tests), UnclosedMain (a child JVM for one test);
                          evaluation/VectorsTest runs fixtures/evaluation
  test-service/src/       the conformance test service (main class TestService) and README.md
```

The test service sources are added to the test compile by `build-helper-maven-plugin`, so one `mvn` run
builds and checks everything; `bin/test-service` runs `java -cp target/classes:target/test-classes`.
