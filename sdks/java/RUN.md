# Java SDK: run report

## Prompt version

- Prompt: 26598eee3b61a0b44edc1c6d50c5d4e4ad08b813
- Model: Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-12
- Input: language Java; runtime chosen: Temurin 25.0.5 LTS (the build), run on the JVM; minimum version chosen: Java 17 (`--release 17`, per `language-notes/java.md` line 6); package manager chosen: Maven
- Toolchain: `mise.toml` pins `java = temurin-25.0.5+8.0.LTS` (`java -version` prints `openjdk version "25.0.5" 2026-10-20 LTS`), `maven = 3.10.0` (`Apache Maven 3.10.0`), `checkstyle = 14.4.0` (`Checkstyle version: 14.4.0`), `google-java-format = 2.0.0` (`google-java-format: Version 2.0.0`). `pom.xml` pins `org.junit.jupiter:junit-jupiter` 6.1.3 (test scope; the only dependency), `maven-clean-plugin` 3.5.0, `maven-resources-plugin` 3.5.0, `maven-compiler-plugin` 3.16.0 (`--release 17`, `-Xlint:all -Werror`), `build-helper-maven-plugin` 3.8.0, `maven-surefire-plugin` 3.6.1, `maven-jar-plugin` 3.5.1. Coordinates: group `com.github.madangryscientist.toggleslash`, artifact `toggleslash-sdk-java`, version 0.0.0 (never published).

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`)
- Conformance: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (run twice from the repository root after the last change to the tree, same line both times)
- Capabilities advertised: `integer-accessor`
- Native tests: 44 passed of 44 (0 failures, 0 errors) in the one Maven module, which compiles the test service into the test source root and has no tests of its own. By class: `ClientTest` 20, `StreamTest` 5, `HoldTest` 4, `TlsTest` 1, `JsonTest` 6, `Utf8Test` 7, `VectorsTest` 1. `VectorsTest.everyVectorAndPin` is one JUnit test that loops over 1371 vectors and 17 pins, which have their own line above.
- Ten runs of `bin/test` in a row, from the committed tree: all ten exit 0, each 44 tests, 0 failures (12 to 13 s each). One more run with `nproc` = 12 busy shell loops (`while :; do :; done`, normal priority) started before `bin/test` and kept running to its end, build included: exit 0, 44 tests, 0 failures, 19 s.

## Decisions

1. Redirects are never followed (`HttpClient.Redirect.NEVER`): the ruleset URL is the operator's own server, and a redirect would carry the `Authorization` header somewhere new (contract 7, 12).
2. HTTP/1.1 only (`HttpClient.Version.HTTP_1_1`): the stream is plain SSE and the contract names no HTTP/2 behaviour; this also keeps the JDK from adding `Upgrade`/`HTTP2-Settings` headers to a cleartext request (see the header table).
3. CI toolchain: `jdx/mise-action` with `install_args: java maven checkstyle google-java-format` over `actions/setup-java`, because it installs the two lint tools from `mise.toml` too. Settled by `language-notes/java.md` line 24.
4. Checkstyle runs as a bare JAR resolved inside `mise exec` (`mise exec -- sh -c 'command -v checkstyle'`), settled by `language-notes/java.md` line 14. `google-java-format` is run with `--dry-run --set-exit-if-changed`, settled by line 23; a question-style Javadoc fails `SummaryJavadoc`, also line 23, and I reworded one (`Clauses.matches`).
5. Lint: `checkstyle.xml` names its rule set (a Google-style core with Javadoc rules) and gives a reason beside every rule it leaves out. It reads `src/main/java` only: the tests and the test service are covered by google-java-format and by `-Xlint:all -Werror`, since Javadoc and naming rules add noise to test methods without catching bugs. `JavadocMethod` uses `accessModifiers=public,protected` because package-private helpers are documented by their names. Line length 100.
6. `StreamState` constants are lowercase to match the contract's spelling (`off`, `connecting`, `connected`, `disconnected`, `closed`): the public spelling is the contract's, not Java's convention.
7. JSON nesting limit 1000 (`JsonTest.nestingHasALimit`): contract 7 "Bounds" names no depth limit, so this is only a stack guard against a hostile ruleset and a context, and a parse past it is `bad_ruleset`.
8. A context is converted once and eagerly into the evaluator's own tree, with a depth and cycle guard and an `Unsupported` sentinel for a value with no JSON form (`ClientTest.anEvaluationThatRaisesAnswersEvaluationFailedWithOneOnError` feeds a hostile map). A map that changes during evaluation cannot change an answer.
9. `Reason` carries `ruleIndex` and `flagKey` beside the kind and error code: contract 4 names them as reason fields.
10. `EvaluationEvent.flagVersion` above 2^63 - 1 is reported as none; settled by contract 10, the `flagVersion` row ("MAY report a version above 2^63 minus 1 as none").
11. A served JSON tree is read-only (`Kinds.freeze`, test `aServedJsonTreeIsReadOnly`): a caller's mutation must not change the next caller's answer.
12. `ready()` waits at most `requestTimeout` plus 1 s for the first load, then settles with whatever is held (`ClientTest.readyAnswersFalseWhenTheRulesetNeverComes`): contract 5.3 requires a bound and names none.
13. The generic accessor's class table: `Boolean`, `String`, `Long`/`Integer`/`Short`/`Byte`, `Double`/`Float`, `Map`/`List`, null; any other class (`BigDecimal`, tested) gives the fallback and `TYPE_MISMATCH`. Settled by contract 4, "Typed languages", and `CHECKLIST.md` line "A fallback of a class with no JSON kind".
14. A hook's returned `CompletionStage` is watched with `whenComplete`; a plain `Future` is read only if already done (never blocked on). A failure of either goes to `onError`.
15. Test seam: `Tuning` (watchdog length, clock, injected `HttpClient`) is a package-private record taken by the package-private `Client.create(Options, Tuning)`. It is an option on create, never a global or an environment variable the SDK reads.
16. Holds are monotonic and capped at 300 s in `Hub.retryAfter`; the ruleset and stream routes share one hold and the events route has its own (contract 7.4, 9; test names under the checklist).
17. Layout: the test service sits under `sdks/java/test-service/src` and is compiled by `build-helper-maven-plugin` as a test source root, so one `mvn` build covers it and `bin/test-service` runs it from `target/`.
18. The prior Java SDK was removed in this task's first commit and I did not read it, in the tree or in git history, as the brief requires. I read the committed sibling SDKs only as the prompt allows.
19. No question was put to the planner this run: nothing the prompt told me to assume and record turned up. (Zero `Asked the planner:` entries.)

## Guesses

None.

## Stuck points

None.

## Proposed prompt changes

None. Every tool fact this run learned (Checkstyle as a bare JAR, `--set-exit-if-changed`, the `SummaryJavadoc` period, `jdx/mise-action`) is already in `language-notes/java.md` (lines 14, 22, 23, 24), and the run followed it. Two things the notes do not say, offered for `language-notes/java.md` only: `-Xlint:all -Werror` rejects `close()` called on an `AutoCloseable` that is also a try-with-resources variable ("explicit call to close()"), which bites a test that closes a client to test `close`; and Checkstyle's `JavadocMethod` takes `accessModifiers`, so a lint that should skip package-private methods needs `public, protected` there.

## Checklist

The two executable checks

- [x] `bin/test` runs every vector but `buckets.json` and every pin, all pass, count equals the phase 2 command's: `VectorsTest.everyVectorAndPin` reads `../../fixtures/evaluation` at test time; `bin/test` prints `vectors: 1371 pins: 17`.
- [x] `bin/test` 10 in a row plus one CPU-loaded run, counts in Results: ten runs of 44 tests, 0 failures; loaded run (12 busy loops, started before, running through the build and tests) 44 tests, 0 failures. The tree did not change after the ten.
- [x] Harness exits 0, only the 2 named vector skips: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`. With `integer-accessor` advertised, no scenario is skipped.

What the harness checks

- [x] Both context shapes: harness `privacy.json` and the `kinds.json` vectors, passing in the run above.
- [x] Result order and mismatch rule: harness `evaluation.json`, `usage.json`.
- [x] `integer-accessor` fraction and `2.0`: harness `evaluation.json`; native `ClientTest.integerAccessorsCheckTheRange`.
- [x] Nothing raises: the harness answers 500 on any raise, and no scenario failed.
- [x] `NOT_READY` at once before the first load: harness `load.json`; native `beforeTheFirstLoadTheAnswerIsNotReadyAndNothingBlocks`.
- [x] `ready` bounded, repeatable, false after `close`: harness `load.json`, `close.json`.
- [x] A non-ruleset never replaces the held one: harness `status.json`.
- [x] Ruleset request, `If-None-Match`, ETag, URL joining: harness `load.json`, `status.json`.
- [x] Stream events, SSE parsing, refresh on change: harness `stream.json`, `load.json`, `status.json`; native `aChangedEventFetchesTheNewRuleset`.
- [x] Loop and jittered backoff: harness `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: harness `rate-limit.json`.
- [x] Status fields, states and `lastError` codes: harness `status.json` and the shape check.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`.
- [x] Hook events and `onError`: harness `hooks.json`.
- [x] `close` ordering, twice at once, during a hold, final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`.
- [x] Privacy (no context, key or attribute in a request or `onError` message, no header beyond the protocol's): harness `privacy.json` and the per-scenario checks; native `requestsCarryOnlyTheKeyAndNeverTheContext` (a sentinel context value never appears in any request, and the headers are an allow-list). Source search for logs and error values: `grep -rn "System\.\(out\|err\)\|Logger\|printStackTrace\|\.getMessage()" src/main` prints nothing, so the SDK writes no log line. Every `new SdkException(` in `src/main` (`Client.java:321`, `Usage.java:141,189,192,199`, `Sync.java:146,371`) builds its message from a fixed word, an exception class name (`getSimpleName`), an error code or an HTTP status, never from a context, key or attribute.

Native tests

- [x] Every operation from many threads, `close` racing `variation`, two `close` calls racing, no error or deadlock: `ClientTest.closeRacingVariationAndRacingClosesEndCleanly` (20 rounds, each waiting for the workers to make progress, with a bound on the wait). Java has no race detector, so this stress test stands in.
- [x] SHOULD, an `https` URL reaches the TLS path with a local certificate: `TlsTest.loadsOverTlsWhenTheCertificateIsTrustedAndRefusesItWhenNot` (a `keytool` PKCS12 certificate and `HttpsServer`: a trusting injected client loads, the default client gets `lastError` `network`).
- [x] `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs: `ClientTest.readyAnswersFalseWhenTheRulesetNeverComes` and `closeAbandonsAHangingRulesetRequest` (ruleset), `aSlowFlushDoesNotBlockVariation` (flush).
- [x] A reader sees one ruleset across a swap: `ClientTest.aRulesetSwapNeverShowsATornPair` (the hook's `etag` and `flagVersion` belong together under concurrent swaps; the swap is one `AtomicReference` write of a `Core` record).
- [x] A hook and an `onError` that call back into the client do not deadlock, and `ready` from inside returns: `ClientTest.callbacksMayCallBackIntoTheClient` calls `status`, `variation` and `ready` from both callbacks. `ready` is bounded by `requestTimeout` plus 1 s (decision 12), so even a call made before the first load settles cannot wait forever.
- [x] A stream quiet for 60 s drops as `timeout`, a keep-alive restarts it, through a seam on a real quiet loopback socket: `StreamTest.aQuietStreamTripsTheWatchdogAndReconnects` and `keepAliveCommentsHoldTheWatchdogOffWithoutBreakingConnection` (`Tuning.watchdog`).
- [x] `Retry-After` above 300 held for 300 s on the ruleset, stream and events routes through a seam: `HoldTest.retryAfterReadsWholeSecondsAndCapsAtThreeHundred`, `aRulesetAnswerOf429HoldsTheRulesRoutesAtMostThreeHundredSeconds`, `aStreamAnswerOf429HoldsTheRulesRoutesToo`, `anEventsAnswerOf429HoldsOnlyTheEventsRoute` (`Tuning.clock`).
- [x] Counts either side of a UTC midnight go out as two days: `StreamTest.usageSplitsAtUtcMidnight` (a fake `Clock`).
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError`: `ClientTest.anEvaluationThatRaisesAnswersEvaluationFailedWithOneOnError`.
- [x] A hook's future that fails later goes to `onError` and changes neither answer nor count: `ClientTest.aHookSeesTheEvaluationAndItsFailuresGoToOnError` (a thrown exception and a failed `CompletableFuture`).
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: harness `huge` scenario in `evaluation.json`; `Kinds.integer` checks `d >= -2^63 && d < 2^63`, and `ClientTest.integerAccessorsCheckTheRange`.
- [x] A client never closed does not keep the process from exiting: every SDK thread is a daemon `SdkThread`; `ClientTest.daemonThreadsOnlyAndNoThreadOutlivesClose`.
- [x] A typed accessor that admits null, given a null fallback and served another kind, answers `TYPE_MISMATCH`: `ClientTest.aStringAccessorWithANullFallbackAcceptsOnlyAString` (`stringVariationDetail("count", ctx, null)` on a served number gives value null and `TYPE_MISMATCH`; `Client.stringVariationDetail` asks for `Want.STRING`, not `Want.ANY`). A null fallback on the generic or JSON accessor accepts any kind (contract 4, line 112).
- [x] Generic accessor with an integer fallback type: `ClientTest.integerAccessorsCheckTheRange` (served `2.0` gives 2 as `Integer`, `Short`, `Byte` and `Long`; a fraction or out-of-range value gives the fallback and `TYPE_MISMATCH`); a class with no JSON kind: `genericAccessorsHandleNullFallbacksAndClassesWithNoJsonKind` (`BigDecimal`).
- [x] The hook event's `value` for the integer accessor has the type the mapping table names: `Long` for `intVariation` (DESIGN.md mapping table, row `accessor shape`); `integerAccessorsCheckTheRange` and the `hooks.json` scenarios.
- [x] A context attribute of each numeric type matches as the JSON number of its value: `ClientTest.contextAttributesOfEveryNumericTypeMatchAWholeNumber` (`int`, `long`, `short`, `byte`, `double`, `float`, `BigDecimal`, `BigInteger`).

Code and documents

- [x] `DESIGN.md` has the mapping table with a row for every contract 2 name, and `README.md` links to it.
- [x] Names, defaults and spellings are canonical: reason kinds, error codes, stream states (lowercase constants) and `lastError` codes kept as the contract spells them; the harness's status shape check passes.
- [x] No regular expression from the platform in the evaluator: `grep -rn "java.util.regex\|Pattern\.compile" src/main/java/.../eval` finds nothing (the only `Pattern` hits are `PatternVm`, the hand-written engine). `Main.java` of the test service uses `java.util.regex` for its own route table, which is not the evaluator.
- [x] The lint configuration names its rule set and each ignored rule carries a reason: `checkstyle.xml`.
- [x] Every runtime dependency has a `DESIGN.md` line: there is none (`DESIGN.md`, "Dependencies": "Runtime: none").
- [x] No telemetry and no extra header: the request-header source search (`grep -rn '\.header(\|setHeader' src/main`) finds `Authorization` (`Hub.java:397`), `Content-Type` (`Usage.java:181`), `Accept: text/event-stream` (`Sync.java:234`) and `If-None-Match` (`Sync.java:161`, the conditional ruleset request). Captured on a loopback listener with the SDK's test service (one request per route, the key shown as `<key>`):

  | Route | Headers the SDK sets | Headers the JDK `HttpClient` added |
  | --- | --- | --- |
  | `GET /sdk/v1/ruleset` | `Authorization` | `Host`, `User-Agent: Java-http-client/25.0.5` |
  | `GET /sdk/v1/ruleset` (conditional) | `Authorization`, `If-None-Match: "e1"` | `Host`, `User-Agent: Java-http-client/25.0.5` |
  | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | `Host`, `User-Agent: Java-http-client/25.0.5` |
  | `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | `Host`, `User-Agent: Java-http-client/25.0.5`, `Content-Length: 73` |

  Why none identifies a user or a machine: `Host` is the operator's own server address, which the operator already knows; `User-Agent` is the library name and the JDK version only (no host name, user name or machine id, and the same on every machine running that JDK); `Content-Length` is the size of a body that holds only flag keys, variation indexes and a day. The client sends no `Accept-Encoding`, `Connection` or `Upgrade` header (HTTP/1.1 only, decision 2).
- [x] Every extra is one contract 13 allows (the `Tuning` seam, read-only JSON, the bounded `ready`, `close`); none weakens contract 5 or sends anything new.
- [x] Seams are options on create, not globals or environment variables: `Tuning` via the package-private `Client.create(Options, Tuning)` (decision 15); a grep for `System.getenv` and `System.getProperty` in `src/main` finds nothing.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and use only the toolchain `mise.toml` pins (`mise exec --`).
- [x] `test-service/README.md` maps every accessor (`bool`, `string`, `number`, `int`, `json`, `any`) to a Java method, and names the capability `integer-accessor`.
- [x] `.github/workflows/sdk-java.yml` follows the skeleton, filters on its paths (including the fixtures, the contract, the protocol and the harness), and pins every action by commit SHA (copied from `ci.yml`, plus `jdx/mise-action@b44d12e7d0d8ee7461bb893e1b1993500433185b`, v5.2.7).
- [x] A `status` read racing a stream failure never shows that failure's `lastError` beside `connected`: `StreamTest.aStatusReadNeverShowsAFailureBesideConnected` (3 reader threads for 3 s). Java has no race detector; status is one immutable snapshot built under the `Hub` lock.

Repository

- [x] No dash: `bun scripts/check-dashes.ts` exits 0 after `git add -A`.
- [x] `bun run format:check` passes ("All matched files use the correct format").
- [x] Only `sdks/java/` and `.github/workflows/sdk-java.yml` changed: the phase 6 scope check prints `scope ok`.
- [x] Nothing published and no name reserved.
- [x] One Conventional Commit, `chore(sdk-java): the Java server SDK regenerated cold from the authoring prompt`, not `feat`, no `Co-Authored-By` and no AI attribution line; the wip commits are squashed away.
- [x] The workflow job is `runs-on: ubuntu-latest` with no `if:`, triggers `pull_request` and `push` only, installs its own toolchain; `bun scripts/check-workflows.ts .github/workflows/*.yml` prints `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` has every template section (the phase 6 section check prints nothing) and evidence for each line.
