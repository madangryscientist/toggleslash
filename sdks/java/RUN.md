# Java SDK: run report

## Prompt version

- Prompt: 29ae975f1b5207f76a6830716a1d7803ee8ab587 (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run). This is run 21 of the sweep and the first at this prompt.
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`)
- Date: 2026-11-12
- Input: language Java; runtime chosen: Java 17 or newer at run time (language notes); minimum version chosen: 17 (`--release 17`); package manager chosen: Maven
- Toolchain (`mise.toml`, and what the version commands print): Temurin 25.0.5+8 LTS (`java -version`: `openjdk version "25.0.5" 2026-10-20 LTS`), Maven 3.10.0, Checkstyle 14.4.0 (`--version`: `Checkstyle version: 14.4.0`), google-java-format 2.0.0 (`google-java-format: Version 2.0.0`). Build file pins (`pom.xml`): JUnit Jupiter 6.1.3, maven-resources-plugin 3.5.0, maven-compiler-plugin 3.16.0 (`-Xlint:all -Werror`), build-helper-maven-plugin 3.8.0, maven-surefire-plugin 3.6.1. Bun 1.4.2 for the harness.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`, equal to the phase 2 count command's `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped` (twice in a row in phase 4, and once more in phase 6)
- Capabilities advertised: `integer-accessor`
- Native tests: 45 passed of 45 (JUnit, six classes: `ClientTest` 21, `TransportTest` 4, `JsonTest` 12, `SourceAuditTest` 4, `SseParserTest` 3, `VectorTest` 1). `VectorTest` is the one test that loops over the 1371 vectors and 17 pins.
- Repeat runs of `bin/test` on the tree that is committed: 10 of 10 passed in a row (17 to 18 s each once warm, 30 s for the first), then the eleventh run with 12 busy shell loops (`nproc` is 12) started before it and running until it ended, build included: passed (23 s). No test needed a fix for load.
- `bin/lint`: `lint ok` (google-java-format `--dry-run` over `src/main/java`, `src/test/java` and `test-service/src`; Checkstyle over `src/main/java`, no findings). No tool printed a warning about itself.

## Decisions

1. **Build tool and test framework.** Maven and JUnit Jupiter, the mainstream pair (DESIGN.md section 6). Plugins pinned in `pom.xml`; `-Xlint:all -Werror` on the compiler so a warning fails the build; surefire runs with `failIfNoTests`, `trimStackTrace` off and `-Xss1m` so a deep-nesting test runs on a normal stack.
2. **No runtime dependency.** The JDK has HTTP, SHA-256, time and concurrency; JSON, UTF-8 and SSE are hand-written because the spec's rules (WHATWG decoding, lone surrogates, `1e999`) differ from the JDK's (language notes; DESIGN.md sections 4 and 5). So DESIGN.md has no per-dependency line to give, only the statement that there are none.
3. **Lint.** google-java-format 2.0.0 for layout (1.37.0 cannot run on JDK 25, language notes) and Checkstyle 14.4.0 for the public API (`checkstyle.xml` names every enabled rule and gives a reason beside each rule it leaves out). The Checkstyle JAR is resolved inside `mise exec` as the language notes say. A Checkstyle 14 finding the run met: a Javadoc first sentence must end in a period (`SummaryJavadoc`), which a question form does not.
4. **Workflow toolchain step.** `jdx/mise-action` limited to `java maven checkstyle google-java-format` over the official `actions/setup-java`, because `bin/lint` also needs Checkstyle and google-java-format, which only mise installs, and one pin file (`mise.toml`) then serves the machine and CI. The sibling `sdk-ruby.yml` gave the inputs; the SHA was resolved with `gh api repos/jdx/mise-action/commits/v5.2.7`.
5. **`bin/test` and the test service build order.** `bin/test` runs `mvn test-compile` (main classes, then the test sources, which include the test service because `build-helper` adds `test-service/src` as a test source root), then `surefire:test`. So the test service is compiled after the SDK in the same step, and it is the last thing built. `bin/test-service` runs the prebuilt classes and builds only if they are missing.
6. **Concurrency model.** One immutable `State` record in an `AtomicReference` (ruleset, ETag, `lastSyncAt`, stream state, `lastError`), so the ruleset and ETag swap together and `status()` is one snapshot; no `synchronized`; a `ReentrantLock` only around usage flushes (DESIGN.md section 2).
7. **Closing.** The JDK `HttpClient` has no `close` before 21, so in-flight ruleset and stream futures are kept and cancelled (language notes). `close` from an SDK thread starts a helper thread and returns, so a callback can call it.
8. **Watchdog.** A daemon `ScheduledThreadPoolExecutor` thread closes the stream's `InputStream` when 60 s pass with no bytes; the sync thread then sees end of stream and, finding the flag set, calls it `timeout`. The 60 s is a package-private seam (`Options.streamIdle`).
9. **Seams.** `Options` has package-private setters `streamIdle`, `clock`, `random`, `evaluation` and `sslContext`; none is a global or an environment variable (a source search finds no `System.getenv` or `System.getProperty` in `src/main/java`: `SourceAuditTest.seamsAreNeverGlobalsOrEnvironmentVariables`).
10. **Typed accessors and the fallback.** `longVariation` is the integer accessor and uses `long`; `doubleVariation` takes a `double`. `jsonVariation` takes a `Json`, and a null or `Json.NULL` fallback accepts any served kind (contract 4). A typed accessor with a `String` fallback that is null still requires the served value to be a string.
11. **The service's accessors.** `int` calls `longVariationDetail`; the value is written to the response through a `double`, so a served integer above 2^53 loses precision in the response, the same limit the JSON number type has everywhere else in this SDK. A fallback of the wrong kind for a typed accessor answers 400 (the Java signature cannot take it).
12. **Hook value.** `EvaluationEvent.value` is the `Json` value the evaluator produced (the served value or the fallback converted to `Json`), not the typed object the caller gets.
13. **Redirects** are never followed; a 3xx is `http_<status>` (contract 7.1 leaves it open).
14. **HTTP version.** Forced to HTTP/1.1, because the JDK's default HTTP/2 upgrade on plain `http` adds `Upgrade`, `HTTP2-Settings` and a second `Connection` value, headers this SDK does not want. The header audit below shows the result.
15. **Nesting limit.** The JSON parser refuses nesting deeper than 1000 levels as `bad_ruleset` (contract 7.1 requires at least 512). `JsonTest.deepNestingIsRefusedNotCrashed` checks 5000 is refused and 600 parses. No other limit was added: no cap on a body, a line or an event.
16. **A timer test cannot be made load-proof by waiting a fixed time**, so the native tests wait on conditions (`Support.eventually`) with generous limits, and the two watchdog tests assert against the clock only in the safe direction (not dropped before the seam's length; dropped within 8 s).
17. **Test text.** The tests are in `src/test/java/.../` (`ClientTest`, `TransportTest`, `JsonTest`, `SseParserTest`, `SourceAuditTest`, `VectorTest`, with the helpers `FakeServer`, `Support`, `UnclosedClientMain`).

## Guesses

1. What "builds the test-service output last" means for a build tool that compiles main and test sources in two steps. Where: PROMPT.md, "Entry points" (`bin/test`). Closest sentence: "`bin/test` builds every time ... and builds the test-service output last". It does not say whether a single test-compile step that compiles the test service with the test classes counts. Assumed: it does, because the test service is built after the SDK's main classes and is the last artifact `bin/test` produces before running tests (Decision 5).
2. How a served integer outside 2^53 should be written back by the service's `int` accessor. Where: sdk-test-service.md, "Accessors". Closest sentence: the one saying `int` answers the integer value. It does not say whether the value is written as an exact integer or through a double. Assumed: through a double (Decision 11); no scenario or vector reaches the difference.
3. Whether a package-private seam on `Options` counts as "an option on create". Where: CHECKLIST.md, "Code and documents" (seams). Closest sentence: "Test seams are options on `create` (which may be internal to the package ...)". It names a field of the SDK's own config; Java's `Options` is a public class with package-private setters, which is the same thing but not stated. Assumed: it counts.

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `PROMPT.md`, "Entry points", after "builds the test-service output last", add: "(a build tool that compiles the test service in the same step as the test sources meets this, as long as the SDK's main sources are built before it)".
2. For guess 3, in `CHECKLIST.md`, the seams line, after "the package's tests set, as `sdks/go`'s tests set `cfg.idle`, is one)", add: "a package-private setter on the public options type is one too".
3. For Decision 3, in `language-notes/java.md`, add: "`google-java-format --dry-run <files>` prints the files it would change and exits 0, so `bin/lint` must test for output, not for the exit code, or pass `--set-exit-if-changed`. The mise build is a native binary and runs on JDK 25. Checkstyle's `SummaryJavadoc` wants the first sentence to end in a period, so a question form (`/** Does ...? */`) fails."
4. For Decision 4, in `language-notes/java.md`, add: "`jdx/mise-action` with `install_args: java maven checkstyle google-java-format` installs the whole Java toolchain in CI, including the two lint tools that `actions/setup-java` does not provide."

## Checklist

- [x] `bin/test` runs every vector but `buckets.json` and every pin; count equals the phase 2 command's: `VectorTest` prints `vectors: 1371 pins: 17`; the phase 2 command printed `1371 17`.
- [x] `bin/test` 10 times in a row, and once under CPU load: 10 of 10 passed; the eleventh ran with 12 busy loops (`nproc` = 12) running before and throughout, build included, and passed (see Results).
- [x] Harness exits 0, with only the 2 named vector skips and no scenario skips (Java advertises `integer-accessor`): `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`.
- [x] Both context shapes: harness, `privacy.json`, `kinds.json` vectors; `Context.user`, `Context.of`, `Context.multi`, `Context.of(Json)`.
- [x] Results order and the mismatch rule: harness `evaluation.json`, `usage.json`; `ClientTest.aNullFallbackAcceptsAnyKindOnTheJsonAccessor`.
- [x] `integer-accessor`: harness `evaluation.json`; `ClientTest.theIntegerAccessorKeepsTheInt64Range` (`2.0` is 2, `1.5` is `TYPE_MISMATCH`).
- [x] No failure makes an operation raise: harness (every scenario); `ClientTest.manyThreadsUseEveryOperationWhileCloseRaces`.
- [x] `NOT_READY` at once: harness `load.json`; `ClientTest.notReadyGivesTheFallback`.
- [x] `ready` bounded and repeatable, false after close if unsettled: harness `load.json`, `close.json`; `ClientTest.readyOnAnSdkThreadReturnsAtOnceAndCloseFromOnErrorDoesNotDeadlock`.
- [x] A non-ruleset never replaces the one held: harness `status.json`; `Sync` accepts only an object with array `flags` and `segments`.
- [x] Ruleset request, `If-None-Match`, ETag kept exactly, URL joining: harness `load.json`, `status.json`; `TransportTest.headerAuditAndNoContextOnTheWire` shows the conditional request.
- [x] The stream: harness `stream.json`, `load.json`, `status.json`; `SseParserTest` (every split point).
- [x] The loop and backoff: harness `backoff.json`, `stream.json`.
- [x] 429 and `Retry-After`: harness `rate-limit.json`.
- [x] Status fields, states, `lastError` codes: harness `status.json`.
- [x] Usage counts: harness `usage.json`, `rate-limit.json`.
- [x] Hook events, `onError`, a hook that raises, an `onError` that raises: harness `hooks.json`.
- [x] `close` in order, twice at once, during a hold, with a final flush: harness `close.json`, `rate-limit.json`, `usage.json`, `status.json`.
- [x] No context in any request or `onError` message, and no header beyond the protocol's: harness `privacy.json`. Source search for logs and error values: `SourceAuditTest.nothingIsPrintedOrLoggedByTheLibrary` (no `System.out`, `System.err`, `printStackTrace` or `java.util.logging` in `src/main/java`) and `SourceAuditTest.noErrorValueIsBuiltFromContextData` (no line that builds a `ToggleSlashException`, calls `report(` or `fail(` mentions a context or attributes); `TransportTest.errorsAndStatusNeverCarryAContext`.
- [x] Many threads, every operation, `close` racing `variation`, two `close` calls racing: Java has no race detector, so the stress tests stand in: `ClientTest.manyThreadsUseEveryOperationWhileCloseRaces` (16 threads on every operation plus 4 closers racing them, bounded joins) and `ClientTest.twoClosesRace`.
- [x] SHOULD: TLS with a local certificate: `TransportTest.speaksTlsToALocalCertificateAndRefusesAnUntrustedOne` (a `keytool` PKCS12 store with SAN `ip:127.0.0.1`, an `HttpsServer`; trusted through the `sslContext` seam, refused without it as `network`).
- [x] `variation` and `variationDetail` bounded while a ruleset request hangs and while a flush hangs: `ClientTest.evaluationIsBoundedWhileARulesetRequestHangs` and `ClientTest.evaluationIsBoundedWhileAUsageFlushHangs` (2000 evaluations each within 1.5 s); `ClientTest.closeAbandonsAHangingRulesetRequest`, `ClientTest.closeKeepsToTwiceTheRequestTimeoutWhileRoutesHang`.
- [x] A hook's `etag` and `flagVersion` belong to one ruleset under concurrent swaps: `ClientTest.hookEtagAndFlagVersionBelongToTheSameRuleset` (the server answers version N with ETag `"vN"`, polled every 2 ms; at least 3 rulesets seen, zero unpaired).
- [x] Hook and `onError` calling back into the client; `ready` on an SDK thread returns at once (SHOULD): `ClientTest.aHookAndOnErrorMayCallBackIntoTheClient`, `ClientTest.readyOnAnSdkThreadReturnsAtOnceAndCloseFromOnErrorDoesNotDeadlock`.
- [x] The 60 s watchdog through a seam on a real quiet loopback socket, and a keep-alive restarts it: `ClientTest.theWatchdogDropsAStreamThatGoesQuiet` (400 ms seam, a real `HttpServer` that stops writing; `lastError` is `timeout`, then the client reconnects) and `ClientTest.aKeepAliveRestartsTheWatchdog` (comments every 150 ms keep a 700 ms watchdog quiet for 1.3 s, then it fires).
- [x] `Retry-After` above 300 held for 300 s on the ruleset, stream and events routes: `ClientTest.retryAfterIsCappedAt300SecondsOnTheRulesetRoute`, `...OnTheStreamRoute` (a 26-digit value), `...OnTheEventsRoute`, through the package-internal `syncHold()` and `usageHold()`; `retryAfterOnAnythingButA429IsIgnored`.
- [x] UTC midnight gives two days: `ClientTest.usageSplitsAtUtcMidnight` (a hand-moved clock seam; `2026-03-01=2` and `2026-03-02=1`).
- [x] An evaluation that raises gives the fallback, `EVALUATION_FAILED` and one `onError`: `ClientTest.aRaisedEvaluationGivesTheFallbackOneOnErrorAndNoCount`.
- [x] n/a: no futures. A hook is a plain `Consumer`, not a function returning a future.
- [x] Outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `ClientTest.theIntegerAccessorKeepsTheInt64Range` (`9223372036854775807` read as binary64 is 2^63, `1e19`, `-1e19`, `1e999`, all `TYPE_MISMATCH`; `4611686018427387904` and `-9223372036854775808` exact).
- [x] An unclosed client does not keep the process alive (SHOULD): `TransportTest.anUnclosedClientDoesNotKeepTheJvmAlive` (a subprocess JVM running `UnclosedClientMain` exits by itself within 20 s); every SDK thread is a `SdkThread`, which calls `setDaemon(true)` (`SdkThread.java:7`), and the watchdog executor's thread is a daemon too.
- [x] `DESIGN.md` passes the row check and `README.md` has the mapping table: `DESIGN.md` section 1; `README.md` "Mapping".
- [x] Mapping names keep canonical meaning and defaults, and closed sets keep their spelling: `ReasonKind`, `ErrorCode`, `StreamState` enums with the canonical words; `lastError` is exactly `http_<status>`, `timeout`, `bad_ruleset` or `network`.
- [x] No platform regex in the evaluator: `SourceAuditTest.theEvaluatorUsesNoPlatformRegex` (no `java.util.regex`, `Pattern.`, `.matches(`, `.split(`, `.replaceAll(` or `.replaceFirst(` in `evaluation/`); the evaluator has its own `PatternVm`.
- [x] Lint configuration names its rule set and each ignored rule carries a reason: `checkstyle.xml` (header comment).
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none (`DESIGN.md` section 4).
- [x] No telemetry and no identifying header; source search of `src/main/java` for `.header(`: `Http.java:57` `Authorization`; `Sync.java:137` `Accept: application/json`; `Sync.java:140` `If-None-Match`; `Sync.java:260` `Accept: text/event-stream`; `Usage.java:157` `Content-Type`. Header audit table below.
- [x] Every extra is one contract 13 allows: the only additions are package-private seams and `readyFuture()`; none weakens contract 5 or sends anything new.
- [x] Test seams are options or package-internal config, never a global or environment variable: Decision 9.
- [x] `bin/lint`, `bin/test`, `bin/test-service` exist, are executable, `cd` to the SDK and use only the mise toolchain: `#!/bin/sh`, `set -eu`, `cd "$(dirname "$0")/.."`, `mise exec --` when mise is on PATH.
- [x] `test-service/README.md` maps every accessor: `bool`, `string`, `number`, `int`, `json`, `any`.
- [x] The workflow follows the skeleton: `.github/workflows/sdk-java.yml`, paths filter, every action pinned by SHA with its tag, `ubuntu-latest`, no `if:`, triggers `pull_request` and `push`; `bun scripts/check-workflows.ts .github/workflows/*.yml` printed `8 workflow(s) read, 0 failure(s)`.
- [x] No em or en dash: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes (`All matched files use the correct format.`).
- [x] Scope check prints `scope ok` (phase 6).
- [x] No package published and no name reserved. Nothing was uploaded.
- [x] Commit subject `chore(sdk-java): the Java server SDK regenerated cold from the authoring prompt`, no `feat`, no `Co-Authored-By` and no AI attribution line.
- [x] The workflow job is `runs-on: ubuntu-latest` with no `if:`, triggers only `pull_request` and `push`, installs its own toolchain.
- [x] `RUN.md` follows `RUN-TEMPLATE.md` with every section.

### Header audit

One request of each route captured on a loopback listener by `TransportTest.headerAuditAndNoContextOnTheWire` (written to `target/headers.txt`), with the test asserting that every header name is either one the SDK sets or one of the client's defaults below, that every request carries `Authorization: Bearer <server key>`, and that no request body or header holds the context's key or attributes.

| Route | Headers received |
| --- | --- |
| `GET /sdk/v1/ruleset` | `accept`, `authorization`, `host`, `user-agent` |
| `GET /sdk/v1/ruleset` (conditional) | `accept`, `authorization`, `host`, `if-none-match`, `user-agent` |
| `GET /sdk/v1/stream` | `accept`, `authorization`, `host`, `user-agent` |
| `POST /sdk/v1/events` | `authorization`, `content-length`, `content-type`, `host`, `user-agent` |

Added by the JDK `HttpClient` on its own:

- `Host`: the URL's host and port, which the server already knows.
- `User-Agent`: `Java-http-client/<JDK version>`, the JDK's default. It names the library and its version and no host or user.
- `Content-Length`: the body length, on the one POST.

No `Accept-Encoding` and no extra `Connection` value is sent (the HTTP/1.1 choice, Decision 14).
