# Java SDK: design

A ToggleSlash server SDK for Java, written to [`docs/sdk-contract.md`](../../docs/sdk-contract.md) (the
contract) and evaluating to [`docs/evaluation.md`](../../docs/evaluation.md) (the spec). Section numbers
below are the contract's. Nothing is published or registered under any name.

## Input and decisions that follow from it

The brief gave a language and nothing else, so each of these is a decision, not a guess:

- **Runtime:** the JVM (HotSpot, OpenJDK builds). No Android, no GraalVM native image promise.
- **Pinned toolchain:** Temurin **25.0.5+8** (the newest LTS) in `mise.toml`, with Maven 3.10.0, Checkstyle
  14.4.0 and google-java-format 2.0.0.
- **Minimum version:** **Java 17**, the oldest LTS still in security support at the run date and the first
  with records, sealed types and `switch` patterns' prerequisites. It is compiled with `--release 17`, so
  the build proves nothing newer is used. `java.net.http.HttpClient` (Java 11) is the only I/O library.
- **Build tool:** Maven, the mainstream choice. One module, one `pom.xml`.
- **Package:** `com.github.madangryscientist.toggleslash.sdks.java`.

## 1. The mapping table

| Name | In Java |
| --- | --- |
| `create` | `Client.create(Options)`, a static factory. It returns a client and never throws (a missing or unusable URL or key is an error the first load reports through `onError` and `status`; a `null` options value is read as empty). It starts two daemon threads and does not wait for the network. |
| `ready` | `boolean Client.ready()`. Blocks until the first load attempt settles, bounded by about twice `requestTimeout`; `true` if a ruleset loaded, else `false`; the answer is fixed by the first attempt. `close` settles an unsettled one `false`. Called from one of the SDK's own callbacks it answers at once from the state held. |
| `variation` | `Client.variation(flagKey, context, fallback)`, generic, and typed methods `boolVariation`, `stringVariation`, `doubleVariation`, `longVariation` and `jsonVariation`, each `(String flagKey, Object context, T fallback)` returning the value. |
| `variationDetail` | The same six names with a `Detail` suffix, each returning `Detail<T>(value, variationIndex, reason)`; `variationIndex` is an `Integer`, null for a fallback. |
| `status` | `Status Client.status()`, one immutable `record Status(String etag, Instant lastSyncAt, StreamState stream, String lastError)`, read from one snapshot. |
| `close` | `void Client.close()` (`AutoCloseable`). Idempotent and safe to call concurrently; every call returns after the one shutdown is done, within about twice `requestTimeout`. |
| `url` | `Options.builder(url, sdkKey)`, first argument. Trailing `/` characters are removed. |
| `sdkKey` | `Options.builder(url, sdkKey)`, second argument. |
| `stream` | `Options.Builder.stream(boolean)`, default `true`. |
| `pollInterval` | `Options.Builder.pollInterval(Duration)`, default 30 s (`Options.DEFAULT_INTERVAL`). |
| `flushInterval` | `Options.Builder.flushInterval(Duration)`, default 30 s. |
| `requestTimeout` | `Options.Builder.requestTimeout(Duration)`, default 10 s (`Options.DEFAULT_REQUEST_TIMEOUT`). |
| `hooks.afterEvaluation` | `Options.Builder.afterEvaluation(Consumer<EvaluationEvent>)`, default none. The event is a record `(flagKey, context, value, variationIndex, reason, etag, flagVersion)`. |
| `onError` | `Options.Builder.onError(Consumer<Throwable>)`, default ignore. The value is a `ToggleSlashException` with a `code()`. |
| `accessor shape` | Both. A generic `variation<T>` that follows the fallback's class (a null fallback takes any JSON value), and typed methods for `boolean`, `String`, `double` (the number accessor), `long` (the integer accessor) and `Object` (the JSON accessor, any kind). `long` is the 64-bit signed integer, so a served whole number outside that range is `TYPE_MISMATCH`. |
| `onError thread` | A failed ruleset, stream or usage request: one of the SDK's two daemon threads, no SDK lock held. A hook that raised and an evaluation that raised (`EVALUATION_FAILED`): the caller's thread, inside the `variation` call, no SDK lock held. Counts lost at `close`: the thread that ran the shutdown (the caller of `close`, or a daemon `toggleslash-close` thread when `close` was called from an SDK thread). `onError` may call back into the client. |

**Closed sets.** `ReasonKind`, `ErrorCode` and `StreamState` are enums whose constant names are the
canonical spelling (`PREREQUISITE_FAILED`, `NOT_READY`) and, for `StreamState`, whose `value()` is the
lowercase spelling (`disconnected`). The constants of `ReasonKind` and `ErrorCode` are written in upper
snake case in the source, so `name()` is the spelling on the wire; `StreamState` keeps the lowercase
spelling in `value()` because a Java constant cannot be lowercase without breaking the naming rule. `Status.lastError` is a `String` because
`http_<status>` is open-ended; `timeout`, `bad_ruleset` and `network` are constants on `Status`.

**Options with a non-zero default.** The builder keeps a `Boolean` for `stream` and a `Duration` for each
interval, all `null` until set. "Not given" is `null` and takes the default; a given `false` stays `false`.
A null, zero or negative `Duration` is treated as not given and takes the default (contract 2). `Options` is immutable once built.

**Contexts.** A context is a plain Java value of the JSON data model: `Map<String, Object>`, `List`,
`String`, `Boolean`, a `Number` (read as binary64) or null. There is no wrapper type, so a Java `String` with a
lone surrogate (which Java strings hold natively) goes into the context untouched.

## 2. Concurrency

- **What holds the ruleset and ETag.** One `AtomicReference<Snapshot>`. `Snapshot` is an immutable record of
  ruleset, ETag, last sync time, stream state and last error. The loader builds the next snapshot from the
  current one and publishes it with one `set`, so the ruleset and the ETag that names it swap together, and
  `status` and the hook's `etag` and `flagVersion` each read one moment. `variation` takes one `get` and
  evaluates from that value.
- **Threads.** Two daemon threads per client: the sync loop (first load, stream, polling, backoff) and the
  flush loop (usage). Both are `SdkThread`s, which record their owner so a callback can tell it runs on the
  SDK's own thread. `close` sets the closing flag, stops counting, signals a `Stop` both loops wait on,
  cancels the in-flight ruleset and stream calls, completes `ready` with `false`, joins both threads (bounded
  by about twice `requestTimeout`), then sends the last flush. A client that is never closed keeps nothing
  alive: both threads are daemons, and a test runs a child JVM to prove it.
- **Locks.** `Usage` has a plain monitor around its count map (`record` is a map increment with no I/O under
  it) and a `ReentrantLock` held for one flush, so flushes are serial. `Hold` (a 429's wait) is an
  `AtomicLong` that only grows. `Stop` and the shutdown future are lock free. No lock is held while `onError`
  or a hook runs, and none is held across I/O except the flush lock, which no callback needs.
- **Shutdown.** `close` is a compare-and-set of a shutdown future: the first caller runs the shutdown, every
  other caller waits on that future. A `close` from an SDK thread cannot join itself, so it starts a daemon
  `toggleslash-close` thread to finish and returns. `close` racing `variation` is safe because `variation`
  needs only the snapshot and the closing flag.

## 3. HTTP and SSE

- **Library.** `java.net.http.HttpClient`, one per client. Before Java 21 it cannot be closed, so the SDK
  keeps every ruleset and stream call and cancels them on `close`.
- **`requestTimeout` and the body.** `HttpRequest.timeout` ends at the response headers, so it is not the
  contract's bound. Each request is sent asynchronously and its future is waited on with a limit of
  `requestTimeout` for the whole request, body included; on the limit the call is cancelled and the error is
  `timeout`. The body is read with a subscriber that accumulates the bytes, so a body that never ends is cut
  by the same limit.
- **Opening the stream.** The same wait, until the response headers arrive; past it, `timeout`. Once open, the
  body is a queue of byte chunks fed by a subscriber, and the reader takes the next chunk with a 60 s wait.
  Every chunk (a keep-alive comment included) restarts it; a wait that finds nothing drops the stream as
  `timeout`. The 60 s is `Options.STREAM_IDLE` and a test seam on `create` (`streamIdle`) shortens it.
- **The SSE parser.** `Utf8.Decoder` turns the bytes into text, a chunk at a time, with replacement (one
  U+FFFD per maximal invalid subpart, as WHATWG) and a sequence split across chunks held over. `SseParser`
  takes that text in any chunking and gives events as the WHATWG standard reads a stream: it skips a leading
  byte order mark, ends a line on `\r\n`, `\n` or a lone `\r` (a `\r\n` split across two chunks is one
  break), ignores a comment line, keeps `event` and `data` only, joins `data:` lines with `\n`, dispatches on
  a blank line and drops an event with no `data`. It has no cap on a line or an event.
- **Retry-After.** `Hold.parse` accepts whole decimal digits only and caps the wait at 300 s; the cap is a
  seam on `create` (`retryAfterCap`).

## 4. Dependencies

- **Runtime: none.** The SDK uses the JDK only: `java.net.http`, `java.util.concurrent`, `java.time`.
- **Tests: `org.junit.jupiter:junit-jupiter` 6.1.3**, test scope, the only dependency in the `pom.xml`.
- **Build plugins:** `maven-compiler-plugin` 3.14.1 (`-Xlint:all -Werror`, release 17),
  `build-helper-maven-plugin` 3.8.0 (adds `test-service/src` as a test source root), `maven-surefire-plugin`
  3.6.1.
- **Lint tools, not dependencies:** Checkstyle 14.4.0 and google-java-format 2.0.0, installed by mise.

## 5. JSON

The SDK has its own parser and writer (`json/Json`), because the data model has to be binary64 numbers and
the lint of what a JDK parser keeps is not enough.

- Every number is a `Double`: `1e999` is positive infinity and `9007199254740993` reads as
  `9007199254740992`. `-0` is `-0.0`; a number that is not finite writes as `null`.
- A duplicate object key keeps the last value. A leading byte order mark is skipped. Invalid UTF-8 is
  replaced as in section 3.
- A lone surrogate, written as an escape in the text or held in a `String`, is kept as that one UTF-16 code
  unit. The public API takes the context as Java values, so a lone surrogate in a context string is not
  lost on the way in.
- Nesting is refused beyond 1000 levels (`bad_ruleset`); the contract allows any limit of at least 512, and a
  test parses 600 levels.
- Integer accessor: a served number is a binary64 value, so the accessor takes it when it is whole and in
  `[-2^63, 2^63)` and gives that `long`; a fraction or a value outside the range is `TYPE_MISMATCH`. (A
  literal such as `9223372036854775807` reads as `2^63` in binary64, which is outside, so it is a mismatch;
  that follows from the data model, not from a check of the text.)

## 6. Package layout

- `...sdks.java`: the public API (`Client`, `Options`, `Status`, `StreamState`, `Detail`, `EvaluationEvent`,
  `ToggleSlashException`) and the package-private machinery (`Sync`, `Http`, `Usage`, `Hold`, `Stop`,
  `Snapshot`, `Reporter`, `SdkThread`, `SseParser`, `Clock`, `Evaluate`).
- `...sdks.java.evaluation`: the evaluator to `docs/evaluation.md`. `Evaluator` is the entry point;
  `Ruleset` validates and holds the parsed ruleset; `Clauses`, `Contexts`, `Dates`, `Semver`, `Regex`,
  `PatternParser`, `Reference`, `Bucket` and `Js` (JavaScript's number and string conversions) are its parts.
  The `matches` op runs a Pike VM written here, with no `java.util.regex`; a test searches the sources for
  the platform regex and fails on a hit.
- `...sdks.java.json`: `Json`, `Utf8`, `JsonException`.
- `test-service/src/testservice/Main.java`: the conformance harness's test service, in the test build.

## 7. Disagreements between the spec, the contract and the other implementations

- **The `json` accessor and a non-null fallback.** Contract section 4 (line 113) says a JSON accessor, whose
  type holds every kind, accepts any served kind. The scenario "a value of another kind than the fallback is
  `TYPE_MISMATCH`" (`fixtures/sdk/evaluation.json:176`) sends the `json` accessor a fallback `{}` and a served
  array and expects `TYPE_MISMATCH`. Both readings cannot hold for one method. `Client.jsonVariation` keeps
  the contract's reading (any kind); the test service maps the protocol's `json` accessor to the generic
  `variationDetail`, which compares the served kind with the fallback's kind. The harness passes.
- No disagreement with the TypeScript or C# evaluator was found: every vector passes.
