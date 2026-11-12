# Java SDK: run report

## Prompt version

- Prompt: 546ffdb2436494c139f3435bec1b2d807f3b7614 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-12
- Input: language Java; runtime chosen: Temurin 25 LTS (the newest LTS on the run date, from `mise ls-remote java`); minimum version chosen: Java 17 (`maven.compiler.release` 17); package manager chosen: Maven (Maven Central coordinates `com.github.madangryscientist.toggleslash:toggleslash-sdk-java`, nothing published)
- Toolchain (as `mise.toml` pins them and as their version commands print):
  - java `temurin-25.0.5+8.0.LTS`; `java -version` prints `openjdk version "25.0.5" 2026-10-20 LTS`, `Temurin-25.0.5+8`
  - maven `3.10.0`; `mvn -v` prints `Apache Maven 3.10.0`
  - checkstyle `14.4.0`; the JAR prints `Checkstyle version: 14.4.0`
  - google-java-format `2.0.0`; prints `google-java-format: Version 2.0.0`
  - pinned in `pom.xml`: junit-jupiter 6.1.3 (`junit.version`), maven-resources-plugin 3.5.0, maven-compiler-plugin 3.16.0, build-helper-maven-plugin 3.8.0, maven-surefire-plugin 3.6.1, maven-jar-plugin 3.5.1

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (run twice on the final tree, both identical)
- Capabilities advertised: `integer-accessor`
- Native tests: 45 passed of 45 (surefire: `Tests run: 45, Failures: 0, Errors: 0, Skipped: 0`). `VectorTest` is one test that loops over 1371 vectors and 17 pins (1388 cases); the rest are ordinary tests: ClientTest 18, SyncTest 11, JsonTest 9, SourceTest 3, TlsTest 2, StressTest 1. The test service has no tests of its own.
- `bin/test` ten times in a row on the committed tree: 10 of 10 passed, every run `Tests run: 45, Failures: 0` and `vectors: 1371 pins: 17`. The eleventh run, under load (12 busy shell loops for `nproc` = 12, started before `bin/test` and running until it ended, build included): passed, 45 of 45, 20.1 s against about 16 s unloaded.

## Decisions

1. Versions: `mise ls-remote java` and Maven Central's metadata gave the newest versions on the run date; LTS 25 is pinned and 17 is the minimum, as `language-notes/java.md` says. The 17 floor gives records, pattern `instanceof` and `java.net.http`; nothing newer is used.
2. CI uses `jdx/mise-action` with `install_args: java maven checkstyle google-java-format`, copied from `sdk-go.yml`'s skeleton with the SHAs that `ci.yml` already pins (the language note's trap about a bare JAR checkstyle needs mise to install it).
3. Lint: google-java-format (`--set-exit-if-changed --dry-run`) over `src/main/java`, `src/test/java` and `test-service/src`, plus Checkstyle 14.4.0 over `src/main/java` only. Checkstyle on tests would force Javadoc on every test method, which says nothing. Rules left out are named with a reason in the header comment of `checkstyle.xml` (MagicNumber, IllegalCatch, FinalParameters, DesignForExtension, JavadocVariable on private fields, Javadoc on private methods). No suppressions file.
4. HTTP: the JDK `java.net.http.HttpClient`, forced to HTTP/1.1 (no `Upgrade: h2c` on cleartext) and with redirects off, so a `3xx` is `http_<status>`. Contract 7 leaves redirects open; this keeps the key from following to another host.
5. Whole-request bound: `HttpRequest.timeout` ends at the headers, so every non-stream request is `sendAsync` and waited on with a deadline measured from just before the call; the stream open is bounded the same way and the 60 s watchdog is `queue.poll(watchdog)` on a hand-written `BodySubscriber` (`StreamBody`), because `BodyHandlers.fromSubscriber` answers only at the end of the body (language note).
6. Hand-written JSON, UTF-8 (WHATWG), SSE parser and Pike VM pattern matcher, no runtime dependency (DESIGN.md sections 3 to 5): the JDK's regex backtracks, its `CharsetDecoder` differs from WHATWG on invalid input, and no JSON parser in the JDK exists. JSON depth limit 1000 (contract needs at least 512).
7. No thread interrupts. Sleeps go through `Wake` (a flag and a monitor), a blocked request is ended by cancelling its future. The JDK before 21 has no closable `HttpClient`, so there is nothing else that stops an in-flight request.
8. `close` or `ready` on an SDK thread: contract 6, item 4 settles it (a `ready` returns at once with the state held; a `close` stops the rest and returns). `ready` answers the settled answer or `false`; `close` skips joining its own thread and does not wait on the shutdown latch.
9. `close` bound: both SDK threads are joined for at most `requestTimeout` each, then the final flush runs, each request bounded by `requestTimeout` (DESIGN.md section 2). A `close` made on an SDK thread skips its own join.
10. State: `Sync` holds one immutable `View(ruleset, etag, lastSyncAt, stream, lastError)` in a `volatile` field; `variation` and `status` read it once, so the ETag always names the ruleset (contract 6.3).
11. Seams are package-private setters on `Options.Builder` (`clock`, `jitter`, `streamIdleTimeout`, `evaluation`, `sslContext`) and package-private reads on `Client` (`rulesetHoldNanos`, `usageHoldNanos`, `pendingUsage`). No global, no environment variable (`sdks/go`'s `cfg.idle` is the model the checklist names).
12. `StreamState` constants are upper case (`OFF`, `CONNECTING`, ...) because Java style forbids a lower-case constant; `value()` and `toString()` answer the contract's lower-case spelling. `lastError` stays a `String` because `http_<status>` is open-ended; the other three are `Status.TIMEOUT`, `Status.BAD_RULESET`, `Status.NETWORK`.
13. `Reason` is a record `(kind, ruleIndex, flagKey, code)` with `ReasonKind` and `ErrorCode` enums. These three live in the root package, as public API, and the `evaluation` package imports them: a one-way package cycle, kept over duplicating the types (DESIGN.md section 7).
14. Accessors: one generic `variation`/`variationDetail` plus `boolVariation`, `stringVariation`, `numberVariation`, `intVariation` (`long`) and `jsonVariation`, each with a `...Detail` twin; `intVariation` answers `TYPE_MISMATCH` for a fraction or a value outside int64 and counts `2.0` as `2` (contract 4, "Typed languages").
15. A context is an `Object`: a `Map` of the JSON shape. A `Context` helper builds one for callers (contract 13).
16. Race detector: Java has none. `StressTest.everyOperationFromManyThreadsWhileTheRulesetChangesAndCloseRaces` stands in: twelve threads call every operation while the ruleset changes and two threads race `close`; it fails on an exception, a torn etag/flagVersion pair, a hang (`@Timeout(120)`) or a lost count. It covers the two-close race too.
17. Source search (`SourceTest`): `noRegexEngineAnywhereInTheSdk` (searches `src/main` for `import java.util.regex`, `java.util.regex.`, `.replaceAll(`, `.replaceFirst(`, `text.split(` and `.split("`; the evaluator's own `Pattern` class is the Pike VM, not the platform's), `noLoggingAndNoPrintingAnywhereInTheSdk` (searches for `java.util.logging`, `System.Logger`, `org.slf4j`, `System.out`, `System.err`, `printStackTrace`), `theEvaluatorImportsNothingFromTheNetworkLayer`.
18. Request headers (source search: `grep -rn '\.header(' src/main`): only `Http.java:82` `Authorization`, `Http.java:92` `If-None-Match`, `Http.java:119` `Accept`, `Http.java:146` `Content-Type`. Table of what a loopback listener saw, printed by `SyncTest.everyRouteSendsTheHeadersTheProtocolNames` (header names, lower-cased; the test asserts authorization, if-none-match, accept and content-type, and prints the full key set of each request):

    | Route | Headers on the wire |
    | --- | --- |
    | `GET /sdk/v1/ruleset` (first) | `authorization`, `host`, `user-agent` |
    | `GET /sdk/v1/ruleset` (conditional) | `authorization`, `host`, `if-none-match`, `user-agent` |
    | `GET /sdk/v1/stream` | `accept` (`text/event-stream`), `authorization`, `host`, `user-agent` |
    | `POST /sdk/v1/events` | `authorization`, `content-length`, `content-type` (`application/json`), `host`, `user-agent` |

    Added by the JDK client: `Host` (the URL's authority, which the server already has), `User-Agent` (the JDK sends `Java-http-client/<JDK version>`: names the library and runtime, not a user or a machine), `Content-Length` (body size). The JDK sends no `Accept-Encoding` and no `Connection` on HTTP/1.1 here. `Authorization` is `Bearer <key>` on all four.
19. Retry-After: read on a `429` only, decimal digits only after trimming, capped at 300 s, held as a monotonic deadline that only lengthens, one hold shared by ruleset and stream and a second one for usage (contract 7.4, 9).
20. Usage: at most 500 entries per request, serial; 5xx, 408, 429, network error and timeout keep the counts, any other status drops the batch; a count is added only for the served value and never after `close` has begun.

## Guesses

1. `flagVersion` for a flag that is well formed but whose segment reference is broken. Where: `docs/sdk-contract.md` section 10, the `flagVersion` row. Closest sentence: "the flag's `version` in that ruleset, or none when the ruleset has no such flag or the flag is malformed". It does not say whether a flag that fails only at evaluation (a missing segment) is "malformed". Assumed: it is not; the version is reported.
2. The type of a hook event's `value` for the integer accessor. Where: `docs/sdk-contract.md` section 10. Closest sentence: the event's value is what the caller got; it does not say in which host type. Assumed: a `Long` for `intVariation`, a `Double` for `numberVariation`, the served JSON value for the generic method.
3. A generic `variation` whose fallback is a non-`Double` `Number` (an `Integer`, a `Long`). Where: `docs/sdk-contract.md` section 4, the mismatch rule. Closest sentence: "number" is one kind ("both \"number\""); it does not say what a language with several numeric classes returns for `T`. Assumed: the served number is converted to the fallback's class when it is whole and in range, and is a `TYPE_MISMATCH` otherwise.
4. A typed accessor with a `null` fallback. Where: `docs/sdk-contract.md` section 4. Closest sentence: "A null fallback accepts any served value". A primitive accessor (`boolVariation`) cannot take null. Assumed: `stringVariation` and `jsonVariation` accept a null fallback and then accept any served kind that fits; the primitive accessors take the primitive.
5. Host numeric types other than `Double` as context attribute values (an `Integer`, a `BigDecimal`). Where: `docs/sdk-contract.md` section 3 and `docs/evaluation.md`, "Context". Closest sentence: an attribute number is a JSON number; neither names host types. Assumed: any `java.lang.Number` is read through `doubleValue()`.
6. What the test service does for an accessor whose fallback does not fit the accessor's type. Where: `docs/sdk-test-service.md`, "Accessors". Closest sentence: the section maps accessor names to fallbacks as JSON values and never says a fallback can be wrong. Assumed: the service answers 400 (the harness never sends one).
7. A `create` request that leaves `pollIntervalMs`, `flushIntervalMs` or `requestTimeoutMs` out. Where: `docs/sdk-test-service.md` and `docs/sdk-contract.md` section 2 (line 71 covers only a non-positive duration). Assumed: absent, zero and negative all take the SDK's default.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `docs/sdk-contract.md` section 10, after "or the flag is malformed", add: "A flag that is well formed but fails at evaluation (a missing segment, a prerequisite cycle) still has its version."
2. For guess 2, in `docs/sdk-contract.md` section 10, add: "In a language with distinct integer and float types, an integer accessor's event carries the integer."
3. For guess 3, in `language-notes/java.md`, add: "The JSON parser yields doubles. A generic accessor whose fallback is an `Integer` or `Long` converts a whole served number to that class and answers `TYPE_MISMATCH` for a fraction."
4. For guess 4, in `PROMPT.md` phase 3, after the accessor shape guidance, add: "A typed accessor may accept a null fallback where the language allows it; say so in the mapping table."
5. For guess 5, in `docs/evaluation.md` "Context", add: "A host language's numeric types other than its double are read as the double they convert to."
6. For guess 6, in `docs/sdk-test-service.md` "Accessors", add: "A request whose fallback does not fit the accessor's type is answered 400; the harness never sends one."
7. For guess 7, in `docs/sdk-test-service.md` "Create", add: "A duration field that is absent, zero or negative takes the SDK's default."

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equals the phase 2 count: `VectorTest.everyVectorAndEveryBucketPinPasses` reads `fixtures/evaluation/` at test time; prints `vectors: 1371 pins: 17`; the bun count command prints `1371 17`.
- [x] `bin/test` ten times in a row plus a CPU-loaded eleventh: ten of ten passed (45 tests, `vectors: 1371 pins: 17` each); the eleventh under 12 busy loops (`nproc` = 12) passed, 45 of 45 (see Results). The tree did not change after the runs except for this file.
- [x] The harness exits 0 with only the 2 named vector skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`. The service advertises `integer-accessor`, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes accepted: harness `privacy.json`, `kinds.json` vectors, `Context` helper in `Context.java`.
- [x] Results follow contract 4's order and mismatch rule: harness `evaluation.json`, `usage.json`; native `ClientTest.aWrongKindIsATypeMismatchAndCountsNothing`, `anUnknownFlagIsAnErrorAndCountsNothing`.
- [x] `integer-accessor`: `2.0` is `2`, a fraction is `TYPE_MISMATCH`: harness `evaluation.json`; native `ClientTest.anIntegerAccessorAcceptsTwoPointZeroAndRefusesAFraction`.
- [x] No failure raises: harness, every scenario; `Client` catches `RuntimeException | StackOverflowError` at each boundary (`Client.java:62`, `94`, `109`, `344`, `395`).
- [x] `NOT_READY` at once before the first load: harness `load.json`; native `ClientTest.answersTheFallbackBeforeTheRulesetLoadsAndWhileTheRequestHangs`.
- [x] `ready` settles, bounded, repeatable, false after `close` if unsettled: harness `load.json`, `close.json`; native `ClientTest.readyIsFalseAfterTheRequestTimeoutAndStatusSaysWhy`, `closeBeforeTheFirstLoadFinishesMakesReadyFalse`.
- [x] A non-ruleset answer never replaces the one held: harness `status.json`; native `SyncTest.aBadRulesetKeepsTheLastGoodOneAndSaysBadRuleset`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`; native `SyncTest.aSecondFetchSendsIfNoneMatchAndA304KeepsTheRuleset`.
- [x] The stream, SSE parsing, refresh on change, failed refresh ends the stream: harness `stream.json`, `load.json`, `status.json`; native `SyncTest.aChangedEventFetchesTheNewRulesetAndAConnectedEventWithTheSameTagDoesNot`.
- [x] The loop and jittered backoff: harness `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`.
- [x] Status fields, states and `lastError` codes: harness `status.json` and the shape check.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`; native `SyncTest.aFiveHundredFromEventsKeepsTheCountsAndTheNextFlushSendsThemAgain`, `aFourHundredFromEventsDropsTheBatch`.
- [x] Hook events, `onError`, a raising hook and a raising `onError`: harness `hooks.json`; native `ClientTest.aThrowingHookAndAThrowingOnErrorNeverReachTheCaller`.
- [x] `close` in order, twice at once, during a hold, with a final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`; native `SyncTest.closeSendsTheFinalFlush`, `ClientTest.closeTwiceAndEvaluationAfterCloseAnswerAndCountNothing`.
- [x] No context, key or attribute in any request or `onError` message, no extra header: harness `privacy.json` and the per-scenario checks; header table in Decisions 18; logging search `SourceTest.noLoggingAndNoPrintingAnywhereInTheSdk` (the SDK writes no log line and builds no error value with a context).

### Native tests

- [x] Every operation from many threads, `close` racing `variation`, two `close` calls racing: `StressTest.everyOperationFromManyThreadsWhileTheRulesetChangesAndCloseRaces` (no race detector in Java, so this stands in; Decision 16).
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TlsTest.loadsFollowsAndFlushesOverHttpsWhenTheCertificateIsTrusted` (self-signed cert from `keytool`, trusted through the `sslContext` seam), and `anUntrustedCertificateIsANetworkErrorAndNeverAFallbackToPlainTrust`.
- [x] `variation` and `variationDetail` return while a ruleset request hangs and while a flush hangs: `ClientTest.answersTheFallbackBeforeTheRulesetLoadsAndWhileTheRequestHangs`, `variationReturnsWhileTheFlushHangs`.
- [x] A reader sees one ruleset or the other: `SyncTest.theEtagAndTheFlagVersionOfAnEventAlwaysBelongTogether` (40 distinct ETags, each paired with its flag version) and `StressTest` (a torn pair fails it).
- [x] Hook and `onError` re-entrancy, `ready` from the SDK's thread: `ClientTest.aHookMayCallBackIntoTheClient`, `onErrorOnTheSyncThreadMayCallReadyAndClose`.
- [x] A silent stream is dropped as `timeout`, a keep-alive restarts the 60 s, through a seam on a real loopback socket: `SyncTest.aStreamThatGoesQuietIsDroppedAndOpenedAgain`, `aKeepAliveRestartsTheWatchdog` (`Options.Builder.streamIdleTimeout`, the `FakeServer` keeps a real socket open and writes nothing).
- [x] `Retry-After` above 300 held for 300 s on ruleset, stream and events: `ClientTest.aRetryAfterOfMoreThanThreeHundredSecondsHoldsThreeHundredOnTheRulesetRoute`, `...OnTheStreamRoute`, `...OnTheEventsRoute` (301 s and 86400 s; reads `rulesetHoldNanos`/`usageHoldNanos`); a non-digit value holds nothing: `aRetryAfterThatIsNotDecimalDigitsHoldsNothing`.
- [x] Counts either side of a UTC midnight go out as two days: `SyncTest.aCountIsSplitByTheUtcDayOfTheEvaluation` (a `MovingClock` seam).
- [x] An evaluation that raises gives the fallback, `EVALUATION_FAILED` and one `onError`: `ClientTest.aThrowingEvaluationIsEvaluationFailedWithOneOnError` (the `evaluation` seam).
- [x] Hook returns a future: n/a: `afterEvaluation` is a plain `Consumer<EvaluationEvent>`, it returns nothing.
- [x] `integer-accessor`: a number outside int64 is `TYPE_MISMATCH`: harness `evaluation.json` (`huge`); native `ClientTest.anIntegerAccessorAcceptsTwoPointZeroAndRefusesAFraction` (covers `9223372036854775808`).
- [x] A client never closed does not keep the process alive: `ClientTest.theSdkThreadsAreDaemonThreads`; both threads are made in `Client.java:85` and `86` as `SdkThread`, whose constructor calls `setDaemon(true)` (`Client.java:27`).

### Code and documents

- [x] `DESIGN.md` has the mapping table (section 1) and `README.md` links to it (`README.md`, "The mapping").
- [x] Canonical names, defaults and spellings kept: DESIGN.md section 1; `ReasonKind`, `ErrorCode` constants named as in the spec; `StreamState.value()` answers the lower-case spelling (Decision 12).
- [x] The evaluator uses no platform regex: `SourceTest.noRegexEngineAnywhereInTheSdk`; the pattern matcher is the Pike VM in `evaluation/Pattern.java`.
- [x] The lint configuration names its rules and each ignored rule carries a reason: header comment of `checkstyle.xml`, `bin/lint` (google-java-format with `--set-exit-if-changed`).
- [x] Every runtime dependency has a reason in `DESIGN.md`: there is none (DESIGN.md section 4); the build and test dependencies are listed there too.
- [x] No telemetry and no identifying header: Decision 18, the header table and the four `.header(` calls.
- [x] Every extra beyond the contract is one contract 13 allows: the `Context` helper, the typed accessors with `...Detail` twins, `AutoCloseable` on `Client`. None weakens contract 5 or sends anything new.
- [x] Test seams are options, never a global or an environment variable: Decision 11; `grep -rn getenv src/main` finds nothing.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK and use only the `mise.toml` toolchain: each starts with `cd "$(dirname "$0")/.."` and runs tools through `mise exec --` when `mise` is on PATH.
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method.
- [x] `.github/workflows/sdk-java.yml` follows the skeleton, filters on its paths, pins every action by commit SHA (the same SHAs as `ci.yml`): `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.

### Repository

- [x] No em or en dash: `bun scripts/check-dashes.ts` prints no error.
- [x] `bun run format:check`: `All matched files use the correct format.`
- [x] Only `sdks/java/` and `.github/workflows/sdk-java.yml` changed: the phase 6 scope check prints `scope ok`.
- [x] No package published and no name reserved: nothing was pushed to Maven Central or any registry.
- [x] Commit subject is a Conventional Commit, not `feat`, with no `Co-Authored-By` or AI attribution: `chore(sdk-java): the Java server SDK regenerated cold from the authoring prompt`.
- [x] The workflow job is `runs-on: ubuntu-latest` with no `if:` guard, triggers only `pull_request` and `push`, installs its own toolchain; `check-workflows` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: every heading is present, and the phase 6 heading loop passes.
