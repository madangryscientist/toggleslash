# Java SDK: run report

## Prompt version

- Prompt: `01bdb4874f46968087cb4c357d0f73239f03cf42` (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), in the Claude Code harness
- Date: 2026-11-12
- Input: language Java; runtime chosen: the JVM (HotSpot, Eclipse Temurin); minimum version chosen: Java 17
  (`--release 17`); package manager chosen: Maven
- Toolchain (pinned in `sdks/java/mise.toml`, as the version commands print them):
  - `java = "temurin-25.0.5+8.0.LTS"`: `openjdk version "25.0.5" 2026-10-20 LTS`, `Temurin-25.0.5+8`
  - `maven = "3.10.0"`: `Apache Maven 3.10.0 (c43a36b8d67be7e0805a411bc0898af1a51f5472)`
  - Maven plugins (pinned in `pom.xml`): maven-compiler-plugin 3.16.0, maven-surefire-plugin 3.6.1,
    maven-checkstyle-plugin 3.6.0 with Checkstyle 14.4.0, spotless-maven-plugin 3.11.0 with google-java-format
    2.0.0, build-helper-maven-plugin 3.8.0; test dependency junit-jupiter 6.1.3. There is no runtime dependency.

## Results

- Vectors: 1371 passed of 1371 (`bin/test` prints `native: vectors 1371 pins 17`, equal to the phase 2 count
  command's `evaluation: vectors 1371 pins 17`); bucket pins: 17 passed of 17.
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
  (`bun run conformance -- --service sdks/java/bin/test-service`, exit 0; the two skips are the named
  `buckets.json` vectors).
- Capabilities advertised: `integer-accessor`.
- Native tests: 50 passed of 50 (surefire; 50 top-level `@Test` methods: ClientTest 22, SeamTest 8,
  ConcurrencyTest 8, JsonTest 8, BucketTest 3, VectorsTest 1), no subtests or table cases: `VectorsTest` is one
  test that walks every vector and reports the failures together, which is why the vector count is printed by
  `bin/test` separately. The test service (`test-service/src`) has no tests of its own; the harness is its test.
- Repeat runs of `bin/test`: runs 1 to 10 each passed (`native: vectors 1371 pins 17`, surefire `tests=50 failures=0 errors=0 skipped=0`), and the loaded eleventh run passed with 12 busy loops (`nproc` = 12). One earlier attempt failed on run 9 and was restarted after a fix (Decisions 20). Log:

  ```
  run 1: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 2: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 3: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 4: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 5: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 6: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 7: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 8: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 9: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 10: pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  run 11 (loaded, 12 busy loops): pass; native: vectors 1371 pins 17; tests=50 failures=0 errors=0 skipped=0
  ```

## Decisions

Choices the prompt left open on purpose, and points a document settles.

1. **Runtime and version.** The JVM (Temurin), pinned to the newest LTS line (25.0.5, found with
   `mise ls-remote java` and the Adoptium support page), as `language-notes/java.md` says; minimum Java 17,
   compiled with `--release 17` so the build cannot call a newer API. 17 is the oldest LTS that has records,
   pattern matching for `instanceof` and text blocks, which keep the code short.
2. **Build tool: Maven over Gradle.** The mainstream choice for a library other builds depend on; mise installs
   it; one `pom.xml` holds compiler flags (`-Xlint:all -Werror`), tests, lint and the test source root.
3. **Test framework: JUnit Jupiter 6.1.3** through surefire; the mainstream one. The loopback fake server is the
   JDK's own `com.sun.net.httpserver`, so no test dependency beyond JUnit.
4. **Lint and format: javac `-Xlint:all -Werror`, Checkstyle (via the Maven plugin) and Spotless with
   google-java-format.** Versions found on Maven Central metadata. The Maven plugin was chosen over the bare
   Checkstyle JAR that `language-notes/java.md` mentions (mise installs it with no launcher), so `bin/lint` is
   one `mvn` call and needs no extra mise tool. The rule set is `checkstyle.xml`, with the reason beside every
   rule left out (`IllegalCatch` is the notable one: the SDK catches `Throwable` at its edges on purpose,
   contract 5.1).
5. **google-java-format 2.0.0, not the newest.** GJF 1.37.0 (the newest when the pom was written) crashes on JDK
   25 inside its use of javac internals; 2.0.0 runs. See proposed change 1.
6. **Coordinates:** group `com.github.madangryscientist.toggleslash`, artifact `toggleslash-sdk-java`, root
   package `com.github.madangryscientist.toggleslash.sdks.java`, from `language-notes/java.md`. Nothing is
   registered under it.
7. **Accessor shape: both.** Typed `bool/string/int/double` accessors plus a generic `jsonVariation`, each with a
   `...Detail` twin (contract 4, last paragraph of "Typed languages"). `integer-accessor` is advertised;
   `intVariation` takes and returns a `long`.
8. **`2^63 - 1` as an int is a `TYPE_MISMATCH`.** Served numbers are binary64, so `2^63 - 1` reads as `2^63`,
   out of range. The contract says so in the same paragraph (`sdk-contract.md`, "Typed languages": "the literal
   `2^63 - 1` reads as `2^63`, out of range"); `ClientTest.accessorsApplyTheKindRules` holds it.
9. **Null fallback.** `jsonVariation` with a null (or non-JSON) fallback accepts any served value
   (`sdk-contract.md` "The mismatch rule": "A null fallback accepts any served value"). `stringVariation` with a
   null `String` fallback still needs a served string or null, because the method's return type is `String`; the
   contract line speaks of kinds and the typed accessor fixes the kind.
10. **One `HttpClient`, not two** (departure from `language-notes/java.md`, "No closable `HttpClient` before 21").
    The note says to use a client for the ruleset and stream that `close` aborts, and one for usage that it
    leaves alone. The SDK keeps one client and tracks the in-flight ruleset and stream futures in a set
    (`Transport.abortAll()` cancels those and closes the stream's input); the events POST is not tracked, so the
    final flush after `abortAll` still goes out. Same behaviour, half the thread and connection pools.
11. **`requestTimeout` bounds the whole request, body included.** `HttpRequest.timeout` ends at the headers, so
    every request is `sendAsync(...).get(requestTimeout)` with a body handler that collects the body
    (`BodyHandlers.ofByteArray`); `SeamTest` and `ClientTest.aBodyThatNeverEndsIsATimeoutToo` hold it. The stream
    uses `BodyHandlers.ofInputStream()` (its future completes at the headers, so it does not repeat the 23-failure
    mistake the language note describes with `fromSubscriber`).
12. **`close` on an SDK thread** (inside `onError`, on the sync or flush thread) runs the drain on a separate
    daemon thread `toggleslash-close` and returns at once (contract 6.4, `sdk-contract.md` line 170: "A `close`
    called on the SDK's own thread ... SHOULD NOT wait for the work that thread is doing").
    `ConcurrencyTest.callbacksMayCallBackIntoTheClientWithoutDeadlock` holds it.
13. **A hand-written WHATWG UTF-8 decoder** (`evaluation/Utf8Decoder`, used by the JSON reader, the ruleset body and the
    SSE parser) and `Bucket.utf8` instead of `CharsetDecoder`/`String.getBytes`. The JDK's decoder with `REPLACE`
    does not give one U+FFFD per maximal invalid subpart in every case, and its encoder turns a lone surrogate into
    `?`, where the spec needs the WHATWG result. This departs from `DESIGN.md`, which said `CharsetDecoder`.
14. **Numbers are `double`.** Contexts and rulesets read every number as binary64 (`Double.parseDouble` on a
    grammar-checked token), as the TypeScript SDK does; `Json.MAX_DEPTH` is 1000 (contract minimum 512).
15. **Evaluator limits are the spec's.** `PatternParser`: length 1024, size 10000, repeat count 100;
    `Evaluator.MAX_PREREQUISITE_DEPTH` 32: all named in `evaluation.md` ("Patterns", "Prerequisites"). The
    contract's "Bounds" bullets (7.2) were re-read against `SseParser` and `Transport`: there is no line, event
    or body size cap anywhere, the only time bounds are `requestTimeout`, the 60 s watchdog and the 300 s
    Retry-After cap. `ToggleSlashClient.close` waits on a second closer for `4 x requestTimeout + 5 s` at most;
    that is a guard against a hung first closer, not a limit on a protocol input, and `close` never raises.
16. **CI toolchain step: `jdx/mise-action` with `working_directory: sdks/java` and `install_args: java maven`**,
    not `actions/setup-java`, because `bin/lint`, `bin/test` and `bin/test-service` call Java and Maven through
    `mise exec`, so mise has to be on PATH and one file (`mise.toml`) names the versions. The same step and SHA
    (`b44d12e7d0d8ee7461bb893e1b1993500433185b`, v5.2.7) appear in the committed `.github/workflows/sdk-go.yml:41`
    and the other `sdk-*.yml` files; the checkout and setup-bun lines and `BUN_VERSION` are copied from `ci.yml`.
17. **Test service layout:** `build-helper-maven-plugin` adds `test-service/src` as a test source root and
    `bin/test-service` runs `java -cp target/classes:target/test-classes` (`language-notes/java.md`, "Test
    service layout"). Maven's first run needs the network; later runs use `-o`.
18. **Departures from `DESIGN.md`** (written before the code, left as written): `SseParser` uses `Utf8Decoder`, not a
    JDK `CharsetDecoder`; one `HttpClient` (item 10); the clock is `Clock` with `await(Sleeper, long)` and the test
    seams are package-private methods of `ClientOptions` (`clock`, `jitter`, `watchdog`, `sslContext`) rather than a
    `ClientConfig` class; the evaluator package is `Model`, `Ruleset`, `Evaluator`, `Clauses`, `Contexts`,
    `PatternParser`/`PatternProgram`, `Semver`, `Instants`, `Bucket`, `Json`, `Utf8Decoder` (not `Flag`, `Segment`,
    `Pattern`, `Dates`).
19. **Test seams** are package-private options on `ClientOptions` that only the package's tests set, never a global
    or an environment variable (`CHECKLIST.md`, "Code and documents"). The TLS test uses a certificate made by
    `keytool` at test time with the throwaway keystore password `test-only`, a constant in `SeamTest`, not a secret.
20. **A test that raced the SDK, found and fixed.** The first ten-run attempt failed on run 9: `SeamTest.
    aStreamQuietForTheWatchdogWindowIsDroppedAsTimeout` waited for `lastError == timeout` and then read
    `status().stream()` in a second call, by which time the SDK could already be reconnecting (`expected:
    DISCONNECTED but was: CONNECTED`). The test asserted a state it did not control. It now checks both fields in
    one `status()` snapshot inside the wait. `ClientTest.aNonOkStreamIsHttpStatusAndAPollFollows` had the same
    shape and got the same fix. The ten runs then started again from the first, as the checklist says.
21. **Search evidence for privacy.** `grep -rniE "Logger|System\.(out|err)|printStackTrace|java\.util\.logging|slf4j"
    src/main` finds nothing: the SDK writes no log line. Error values are `ToggleSlashException` with a fixed code
    (`http_<status>`, `timeout`, `bad_ruleset`, `network`) or the JDK exception of the request, never built from a
    context. The only `.header(` calls in `src/main` are in `Transport.java` (lines 117, 130, 143, 176):
    `If-None-Match`, `Content-Type`, `Accept`, `Authorization`.
22. **No platform regular expression in the evaluator.** `grep -rnE "java\.util\.regex|Pattern\.compile|Matcher|\.replaceAll|\.split\(" src/main`
    finds only the word in a comment in `PatternProgram.java:17`; the `.matches(` hits are the SDK's own
    `PatternProgram.matches` and `Clauses.matches`. The matcher is a Pike VM.
23. **README and mapping table.** `README.md` links `DESIGN.md` for the mapping table (`CHECKLIST.md` allows
    "has the mapping table or links to it"); the table has every row of contract 2.

24. **`flagVersion` for a flag with a bad segment.** `sdk-contract.md` section 10 says `flagVersion` is "the flag's
    `version` in that ruleset, or none when the ruleset has no such flag or the flag is malformed". The TypeScript SDK
    settles which flags count: `packages/sdk/src/client.ts:122-126` (`versionOf`) reports the version whenever the
    flag validated far enough to be `ready`, and a missing segment is an evaluation-time `ERROR`, not a validation
    failure. The Java client does the same: the version is reported for any flag whose record parsed.

### Request headers the client adds (captured on a loopback listener)

`SeamTest.eachRouteSendsOnlyTheHeadersThePrivacyRuleAllows` records every header name of one request per route and
writes `target/headers.txt` (run with `bin/test`). The HTTP client is `java.net.http.HttpClient`, forced to HTTP/1.1
(the default `h2c` upgrade would add `Upgrade` and `HTTP2-Settings`).

| Route | Headers received | Set by the SDK | Added by the JDK client, and why it does not identify a user or a machine |
| --- | --- | --- | --- |
| `GET /sdk/v1/ruleset` (first) | `authorization host user-agent` | `Authorization` | `Host` (from the URL); `User-Agent` is the JDK's fixed `Java-http-client/<java version>`: library and runtime version only, no host or user name |
| `GET /sdk/v1/ruleset` (conditional) | `authorization host if-none-match user-agent` | `Authorization`, `If-None-Match` | the same `Host` and `User-Agent` |
| `GET /sdk/v1/stream` | `accept authorization host user-agent` | `Accept`, `Authorization` | the same |
| `POST /sdk/v1/events` | `authorization content-length content-type host user-agent` | `Authorization`, `Content-Type` | `Content-Length` (the body's size), plus the same `Host` and `User-Agent` |

## Guesses

1. **Does writing `RUN.md` after the ten runs start them again?** Where: `CHECKLIST.md`, "The two executable
   checks": "Any change to the tree after a run (a fix, a new test, a comment) also starts them again: the ten are
   of the tree that is committed." `RUN.md` is part of the committed tree but nothing the tests read, and it must
   contain the counts of the runs, so it cannot exist before them. Assumed: a change to `RUN.md` alone does not
   restart the runs; any other file's change did (decision 20 did exactly that).
2. **What counts as one "native test" for the Results line.** Where: `RUN-TEMPLATE.md`, Results: "Native tests: <n>
   passed of <n> (top-level tests in every package of the SDK, its test service included; give the subtest or table-
   case count beside it)". Java has no test-service tests, and `VectorsTest` runs ~1400 vectors inside one `@Test`.
   Assumed: count surefire's tests (50) and say the vectors are reported separately by `bin/test`.

## Stuck points

None.

## Proposed prompt changes

1. For decision 5, in `language-notes/java.md`, add: "**Spotless needs google-java-format 2.0.0 or later on JDK
   25.** 1.37.0 fails on the pinned JDK (it reaches into javac internals); set `<googleJavaFormat><version>` to 2.0.0
   explicitly."
2. For decision 13, in `language-notes/java.md`, add: "**Do not rely on the JDK's UTF-8 codec for the spec.** A
   `CharsetDecoder` with `REPLACE` and `String.getBytes(UTF_8)` differ from the WHATWG rules the contract names
   (one U+FFFD per maximal invalid subpart; a lone surrogate must not become `?`). Write the 40-line decoder and
   encoder and test them against the examples in the Encoding standard."
3. For decision 10, in `language-notes/java.md`, replace the two-client advice with: "One client is enough: track
   the in-flight ruleset and stream futures, cancel them in `close`, and do not track the events POST. A closable
   client is only needed if you cannot track the futures."
4. For decision 11, in `language-notes/java.md`, add: "`HttpRequest.timeout` is not a request timeout. Bound a
   request with `sendAsync(...).get(requestTimeout)`; cancelling that future closes the connection."
5. For decision 16, in `PROMPT.md` phase 5, after "The toolchain step", add: "A language whose `bin/` scripts call
   its tools through `mise exec` uses `jdx/mise-action` with `working_directory: sdks/<lang>` and `install_args`
   naming the tools; copy the SHA from a committed `sdk-*.yml`." (The skeleton leaves this step open, and nothing
   else in the prompt says whether mise or a setup action is expected.)
6. For decision 20, in `CHECKLIST.md`, "The two executable checks", after "a timer test that races the SDK's
   loops fails only under load", add: "The usual form is a test that waits for one field of `status()` and then reads
   another in a second call: read both from one snapshot."
7. For guess 1, in `CHECKLIST.md`, "The two executable checks", add: "`RUN.md` itself is exempt: it holds the counts."

8. For guess 2, in `RUN-TEMPLATE.md`, Results, after "give the subtest or table-case count beside it)", add: "A test that loops over data inside one test method counts once; say how many cases it runs."

## Checklist

Evidence is a test (`ClientTest`, `SeamTest`, `ConcurrencyTest`, `JsonTest`, `BucketTest`, `VectorsTest`, all in
`src/test/java/com/github/madangryscientist/toggleslash/sdks/java/`), a command, or a file. "Harness" means
`bun run conformance -- --service sdks/java/bin/test-service`, run on the final tree: exit 0, summary line in Results.

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equal to phase 2's: `VectorsTest` reads
  `fixtures/evaluation/*.json` at test time; `bin/test` prints `native: vectors 1371 pins 17`, the phase 2 command printed `evaluation: vectors 1371 pins 17`.
- [x] `bin/test` ten times in a row plus a loaded eleventh: runs 1 to 10 passed, each `native: vectors 1371 pins 17`,
  surefire `tests=50 failures=0 errors=0 skipped=0`; run 11 passed with 12 busy shell loops (`nproc` = 12) started
  before it and killed after it. The first attempt failed on run 9 (decision 20), was fixed, and the ten started again.
  The log is in Results.
- [x] The harness exits 0 with only the 2 named vector skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`; `integer-accessor` is advertised, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes: harness `privacy.json`, `kinds.json` vectors. The context is a `Map` (single kind has `key`; multi has `kind` keys).
- [x] Result order and mismatch rule: harness `evaluation.json`, `usage.json`; native `ClientTest.accessorsApplyTheKindRules`.
- [x] `integer-accessor`: fraction is `TYPE_MISMATCH`, `2.0` is `2`: harness `evaluation.json`; native `ClientTest.accessorsApplyTheKindRules`.
- [x] No failure raises: harness (every scenario; the service answers 500 on a raise); native `ClientTest.aRaiseInTheBuildIsEvaluationFailedAndOneOnErrorCall`, `ConcurrencyTest`.
- [x] `NOT_READY` at once before the first load: harness `load.json`; native `ClientTest.notReadyUntilTheFirstRulesetThenServes`.
- [x] `ready` settles, bounded, repeatable, false after `close` if unsettled: harness `load.json`, `close.json`; native `ClientTest.aRulesetThatNeverAnswersIsATimeoutAndReadyIsBounded`, `closeBeforeTheFirstLoadSettlesReadyFalse`.
- [x] A non-ruleset answer never replaces the held one: harness `status.json`; native `ClientTest.aBadRulesetKeepsTheLastGoodOne`.
- [x] Ruleset request, `If-None-Match`, ETag exact, URL joining: harness `load.json`, `status.json`; native `ClientTest.aPollSendsIfNoneMatchOnlyOnceBothAreHeldAndA304IsASync`.
- [x] The stream: harness `stream.json`, `load.json`, `status.json`; native `ClientTest.aStreamChangeIsFetchedAndTheSameTagIsNot`, `aStreamThatEndsCleanLeavesLastErrorAloneAndIsRetried`, `aNonOkStreamIsHttpStatusAndAPollFollows`.
- [x] The loop and jittered backoff: harness `backoff.json`, `stream.json`. `ToggleSlashClient.delayNanos` (`Math.min(poll, 1 s << min(failures,16))` with the injected jitter).
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; native `SeamTest.aRetryAfterAbove300Holds...` (three tests).
- [x] Status fields, states, `lastError` codes: harness `status.json` and its shape check.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`; native `ClientTest.usageGoesOutOnCloseAsExactlyTheFourFields`, `moreThanFiveHundredEntriesGoAsSeveralRequests`, `aFailedFlushKeepsTheCountsAndTheNextOneSendsThem`, `aClientErrorDropsTheBatchAndGoesOn`, `nothingPendingSendsNoRequest`.
- [x] Hook events and `onError`, raising hook and raising `onError`: harness `hooks.json`; native `ClientTest.hooksSeeEveryCallAndARaisingHookChangesNothing`, `aThrowingOnErrorIsIgnored`.
- [x] `close` in order, twice at once, during a hold, final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`; native `ClientTest.closeIsSafeTwiceAndEvaluationStillAnswers`, `aHeldRetryAfterMeansCloseLosesTheCountsAndSaysSo`, `ConcurrencyTest.closeRacingEvaluationAndTwoClosesRacingFinishWithinTheBound`.
- [x] No context, key or attribute in a request, `onError` message or log, and no extra header: harness `privacy.json` and its per-scenario checks; header table in Decisions; source search for logs in Decisions 21 (`grep -rniE "Logger|System\.(out|err)|printStackTrace|java\.util\.logging|slf4j" src/main` found nothing).

### Native tests

- [x] Every operation from many threads, `close` racing `variation`, two `close` calls racing: `ConcurrencyTest.manyThreadsOverAllOperationsWhileTheRulesetSwapsKeepEveryEventPaired` (16 threads over the accessors, detail, `status`, with the ruleset swapping every 2 ms; waits for 60 s at most) and `closeRacingEvaluationAndTwoClosesRacingFinishWithinTheBound` (8 workers, 2 racing closes, a 3 s bound). The JVM has no race detector; these stress tests (many threads, every operation, `close` racing, a bound on the wait) stand in, as the line allows.
- [x] SHOULD: `https` through a local certificate: `SeamTest.anHttpsUrlReachesTheTlsPathWithALocalCertificate` (a `keytool`-made certificate; also proves the default trust store refuses it with `network`).
- [x] `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs: `ConcurrencyTest.evaluationDoesNotWaitForAHungRulesetRequest`, `evaluationDoesNotWaitForAHungUsageFlushAndCloseIsBounded` (2000 evaluations in under 2 s; close bounded).
- [x] One ruleset or the other across a swap: `ConcurrencyTest.manyThreadsOverAllOperationsWhileTheRulesetSwapsKeepEveryEventPaired` checks every event's `etag` equals `"v" + flagVersion` (0 unpaired).
- [x] SHOULD: callbacks that call back do not deadlock, `ready` on the SDK's own thread returns without waiting: `ConcurrencyTest.callbacksMayCallBackIntoTheClientWithoutDeadlock` (`status`, `ready`, `variation`, `close` twice from `onError`) and `aHookMayReadStatusAndAskReadyOnTheCallersThread`.
- [x] A stream with no bytes for 60 s is `timeout`, a keep-alive restarts it, through a seam and a real loopback socket: `SeamTest.aStreamQuietForTheWatchdogWindowIsDroppedAsTimeout` (seam `watchdog(400 ms)`, a socket that goes quiet) and `aKeepAliveRestartsTheWatchdog`; `aStreamOpenThatNeverSendsHeadersIsATimeout` for the open bound.
- [x] `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes: `SeamTest.aRetryAfterAbove300HoldsTheRulesetRouteFor300Seconds`, `...TheStreamRouteFor300Seconds`, `...TheEventsRouteSeparately` (a fake clock; 30 flush waits of 10 s before the second request).
- [x] Counts either side of a UTC midnight are two days: `ClientTest.countsEitherSideOfUtcMidnightGoOutAsTwoDays`.
- [x] An evaluation that raises: fallback, `ERROR EVALUATION_FAILED`, one `onError`: `ClientTest.aRaiseInTheBuildIsEvaluationFailedAndOneOnErrorCall`.
- [x] Hooks that may return a future: n/a: the hook is a `Consumer<EvaluationEvent>`, a plain function with no futures.
- [x] `integer-accessor`, a served number outside signed 64 bits is `TYPE_MISMATCH`: `ClientTest.accessorsApplyTheKindRules` (`intVariation` of the served `9223372036854775807`, which reads as `2^63`, is the fallback with `TYPE_MISMATCH`, and `-9223372036854775808` is accepted) and the harness `huge` scenario in `evaluation.json`.
- [x] SHOULD: a client never closed does not keep the process alive: `ConcurrencyTest.aProcessWithAClientThatWasNeverClosedStillExits` (a child JVM creates a streaming client and returns from `main`; it exits 0 within 20 s) and `theClientsThreadsAreDaemons`; the threads are made in `ToggleSlashClient.create` with `setDaemon(true)`.

### Code and documents

- [x] `DESIGN.md` has every phase 1 row; `README.md` links it for the mapping table (`README.md`, intro) and lists the accessors. `DESIGN.md` section 1 is the table.
- [x] Names, defaults, reason kinds, error codes, stream states, `lastError` codes keep their spelling: `ReasonKind`, `ErrorCode`, `StreamState` constants are the canonical names in upper case (the language's constant style); the wire form is `wire()`. `DESIGN.md` section 1.
- [x] No platform regular expression in the evaluator: Decisions 22.
- [x] The lint configuration names its rule set and each ignored rule has a reason: `checkstyle.xml` (the "Left out on purpose" comment), `pom.xml` compiler flags `-Xlint:all -Werror`.
- [x] Every runtime dependency has a `DESIGN.md` line: there is none (`DESIGN.md` section 4, "Runtime: none"); `pom.xml` lists only `junit-jupiter` at test scope.
- [x] No telemetry or identifying header: Decisions 21 and the header table (`SeamTest.eachRouteSendsOnlyTheHeadersThePrivacyRuleAllows`); `Transport.java` sets only `If-None-Match`, `Content-Type`, `Accept`, `Authorization`. `User-Agent` is the JDK's `Java-http-client/<version>`.
- [x] Every extra beyond the contract is one contract 13 allows: the extras are the `intVariation`/`...Detail` twins and typed accessors (contract 4), the `Duration` options, and package-private test seams; none changes contract 5 or sends anything new.
- [x] Test seams are options on `create` (`ClientOptions` package-private `clock`, `jitter`, `watchdog`, `sslContext`) and never a global or an environment variable: `grep -rn "System.getenv\|System.getProperty" src/main` finds nothing.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable (`ls -l bin`), `cd` to the SDK and run through `mise exec` with only what `mise.toml` pins.
- [x] `test-service/README.md` maps every accessor to an SDK method (table).
- [x] The workflow follows the skeleton, filters on its paths and pins every action by SHA: `.github/workflows/sdk-java.yml`; `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.

### Repository

- [x] No dash: `bun scripts/check-dashes.ts` prints nothing and exits 0.
- [x] `bun run format:check` passes (`All matched files use the correct format.`).
- [x] Only `sdks/java/` and the one workflow changed: the phase 6 scope check prints `scope ok`.
- [x] No package published, no name reserved: nothing was pushed to a registry; the build is `mvn package` to `target/` only.
- [x] Commit subject `chore(sdk-java): the Java server SDK regenerated cold from the authoring prompt`; no `Co-Authored-By`, no AI attribution line.
- [x] The workflow job: `runs-on: ubuntu-latest`, no `if:`, triggers `pull_request` and `push` only, installs its own toolchain (`jdx/mise-action`); `check-workflows.ts` passes.
- [x] `RUN.md` follows the template, with evidence for every line of this list.

