# ToggleSlash Java server SDK: design

This page is written before the code (phase 1 of `docs/sdk-authoring/PROMPT.md`) and kept true after it.
The contract is `docs/sdk-contract.md`; evaluation is `docs/evaluation.md`.

## Mapping table

Package `com.github.madangryscientist.toggleslash.sdks.java`. The client class is `ToggleSlashClient`; it
implements `AutoCloseable`, and `close()` declares no checked exception.

| Name | In Java |
| --- | --- |
| `create` | `ToggleSlashClient.create(Options)`, where `Options.builder(url, sdkKey)...build()` makes the options. It starts two daemon threads and returns at once. |
| `ready` | `boolean ready()`. Blocks until the first ruleset attempt settles (bounded by `requestTimeout`), then answers the same value every time. |
| `variation` | typed methods `boolVariation`, `stringVariation`, `intVariation` (`long`) and `doubleVariation`, and the generic `Object variation(String flagKey, Object context, Object fallback)`, which is also the `json` and `any` accessor (the fallback's JSON kind does the work; a null fallback accepts any kind; a fallback of a Java type that is no JSON value always answers `TYPE_MISMATCH`). |
| `variationDetail` | one per accessor: `boolVariationDetail`, `stringVariationDetail`, `intVariationDetail`, `doubleVariationDetail` and the generic `variationDetail`. Each returns a `Detail<T>` record `(value, variationIndex, reason)`; `variationIndex` is an `Integer`, null for a fallback. |
| `status` | `Status status()`: a record `(etag, lastSyncAt, stream, lastError)`. `etag` and `lastError` are `String` or null, `lastSyncAt` is an `Instant` or null, `stream` is a `StreamState`. |
| `close` | `void close()`. Idempotent, safe from many threads, never throws. |
| `url` | `Options.Builder.url`, required (the first argument of `Options.builder`). |
| `sdkKey` | `Options.Builder.sdkKey`, required (the second argument of `Options.builder`). |
| `stream` | `Options.Builder.stream(boolean)`. Default on. |
| `pollInterval` | `Options.Builder.pollInterval(Duration)`. Default 30 s. |
| `flushInterval` | `Options.Builder.flushInterval(Duration)`. Default 30 s. |
| `requestTimeout` | `Options.Builder.requestTimeout(Duration)`. Default 10 s. |
| `hooks.afterEvaluation` | `Options.Builder.afterEvaluation(Consumer<EvaluationEvent>)`. Default none. |
| `onError` | `Options.Builder.onError(Consumer<Throwable>)`. Default ignore. |
| `accessor shape` | both: typed methods (`bool`, `string`, `int` as `long`, `double`) and one generic method over the fallback's kind (`json` and `any`). All ten apply the rules of contract section 4. |
| `onError thread` | the thread that saw the failure: the SDK's sync thread for a failed ruleset or stream attempt, the SDK's flush thread for a periodic usage flush, the thread that called `close` for the final flush and for counts lost at close, and the caller's own thread for a hook that raised or an evaluation that raised. Never while an SDK lock is held. |

How the closed sets keep their spelling:

- Reason kinds are the enum `ReasonKind` with constants spelled `OFF`, `PREREQUISITE_FAILED`, `TARGET_MATCH`,
  `RULE_MATCH`, `FALLTHROUGH`, `ERROR`. A `Reason` record holds the kind, a `flagKey` (only for
  `PREREQUISITE_FAILED`), a `ruleIndex` (only for `RULE_MATCH`) and an `ErrorCode` (only for `ERROR`).
- Error codes are the enum `ErrorCode`: `INVALID_CONTEXT`, `FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`,
  `MALFORMED_FLAG`, `MALFORMED_SEGMENT`, `PREREQUISITE_CYCLE`, `PREREQUISITE_DEPTH_EXCEEDED`, `NOT_READY`,
  `TYPE_MISMATCH`, `EVALUATION_FAILED`.
- Stream states are the enum `StreamState`, constants `OFF`, `CONNECTING`, `CONNECTED`, `DISCONNECTED`,
  `CLOSED`; `value()` (and `toString()`) gives the canonical lower-case spelling, `off` and so on.
- `lastError` codes stay strings, because `http_<status>` is open: `http_500`, `timeout`, `bad_ruleset`,
  `network`.
- An option whose default is on or non-zero (`stream`, the intervals, the timeout) is held in the builder as
  a nullable field (`Boolean`, `Duration`): null is "not given" and takes the default. A non-positive
  duration also takes the default (contract section 2, a SHOULD).

## Concurrency

- **The ruleset and its ETag** live in one immutable `Held(ruleset, etag)` object in a `volatile` field.
  One write replaces both, so a reader sees the old pair or the new pair. An evaluation reads the field
  once and uses that `Held` throughout, including for the hook's `etag` and `flagVersion`.
- **Status** is one immutable `Status` object in a second `volatile` field, replaced under a small
  `stateLock` (held for field assignments only, never across I/O, a callback or an evaluation). The ruleset
  swap and the matching status update happen in one `stateLock` section. `status()` reads the field once, so
  it is one consistent snapshot.
- **Threads.** `create` starts two daemon threads: `toggleslash-sync` (first load, stream, backoff, polls)
  and `toggleslash-flush` (the usage flush loop). Daemon threads do not keep the process alive (contract
  6.6). The JDK's `HttpClient` has its own daemon selector and worker threads.
- **Locks.** `stateLock` (above). `usageLock` guards the pending-counts map and the closed flag for
  recording, held for an add or a swap only. `flushLock` serialises flushes and is held across the usage
  network calls; evaluation never takes it. Evaluation touches only the `volatile` fields and `usageLock`
  for a few instructions.
- **No lock during a callback.** `afterEvaluation` and `onError` are called with no SDK lock held. A hook
  that calls `status`, `variation` or `close` cannot deadlock the client.
- **`close`.** The first caller starts the shutdown, the others wait on the same latch. Shutdown, in order:
  set `closed` (under `stateLock`, which also sets the stream state to `closed`), release the closed latch
  that every wait in the SDK is on, abort the in-flight ruleset or stream call, join the sync and flush
  threads, send the final usage flush, settle `ready` as false if it had not settled, then open the done
  latch. A `close` called on one of the SDK's own threads (from a callback) cannot join itself: it starts
  the same shutdown on a short-lived daemon thread and returns at once; other `close` callers still wait for
  the done latch. A `ready` called on the SDK's own thread returns the state held (a ruleset is held now)
  without waiting.
- **Bound on close.** The in-flight ruleset or stream call is abandoned at once. A flush already running is
  waited for, and each of its requests is bounded by `requestTimeout`, then the final flush may take one
  more `requestTimeout` per request, so a hung server makes `close` return within about twice
  `requestTimeout` (one running request, then one final request, because a final flush stops at its first
  failed request).
- **Never raises.** Evaluation, a hook and the SDK's own threads catch `Exception` and `StackOverflowError`
  (deep input) and report to `onError`; other `Error`s (`OutOfMemoryError`) are the JVM's to handle and are not
  caught.

## HTTP and SSE

- **Client.** `java.net.http.HttpClient` (HTTP/1.1 only, so no `Upgrade: h2c` headers; no redirects, so a
  `3xx` is `http_<status>`). One client; every call is a `sendAsync` whose response handler is a custom
  `BodySubscriber` that hands itself over at once, so the response future completes at the response
  headers, not at the end of the body. The SDK keeps the `Flow.Subscription` and its own futures.
- **`requestTimeout` covers the body.** The calling thread owns a deadline: it waits for the headers until
  `start + requestTimeout`, then for the body to end until the same instant. Past it the call is aborted
  (the future is cancelled and the subscription cancelled, which closes the connection) and the result is
  `timeout`. `HttpRequest.timeout` is not used because it ends at the headers.
- **The stream.** Opening is bounded by `requestTimeout` from the request to the headers. After that the
  subscriber parses Server-Sent Events as bytes arrive and puts `connected` and `changed` events on a queue.
  The sync thread takes events with a watchdog: no byte for 60 s (a seam in the package's own config sets
  the length for tests) aborts the stream as `timeout`; every `onNext`, a keep-alive comment included,
  restarts it. The sync thread, not the subscriber, fetches the ruleset an event asks for, so the
  subscription keeps reading while the fetch runs.
- **Abort.** `close` and the end of a stream call `Call.abort()`: it cancels the send future and the body
  subscription, completes the call's own futures with an abort marker (so the waiting thread wakes), and
  records no error. `HttpClient` has no `close` before Java 21, so a per-call abort is the way to stop work.
- **SSE parser.** A hand-written parser (`Sse`) over bytes: it decodes UTF-8 incrementally (a code point
  split across reads is held), ends a line at LF, CRLF or a lone CR (a CR at the end of a chunk waits for a
  possible LF), ignores comment lines, reads only `event` and `data`, joins several `data` lines with LF,
  drops an event with no `data`, defaults the name to `message`, strips one leading space of a value, and
  discards an event still incomplete at the end. No library keeps all of that and the tests depend on it.
- **Ruleset body.** Bytes are collected, decoded with the package's own WHATWG UTF-8 decoder (below), a
  leading BOM is skipped, and the text is parsed.

## Dependencies

- **Runtime: none.** The JDK has HTTP (`java.net.http`) and SHA-256 (`java.security.MessageDigest`). It has
  no JSON parser, so the package has a small one (below); a library would still need the same care for
  binary64 numbers and lone surrogates, so it buys nothing.
- **Test:** JUnit Jupiter 6.1.3 (the test framework). The loopback fake server in the tests is the JDK's own
  `com.sun.net.httpserver` and plain sockets, so there is no test HTTP library.
- **Build and lint (Maven plugins, not on the SDK's classpath):** maven-compiler-plugin, maven-surefire-plugin,
  build-helper-maven-plugin (adds `test-service/src` as a test source root), Spotless with google-java-format
  2.0.0 for formatting (the 1.x line fails on JDK 25), and the Checkstyle command-line JAR that mise
  installs.

## JSON

- `Json.parse(String)` is a recursive-descent parser to plain Java values: `null`, `Boolean`, `Double`,
  `String`, `List<Object>`, `Map<String, Object>` (a `LinkedHashMap`, so a duplicate key keeps the last
  value and order is kept). Numbers are parsed with `Double.parseDouble` over the validated JSON number
  text, so they are binary64: `1e999` is `Infinity`, `9007199254740993` is `9007199254740992`, `-0.0` is
  `-0.0`. A Java `String` is UTF-16, so a lone surrogate written as `\ud800` stays one `char` and compares
  equal only to the same lone surrogate. Depth is limited to 1000 levels (above the contract's 512), which
  gives `bad_ruleset`.
- **The context.** The public API takes `Object context`: a `Map<String, ?>` whose values are JSON values
  (`null`, `Boolean`, any `Number`, `String`, `List`, `Map`). A map from `Json.parse` is exactly the JSON, so
  `1e999` and lone surrogates reach the evaluator intact. Any other object is `ERROR INVALID_CONTEXT`.
  `Json.parse` and `Json.write` are public for that reason.
- **Bytes to text.** `Utf8.decode` implements the WHATWG Encoding standard's UTF-8 decoder (one U+FFFD per
  maximal invalid subpart) and `Utf8.encode` encodes a string with every lone surrogate as U+FFFD (the JDK
  codec writes `?`). Both are tested against the Encoding standard's examples.

## Minimum runtime and toolchain

- **Minimum: Java 17**, set with `--release 17`. It is given in the language notes as a fact. It is the
  oldest LTS still in security support on the run date that has records, `instanceof` patterns, text blocks
  and `HttpClient`.
- **Pinned toolchain** (`mise.toml`): Temurin 25.0.5 (the newest LTS; 27 is a feature release that leaves
  support after six months), Maven 3.10.0 (Maven 4 is still a release candidate), Checkstyle 14.4.0.
- **Build tool: Maven**, the language's mainstream choice; one `pom.xml`.
- **Format: Spotless with google-java-format 2.0.0**; **lint: Checkstyle** with a rule set named in
  `checkstyle.xml` (Javadoc and naming rules for the public API, plus a short list of correctness rules).
- **Test framework: JUnit Jupiter.** The integer accessor returns `long`, Java's signed 64-bit type.

## Layout

```
sdks/java/
  pom.xml  mise.toml  checkstyle.xml  .gitignore  DESIGN.md  README.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/
    ToggleSlashClient, Options, Status, StreamState, Detail, Reason, ReasonKind, ErrorCode,
    EvaluationEvent, ToggleSlashException      the public API
    Json (public), Utf8                        JSON helper; internal UTF-8 codec
    Evaluator, Ruleset, ClauseMatcher, ContextKinds, Bucketing, Pattern, SemanticVersion,
    RfcInstant, AttributeReference, EvalResult the evaluator: no network code, no java.util.regex
    Http, Sse, Usage                           the client's machinery (package-private)
  src/test/java/...                            the tests, including every evaluation vector
  test-service/src/.../service/Main.java       the conformance test service (a test source root)
  test-service/README.md
```

The Maven group is `com.github.madangryscientist.toggleslash` and the artifact `toggleslash-sdk-java`, the
reverse-domain form of `github.com/madangryscientist/toggleslash/sdks/java`. Nothing is registered under it.
`bin/test` compiles and tests everything, then leaves the compiled classes in `target/`, and
`bin/test-service` runs `java -cp target/classes:target/test-classes` on them, building only when they are
missing.
