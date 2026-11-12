# Java SDK: run report

## Prompt version

- Prompt: 19b29aaa592665f244fccd42163ef68add0a3d4c (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: claude-sonnet-5-5 (Sonnet 5.5)
- Date: 2026-11-12
- Input: language Java; runtime chosen: Temurin 25.0.5+8 (the newest LTS); minimum version chosen: Java 17
  (`--release 17`); package manager chosen: Maven 3.10.0
- Toolchain (`sdks/java/mise.toml`, and what the version commands print):
  - java `temurin-25.0.5+8.0.LTS`: `openjdk version "25.0.5" 2026-10-20 LTS`, `Temurin-25.0.5+8`
  - maven `3.10.0`: `Apache Maven 3.10.0`
  - checkstyle `14.4.0`: `Checkstyle version: 14.4.0`
  - google-java-format `2.0.0`: `google-java-format: Version 2.0.0`
  - JUnit Jupiter 6.1.3 (a test dependency in `pom.xml`); Maven plugins: clean 3.5.0, resources 3.5.0, compiler 3.16.0,
    build-helper 3.8.0, surefire 3.6.1
  - Lookups: `mise ls-remote java` for the newest 25 build and Adoptium's release page for which release is the newest
    LTS; maven.apache.org for the latest 3.x; `mise ls-remote checkstyle` and `google-java-format`; Maven Central
    `maven-metadata.xml` for the JUnit and plugin versions (stable releases chosen over the 4.0.0 betas of the plugins).

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2
  count command prints `1371 17`).
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 64 passed of 64 (`Tests run: 64, Failures: 0, Errors: 0, Skipped: 0`): ClientTest 35, SseParserTest 10,
  JsonTest 9, Utf8Test 6, RetryAfterTest 3, VectorsTest 1. `VectorsTest` is one test that loops over 1371 vectors and 17
  pins. The test service (`test-service/src`) has no tests of its own; the harness is its test.
- `bin/test` ten times in a row on the committed tree: runs 1 to 10 each `Tests run: 64, Failures: 0, Errors: 0, Skipped:
  0` and `vectors: 1371 pins: 17`, exit 0, about 10 to 12 s each. Eleventh run with the CPU loaded (12 busy shell loops,
  `nproc` is 12, started before `bin/test` and killed after it ended): 64 passed, 0 failed, exit 0, 12.2 s. The ten
  restarted once in this run, because phase 6 found a missing test (see Decisions, 14) and the tree changed.

## Decisions

1. **Toolchain.** Temurin 25.0.5+8 (newest LTS) with `--release 17`, per `language-notes/java.md` ("Pin the newest
   LTS ... Minimum Java 17 is separate"). Maven over Gradle: the mainstream Java build and the language note's own
   layout (`build-helper-maven-plugin`) is Maven.
2. **Test framework: JUnit Jupiter 6.1.3** through surefire, the native Java framework; the vectors run as one test with
   a count line (`vectors: N pins: M`) for the phase 2 check.
3. **Lint and format.** Checkstyle 14.4.0 for `src/main/java` (`checkstyle.xml`, severity error, plus `javac -Xlint:all
   -Werror` in the build), and google-java-format 2.0.0 in check mode for every Java file under `src` and `test-service/src`.
   Checkstyle's own rule set is in `checkstyle.xml`; its header lists every rule left out with a reason (IllegalCatch,
   MagicNumber, FinalParameters, DesignForExtension, JavadocPackage, TodoComment, HiddenField, LeftCurly,
   UnnecessaryParentheses, AvoidInlineConditionals, InnerAssignment). LeftCurly and UnnecessaryParentheses are off
   because google-java-format owns layout and places case blocks its own way. `JavadocStyle` does not exist in 14.4.0, so
   `SummaryJavadoc` and `JavadocParagraph` stand in. Formatter 2.0.0 is the first version that runs on the JDK 25 javac
   (`language-notes/java.md`, "The formatter on the pinned JDK").
4. **Accessor shape.** Typed methods, no generic one (Java erases `T`, so a `null` fallback or `Long` against `Double`
   could not be told apart): `boolVariation`, `stringVariation`, `longVariation` (the integer accessor, Java `long`),
   `numberVariation` (`double`) and `jsonVariation` (any kind), each with a `Detail` twin. Mapping table: `DESIGN.md`
   section 1. The integer type counts for `integer-accessor` (`PROMPT.md` phase 1, "Typed languages": Java is named).
5. **Two `HttpClient`s and forced HTTP/1.1.** JDK 17 has no `HttpClient.close` (`language-notes/java.md`). The JDK would
   otherwise send `Connection: Upgrade, HTTP2-Settings` on plain `http`, headers the harness's protocol check refuses.
6. **Hand-written stream body, SSE parser, JSON parser, UTF-8 codec and regex engine.** The language note names the
   `BodySubscriber` and the UTF-8 codec. The JSON parser (no JSON in the JDK) and the Pike VM for `matches` (the platform
   regex backtracks, `evaluation.md` "Patterns", "Cost") are mine. SSE follows the WHATWG rules, which
   `docs/sdk-contract.md` line 230 names.
7. **`ready` and `close` bounds.** The first load is bounded by `requestTimeout` (one `Future.get(requestTimeout)` covers
   connect, headers and body, `HttpCalls`). `Client.ready` adds a backstop of `2 * requestTimeout + 1 s` that only
   matters if a hang escaped that bound; it never returns early. `close` joins each SDK thread for at most
   `requestTimeout + 1 s`, then does the final flush, so it returns within about twice `requestTimeout` as contract 11
   asks (`ClientTest.closeIsBoundedWhenARequestHangs`).
8. **Zero or negative durations** fall back to the defaults, as contract 2 says an SDK SHOULD. `stream(false)` is "off"
   and not given is "on" (the builder holds `null` until set).
9. **An unexpected `RuntimeException` in the sync loop** is reported as `network` and the loop waits one poll interval.
   Contract 8 (`docs/sdk-contract.md` line 335) says `network` is "the connection failed, or any other failure".
10. **`flagVersion` is a `Long`**, `null` when the flag is absent or malformed or its version is not a whole number
    below 2^63: contract 10 (line 410) allows reporting a version above 2^63 minus 1 as none.
11. **Typed test-service accessors answer 400 on a fallback of the wrong kind**: `docs/sdk-test-service.md` line 80 lets a
    service do so. `any` and `json` take any fallback; a non-finite number is written back as JSON `null`.
12. **Served arrays and objects** reach the caller as deep unmodifiable copies, so a caller cannot change the ruleset held
    through a value it was handed. `Context.of` accepts any `Number` and `CharSequence` besides the JSON model and reads
    a number as the nearest `double` (`DESIGN.md` section 5); `Context.parse` keeps raw JSON text exactly (`1e999`, a lone
    surrogate).
13. **JSON nesting limit 1024**, over the contract's floor (contract 7, line 207: "at least 512 levels"). The stream has
    no cap on a line or an event, as contract 7.2 "Bounds" says; the one limit the SDK adds there is none.
14. **A test added in phase 6.** The checklist asks for `close` racing `variation`; phase 3 had two closes racing and
    many threads, but no close in the middle of running evaluations. `closeRacingVariationAndAnotherCloseRaisesNothing`
    was added and the ten runs restarted. The swapper thread in `manyThreadsOnEveryOperationAtOnce` is background work too, but the test asserts nothing
    about how many swaps happened, so it does not depend on it having made progress.
15. **`Evaluate` seam.** `Client` has a package-private constructor taking an `Evaluate` function, so a test can make the
    evaluator raise (`anEvaluatorFailureIsOneOnErrorAndTheFallback`); the public API cannot reach it (contract 13: a seam
    that is an option of the package's own config).
16. **No TLS test.** The checklist makes it a SHOULD. `java.net.http` uses the JDK's TLS and trust store for an `https`
    URL and the SDK sets no `SSLContext`; building a local certificate needs `keytool` plus test plumbing. The TLS path is
    untested and `RUN.md` says so.
17. **Workflow toolchain step.** `jdx/mise-action` with `working_directory: sdks/java` and `install_args: java maven
    checkstyle google-java-format`, copied from the form `sdk-go.yml` uses, so the root `.tool-versions` (.NET, a second
    Bun) is not installed.
18. **Spec versus the TypeScript engine.** No disagreement found: every vector passed on the first full run against the
    spec's words.

## Guesses

1. **Where Checkstyle's jar is found.** Where: `docs/sdk-authoring/language-notes/java.md`, "Checkstyle under mise is a
   bare JAR". Closest sentence: "run `java -jar "$(command -v checkstyle)" -c checkstyle.xml src/main/java`". It does
   not settle the point: on a machine with mise shims first on `PATH` (this one), `command -v checkstyle` returns the shim,
   which is a symlink to `mise`, and `java -jar` fails with "Invalid or corrupt jarfile". Assumed: `bin/lint` resolves
   the jar with `mise which checkstyle || command -v checkstyle`. In CI, where `mise-action` puts the tools on `PATH`
   directly, either works.
2. **Whether `bin/lint` covers the test code and the test service.** Where: `PROMPT.md`, "Phase 5" item 3 ("the native
   lint and format check, with no warnings allowed"). Closest sentence: that one. It does not say which source roots.
   Assumed: Checkstyle on `src/main/java` (the language note's own command line) and google-java-format on all three
   roots (`src/main`, `src/test`, `test-service/src`), because Checkstyle's Javadoc rules would only add noise to tests.
3. **How the vectors are counted for "its count equals the phase 2 count command's".** Where: `PROMPT.md` phase 2
   check and `CHECKLIST.md`. Closest sentence: "the counts match the files (the second command prints vectors, then
   pins)". It does not say what `bin/test` prints. Assumed: it prints `vectors: <n> pins: <n>`, and the same numbers
   (`1371 17`) are compared by eye here.
4. **Which surefire/compiler plugin lines count as the "toolchain".** Where: `RUN-TEMPLATE.md`, "Toolchain: each tool and
   its exact version, as `mise.toml` pins them". Closest sentence: that one. Maven plugins and JUnit are not in
   `mise.toml` but are pinned in `pom.xml`. Assumed: listed above, from `pom.xml`.

## Stuck points

None. (The `ob` tracker at `https://ob.madangryscientist.com:5050` was unreachable for `ob agent register` and `ob spinoff
handoff` during this run; that is the spinoff harness and not this prompt.)

## Proposed prompt changes

1. For guess 1, in `language-notes/java.md`, replace the Checkstyle line with: "Checkstyle under mise is a bare JAR,
   with no launcher. With mise shims on `PATH`, `command -v checkstyle` returns the shim, not the JAR, so find the JAR
   with `mise which checkstyle` (fall back to `command -v checkstyle` where `mise-action` puts it on `PATH` directly) and
   run `java -jar "$jar" -c checkstyle.xml src/main/java`."
2. For guess 2, in `PROMPT.md` phase 5 item 3, after "with no warnings allowed", add: "It checks the SDK's source and the
   test code and the test service as well, or `README.md`/`DESIGN.md` says which roots it leaves out."
3. For guess 3, in `PROMPT.md` phase 2, after "reports how many it ran", add: "(print it as `vectors: <n> pins: <n>`,
   so the check below can be compared with one `grep`)".
4. For guess 4, in `RUN-TEMPLATE.md`, "Toolchain", add: "Library and plugin versions pinned in the build file (a
   `pom.xml`, a `go.mod`) go here as well."
5. In `language-notes/java.md`, add: "`checkstyle` has no `JavadocStyle` check in 14.x; use `SummaryJavadoc` and
   `JavadocParagraph`. `LineLength` belongs to the `Checker`, not the `TreeWalker`. A `{@code}` with unbalanced braces
   breaks the Javadoc parser." (Hit during this run; not a guess, a trap.)
6. In `language-notes/java.md`, add: "The JDK client sends `User-Agent: Java-http-client/<jdk version>` and, for a POST,
   `Content-Length`; with `HttpClient.Version.HTTP_1_1` it adds no `Connection` or `Accept-Encoding`."

## Checklist

### The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, count equal to the phase 2 command's: `VectorsTest`
  prints `vectors: 1371 pins: 17`; the phase 2 command printed `1371 17`.
- [x] `bin/test` ten times in a row and a CPU-loaded eleventh: see Results (ten of `Tests run: 64`, exit 0; loaded run 12
  loops, 64 passed). The ten were run on the committed tree after the last change to a file under `sdks/java` (except
  this `RUN.md`).
- [x] The harness exits 0 with only the two named vector skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369
  passed, 0 failed, 2 skipped`; `integer-accessor` is advertised, so no scenario is skipped.

### What the harness checks

- [x] Both context shapes (contract 3): harness `privacy.json` and the `kinds.json` vectors through the service;
  `ClientTest.aBadContextAndAnUnknownKindGiveAFallbackNotAThrow`.
- [x] Order of contract 4 and the mismatch rule: harness `evaluation.json`, `usage.json`;
  `aServedValueOfTheWrongTypeIsAMismatchAndNotCounted`, `aNullJsonFallbackAcceptsAnything...`.
- [x] Integer accessor: harness `evaluation.json`; `theIntegerAccessorNeedsAWholeNumberWithin64Bits`.
- [x] Nothing raises (5.1): harness, every scenario (the service answers 500 on an exception and the scenario fails).
- [x] `NOT_READY` at once (5.2, 5.4): harness `load.json`; `answersFallbacksWhileTheRulesetHangs`.
- [x] `ready` after the first attempt, bounded, callable again, false after `close`: harness `load.json`, `close.json`;
  `closeDuringReadyReleasesReady`, `aFirstBadRulesetLeavesTheClientNotReady`.
- [x] A non-ruleset answer never replaces the one held (5.5): harness `status.json`; `aBadRulesetNeverReplacesAGoodOne`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`;
  `sendsIfNoneMatchOnlyWithARulesetHeld`.
- [x] The stream (7.2): harness `stream.json`, `load.json`, `status.json`; `SseParserTest` (10 tests),
  `theStreamAnnouncesAndAChangedEventFetches`, `aStreamThatEndsIsFollowedByAReconnect`,
  `aStreamAnswerOtherThan200IsNamedAndFallsBackToPolling`.
- [x] The loop and jittered backoff (7.3): harness `backoff.json`, `stream.json`; `Sync.delay`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`; `RetryAfterTest` (3),
  `aLongRetryAfterIsCappedAndHeldOnEveryRoute`.
- [x] Status fields, states and `lastError` codes: harness `status.json` and its shape check;
  `aServerErrorIsNamedAndKeepsWhatIsHeld`, `aRefusedConnectionIsNetwork`.
- [x] Usage counts (9): harness `usage.json`, `rate-limit.json`; `usageBodyHasCountsAndNothingAboutTheUser`,
  `usageIsSentInBatchesOfAtMost500`, `aServerErrorKeepsTheCountsAndABadRequestDropsThem`.
- [x] Hook events and `onError`: harness `hooks.json`; `theHookRunsBeforeReadyWithNotReady`,
  `theHookGetsFallbacksTooAndAThrowingHookChangesNothing`, `anOnErrorThatThrowsIsIgnored`.
- [x] `close` (11): harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`;
  `closeSendsTheLastCountsAndIsIdempotent`.
- [x] No context, key or attribute in any request or `onError` message, no extra header (12): harness `privacy.json` and
  the checks after every scenario. Source search `grep -rn "System.out\|System.err\|Logger\|printStackTrace\|java.util.logging"
  src/main` finds nothing: the SDK writes no log line. Every exception message is built from a route, a status code or
  a millisecond count (`grep -rn "Exception(.*+" src/main`: `HttpCalls`, `Sync`, `Usage`, `Json` position only), never a
  context value. `Json`'s `cannot write <class name>` names a class. Header table below.

### Native tests

- [x] Every operation from many threads, `close` racing `variation`, two `close` racing, no error or deadlock:
  `manyThreadsOnEveryOperationAtOnce` (16 threads x 3000 calls of `boolVariation`, `status`, `flush` and a fallback
  `Detail`, with the ruleset swapping every 3 ms and a 20 ms flush and 5 ms poll, then asserts zero wrong answers, no
  `onError`, and the exact count of 48 000 evaluations in the usage bodies);
  `closeRacingVariationAndAnotherCloseRaisesNothing` (8 threads in `variation`, `variationDetail` and `status` while
  two `close` calls race, then more calls after, status `closed`, no `onError`);
  `concurrentClosesAllReturnAfterTheFinalFlush` (8 threads; each sees exactly one events POST when its `close` returns).
  Java has no race detector in the JDK; these stress tests stand in, each with a bounded `Future.get`.
- [ ] SHOULD, `https` through a local certificate: not done, and the TLS path is untested (Decisions, 16).
- [x] `variation` returns within a bound while a ruleset request hangs and while a flush hangs:
  `answersFallbacksWhileTheRulesetHangs`, `closeIsBoundedWhenARequestHangs`; harness `usage.json` ("a usage request is
  bounded by the request timeout, close included").
- [x] A hook's `etag` and `flagVersion` belong to the same ruleset under swaps:
  `theHookSeesTheEtagAndVersionOfTheSameRuleset` (`Snapshot` is one immutable pair in one `volatile` field).
- [x] A hook and `onError` that call into the client do not deadlock; `ready` from such a callback returns at once:
  `aHookMayCallBackIntoTheClient`, `readyAndCloseFromACallbackOnAnSdkThreadReturnAtOnce`.
- [x] A stream with no bytes for 60 s is dropped as `timeout`, a keep-alive restarts it, through a seam on a real
  loopback socket: `aQuietStreamIsDroppedByTheWatchdogAndAKeepAliveHoldsIt` (`Options.streamIdleTimeout`, the
  `FakeServer` goes quiet on a real socket).
- [x] A `Retry-After` above 300 held for 300 s on the ruleset, stream and events routes:
  `aLongRetryAfterIsCappedAndHeldOnEveryRoute` (a `TestClock`, `RetryAfter.MAX_MILLIS = 300_000`).
- [x] Counts either side of a UTC midnight go out as two days: `usageSplitsAtUtcMidnight`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`:
  `anEvaluatorFailureIsOneOnErrorAndTheFallback`.
- [x] A future from a hook that fails later: n/a: no futures. `AfterEvaluation.accept` returns `void` (it may throw
  `Exception`, which goes to `onError`: `theHookGetsFallbacksTooAndAThrowingHookChangesNothing`).
- [x] A number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `theIntegerAccessorNeedsAWholeNumberWithin64Bits`;
  harness `evaluation.json` (the `huge` scenario).
- [x] A client never closed does not keep the process from exiting: `Client.java` line 49 starts both threads with
  `setDaemon(true)`; `theSdkThreadsAreDaemonsAndGoAwayOnClose` checks every `toggleslash-*` thread is a daemon and gone
  after `close`.

### Code and documents

- [x] `DESIGN.md` has the mapping table (section 1), and `README.md` links to it.
- [x] Names keep their canonical meaning and defaults; reason kinds, error codes, stream states and `lastError` codes
  keep their spelling: `ReasonKind`, `ErrorCode` and `StreamState.wire()` (`DESIGN.md` section 1, "Closed sets"); the
  harness `status.json` shape check.
- [x] The evaluator has no regular expression from the platform: `grep -rn "java.util.regex\|\.matches(\|replaceAll\|replaceFirst\|\.split(\|Pattern.compile"
  src/main` finds only the SDK's own `evaluation.Pattern` (`Pattern.java`, a Pike VM; the doc comment says "no {@code
  java.util.regex}").
- [x] The lint configuration names its rule set and each ignored rule carries a reason: `checkstyle.xml` header.
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none (section 4).
- [x] No telemetry, no identifying header: `grep -rn "\.header(" src/main` finds only `Authorization`, `Accept`,
  `If-None-Match` and `Content-Type`. Headers the client adds (the `headerAudit` test captures one request of each
  route, with `ClientTest.headerAudit` asserting each is in the allowed set):

  | Route | Headers on the wire |
  | --- | --- |
  | `GET /sdk/v1/ruleset` | `authorization`, `host`, `user-agent` |
  | `GET /sdk/v1/ruleset` with `If-None-Match` | `authorization`, `host`, `if-none-match`, `user-agent` |
  | `GET /sdk/v1/stream` | `accept` (`text/event-stream`), `authorization`, `host`, `user-agent` |
  | `POST /sdk/v1/events` | `authorization`, `content-length`, `content-type` (`application/json`), `host`, `user-agent` |

  Added by the JDK client: `host` (the URL's host, which the operator configured), `user-agent`
  (`Java-http-client/25.0.5`: the library and JDK version, no host or user name) and `content-length` (the body size).
  No `Connection` or `Accept-Encoding` is sent with HTTP/1.1 forced.
- [x] Every extra beyond the contract is one contract 13 allows and none weakens contract 5: `Context` helpers,
  `Client.flush()`, the `Options` seams `clock`, `random` and `streamIdleTimeout` (`DESIGN.md` section 1, "Beyond the
  contract").
- [x] Test seams are options on `create`, never a global or an environment variable: `grep -rn "getenv\|getProperty"
  src/main` finds nothing; the package-private `Evaluate` seam is a constructor argument.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK and need only the pinned toolchain.
- [x] `test-service/README.md` maps every accessor (`bool`, `string`, `number`, `int`, `json`, `any`) to a method.
- [x] The workflow follows the skeleton, filters on its paths and pins every action by commit SHA with the tag in a
  comment; `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.

### Repository

- [x] No em dash or en dash in any file written: `bun scripts/check-dashes.ts` exit 0.
- [x] `bun run format:check` passes: `All matched files use the correct format.`
- [x] Only `sdks/java/` and `.github/workflows/sdk-java.yml` changed: the phase 6 scope check prints `scope ok`.
- [x] Nothing published and no name reserved: no `mvn deploy`, no `install`, no tag; the pom's version is `0.0.0` and the
  README names no registry.
- [x] The commit subject is a Conventional Commit (`chore(sdk-java): ...`), not `feat`, with no `Co-Authored-By` or AI
  attribution line.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:` guard, its triggers are `pull_request` and `push`
  only, it installs its own toolchain (`jdx/mise-action`), and `check-workflows.ts` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: every section above, in order, with evidence for each line.
