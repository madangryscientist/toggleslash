# Java SDK: run report

## Prompt version

- Prompt: f83c46d3f9eca172b52da061c850f8b07cca21d2 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), run as FS-157 (Java sweep run 20, a cold regeneration from an empty `sdks/java`)
- Date: 2026-11-12
- Input: language Java; runtime chosen: Temurin 25.0.5+8 LTS (newest LTS, per the language note); minimum version chosen: Java 17 (`--release 17`, `<release>17</release>` in `pom.xml`); package manager chosen: Maven 3.10.0
- Toolchain: `mise.toml` pins `java = "temurin-25.0.5+8.0.LTS"` (`java -version` prints `openjdk version "25.0.5" 2026-10-20 LTS`, `Temurin-25.0.5+8`), `maven = "3.10.0"` (`mvn -v` prints `Apache Maven 3.10.0`) and `checkstyle = "14.4.0"` (a bare JAR under mise; `bin/lint` resolves it with `mise exec -- sh -c 'command -v checkstyle'`). `pom.xml` pins maven-compiler-plugin 3.16.0 (`-Xlint:all -Werror`), maven-surefire-plugin 3.6.1, build-helper-maven-plugin 3.8.0, spotless-maven-plugin 3.11.0 with google-java-format 2.0.0, and JUnit Jupiter 6.1.3 (the only test library). No runtime dependency.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (the two named `buckets.json` skips only; run four times on the final tree, all exit 0)
- Capabilities advertised: `integer-accessor`
- Native tests: 31 passed of 31 top-level tests (`ClientTest` 13, `ConcurrencyTest` 7, `LeafTest` 10, and the `VectorsTest` `@TestFactory`, which counts as one test), 0 failed. Surefire counts 1418 including the 1388 dynamic cases the factory makes (1371 vectors and 17 pins). The test service has no tests of its own; it is a test source root compiled and run by the conformance harness.
- `bin/test` ten times in a row on the committed tree: runs 1 to 10 each exited 0 with `tests=1418 failed=0` and `vectors: 1371 pins: 17`. The eleventh run, under CPU load (12 busy shell loops, one per `nproc`, started before `bin/test` and killed after it ended, the build included): exit 0, 1418 tests, 0 failed, `vectors: 1371 pins: 17`. The ten were restarted from the first after the last change to the tree (a path fix in `test-service/README.md`); the counts above are of the final tree.
- Lint: `bin/lint` exits 0 (Spotless check, then Checkstyle over `src/main/java`). `bun run format:check`, `bun scripts/check-dashes.ts` and `bun scripts/check-workflows.ts .github/workflows/*.yml` pass.

## Decisions

1. Lint and format: Spotless with google-java-format 2.0.0 and Checkstyle 14.4.0 over Error Prone, SpotBugs or PMD, because the language note names Spotless and Checkstyle, and they need no annotation processor. google-java-format 2.0.0 and not 1.x, because the 1.x line fails on JDK 25 (language note, "The formatter on the pinned JDK"). `-Xlint:all -Werror` is on for the compiler too.
2. Checkstyle rule set: `checkstyle.xml` carries a header comment naming the rule set it enables (Javadoc rules, naming, imports, whitespace, braces, coding hygiene) and each rule it leaves out has a reason in a comment at the end of the file: `MagicNumber` (the evaluator is full of the spec's own numbers, each named by a comment), `FinalParameters` and `DesignForExtension` (every class is final or package-private), `CyclomaticComplexity`, `NPathComplexity` and `MethodLength` (the pattern matcher and the UTF-8 decoder are state machines written to the spec), `IllegalCatch` (the contract has the SDK catch whatever a hook, callback or evaluator raises and report it to `onError`), `TodoComment` (there is none). The doc comment rules run on `src/main/java` only; `bin/lint` names the roots each tool reads in a comment, and Spotless reads the tests and the test service too (PROMPT.md, "Entry points", the `bin/lint` row).
3. Formatter warnings: `bin/lint` prints four `WARNING: sun.misc.Unsafe` lines from Spotless's own formatter code on JDK 25. They are not lint warnings about this code (PROMPT.md, "Entry points", `bin/lint` row: "a line a tool's own runtime prints about the tool itself ... is not a lint warning: leave it visible and name it in RUN.md"). They are left visible and `README.md` says so.
4. HTTP: `java.net.http.HttpClient` over a third-party client, because the JDK has HTTP/1.1, timeouts and cancellation, and the SDK adds no dependency (`DESIGN.md`, "Dependencies"). The stream body is a custom `BodySubscriber` that hands itself over at once (language note: `BodyHandlers.fromSubscriber` answers only when the body ends).
5. One `HttpClient` with a per-call abort (`Http` tracks each call, and `close` runs `abortSync()` to cancel the ruleset and stream calls, waits for the two threads for up to the request timeout, runs `abortAll()` only if the flush thread is still stuck, and then sends the final usage flush on the same client; `ToggleSlashClient.java:895-910`) rather than the two clients the language note suggests. See guess 2.
6. JSON: a small hand-written reader and writer (`Json.java`) over a library, because the JDK has no parser and a library would still need the same care for binary64 numbers and lone surrogates (`DESIGN.md`, "Dependencies").
7. UTF-8: a hand-written WHATWG decoder and encoder (`Utf8.java`) over `CharsetDecoder` and `String.getBytes`, because they differ from the Encoding standard (language note, "Do not rely on the JDK's UTF-8 codec"). `LeafTest.utf8DecodesEachMaximalInvalidSubpartToOneReplacement` and `utf8EncodesALoneSurrogateAsReplacementNotQuestionMark` hold the standard's examples.
8. SemVer, instants and attribute references are read by hand-written walks over the characters (`SemanticVersion.java`, `RfcInstant.java`, `AttributeReference.java`) and patterns by the package's own `Pattern.java`, because `java.util.regex` and `java.time` parsers differ from the spec (evaluation.md, "Patterns", "Cost"). Numeric SemVer parts stay strings and compare by length and then by code unit, so they have no size limit; short forms `2` and `2.1` are valid (evaluation.md).
9. Accessors: typed methods `boolVariation`, `stringVariation`, `doubleVariation` and `intVariation` (returns `long`), their `Detail` forms, and one generic `variation`/`variationDetail` for `json` and `any` (the fallback's JSON kind does the work, a `null` fallback accepts any kind). A fallback of a Java type that is no JSON value answers `TYPE_MISMATCH`. Mapped in `DESIGN.md#mapping-table` and `test-service/README.md`.
10. Contexts: the caller's `Map`/`List`/`String`/`Boolean`/`Number` values are deep-converted into the evaluator's model, and a value of a type JSON cannot hold (a Java object, say) is `ERROR INVALID_CONTEXT` with the fallback (contract 3 and 4, `docs/sdk-contract.md` line 79).
11. Integer accessor: `integer-accessor` is advertised; `intVariation` returns `long` and answers `TYPE_MISMATCH` for a fraction or a value outside signed 64 bits (contract 4, "Typed languages").
12. No public `flush`: contract 2 lists none. A package-private `flushNow()` and the package-private `rulesetHoldUntil()` and `usageHoldUntil()` are seams for the tests, and `Options` carries package-private `clock`, `jitter`, `watchdog` and `evaluator` seams (contract 13 allows seams as options on `create`; none is a global or an environment variable).
13. Threads: two daemon threads, `toggleslash-sync` and `toggleslash-flush`, started in `ToggleSlashClient` (the `SdkThread` class sets `setDaemon(true)`; constructed at `ToggleSlashClient.java:112-113`). The never-closed-client line is met by construction and by `ClientTest.daemonThreadsDoNotKeepTheProcessAlive`.
14. Headers the JDK client adds by itself (`User-Agent`, `Host`, `Content-Length`) are accepted and listed in the table under the Checklist's header line, each with why it does not identify a user or a machine.
15. Layout: `test-service/src` is a test source root added by build-helper-maven-plugin, so `bin/test` compiles the service and `bin/test-service` runs `java -cp target/classes:target/test-classes` and builds only when the classes are missing (language note).
16. Workflow: `.github/workflows/sdk-java.yml` follows the skeleton, with every action pinned by the same commit SHAs `ci.yml` uses (checkout, setup-bun) and `jdx/mise-action@b44d12e7d0d8ee7461bb893e1b1993500433185b # v5.2.7` to install `java maven checkstyle` from `sdks/java/mise.toml`.
17. TLS: not tested. See the Checklist (SHOULD line).

## Guesses

1. Which Checkstyle 14 properties take which names. Where: `language-notes/java.md`, "Checkstyle 14 traps". Closest sentence: "It has no `JavadocStyle` check; use `SummaryJavadoc` and `JavadocParagraph`. `LineLength` belongs under `Checker`, not `TreeWalker`." It covers two traps, not the others this run hit: `JavadocVariable` takes `accessModifiers` while `MissingJavadocMethod` and `MissingJavadocType` take `scope`, `JavadocPackage` fails every package without a `package-info.java`, and `JavadocType` needs `allowMissingParamTags` for a record or a type with no type parameters to document. Assumed: found each by running the JAR on the config, fixed the config and added `package-info.java`.
2. Whether two `HttpClient`s are required. Where: `language-notes/java.md`, "No closable `HttpClient` before 21". Closest sentence: "Use two clients: abort the one for the ruleset and stream on `close`, and leave the one for usage so the final flush can still go out." It states a means and not the requirement behind it (contract 11: `close` stops the loops, aborts the ruleset and stream requests and then sends the final flush), so a reader cannot tell whether one client with per-call abort is allowed. Assumed: it is, because the contract says what must happen, not how. One client keeps each in-flight request as a cancellable future and `close` cancels the ruleset and stream ones and lets the final usage request go; `close.json` and `rate-limit.json` pass.
3. How the "native tests" count is reported for a vector factory. Where: `RUN-TEMPLATE.md`, "Results". Closest sentence: "top-level tests as the native runner counts them ... a test that loops over the vectors is one test". JUnit's `@TestFactory` makes 1388 dynamic tests that Surefire counts as tests individually, and the sentence does not say whether a factory is "a test that loops". Assumed: yes, the factory is one top-level test with 1388 cases, so 31 top-level tests; the surefire total of 1418 is given beside it.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `language-notes/java.md`, "Checkstyle 14 traps", after "`LineLength` belongs under `Checker`, not `TreeWalker`.", add: "`JavadocVariable` takes `accessModifiers`, while `MissingJavadocMethod` and `MissingJavadocType` take `scope`; `JavadocPackage` needs a `package-info.java` in every package; `JavadocType` needs `allowMissingParamTags` to be true for a record or a type with nothing to document."
2. For guess 2, in `language-notes/java.md`, "No closable `HttpClient` before 21", replace "Use two clients: abort the one for the ruleset and stream on `close`, and leave the one for usage so the final flush can still go out." with: "`close` cannot close the client, so keep each in-flight ruleset and stream request as a `CompletableFuture` and cancel those on `close`, and let the final usage flush go out afterwards. One client with a per-call abort is enough (two clients also work)."
3. For guess 3, in `RUN-TEMPLATE.md`, "Results", after "a test that loops over the vectors is one test", add: "(a framework's dynamic or parameterised test factory over the vectors counts as that one test, with its case count beside it)".

## Checklist

Header table for the privacy line, captured by `ClientTest.theOnlyHeadersTheSdkSetAreTheProtocolsAndTheTableIsPrinted` on a loopback listener (it prints `headers: <route>: ...` lines and fails if an unexpected one appears):

| Route | Headers the SDK sets | Headers the JDK client added | Why they do not identify a user or a machine |
| --- | --- | --- | --- |
| ruleset (`GET`) | `Authorization` | `Host`, `User-Agent` | `Host` is the URL's own host and port; `User-Agent` is the JDK's fixed `Java-http-client/<JDK version>`, naming no host or user |
| ruleset, conditional | `Authorization`, `If-None-Match` | `Host`, `User-Agent` | same |
| stream | `Authorization`, `Accept` | `Host`, `User-Agent` | same |
| events (`POST`) | `Authorization`, `Content-Type` | `Content-Length`, `Host`, `User-Agent` | `Content-Length` is the body length; the rest as above |

The JDK client sent no `Accept-Encoding` and no `Connection` header in these captures.

- [x] `bin/test` runs every vector but `buckets.json` and every pin: `VectorsTest` (`@TestFactory`) reads `fixtures/evaluation/*.json` from the repository at test time; it prints `vectors: 1371 pins: 17`, and the phase 2 count command prints `1371 17`. All pass.
- [x] `bin/test` ten times in a row plus one loaded run: ten lines `exit=0 tests=1418 failed=0`, and the loaded run `exit=0` with 12 busy loops (see Results).
- [x] The harness exits 0 with only the 2 named skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`. Java declares `integer-accessor`, so no scenario is skipped for it.
- [x] Both context shapes: the harness, `privacy.json`, and the `kinds.json` vectors through the service (pass in the conformance run).
- [x] Result order and mismatch rule with the SDK's error codes: the harness, `evaluation.json`, `usage.json`; native `ClientTest.accessorsApplyTheMismatchRuleAndTheInt64Range`.
- [x] `integer-accessor`, fraction is `TYPE_MISMATCH` and `2.0` is `2`: the harness, `evaluation.json`; `ClientTest.accessorsApplyTheMismatchRuleAndTheInt64Range`.
- [x] No failure raises: the harness, every scenario (the service answers 500 on anything raised).
- [x] `NOT_READY` before the first load: the harness, `load.json`; `ClientTest.evaluationBeforeTheFirstLoadIsNotReady`.
- [x] `ready` settles after the first attempt, may be called again, false after `close` if unsettled: the harness, `load.json`, `close.json`.
- [x] A non-ruleset answer never replaces the one held: the harness, `status.json`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: the harness, `load.json`, `status.json`.
- [x] The stream (events, SSE parsing, refresh on change, a failed refresh ends the stream): the harness, `stream.json`, `load.json`, `status.json`; `LeafTest.sseSplitsAtAnyByteAndStillReadsTheSameEvents` and `sseIgnoresAnEventWithNoData`.
- [x] The loop and jittered backoff: the harness, `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`.
- [x] Status fields, states and `lastError` codes: the harness, `status.json` and the shape check on every status read.
- [x] Usage counts (what counts, summing, batches of 500, keep or drop by answer): the harness, `usage.json`, `rate-limit.json`; `ClientTest.usageStatusRulesKeepOrDropByAnswer`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: the harness, `hooks.json`; `ClientTest.aHookThatThrowsReachesOnErrorAndNotTheCaller`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`, `rate-limit.json`, `usage.json`, `status.json`; `ConcurrencyTest.manyThreadsEvaluateWhileCloseRacesThemAndAnotherClose`.
- [x] No context, key or attribute in any request or `onError` message, no header beyond the protocol's: the harness (`privacy.json` and the checks after every scenario); source search for headers: `grep -rn '\.header("' sdks/java/src/main` finds only `Authorization`, `If-None-Match`, `Accept` and `Content-Type` (`Http.java:179`, `ToggleSlashClient.java:653`, `:720`, `:867`); source search for logging: no `System.out`, `System.err` or logger in `src/main`; every error value is a `ToggleSlashException` whose message is built from a status or a code only. The header table above lists what the JDK client adds.
- [x] Many threads, every operation, `close` racing, two `close` calls racing: `ConcurrencyTest.manyThreadsEvaluateWhileCloseRacesThemAndAnotherClose` (workers call `boolVariation`, `stringVariation`, `doubleVariation`, `intVariationDetail`, `variation`, `status` and `ready`, with `close` racing from two threads, and a bound on every wait). Java has no race detector; this stress test stands in.
- [ ] SHOULD: an `https` URL reaches the TLS path in a test with a local certificate. Not met: the TLS path is untested. The SDK uses the JDK's default `HttpClient` TLS, with no custom `SSLContext`, and building a keystore for a local certificate in the tests was left out.
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a usage flush hangs: `ConcurrencyTest.evaluationStaysFastWhileTheRulesetRequestAndTheUsageFlushHang` (close under 1.5 s).
- [x] One ruleset or the other across a swap, `etag` and `flagVersion` paired: `ConcurrencyTest.aHookSeesTheEtagAndTheFlagVersionOfOneRulesetAcrossASwap`; `ClientTest.hookEventsCarryTheEtagAndTheFlagVersionOfTheRulesetUsed`.
- [x] Callbacks that call back into the client do not deadlock, and `ready` from inside one returns without waiting: `ConcurrencyTest.callbacksMayCallBackIntoTheClient`.
- [x] A stream with no bytes for 60 s is `timeout`, and a keep-alive restarts it, through a seam on a real loopback socket: `ConcurrencyTest.aQuietStreamEndsWithTimeoutWhenTheWatchdogExpires` and `keepAlivesRestartTheWatchdogAndDoNotMoveLastSyncAt` (the `Options` watchdog seam sets the length).
- [x] `Retry-After` above 300 held for 300 s on the ruleset, stream and events routes, through the clock seam: `ClientTest.aRetryAfterAboveThreeHundredIsHeldForThreeHundredOnTheRulesetRoute`, `...OnTheStreamRoute`, `...OnTheEventsRoute`; parsing in `aRetryAfterIsReadOnlyAsWholeSeconds`.
- [x] Counts either side of a UTC midnight go out as two days: `ClientTest.countsEitherSideOfUtcMidnightGoOutAsTwoDays`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`: `ClientTest.anEvaluationThatRaisesGivesTheFallbackAndOneOnError` (through the `evaluator` seam; no count is recorded).
- [x] Hook futures: n/a: the hook is a plain `Consumer<EvaluationEvent>`, which returns no future. A hook that raises at once is `hooks.json` and `ClientTest.aHookThatThrowsReachesOnErrorAndNotTheCaller`.
- [x] A number outside signed 64 bits is `TYPE_MISMATCH` on `intVariation`: `ClientTest.accessorsApplyTheMismatchRuleAndTheInt64Range` and the harness `huge` scenario.
- [x] A client that is never closed does not keep the process alive: the sync and flush threads are daemon by construction (`SdkThread`, `setDaemon(true)`, started at `ToggleSlashClient.java:112-115`); `ClientTest.daemonThreadsDoNotKeepTheProcessAlive`.
- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` links to the mapping table: `DESIGN.md#mapping-table` (`README.md`, "The API", links to it); `test-service/README.md` has the accessor table.
- [x] Names keep their canonical meaning and defaults, and reason kinds, error codes, stream states and `lastError` codes keep their spelling: `DESIGN.md#mapping-table` (enums `ReasonKind`, `ErrorCode`, `StreamState`, with `value()` giving the lower-case spelling; `lastError` stays a string).
- [x] No platform regex in the evaluator: `grep -rnE 'java\.util\.regex|\.split\(|replaceAll|replaceFirst|\.matches\(' sdks/java/src/main` finds only a comment in `Pattern.java:8` that says there is none.
- [x] The lint configuration names its rule set and each ignored rule carries a reason: `checkstyle.xml` (header comment and a reason beside each exclusion); `bin/lint` exits 0.
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none ("Runtime: none", `DESIGN.md`, "Dependencies").
- [x] No telemetry and no extra header: the header table above and the source search in the privacy line.
- [x] Every extra beyond the contract is one contract 13 allows: the package-private seams in `Options` (`clock`, `jitter`, `watchdog`, `evaluator`) and the typed accessors with a generic method; none weakens contract 5 or sends anything new.
- [x] Seams are options on `create`, never a global or an environment variable: `Options` fields set by the tests; a search for `System.getenv`/`System.getProperty` in `src/main` finds nothing the SDK reads.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and run with the toolchain `mise.toml` pins: each runs `mise exec -- ...` from `sdks/java`.
- [x] `test-service/README.md` maps every accessor of the protocol (`bool`, `string`, `number`, `int`, `json`, `any`) to an SDK method: its table.
- [x] `.github/workflows/sdk-java.yml` follows the skeleton, filters on its paths and pins every action by commit SHA: `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.
- [x] No em dash or en dash: `bun scripts/check-dashes.ts` passes.
- [x] `bun run format:check` passes: `All matched files use the correct format.`
- [x] Only `sdks/java/` and the one workflow file changed: the phase 6 scope check prints `scope ok`.
- [x] No package published and no name reserved: nothing was uploaded and no tag was made; `README.md` installs with `mvn install` from a checkout.
- [x] Commit subjects are Conventional Commits, none `feat`, with no `Co-Authored-By` or AI attribution: `chore(sdk-java): the Java server SDK regenerated cold from the authoring prompt`.
- [x] The workflow job is `runs-on: ubuntu-latest` with no `if:` guard, triggers only `pull_request` and `push`, installs its own toolchain (`jdx/mise-action` with `java maven checkstyle`), and `check-workflows.ts` passes.
- [x] `RUN.md` follows `RUN-TEMPLATE.md` with evidence for every line: this file.
