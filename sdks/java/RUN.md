# Java SDK: run report

## Prompt version

- Prompt: 7622421acf08c310e86e160394d1b26ee36f0f12
- Model: Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-10
- Input: language Java; runtime chosen: the JVM, developed on Temurin 25 LTS; minimum version chosen: Java 17
  (`maven.compiler.release` is 17); package manager chosen: Maven
- Toolchain: pinned in `mise.toml`: Temurin 25.0.5+8 LTS (`temurin-25.0.5+8.0.LTS`), Maven 3.10.0,
  google-java-format 1.37.0, Checkstyle 14.4.0. JUnit Jupiter 6.1.3 is the one test dependency in `pom.xml`.
  Bun 1.4.2 runs the harness (the repository's pin).

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 17 passed of 17 (15 in `ClientTest`, 2 in `VectorsTest`, which runs all the vectors and pins)

## Decisions

1. Minimum Java 17, developed on the Temurin 25 LTS: 17 is the oldest LTS that has `java.net.http.HttpClient`,
   records, sealed types and pattern `instanceof`, and 25 is the current LTS. `--release 17` makes the compiler
   refuse an API newer than 17.
2. Maven over Gradle: one declarative file, no wrapper script to pin, and mise installs it.
3. Lint and format: google-java-format (format) and Checkstyle (rules) over Spotless or Error Prone, plus
   `javac -Xlint:all -Werror`. Both tools come from mise, so `bin/lint` needs nothing else.
4. JUnit Jupiter only: no mocking library, no assertion library. The fake server is a loopback
   `com.sun.net.httpserver.HttpServer` in `FakeServer.java`, so tests exercise real sockets.
5. Coordinates: groupId `com.github.madangryscientist.toggleslash`, artifactId `toggleslash-sdk-java`, version
   `0.1.0-SNAPSHOT`. The root package is `com.github.madangryscientist.toggleslash.sdks.java`, from the repository
   path `sdks/java`. Nothing is published.
6. HTTP: `java.net.http.HttpClient` (no dependency), HTTP/1.1, redirects `NEVER`. Each client has two `Http`
   wrappers, each with its own `HttpClient`: one for sync (ruleset and stream), aborted at `close`, and one for
   usage, which is not aborted so the final flush at `close` can go out.
7. Whole-request bound: `sendAsync` plus `future.get(timeout)` plus `cancel(true)`, and the body is read only for a
   200. `HttpRequest.timeout` alone ends at the response headers and would not bound a slow body.
8. The stream is a custom `BodySubscriber` (`Http.Stream`) with back-pressure of one and a
   `LinkedBlockingQueue`. The 60 s watchdog is `queue.poll(streamIdle)`; abort pushes a sentinel and cancels the
   subscription.
9. JSON: a hand-written parser (`evaluation/Json.java`), so numbers, duplicate keys and `NaN` follow
   `docs/evaluation.md` and not a library's choices. The evaluator has its own `Pattern`/`PatternParser` (a Pike VM),
   and does not use `java.util.regex`.
10. Typed accessors only (`boolVariation`, `stringVariation`, `doubleVariation`, `intVariation`, `jsonVariation`
    and the `Detail` forms), with `integer-accessor` advertised. `intVariation` returns a `long`.
11. The test service is Maven test source (`test-service/src` added with build-helper), so one `mvn test-compile`
    builds the SDK, its tests and the service; `bin/test-service` compiles only when the class file is missing.
12. Two daemon threads per client (`toggleslash-sync`, `toggleslash-flush`), and an unclosed client does not keep the
    JVM alive. `close` is a CAS plus a latch, so two racing closes both return after the first finishes.
13. Test seams are package-private methods on `Options.Builder` (`clock`, `nanoTime`, `jitter`, `streamIdle`,
    `evaluator`), plus package-private accessors on `Client` (`syncHoldRemaining`, `usageHoldRemaining`,
    `flushNow`). No global and no environment variable.
14. The `afterEvaluation` hook runs on the caller's thread, synchronously, under a catch of `Throwable` that sends
    the failure to `onError`. A plain `Consumer`, no futures.
15. CI uses `jdx/mise-action` with `install_args: java maven google-java-format checkstyle`, so CI and the
    developer machine use the same pins.

## Guesses

1. "The exact current stable release" of Java: the latest feature release is not an LTS. Where: PROMPT.md, phase 0
   (toolchain pin). Assumed: the current LTS (Temurin 25.0.5+8), because a JDK feature release leaves support after six
   months and an SDK's users run LTS.
2. Which Maven coordinates to use. Where: PROMPT.md, phase 1 item 7 gives the Go-style host path and says to
   follow the language's convention; Maven has no host path. Assumed: group id `com.github.madangryscientist.toggleslash`
   (the reverse-domain form of the given path), root package `<groupId>.sdks.java`.
3. How to bound a whole request body in `java.net.http`. Where: PROMPT.md, phase 1 item 3 and contract 7.1 say the
   bound covers the body; neither says how in any one library. Assumed: `sendAsync` with `future.get(timeout)` and
   `cancel(true)`, because `HttpRequest.timeout` ends at the response headers. A first try with
   `BodyHandlers.fromSubscriber` held the response future until the body ended, so the stream open timed out and
   23 stream scenarios failed with `lastError: timeout`; the fix was a `BodySubscriber` that hands itself over at once.
4. Checkstyle under mise is a bare JAR with no launcher. Where: PROMPT.md, phase 5 (`bin/lint`). Assumed:
   `java -jar "$(command -v checkstyle)"`.
5. No race detector exists for Java. Where: CHECKLIST.md, "Native tests", first line. Assumed: stress tests with many
   threads, a racing `close`, a hang in each request, and `-Xlint` for what the compiler can find; the line is met
   by `everyOperationFromManyThreadsWithCloseRacingAndTwoClosesRacing`.
6. Where the test service lives for a Maven project. Where: PROMPT.md, phase 1 item 7 (package layout) and
   phase 4. Assumed: `test-service/` as a second test source root (decision 11).
7. `language-notes/java.md` does not exist. Where: PROMPT.md, phase 0 item 9 ("if it exists"). Assumed: nothing in
   it, which is correct for a first run, and proposed below.
8. A fresh checkout needs the network once for `mvn`, and a sandboxed run is offline. Where: PROMPT.md, phase 0
   (set up). Assumed: the first `bin/test` run, online, caches the plugins; later runs work offline.
9. A JDK 17 `HttpClient` is not `AutoCloseable` (that came in 21). Where: contract 11 (`close` stops everything).
   Assumed: abort the in-flight requests and stop the threads; the client's selector thread is daemon and idle, so
   nothing is left running that matters.
10. Headers the JDK adds. Where: CHECKLIST.md, "Code and documents", the header line. Listed in the Checklist
    below: `Host`, `User-Agent` (`Java-http-client/<version>`, the library and version only) and `Content-Length`.
    There is no `Connection` header on an HTTP/1.1 client, and no `Accept-Encoding` is sent.
11. A `Retry-After` longer than nine digits. Where: contract 7.4. Assumed: treated as above 300, so held for 300 s,
    which avoids an overflow in the parse.
12. A 200 stream whose body is empty and ends at once. Where: contract 7.2 and `stream.json`. Assumed: it ends
    cleanly with no `lastError`; the TypeScript SDK reports `http_200` for a missing body. The scenarios pass
    either way.
13. A fallback for the integer accessor given as a JSON number. Where: sdk-test-service.md, "Accessors" (`int`).
    Assumed: a whole number in signed 64 bits, taken as a `long`.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md` phase 0, after "the exact current stable release", add: "For a language with
   long-term-support releases, pin the newest LTS and say so in `DESIGN.md`."
2. For guess 2, in `PROMPT.md` phase 1 item 7, after "use this repository's `github.com/madangryscientist/toggleslash/sdks/<lang>`",
   add: "Where the convention is reverse-domain (Maven), use `com.github.madangryscientist.toggleslash` as the group."
3. For guess 3, in `language-notes/java.md`: "`HttpRequest.timeout` ends at the headers. Do not use `BodyHandlers.fromSubscriber`
   for the stream: it answers the response future only when the body ends. Make the subscriber the body and hand it
   over at once."
4. For guess 4, in `language-notes/java.md`: "Checkstyle under mise is a bare JAR: run `java -jar "$(command -v
   checkstyle)" -c checkstyle.xml src/main/java`."
5. For guess 5, in `CHECKLIST.md`, first native line, after "where the language has one", add: "A language with none
   says so in `RUN.md` and names the stress test that stands in."
6. For guess 6, in `docs/sdk-authoring/PROMPT.md` phase 4, add: "Put the test service where your build tool compiles
   it with the tests, and say where in `test-service/README.md`."
7. For guess 7, create `docs/sdk-authoring/language-notes/java.md` with the facts of guesses 1, 3, 4, 8, 9 and 12:
   LTS pin, the `HttpClient` body rule, the Checkstyle JAR, a first online `mvn` run to cache plugins, no closable
   `HttpClient` before 21 (two clients, abort the sync one, leave the usage one for the final flush), and
   the Maven plus build-helper layout for the service.
8. For guess 10, in `CHECKLIST.md` header line, add: "`User-Agent` values such as `Java-http-client/<version>`
   name only the library and the version and are allowed with the listing."
9. For guess 12, in `docs/sdk-contract.md` section 7.2, say whether a 200 with an empty body is `http_200` or a
   clean end, so SDKs agree.

## Checklist

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equal to the phase 2 count: `VectorsTest`
  (2 tests) reads `fixtures/evaluation/*.json` at test time; `bin/test` prints 17 passed; the count command printed
  `1371 17` and the harness reports `vectors: 1369 passed` with the 2 named skips (1371 minus 2).
- [x] Harness exits 0 with only the 2 named vector skips: `bun run conformance -- --service sdks/java/bin/test-service`
  printed `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, with
  `integer-accessor` advertised, so no scenario skipped.
- [x] Both context shapes: the harness, `privacy.json`, `kinds.json` vectors through the service.
- [x] Result order and the mismatch rule with the SDK's own codes: the harness, `evaluation.json`, `usage.json`.
- [x] `integer-accessor`, fraction is `TYPE_MISMATCH` and `2.0` is `2`: the harness, `evaluation.json`; also
  `ClientTest.theIntegerAccessorTakesWholeNumbersInSigned64BitsAndNothingElse`.
- [x] No failure makes an operation raise: the harness (the service answers 500 on anything raised, and no scenario
  got one).
- [x] `NOT_READY` before the first load: the harness, `load.json`.
- [x] `ready` settles, bounded, repeatable, false after `close` if unsettled: the harness, `load.json`, `close.json`.
- [x] A non-ruleset answer never replaces the one held: the harness, `status.json`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: the harness, `load.json`, `status.json`.
- [x] The stream: the harness, `stream.json`, `load.json`, `status.json`.
- [x] The loop and the jittered backoff: the harness, `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`.
- [x] Status fields, states and `lastError` codes: the harness, `status.json`, shape check on each status read.
- [x] Usage counts: the harness, `usage.json`, `rate-limit.json`.
- [x] Hook events and `onError`, a raising hook and a raising `onError`: the harness, `hooks.json`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`,
  `rate-limit.json`, `usage.json`, `status.json`.
- [x] No context, key or attribute in any request or `onError` message, no extra header: the harness,
  `privacy.json` and the per-scenario checks. Source search for logs and error values: `grep -rln
  "Logger\|System.out\|System.err\|printStackTrace" src/main` finds nothing (the SDK writes no log line), and
  `grep -rn "new ToggleSlashException" src/main` finds `Usage.java:116,144,154` and `Sync.java:171`, each built from a
  fixed code, an HTTP status or a fixed message, never from a context.
- [x] Many threads on every operation, `close` racing `variation`, two `close` calls racing:
  `ClientTest.everyOperationFromManyThreadsWithCloseRacingAndTwoClosesRacing`. No race detector exists for Java, so
  this is a stress test (see guess 5).
- [x] `variation` returns within a bound while a ruleset request and a usage flush hang:
  `ClientTest.variationReturnsQuicklyWhileARulesetRequestAndAUsageFlushHang`.
- [x] A reader sees one ruleset across a swap, the hook's `etag` and `flagVersion` pair:
  `ClientTest.aHooksEtagAndFlagVersionBelongToTheSameRulesetUnderConcurrentSwaps`.
- [x] Hook and `onError` calling back into the client do not deadlock:
  `ClientTest.aHookAndAnOnErrorThatCallBackIntoTheClientDoNotDeadlock`.
- [x] A silent stream is dropped as `timeout`, a keep-alive restarts the 60 s, through a seam on a real loopback
  socket: `ClientTest.aStreamWithNoBytesIsDroppedAsTimeout`, `ClientTest.aKeepAliveRestartsTheWatchdog` (seam
  `Options.Builder.streamIdle`, `FakeServer` socket that goes quiet).
- [x] `Retry-After` above 300 held for 300 s on the three routes:
  `ClientTest.aRetryAfterAbove300IsHeldFor300SecondsOnTheRulesetRoute`, `...OnTheStreamRoute`, `...OnTheEventsRoute`.
- [x] Counts either side of UTC midnight go out as two days:
  `ClientTest.countsRecordedEitherSideOfUtcMidnightGoOutAsTwoDays`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`:
  `ClientTest.anEvaluationThatRaisesGivesTheFallbackEvaluationFailedAndOneOnError`.
- [x] n/a: no futures. The Java hook is a plain `Consumer<EvaluationEvent>`, so there is no later failure to route.
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor:
  `ClientTest.theIntegerAccessorTakesWholeNumbersInSigned64BitsAndNothingElse`, and the harness `huge` scenario.
- [x] A client never closed does not keep the process alive: `ClientTest.theSdksThreadsAreDaemonThreads` and
  `ClientTest.aClientThatIsNeverClosedDoesNotKeepTheProcessFromExiting` (a child JVM, `UnclosedMain`, that never closes).
- [x] `DESIGN.md` passes the phase 1 row check and `README.md` has the mapping table: both in `sdks/java`;
  the row check in PROMPT.md phase 1 was run and printed nothing, and `README.md` line 48 links to the mapping table.
- [x] Mapping names keep the canonical meaning and defaults, and reason kinds, error codes, stream states and
  `lastError` codes keep their spelling: `Status`, `StreamState`, `ReasonKind` and `ErrorCode` use the contract's
  spellings, held by the harness's status shape check.
- [x] No platform regular expression in the evaluator: `grep -rn "java.util.regex" src/main/.../evaluation`
  finds nothing. The only `.matches(` hit is `Clauses.java:57`, a call on the SDK's own `Pattern` class.
- [x] Every runtime dependency has a `DESIGN.md` line: there are none (`DESIGN.md`, "Dependencies"); `pom.xml`
  lists JUnit Jupiter as the one test-scope dependency.
- [x] No telemetry and no header beyond the protocol's: `grep -n` on the header names finds only `Sync.java:223`
  (`Authorization`), `:225` (`If-None-Match`), `:269` (`Authorization`), `:270` (`Accept`) and `Usage.java:137`
  (`Authorization`), `:138` (`Content-Type`). Headers the JDK `HttpClient` adds, each naming no user or machine:
  `Host` (the server's own authority, from the URL), `User-Agent` (`Java-http-client/<version>`, the library and
  JDK version only, not the host or user), `Content-Length` (the body size on the events POST). No
  `Connection` header (HTTP/1.1 client) and no `Accept-Encoding`.
- [x] Every extra is one contract 13 allows: typed accessors, a `Detail` form, the `flushNow` and seam accessors
  (package-private test seams), and nothing that weakens contract 5 or sends anything new.
- [x] Seams are options on the builder, never a global or an environment variable: `Options.Builder.clock`,
  `nanoTime`, `jitter`, `streamIdle`, `evaluator` (all package-private); the SDK reads no environment variable.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK and use only the mise pin:
  `ls -l bin` shows mode `-rwxr-xr-x` on all three; each runs through `mise exec`.
- [x] `test-service/README.md` maps every accessor to an SDK method: bool, string, number, int, json and any.
- [x] `.github/workflows/sdk-java.yml` follows the skeleton, filters on paths and pins each action by commit SHA:
  `actions/checkout`, `oven-sh/setup-bun` and `jdx/mise-action` are each pinned to a 40-character SHA with the version
  in a comment.
- [x] No em dash or en dash: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes (phase 5 check exited 0).
- [x] Only `sdks/java/` and the one workflow file changed: the phase 6 scope check prints `scope ok`.
- [x] No package published and no name reserved: nothing was uploaded or registered; the version is
  `0.1.0-SNAPSHOT`.
- [x] Commit subject is `chore(sdk-java): a Java server SDK built from the authoring prompt`, not `feat`, with no
  `Co-Authored-By` and no AI attribution line.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of this list: this file.
