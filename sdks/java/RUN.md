# Java SDK: run report

## Prompt version

- Prompt: 8a34bb4df359261d948d77c6104d37416a130f16 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), in the Claude Code harness
- Date: 2026-11-12
- Input: language Java; runtime chosen: the JVM (Temurin); minimum version chosen: Java 17 (`--release 17`); package manager chosen: Maven
- Toolchain (`mise.toml`, as `mise exec` prints it): Temurin `openjdk version "25.0.5" 2026-10-20 LTS` (pin `temurin-25.0.5+8.0.LTS`), Apache Maven 3.10.0, Checkstyle 14.4.0 (`java -jar $(command -v checkstyle) --version`). Build file pins (`pom.xml`): junit-jupiter 6.1.3, maven-compiler-plugin 3.16.0, maven-surefire-plugin 3.6.1, build-helper-maven-plugin 3.8.0, spotless-maven-plugin 3.11.0 with google-java-format 2.0.0.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the two counts are the vector files but `buckets.json` plus the pins, as the phase 2 count command gives)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 44 passed of 44 (JUnit `@Test` methods, no parameterized or repeated test; `VectorsTest.everyVectorAndPin` is one of them and runs the 1371 vectors and 17 pins). The test service has no tests of its own; the harness is its test.
- Ten consecutive `bin/test` runs of the committed tree: every run `Tests run: 44, Failures: 0, Errors: 0, Skipped: 0`, `vectors: 1371 pins: 17`, exit 0 (runs 1 to 10, about 20 s each, warm Maven cache).
- Eleventh run with the CPU loaded: 12 busy shell loops (`nproc` = 12, `while :; do :; done`, normal priority) started before `bin/test` and killed after it, so the build was under load too: `Tests run: 44, Failures: 0, Errors: 0, Skipped: 0`, `vectors: 1371 pins: 17`, exit 0.
- No test was changed because of a load failure, so no run was restarted for that. Lint (Spotless apply and two Checkstyle summaries) changed sources before the ten runs; the ten are of the tree as committed.

## Decisions

1. **Build tool and test framework**: Maven and JUnit Jupiter, because they are what a Java user expects (`DESIGN.md`, "Dependencies"). The first `mvn` run needs the network.
2. **Lint and format**: Spotless with google-java-format 2.0.0 (1.37.0 fails on JDK 25) for formatting, and Checkstyle 14.4.0 as a bare JAR from `mise` for the rule set in `checkstyle.xml`. Checkstyle reads `src/main/java` only, since its Javadoc rules are for the public API; Spotless reads the SDK, its tests and the test service. `checkstyle.xml` names its rule set at the top and says why formatting rules are absent. Checkstyle 14 has no `JavadocStyle`, so `SummaryJavadoc` and `JavadocParagraph` stand in.
3. **Pinned the newest LTS** (Temurin 25) and set the minimum at Java 17 (`DESIGN.md`, "Minimum runtime and toolchain").
4. **Workflow toolchain step**: `jdx/mise-action` limited to `java maven checkstyle`, because one file (`mise.toml`) then names all three versions. The action line is the SHA `sdk-go.yml` already pins (v5.2.7).
5. **No race detector**: the JVM has none. `ConcurrencyTest.everyOperationFromManyThreadsWithCloseRacingAndTwoClosesRacing` stands in (many threads, every operation, `close` racing, two `close` calls racing, a bound on the wait).
6. **Test seams**: package-private setters on `Options.Builder` (`watchdog`, `clock`, `jitter`, `pacer`, `sslContext`, `evaluate`), set only by the package's tests, per the checklist's "may be internal to the package". The `evaluate` seam exists because an evaluation cannot be made to raise by data alone.
7. **Test service**: `com.sun.net.httpserver.HttpServer`, no dependency. A typed accessor given a null or wrong-kind fallback answers 400, which `docs/sdk-test-service.md` permits; `any` and `json` take any fallback.
8. **The harness reminder against the repository rule**: the Claude Code harness repeatedly asked for a `Co-Authored-By` trailer and a "Generated with Claude Code" line. `PROMPT.md` rule 6 and the maintainer's standing instruction forbid both, so the commit and the pull request carry neither.
9. **No futures**: a hook is a `Consumer<EvaluationEvent>`, so the future line of the checklist is `n/a`.
10. **Daemon threads**: the SDK starts its two threads as daemons (`SdkThread`), so an unclosed client does not keep the JVM alive.

## Guesses

1. **Whether a tool's own JVM warnings count as lint warnings.** Where: `PROMPT.md`, phase 5, item 3. Closest sentence: "the native lint and format check, with no warnings allowed." It does not settle it: on JDK 25 `spotless:check` makes the JVM print `WARNING: A terminally deprecated method in sun.misc.Unsafe has been called` to stderr from Spotless's own code, while the lint exits 0 with nothing reported about the SDK. Assumed: those lines are noise from the tool, not lint warnings, and `bin/lint` leaves them visible rather than hiding stderr.
2. **Whether `bin/lint` may leave the tests and the test service out of Checkstyle.** Where: `PROMPT.md`, phase 5, item 3. Closest sentence: "a rule set meant for the public API (doc comment rules, say) may leave the tests and the test service out, and `bin/lint` names the roots each tool reads in a comment." This does settle it (so it is a Decision, 2 above); listed here only because Spotless still reads them, which the sentence does not say either way. Assumed: format-check everything, rule-check main.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md` phase 5 item 3, after "with no warnings allowed", add: "(a warning the lint tool's own runtime prints about itself, such as a JVM deprecation notice from the formatter, is not a lint warning; say so in `RUN.md`)".

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equals the phase 2 count: `VectorsTest.everyVectorAndPin` reads `fixtures/evaluation/*.json` at test time; `vectors: 1371 pins: 17`.
- [x] ten runs plus a loaded eleventh: see Results above (11 of 11 passed, 44 tests each).
- [x] harness exits 0 with only the 2 named skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`.

### What the harness checks

- [x] Both context shapes: `privacy.json` and `kinds.json` via the service; the harness passes.
- [x] Result order and mismatch rule: `evaluation.json`, `usage.json`; passes.
- [x] Integer accessor fraction and `2.0`: `evaluation.json`; passes.
- [x] No operation raises: the service answers 500 on any throw and every scenario passes.
- [x] `NOT_READY` before the first load: `load.json`; also `ClientTest.answersNotReadyBeforeTheFirstLoad`.
- [x] `ready` settles, bounded, repeatable, false after `close`: `load.json`, `close.json`.
- [x] A non-ruleset never replaces the one held: `status.json`; also `ClientTest.aBadRulesetNeverReplacesTheOneHeld`.
- [x] Ruleset request, `If-None-Match`, ETag, URL joining: `load.json`, `status.json`; `ClientTest.aConditionalRequestFollowsAnEtagAnd304KeepsTheRuleset`, `aTrailingSlashOnTheUrlIsStripped`.
- [x] The stream: `stream.json`, `load.json`, `status.json`; `StreamTest.aStreamChangedEventFetchesTheNewRulesetAndAFailedFetchEndsTheStream`.
- [x] The loop and the jittered backoff: `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: `rate-limit.json`.
- [x] Status fields, states and `lastError` codes: `status.json` and the shape check on every read.
- [x] Usage counts: `usage.json`, `rate-limit.json`.
- [x] Hook events and `onError`, a hook or `onError` that raises: `hooks.json`; `ClientTest.aHookAndAnOnErrorThatRaiseChangeNothing`.
- [x] `close` in order, twice at once, during a hold, final flush: `close.json`, `rate-limit.json`, `usage.json`, `status.json`.
- [x] No context in any request or `onError` message, no header beyond the protocol's: `privacy.json` and the after-scenario checks. The log and error-value source search is `SourceTest.theSdkWritesNoLogLine` (a search of `src/main` for `System.out`, `System.err`, `java.util.logging`, `org.slf4j`, `printStackTrace` and `Logger` finds nothing) and `SourceTest.noErrorValueIsBuiltFromAContext` (a search for an exception built with a `context`, `attribute` or `key` in its message, and for `fail(` or `report(` called with one, finds nothing).

### Native tests

- [x] Many threads, `close` racing, two closes racing: `ConcurrencyTest.everyOperationFromManyThreadsWithCloseRacingAndTwoClosesRacing` (names both; bounded wait). The JVM has no race detector; this test stands in.
- [x] TLS (SHOULD): `TlsTest.aTrustedCertificateLoadsTheRulesetOverTls` and `anUntrustedCertificateIsRefusedAsANetworkError`, with a `keytool`-made certificate in `target/tls` and the `sslContext` seam.
- [x] `variation` returns within a bound while a ruleset request or a usage flush hangs: `ConcurrencyTest.variationReturnsAtOnceWhileARulesetRequestHangs`, `firstLoadThatNeverAnswersSettlesReadyAfterTheTimeout`, `variationReturnsAtOnceWhileAUsageFlushHangs`.
- [x] One ruleset or the other across a swap, `etag` pairing: `ConcurrencyTest.aHooksEtagAndFlagVersionBelongToTheSameRulesetUnderConcurrentSwaps`.
- [x] Callbacks that call back in, and `ready` on the SDK's own thread: `ConcurrencyTest.callbacksThatCallBackIntoTheClientDoNotDeadlock`, `readyAndCloseFromTheSdksOwnThreadDoNotWait`.
- [x] 60 s watchdog, tested through the seam on a real loopback socket: `StreamTest.aStreamThatGetsNoBytesIsDroppedAsTimeout`, `aKeepAliveRestartsTheWatchdog`.
- [x] `Retry-After` above 300 held for 300 s on ruleset, stream and events: `HoldTest.theRulesetRouteHoldsForTheCap`, `theStreamRouteHoldsForTheCapForTheRulesetToo`, `theEventsRouteHoldsForTheCapAndKeepsTheCounts` (also `aValueBelowTheCapIsHeldAsGiven`, `anHttpDateOrAFractionIsNotHeld`, `UnitTest.retryAfterCountsOnlyWholeDecimalSeconds`).
- [x] Counts either side of a UTC midnight go out as two days: `UsageTest.countsEitherSideOfAUtcMidnightGoOutAsTwoDays`.
- [x] An evaluation that raises: `ClientTest.anEvaluationThatRaisesGivesTheFallbackAndOneOnErrorCall`.
- [x] Futures: n/a: no futures (a hook is a plain `Consumer`).
- [x] Integer accessor outside int64: `ClientTest.anIntegerAccessorRefusesAFractionAndAnOutOfRangeValue`; `evaluation.json` `huge`.
- [x] A client never closed does not keep the process alive: `ClientTest.aClientThatIsNeverClosedLeavesOnlyDaemonThreads`; the threads are started in `SdkThread` as daemons.

### Code and documents

- [x] `DESIGN.md` passes the phase 1 row check and `README.md` links the mapping table and gives its own summary table.
- [x] Names keep the canonical meaning and defaults; the closed sets keep their spelling (`ReasonKind`, `ErrorCode`, `StreamState` constants with `value()`, `LastError` strings): `DESIGN.md`, "Closed sets".
- [x] No platform regex in the evaluator: `SourceTest.theEvaluatorUsesNoPlatformRegex` (a search of `src/main/.../evaluation` for `java.util.regex`, `Pattern.compile`/`Pattern.matches`, `Matcher`, `.matches(` outside our own `Patterns`, `.replaceAll(`, `.replaceFirst(`, `.split(` and `.splitAsStream(` finds nothing).
- [x] The lint configuration names its rule set and each rule it leaves out has a reason: `checkstyle.xml` (header comment, a comment beside each group); Spotless is google-java-format as is.
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none ("Dependencies": "Runtime: none").
- [x] No telemetry and only protocol headers set: `SourceTest.theSdkSendsOnlyTheProtocolsHeaders` (every `.header("name", ...)` call in `src/main` names one of `Authorization`, `Accept`, `If-None-Match`, `Content-Type`) and `HeadersTest.everyRouteSendsOnlyTheProtocolsHeaders`, whose capture is below.
- [x] Extras beyond the contract are ones contract 13 allows: `Client.flush()` and the package-private seams, both listed in contract 13; `ToggleSlashException` is only the value handed to `onError`, never thrown at a caller. None weakens contract 5 or sends anything new.
- [x] Seams are options on `create` internal to the package, never a global or environment variable: `Options.Builder` package-private setters; `grep -rn 'getenv\|getProperty' sdks/java/src/main` finds nothing.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK and use `mise exec --` when `mise` is on `PATH`.
- [x] `test-service/README.md` maps every accessor (`bool`, `string`, `number`, `int`, `json`, `any`) to an SDK method.
- [x] `.github/workflows/sdk-java.yml` follows the skeleton, filters on its paths and pins every action by commit SHA.

Headers the JDK `HttpClient` adds on its own, one request of each route on a loopback listener (`HeadersTest`, which writes `target/headers-table.md`; the port varies):

| Route | Headers the SDK sets | Headers the client adds, and why they do not identify a user or a machine |
| --- | --- | --- |
| `GET /sdk/v1/ruleset` | `Authorization: Bearer <key>` | `Host: 127.0.0.1:<port>` (the URL's own host); `User-Agent: Java-http-client/25.0.5` (the JDK version only, no user or host) |
| `GET /sdk/v1/ruleset` (conditional) | `Authorization: Bearer <key>`; `If-None-Match: "v1"` | the same two |
| `GET /sdk/v1/stream` | `Accept: text/event-stream`; `Authorization: Bearer <key>` | the same two |
| `POST /sdk/v1/events` | `Authorization: Bearer <key>`; `Content-Type: application/json` | `Content-Length` (the body size); `Host`; `User-Agent` as above |

The client is built with HTTP/1.1, so it adds no `Connection: Upgrade` or `HTTP2-Settings`, and no `Accept-Encoding`.

### Repository

- [x] No em or en dash: `bun scripts/check-dashes.ts` printed nothing and exited 0 in the phase 5 check.
- [x] `bun run format:check`: `All matched files use the correct format.`
- [x] Scope: the phase 6 scope check printed only `scope ok`.
- [x] No package published and no name reserved: `mvn install` was run only to the local repository on this machine.
- [x] Commit subject `chore(sdk-java): the Java server SDK regenerated cold from the authoring prompt`, not `feat`, with no `Co-Authored-By` and no AI attribution (Decision 8).
- [x] The workflow: `ubuntu-latest`, no `if:`, triggers `pull_request` and `push` only, installs its own toolchain; `bun scripts/check-workflows.ts .github/workflows/*.yml` printed `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`.
