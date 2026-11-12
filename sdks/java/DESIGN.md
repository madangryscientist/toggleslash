# Java SDK: design

A ToggleSlash server SDK for Java, built from `docs/sdk-authoring/PROMPT.md` against `docs/sdk-contract.md`
(the contract) and `docs/evaluation.md` (the spec). Everything below is about this tree, `sdks/java`.

## Mapping table (contract section 2)

The root package is `com.github.madangryscientist.toggleslash.sdks.java`; the entry class is `Client`, its options
are `Options` (a builder), and `Client.create(Options)` starts it. Nothing is registered under these names.

| Name | In Java |
| --- | --- |
| `create` | `Client.create(Options options)`: a static factory that returns a `Client` at once, starts two daemon threads and does not wait for the network. It never throws: an `Options` with a missing or unusable `url` or `sdkKey` gives a client whose every request is a `network` error. |
| `ready` | `boolean Client.ready()`: blocks the calling thread until the first load has finished (bounded by `requestTimeout`), then answers whether a ruleset is held. The answer is fixed at the first settle and repeats on every call. On the SDK's own threads it never blocks (it answers from the state held). |
| `variation` | typed `boolVariation`, `stringVariation`, `intVariation`, `floatVariation`, `jsonVariation`, and the generic `<T> T variation(String flagKey, Object context, T fallback)`. Each returns the value or the fallback and never throws. |
| `variationDetail` | the same names with `Detail` after `variation`: `boolVariationDetail`, `stringVariationDetail`, `intVariationDetail`, `floatVariationDetail`, `jsonVariationDetail`, and the generic `<T> Detail<T> variationDetail(String flagKey, Object context, T fallback)`. A `Detail<T>` is a record `(T value, Integer variationIndex, Reason reason)`; `variationIndex` is null for a fallback. A `Reason` is a record `(ReasonKind kind, Integer ruleIndex, String flagKey, ErrorCode errorCode)`: `ruleIndex` is set for `RULE_MATCH`, `flagKey` (the failed prerequisite) for `PREREQUISITE_FAILED`, `errorCode` for `ERROR`, and each is null otherwise. |
| `status` | `Status Client.status()`: a record `(String etag, Long lastSyncAt, StreamState stream, String lastError)`, one consistent snapshot. `etag` and `lastError` are null when there is none; `lastSyncAt` is whole epoch milliseconds or null. |
| `close` | `void Client.close()`: idempotent and safe from any thread, concurrently included. It returns after the shutdown is done, within about twice `requestTimeout`; on the SDK's own thread it stops the rest and returns without waiting for itself. `Client` also implements `AutoCloseable`. |
| `url` | `Options.Builder.url(String)`: required. Trailing `/` characters are removed. |
| `sdkKey` | `Options.Builder.sdkKey(String)`: required. |
| `stream` | `Options.Builder.stream(boolean)`, default on. The field is a `Boolean` that is null until set, so "not given" is told from false. |
| `pollInterval` | `Options.Builder.pollInterval(Duration)`, default 30 s. A null, zero or negative duration is "not given" and the default applies. |
| `flushInterval` | `Options.Builder.flushInterval(Duration)`, default 30 s. Same rule. |
| `requestTimeout` | `Options.Builder.requestTimeout(Duration)`, default 10 s, covering the whole request, body included. Same rule. |
| `hooks.afterEvaluation` | `Options.Builder.afterEvaluation(Hook)`: `Hook` is a functional interface, `Object onEvaluation(EvaluationEvent event) throws Exception`, default none. The return value is ignored, except that a `Future` or `CompletionStage` it returns that later fails goes to `onError`. An `EvaluationEvent` is a record `(String flagKey, Object context, Object value, Integer variationIndex, Reason reason, String etag, Long flagVersion)`. |
| `onError` | `Options.Builder.onError(Consumer<Throwable>)`, default ignore. |
| `accessor shape` | both: typed methods (`bool`, `string`, `int`, `float`, `json`, each with a `Detail` twin) and one generic `variation`/`variationDetail` over the fallback's class. The typed integer accessor is `intVariation` and uses `long`, the signed 64-bit type. The generic method reads the fallback's class: `Boolean`, `String`, `Byte`, `Short`, `Integer`, `Long` (integer accessor semantics in that type's range), `Float`, `Double`, `List`, `Map`, or null (any kind). A class with no JSON kind gives the fallback and `TYPE_MISMATCH`. The hook event's `value` is what the caller gets, boxed: `Boolean`, `String`, `Double`, `Long` for `intVariation` (the accessor's own integer type), `Integer`/`Short`/`Byte` for a generic call with that fallback class, and the JSON tree (`List`, `Map`, `Double`, `String`, `Boolean`, null) for `json`. |
| `onError thread` | the thread that saw the failure: the sync thread (`toggleslash-sync`) for ruleset and stream failures, the flush thread (`toggleslash-flush`) for usage failures, the caller's own thread for a hook that raised, an evaluation that raised and counts lost at `close`. No SDK lock is held while it runs. |

**The closed sets keep their spelling.** `ReasonKind` is an enum with the constants `OFF`,
`PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`. `ErrorCode` is an enum with
`INVALID_CONTEXT`, `FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`, `MALFORMED_FLAG`, `MALFORMED_SEGMENT`,
`PREREQUISITE_CYCLE`, `PREREQUISITE_DEPTH_EXCEEDED`, `NOT_READY`, `TYPE_MISMATCH`, `EVALUATION_FAILED`.
`StreamState` is an enum with `off`, `connecting`, `connected`, `disconnected`, `closed` as lower-case
constants (`StreamState.off`), so the spelling is the contract's; Java allows it and checkstyle's constant
name rule is configured for it. `lastError` is a `String` holding exactly one of `http_<status>`, `timeout`,
`bad_ruleset`, `network`, or null. Each enum has `toString()` equal to its constant's name.

**"Not given" versus false or zero.** `stream` is stored as a `Boolean` (null until `stream(boolean)` is
called), so an unset option is on and an explicit `false` is off. A duration is stored as a `Duration` that is
null until set; null, zero and negative all resolve to the default (the contract says that behaviour is
unspecified and recommends the default).

## Concurrency (contract section 6)

- **One atomic holder.** All mutable client state a reader needs lives in one immutable record, `Core`
  `(Ruleset ruleset, String etag, Long lastSyncAt, StreamState stream, String lastError)`, held in one
  `AtomicReference<Core>`. The ruleset and its ETag change in one compare-and-set, so a reader sees the old
  pair or the new pair, never a mix (6.3). `variation*` reads the reference once and uses that snapshot for the
  evaluation, the hook's `etag` and its `flagVersion`. `status` reads it once too, which makes it one
  consistent snapshot (6.5): a stream attempt's end sets `lastError` and `stream` in one update.
- **Threads started.** `toggleslash-sync` (daemon): the first load, then the loop of 7.3, and the stream
  reads. `toggleslash-flush` (daemon): sleeps `flushInterval`, flushes, repeats. The JDK's `HttpClient` adds its
  own daemon selector and executor threads. A client never closed therefore does not keep the process alive
  (6.6).
- **Locks.** There is no lock on the evaluation path. Usage counts go in a `ConcurrentHashMap` with `merge`
  (lock free for the caller). One `ReentrantLock` serialises usage flushes (9, "Flushes are serial"); it is held
  across the network, and only a flush takes it: no `variation`, `status`, `ready` or `close` waits for it
  except `close`'s final flush, which is meant to. `ready` waits on a `CountDownLatch`, not a lock. The
  in-flight request set is a concurrent set. A `Gate` (a monitor with a flag) tells the loops to stop and wakes
  their sleeps.
- **No lock during callbacks.** `afterEvaluation` and `onError` are called with nothing held by the SDK: not the
  flush lock, not a monitor. A callback that calls `status`, `variation`, `ready` or `close` cannot deadlock.
  `ready` and `close` check whether the calling thread is one of the SDK's own two threads: `ready` then
  answers from the state held, `close` stops what it can, starts the rest of the shutdown on a short-lived
  daemon thread and returns without waiting.
- **Close.** The first `close` call marks the core `closed` (the stream state becomes `closed`, and an error
  recorded after that is dropped, so nothing reaches `lastError` or `onError` for an abandoned request),
  opens the stop gate, cancels every in-flight ruleset and stream request, settles `ready` false when it had not
  settled, then waits for the two threads to end (`join`), and sends the final usage flush. Every other call
  waits on the same done latch. A usage request that is running is not abandoned: it is bounded by
  `requestTimeout`, which gives the "about twice `requestTimeout`" bound (one running request plus the final
  flush's own request).
- **Evaluation is bounded.** `variation` does a memory read, the evaluator and a `ConcurrentHashMap` add. A
  hung ruleset request or flush cannot slow it, which a native test measures.

## HTTP and SSE (contract section 7)

- **Client.** The JDK's `java.net.http.HttpClient`, HTTP/1.1 (so a cleartext request carries no `Upgrade` or
  `HTTP2-Settings` header), redirects never followed (a `3xx` is `http_<status>`, and the key is never sent to
  a second host).
- **`requestTimeout` covers the body.** `HttpRequest.timeout` ends at the headers, so it is not used. Every
  ruleset and events request is `sendAsync(request, BodyHandlers.ofByteArray())`, and the thread waits on that
  future with `get(requestTimeout)`. On expiry the future is cancelled (which aborts the exchange) and the
  attempt is `timeout`. The future is the whole request, headers and body. The stream open is the same wait on
  a `BodyHandlers.ofInputStream()` future, which completes when the headers arrive, so only the open is bounded
  by `requestTimeout`.
- **In-flight set.** Each ruleset and stream future is added to a concurrent set while it runs. `close` cancels
  all of them (the JDK `HttpClient` has no `close` before Java 21, and the minimum is 17).
- **The watchdog.** After the open, each `read` on the response `InputStream` re-arms a timer for 60 s on one
  daemon `ScheduledThreadPoolExecutor` thread per client. When it fires it marks the attempt `timeout` and
  closes the stream, which unblocks the blocked `read`. The length is the `Tuning.watchdog` field (60 s by default), a seam in `internal` that only a test sets (with `Client.create(Options, Tuning)`, package-private); `Tuning` also carries the clock and an injected `HttpClient`, which the TLS test uses.
  Every read, a keep-alive comment included, re-arms it; the native test reads a real loopback socket that goes
  quiet.
- **SSE parsing.** A hand-written parser (`SseParser`), WHATWG rules: bytes are decoded by an incremental
  UTF-8 decoder that keeps a half sequence between reads, then a line ends at LF, CRLF or a lone CR (a CR at the
  end of a chunk is held until the next character tells which), a line starting with `:` is a comment, the
  field is the text before the first `:` with one leading space removed from the value, `data` lines are joined
  with LF, an event with no `data` is dropped, the name defaults to `message`, and an event still incomplete at
  the end is discarded. A leading byte order mark is skipped.
- **Ruleset body.** Decoded by the same WHATWG UTF-8 decoder (one U+FFFD per maximal invalid subpart), a leading
  BOM skipped, then parsed by the SDK's JSON parser; a duplicate key keeps the last value. The nesting limit is
  1000 levels (the contract's floor is 512); a deeper body is `bad_ruleset`.
- **Retry-After.** Read only on a `429`, whole decimal seconds with surrounding spaces ignored, capped at 300 s.
  Two holds: one for the ruleset and stream routes, one for the events route, each a time on the monotonic clock.

## Choices the code makes

- **The generic accessor's class table.** `variation(flagKey, context, T fallback)` reads the fallback's class:
  `Boolean`, `String`, `Long`/`Integer`/`Short`/`Byte` (the integer accessor, checked against that class's own
  range), `Double`/`Float`, `Map`/`List` (a JSON value), or null (any served kind). Any other class, `BigDecimal`
  included, has no JSON kind: the call answers the fallback and `TYPE_MISMATCH`. (A fallback of a `Number`
  subclass with no kind could be converted, but the contract's table has no such row, so the mismatch is the
  safe reading.)
- **A served JSON tree is read-only.** The SDK hands out a deep read-only copy (`Kinds.freeze`), so a caller
  that mutates it cannot change what the next caller gets.
- **The hook's return value.** A `CompletionStage` it returns is watched with `whenComplete`; a plain `Future`
  is looked at only if it is already done (the SDK never blocks on it). A failure of either goes to `onError`.
- **`ready()` is bounded.** It waits at most `requestTimeout` plus one second for the first load, and then
  settles with whatever is held, so a request that hangs past its own timeout cannot hold a caller.
- **A context is converted once, eagerly.** `Evaluator.evaluate` deep-copies the caller's `Map` into the
  evaluator's own tree before any rule runs, with a depth and cycle guard and an `Unsupported` sentinel for a value
  with no JSON form, so a map that changes while it is read, or a hostile one, cannot change an answer half way.
- **A flag version above 2^63 - 1** is reported to the hook as none, since `Long` cannot hold it.

## Dependencies

- **Runtime: none.** The JDK has the HTTP client, SHA-256 (`MessageDigest`) and everything else the SDK needs.
  The JSON parser, the UTF-8 decoder and encoder, the semver and date parsers and the pattern engine are
  written by hand because the spec pins their behaviour (see JSON, below).
- **Build and test (test scope):** `org.junit.jupiter:junit-jupiter` (the test framework);
  `maven-compiler-plugin`, `maven-surefire-plugin`, `maven-resources-plugin`, `maven-clean-plugin` (the build);
  `build-helper-maven-plugin` (adds `test-service/src` as a test source root so the test service compiles
  with the tests).
- **Lint and format:** google-java-format (the format check) and Checkstyle (static rules), both pinned in
  `mise.toml` and run from `bin/lint`.

## JSON

- **Value type.** The SDK's own tree is made of the plain JDK types: `null`, `Boolean`, `Double`, `String`,
  `List<Object>` and `Map<String, Object>`. Every number the parser makes is a `Double` (binary64): `1e999` is
  `Infinity` (not an error), `9007199254740993` reads as `9007199254740992`, and `-0` is `-0.0`. A context built
  from Java values may hold any `Number` (`byte`, `short`, `int`, `long`, `float`, `double`, `BigInteger`,
  `BigDecimal`, `AtomicLong` and so on); it is read as the JSON number of the same value, by `doubleValue()`.
- **Lone surrogates.** A Java `String` is a sequence of UTF-16 units and holds a lone surrogate as it is, so the
  parser keeps `"\ud800"` as the single unit U+D800. Pairs written as two escapes join into one code point
  naturally. The pattern engine and the hash walk the string by code point and treat a lone surrogate as its own
  code point (the hash input turns it into U+FFFD, as the spec says).
- **Why a hand-written parser.** The JDK has no JSON parser. Mine is a recursive descent parser over the
  decoded text with an explicit depth counter, strict RFC 8259 grammar (no trailing commas, no comments, no
  leading zeros, no lone `-`), and `Double.parseDouble` on a validated number token, which rounds correctly.
- **Public entry.** `Json.parse(String)` returns the tree (or throws a `JsonException`) and is what the test
  service uses for requests, so a context with `1e999` or a lone surrogate reaches the SDK intact.
  `Client.variation*` accepts a context as `Object`: a `Map<String, Object>` (single or multi, as the spec
  defines them). `Context` is a set of static helpers that return plain `Map`s (`Context.of("user-1")`,
  `Context.with(context, "plan", "pro")`, `Context.multi(...)`), so a context built by hand or parsed from JSON
  is the same thing.
- **Disagreements with other implementations.** None found so far; this section records one if a reading of
  `packages/engine` or `apps/server/ToggleSlash.Evaluation` differs from the spec.

## Minimum runtime and toolchain

- **Pinned for development and CI:** Temurin 25 (the newest LTS, `mise.toml`), Maven 3.10.0, Checkstyle 14.4.0,
  google-java-format 2.0.0 (1.37.0 fails on JDK 25; see the language notes).
- **Minimum:** Java 17, set with `--release 17` in the pom. It is the oldest LTS still in vendor support on the
  run date and gives records, sealed types, `instanceof` patterns and `HttpClient` cancellation that the SDK
  uses; nothing newer is needed. The test suite and the build run on the pinned 25.
- **Test framework:** JUnit Jupiter, the mainstream choice and the one Surefire runs by default. **Build tool:**
  Maven, the mainstream choice and the one `mise` pins. **Lint:** google-java-format for layout and Checkstyle
  for the Javadoc and naming rules; the Checkstyle config names the rule set it enables and gives a reason for
  each rule it leaves out.
- **The integer accessor** uses `long`, Java's signed 64-bit integer. A served number with a fractional part or
  outside `[-2^63, 2^63 - 1]` is `TYPE_MISMATCH` (2^63 reads as out of range because a `double` cannot be
  `2^63 - 1`).

## Package layout

```
sdks/java/
  mise.toml  pom.xml  checkstyle.xml  .gitignore  README.md  DESIGN.md  RUN.md
  bin/lint  bin/test  bin/test-service
  src/main/java/com/github/madangryscientist/toggleslash/sdks/java/
    Client, Options, Status, StreamState, Detail, Reason, ReasonKind, ErrorCode,
    Hook, EvaluationEvent, Context, Json, JsonException       the public API
    Kinds        (package-private) the accessor kinds, the integer range check, the read-only JSON copy
    eval/        PatternParser, PatternVm, Semver, Instants, Reference, Model, Parse, Values, Contexts,
                 Clauses, Bucket, Ruleset, Evaluator, Evaluation, Utf8 (the evaluator and its parts)
    internal/    SDK plumbing, public only so the root package can reach it: Hub, Sync, Usage, SseParser,
                 Gate, Clock, Core, Tuning, SdkThread, SdkException
  src/test/java/...            native tests, the vector test and the fake loopback server
  test-service/src/...         the conformance test service (compiled with the tests)
  test-service/README.md       the accessor mapping
```

The evaluator is in `eval/` with no network and no `java.util.regex` import. The client (`Client`,
`internal/`) imports the evaluator. `eval/` imports only three small root types, `Reason`, `ReasonKind` and
`ErrorCode` (the result it builds), and nothing in it imports `Client` or `internal/`. The vector test reads
`../../fixtures/evaluation` from the repository at test time, never a copy. The test service lives in
`test-service/src` and compiles with the tests through `build-helper-maven-plugin`; `bin/test-service` runs
`java -cp target/classes:target/test-classes` and builds only when the classes are missing.
