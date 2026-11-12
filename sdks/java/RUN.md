# Java SDK: run report

## Prompt version

- Prompt: e209913fe039e364f759bab5a756edc9b3c857e8
- Model: Claude Sonnet 5.5 (`claude-sonnet-5-5`), run as FS-167 "Java sweep run 25, cold regen at e209913"
- Date: 2026-11-12
- Input: language Java; runtime chosen: the JVM (Temurin); minimum version chosen: Java 17 (`--release 17`);
  package manager chosen: Maven (build tool; nothing is published)
- Toolchain: `mise.toml` pins `java = "temurin-25.0.5+8.0.LTS"` (`java -version` prints `openjdk version "25.0.5"
  2026-10-20 LTS`, `Temurin-25.0.5+8`), `maven = "3.10.0"` (`mvn -v` prints `Apache Maven 3.10.0`),
  `checkstyle = "14.4.0"` (`Checkstyle version: 14.4.0`) and `google-java-format = "2.0.0"` (`Version 2.0.0`);
  bun 1.4.2 (`bun --version`) for the harness and the repository checks. `pom.xml` pins maven-clean-plugin 3.5.0,
  maven-resources-plugin 3.5.0, maven-compiler-plugin 3.16.0, maven-surefire-plugin 3.6.1,
  build-helper-maven-plugin 3.8.0 and JUnit (`junit-jupiter`) 6.1.3, the only dependency, test scope.

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2
  count command prints the same two numbers)
- Conformance: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 41 passed of 41 (surefire, 14 classes, about 15 s). `VectorsTest.vectorsAndPins` is one of the 41
  and loops over 1371 vectors and 17 pins; no other test has subtests. The test service has no tests of its own.
  `bin/test` passed 10 times in a row (41 of 41 each time, 14 s to 15 s), and an eleventh time with 12 busy shell
  loops running (one per logical CPU, `nproc` = 12), started before `bin/test` and killed after it: 41 of 41, 29 s.
  The runs were of the final tree, after the fix in Decision 9.

## Decisions

1. **Runtime, minimum version and build tool: the JVM on Temurin, Java 17, Maven.** The brief gave only the language.
   Java 17 is the oldest LTS in security support and has records; `maven.compiler.release` 17 makes the compiler refuse
   any newer API whatever JDK builds. Development uses the newest LTS, 25.0.5. `DESIGN.md` has the table.
2. **Lint and format: google-java-format 2.0.0 and Checkstyle 14.4.0 over Spotless, PMD or SpotBugs.** Two pinned
   tools from mise, no Maven plugin, so `bin/lint` needs only the toolchain. 2.0.0 is the first release that runs on
   JDK 25 (`language-notes/java.md`, FS-151). `bin/lint` passes `--set-exit-if-changed` because `--dry-run` exits 0
   (java.md, FS-159) and runs Checkstyle from its bare jar, found inside `mise exec` (java.md, FS-153). The rule set
   and each excluded rule's reason are in the comment at the top of `checkstyle.xml`. The compiler runs `-Xlint:all`
   with `failOnWarning`, which also covers the tests.
3. **Tests: JUnit 6.1.3 with surefire**, the mainstream Java framework. The vector test is one test that loops.
4. **No runtime dependency.** The JDK has HTTP, SHA-256 and concurrency; JSON, a WHATWG UTF-8 codec and a Pike-VM
   regular expression engine are written in the SDK, because the spec's rules (binary64 numbers, lone surrogates,
   `-0.0`, no backtracking) need parsers that are ours. The evaluator uses no platform regex (Checklist below).
5. **`HttpClient` fixed to `HTTP_1_1`**: the server speaks 1.1, and an upgrade attempt over cleartext adds headers the
   audit would list. A request is bounded by waiting on its future, because `HttpRequest.timeout` ends at the response
   headers (java.md). There is no closable `HttpClient` before Java 21, so `close` cancels the in-flight futures.
6. **The integer accessor is `long`** (`longVariation`), boxed as `Long` in the hook event; the test service's `int`
   accessor calls `longVariation`, and `integer-accessor` is advertised. A fallback of `Integer`, `Short` or `Byte`
   in the generic method is an integer accessor bounded to that type (contract section 4, "Typed languages").
7. **A public `json` package** (`Json`, `JsonException`, `Utf8`) holds the parser and the UTF-8 codec, used by both
   the client and the evaluator; `Json.parse` lets a caller build a context from raw text. The nesting limit is 1024
   (the contract needs at least 512), with an explicit counter, so a deep document is `bad_ruleset`, not a stack overflow.
8. **Small points a document settles.** `ready` completes before `onError` is called (contract section 5.3: "no set
   order"; `lastError` is set before it settles). A leading byte order mark is skipped in the ruleset body (contract
   section 7.1, "a leading byte order mark is ..."). A prerequisite variation is not range-checked (evaluation.md,
   "Prerequisite": "so it is not range-checked here") and is clamped to `Integer.MAX_VALUE` only to store it. A
   `close` called on an SDK thread starts a `toggleslash-close` thread and returns (contract section 6.4, SHOULD).
   Patterns, semver ranges and instants are parsed once when the ruleset compiles, and parsed collections are
   unmodifiable. SSE strips a leading BOM from the stream. `bin/test` runs `clean` first, so no result is a cached
   one, and passes the vector line through `target/vectors.txt`. The CI job uses `jdx/mise-action` with
   `install_args: java maven checkstyle google-java-format` (java.md, FS-159).
9. **The ten runs restarted once, after a flaky `WatchdogTest`, and the fault was in the SDK, not only the test.**
   In the first ten, runs 2 and 3 failed: `aStreamThatGetsNoBytesForTheWatchdogIsDroppedAsATimeout` asserted the
   stream was not `connected` as soon as `lastError` read `timeout`, and read `connected` (a reproduction loop failed
   once in four tries). The SDK set `lastError` and then, a step later, `DISCONNECTED`, in two state changes. Now
   `Sync.streamFailed` sets both in one compare-and-set, so no reader sees the error beside a connected stream. This
   is a status snapshot that is now consistent, not a test assertion loosened. The ten then ran again from the first
   on the changed tree, and 12 more runs of the stream and callback tests were green before them.
10. **There is no race detector in the JDK**, so a stress test stands in: `ConcurrencyTest` (Checklist, first item
    under "Native tests").
11. **Test seams** are package-private setters on `Options.Builder` (`clock`, `jitter`, `watchdog`, `httpClient`,
    `evaluator`); nothing reads a global or an environment variable.
12. **An empty ETag counts as none**: `If-None-Match` is sent only when the ETag is non-empty, and `status` reports
    `null` for an empty one. The TypeScript SDK says so (`packages/sdk/src/sync.ts` line 172: "An empty ETag counts as
    none, so it is never sent back as If-None-Match").
13. **The old Java SDK was never read**, in the tree or in git history. Sibling SDKs in `sdks/` were fair reading and
    were used for the shape of `bin/lint`, the workflow and this report; no Java line came from them.

## Guesses

1. **Whether a status read may show a stream error beside `connected`.** Where: `docs/sdk-contract.md`, section 8
   (status). Closest sentence: "`disconnected`: the last stream attempt ended, by failing or by dropping, and the SDK
   is waiting to ..."; it defines the state, and does not say whether `lastError` and the new state change together.
   Assumed: they change in one step (see Decision 9), so `lastError` set by a stream failure is never seen with
   `stream` equal to `connected`.
2. **What an unrecognised fallback class gives in the generic accessor.** Where: `docs/sdk-contract.md`, section 4,
   the mismatch rule. Closest sentence: "A generic method whose result has the fallback's static type (Java, C#)
   treats a fallback of an integer type as an integer accessor"; it covers integer types and the JSON kinds, not a
   class such as `StringBuilder` or `Object[]`. Assumed: such a fallback always gives `TYPE_MISMATCH` with the
   fallback returned (`kindOf` answers "string" only for `String` and "array" only for `List`); test
   `AccessorsTest.anUnknownFallbackClassIsATypeMismatch`.
3. **The checklist item for `ready` called from inside a callback on the SDK's thread, with an unsettled `ready`.**
   Where: `docs/sdk-authoring/CHECKLIST.md`, "Native tests"; contract section 5.3 and 6.4. Closest sentence: "a `ready`
   called from inside such a callback on the SDK's own thread returns without waiting". Because `ready` completes
   before `onError` is called (Decision 8), an `onError` on the sync thread can never see an unsettled `ready`, and no
   other callback on that thread runs before the first attempt settles. Assumed: the case is unreachable for the
   hook and the load error, and the test calls `ready` and `close` from `onError` on the sync thread and the flush
   thread and checks they return at once (`CallbackTest`).

## Stuck points

None.

## Proposed prompt changes

1. For guess 1, in `CHECKLIST.md`, in the line "Status fields, states and `lastError` codes (contract 8)", add: "A
   failure that ends the stream sets `lastError` and the `disconnected` state in one change: a native test that waits
   for the `lastError` and then asserts the state must not see a connected stream."
2. For guess 2, in `CHECKLIST.md`, after "A generic accessor with an integer fallback type gives that type for a whole
   served number", add: "A fallback whose class is none of the language's JSON kinds (a string builder, an array of
   objects) gives `TYPE_MISMATCH` and the fallback."
3. For guess 3, in `CHECKLIST.md`, in the callback line, add: "n/a for the case an SDK cannot reach: where `ready`
   settles before the first `onError` call, say so in `RUN.md` and test that `ready` and `close` return at once from
   `onError` on each SDK thread."

## Checklist

- [x] `bin/test` runs every vector but `buckets.json` and every pin, and the count equals phase 2's:
  `sdks/java/bin/test` prints `vectors: 1371 pins: 17`; `evaluation.VectorsTest.vectorsAndPins` reads
  `fixtures/evaluation/*.json` from the repository at test time.
- [x] `bin/test` passes 10 times in a row and once under CPU load: 10 of 10 (41 tests each) on the final tree, then
  one run with 12 busy loops (`nproc` = 12) started first and killed after it, 41 of 41. Counts above; the first
  ten failed twice and were restarted after the fix in Decision 9.
- [x] The harness exits 0 with only the 2 named skips: `bun run conformance -- --service sdks/java/bin/test-service`
  exit 0, `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`.
- [x] Both context shapes accepted: the harness (`privacy.json`, `kinds.json` vectors); `Context` builder and any
  `Map` or JSON tree accepted by `Client` (DESIGN.md section 5).
- [x] Results follow the order of contract 4: the harness, `evaluation.json`, `usage.json`; `Client.detail` follows
  steps 1 to 5.
- [x] With `integer-accessor`, a fraction is `TYPE_MISMATCH` and `2.0` is `2`: the harness, `evaluation.json`;
  `AccessorsTest.aServedHugeNumberIsATypeMismatchOnTheIntegerAccessor`, `aWholeNumberWithinSigned64BitsIsServed`.
- [x] No failure makes an operation raise: the harness, every scenario.
- [x] Evaluating before the first load answers `NOT_READY` at once: the harness, `load.json`.
- [x] `ready` settles after the first attempt, bounded, may be called again, false after `close`: the harness,
  `load.json`, `close.json`.
- [x] An answer that is not a ruleset never replaces the one held: the harness, `status.json`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining: the harness, `load.json`,
  `status.json`; `HeadersTest` shows the conditional request.
- [x] The stream: events, SSE parsing, refresh on change, a failed refresh ends the stream: the harness,
  `stream.json`, `load.json`, `status.json`; `ParsingTest.anEventIsReadAcrossChunksAndLineEnds` and
  `commentsAndIncompleteEventsAreNotEvents`.
- [x] The loop and the jittered backoff: the harness, `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`; `ParsingTest.retryAfterCountsOnlyWholeDecimalSecondsAndCapsAt300`.
- [x] Status fields, states and `lastError` codes: the harness, `status.json`, the shape check on every status read.
- [x] Usage counts: the harness, `usage.json`, `rate-limit.json`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises: the harness, `hooks.json`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`,
  `rate-limit.json`, `usage.json`, `status.json`.
- [x] No context, context key or attribute in any request or `onError` message, and no header beyond the protocol's:
  the harness, `privacy.json`. Source search for logs and errors: `grep -rnE 'System\.(out|err)|Logger|printStackTrace'
  sdks/java/src/main` prints nothing (the SDK has no logger and writes no line); the messages passed to `onError` are
  built in `Sync`, `Usage` and `Client` from a code and a route name only (`"stream failed: " + code`,
  `"closed with N usage counts unsent: the server asked to wait"`).
- [x] Every operation from many threads, `close` racing `variation`, two `close` calls racing:
  `ConcurrencyTest.everyOperationFromManyThreadsWithCloseRacingVariationAndTwoClosesRacing` (16 workers, every
  operation, two racing closes, each worker must make progress after the rounds, and a bound on the wait). The JDK has
  no race detector; this stress test stands in (Decision 10). One test covers both races.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TlsTest.anHttpsUrlLoadsTheRulesetFollowsTheStreamAndSendsUsage`
  makes a PKCS12 key with `keytool` (SAN `127.0.0.1` and `localhost`), serves HTTPS on loopback, and uses an injected
  `HttpClient` that trusts it for the ruleset, the stream and usage; the default client refuses it and `lastError` is `network`.
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a usage flush
  hangs: `HangTest.variationReturnsWithinABoundWhileTheRulesetRequestHangs`, `variationReturnsWithinABoundWhileAUsageFlushHangs`.
- [x] A hook's `etag` and `flagVersion` belong to one ruleset under concurrent swaps:
  `SwapTest.aHooksEtagAndFlagVersionBelongToTheSameRulesetUnderConcurrentSwaps`.
- [x] A hook and an `onError` that call back into the client do not deadlock, and `ready` from inside returns at once:
  `CallbackTest.aHookThatCallsStatusAndVariationDoesNotDeadlock`,
  `anOnErrorOnTheSyncThreadCanCallStatusVariationAndReadyWithoutWaiting`,
  `aCloseFromAnOnErrorOnTheFlushThreadReturnsAtOnce` (see Guess 3).
- [x] A stream with no bytes for the watchdog is dropped as `timeout`, and a keep-alive restarts it, through a seam on
  a real loopback socket: `WatchdogTest.aStreamThatGetsNoBytesForTheWatchdogIsDroppedAsATimeout` and
  `aKeepAliveRestartsTheWatchdog` (watchdog set to 600 ms by `Options.Builder.watchdog`; the server socket goes quiet).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through the clock seam:
  `ClockSeamTest.aRetryAfterAbove300IsHeldFor300SecondsOnTheRulesetRoute` (and `...WhenPolling`), `...OnTheStreamRoute`,
  `aRetryAfterAbove300HoldsTheUsageReportsFor300SecondsAndNotLonger`.
- [x] Counts either side of a UTC midnight go out as two days: `ClockSeamTest.countsEitherSideOfAUtcMidnightGoOutAsTwoDays`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call:
  `ClockSeamTest.anEvaluationThatRaisesGivesTheFallbackEvaluationFailedAndOneOnErrorCall` (the `evaluator` seam).
- [x] n/a: no futures. The hook is a plain `void accept(EvaluationEvent)`; one that throws goes to `onError` (the
  harness, `hooks.json`).
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: the harness `huge` scenario;
  `AccessorsTest.aServedHugeNumberIsATypeMismatchOnTheIntegerAccessor` (2^63 and above, a huge exponent).
- [x] A client that is never closed does not keep the process from exiting: `LifecycleTest.theSdkThreadsAreDaemons`
  (the `toggleslash-sync` and `toggleslash-flush` threads are daemons, set at `Client.java` lines 57 and 59) and
  `aProcessThatNeverClosesItsClientExits`, which runs `NeverClosedMain` in a child JVM and waits for exit 0.
- [x] A typed accessor given a null fallback and served another kind is `TYPE_MISMATCH`:
  `AccessorsTest.aNullFallbackStringAccessorGivenAnotherKindIsATypeMismatch`,
  `theJsonAccessorComparesKindsAndANullFallbackAcceptsAny`.
- [x] A generic accessor with an integer fallback type: `AccessorsTest.aGenericAccessorWithAnIntegerFallbackGivesThatType`
  and `aGenericAccessorWithAnIntegerFallbackMismatchesAFractionOrAValueOutOfRange`.
- [x] The hook event's `value` for the integer accessor is a `Long`, as the mapping table names:
  `AccessorsTest.theHookEventValueIsTheBoxedValueTheCallerReceives`.
- [x] A context attribute of each numeric type matches as the JSON number of its value:
  `AccessorsTest.aContextAttributeOfEachNumericTypeMatchesAsTheJsonNumberOfItsValue` (byte, short, int, long, float,
  double, `BigInteger`, `BigDecimal`, `AtomicInteger`, `AtomicLong`).
- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` links to the mapping table: the row check printed
  nothing in phase 1; `README.md` section "Mapping from the contract" links `DESIGN.md` section 1.
- [x] Names keep the canonical meaning and defaults, and the closed sets keep their spelling: `DESIGN.md` section 1,
  "The closed sets keep their spelling" (`ReasonKind`, `ErrorCode` constants as the contract writes them; `StreamState`
  constants upper case with the contract's lower-case `wire()` and `toString()`; `lastError` codes as plain strings).
- [x] The evaluator has no platform regular expression: `grep -rniE 'java\.util\.regex|Pattern\.compile|\.matches\(|replaceAll|replaceFirst|\.split\(' sdks/java/src/main`
  finds only `Regex.matches` and `Clauses.matches`, the SDK's own methods. The Pike VM is `evaluation/Regex.java`.
  (`Reference.parse` was changed to a loop so `String.split` is not used either.)
- [x] The lint configuration names its rule set and gives a reason for each rule it leaves out: the comment at the top
  of `sdks/java/checkstyle.xml`.
- [x] Every runtime dependency has a line in `DESIGN.md`: there are none (`DESIGN.md` section 4, "Runtime: none").
- [x] No telemetry and no header beyond the four; headers the client adds are listed. Source search:
  `grep -rn '\.header(' sdks/java/src/main` finds `Authorization` and `Accept` (`Sync.java` 92 and 93), `If-None-Match`
  (`Sync.java` 124), and `Authorization`, `Content-Type` and `Accept` (`Usage.java` 149 to 151). One captured request
  per route from `HeadersTest` (`target/headers.txt`, loopback), with the JDK client's own headers:

  | Route | Sent by the SDK | Added by the JDK client |
  | --- | --- | --- |
  | GET ruleset (first) | `authorization: Bearer <key>`, `accept: application/json` | `host`, `user-agent: Java-http-client/25.0.5` |
  | GET ruleset (conditional) | the same, and `if-none-match: "v1"` | `host`, `user-agent` |
  | GET stream | `authorization`, `accept: text/event-stream` | `host`, `user-agent` |
  | POST events | `authorization`, `accept: application/json`, `content-type: application/json` | `host`, `user-agent`, `content-length: 80` |

  `host` is the server's own address from the URL; `user-agent` is the library and its Java version (the JDK's
  `Java-http-client/<version>`), which names neither a user nor a machine; `content-length` is the body's size. The
  JDK client sends no `Accept-Encoding` and no `Connection` header here. `HeadersTest` asserts the names are in this set.
- [x] Every extra beyond the contract is one contract 13 allows, and none weakens contract 5: the extras are the
  typed accessors, the generic `variation`, `ready(Duration)` and the public `Json`/`Utf8`; none sends anything.
- [x] Test seams are package-private options on `Options.Builder`, never a global or an environment variable: Decision 11;
  `grep -rn 'System.getenv\|System.getProperty' sdks/java/src/main` finds nothing.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and need only the pinned
  toolchain: `ls -l sdks/java/bin`; each starts with `cd "$(dirname "$0")/.."` and uses `mise exec` when mise exists.
- [x] `test-service/README.md` maps every accessor to an SDK method: its table (`bool`, `string`, `number`, `int`,
  `json`, `any`).
- [x] `.github/workflows/sdk-java.yml` follows the skeleton, filters on its paths, and pins every action by commit
  SHA: checkout and setup-bun SHAs copied from `ci.yml`, `jdx/mise-action` at `b44d12e7d0d8ee7461bb893e1b1993500433185b`
  (`gh api repos/jdx/mise-action/commits/v5.2.7`).
- [x] No em dash or en dash in any file written: `bun scripts/check-dashes.ts` exits 0.
- [x] `bun run format:check` passes (`All matched files use the correct format.`).
- [x] Only `sdks/java/` and the one workflow changed: the phase 6 scope check prints `scope ok` (below).
- [x] No package was published and no name reserved: nothing was pushed to any registry; `pom.xml` has no
  distribution management and the description says "Not published."
- [x] Commit subject is a Conventional Commit, not `feat`: `chore(sdk-java): the Java server SDK regenerated cold
  from the authoring prompt`; no `Co-Authored-By` and no AI attribution line.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:` guard, triggers only `pull_request` and `push`,
  installs its own toolchain (`jdx/mise-action`), and `bun scripts/check-workflows.ts .github/workflows/*.yml` prints
  `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of this list: this file.
