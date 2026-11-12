# Java SDK: run report

## Prompt version

- Prompt: `92be3c4dab22d348092b44f509b86b97eb9f42ae` (`git log -1 --format=%H -- docs/sdk-authoring`, taken at the start of the run)
- Model: Sonnet 5.5 (`claude-sonnet-5-5`), run as a spun-off builder thread (ticket FS-163)
- Date: 2026-11-12
- Input: language Java; runtime chosen: the JVM (HotSpot, OpenJDK); minimum version chosen: Java 17; package manager chosen: Maven
- Toolchain, as `mise.toml` pins it and as the version commands print:
  - java `temurin-25.0.5+8.0.LTS`: `openjdk version "25.0.5" 2026-10-20 LTS`, `OpenJDK Runtime Environment Temurin-25.0.5+8 (build 25.0.5+8-LTS)`
  - maven `3.10.0`: `Apache Maven 3.10.0 (c43a36b8d67be7e0805a411bc0898af1a51f5472)`
  - checkstyle `14.4.0`: `Checkstyle version: 14.4.0`
  - google-java-format `2.0.0`: `google-java-format: Version 2.0.0`
  - mise itself (not pinned by the SDK): `mise 2026.11.0 linux-x64`
  - `pom.xml`: junit-jupiter 6.1.3 (test scope, the only dependency); maven-compiler-plugin 3.14.1 (`-Xlint:all -Werror`, `release` 17); build-helper-maven-plugin 3.8.0 (adds the test service as a source root); maven-surefire-plugin 3.6.1

## Results

- Vectors: 1371 passed of 1371; bucket pins: 17 passed of 17 (`bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command prints `1371 17`)
- Conformance: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`
- Capabilities advertised: `integer-accessor`
- Native tests: 44 passed of 44 (ClientTest 21, JsonTest 9, SseParserTest 7, SourceSearchTest 3, HoldTest 2, ConcurrencyTest 1, VectorsTest 1; the vectors test is one test over 1371 vectors and 17 pins). The test service has no tests of its own.
- `bin/test` ten times in a row on the committed tree: runs 1 to 10 all passed (14, 12, 13, 12, 12, 12, 12, 12, 12, 12 s). Eleventh run under load: 12 busy shell loops (`nproc` is 12) at normal priority, started before `bin/test` and killed after it ended, build included: passed, 18 s. The tree did not change after the first of these runs. `RUN.md` is the only file written since.

## Decisions

1. Runtime and minimum: the JVM, Java 17 (`release` 17, so records, `switch` expressions and `java.net.http` are available), over Java 11 or 21: 17 is the oldest long-term-support line with records. The toolchain is pinned to Temurin 25.0.5 so a run is repeatable; the code compiles for 17.
2. Package manager and build: Maven over Gradle, because it is declarative and its plugin set is small. `junit-jupiter` is the only dependency, and it is test scope. There is no runtime dependency (`DESIGN.md` section 4).
3. JSON: a hand-written parser and writer (`json/Json`) over a library, because the data model must be binary64 numbers with `1e999` read as Infinity, duplicate keys last-wins and depth at least 512. `Json.MAX_DEPTH` is 1000, which the contract allows (at least 512); 512 and 600 levels are tested in `JsonTest`.
4. Regular expressions: the evaluator has no platform regex. `matches` runs on a hand-written parser (`PatternParser`) and a Pike VM (`Regex`). `String.split` takes a regex, so `evaluation/Text.split` (an `indexOf` and `substring` walk that keeps empty parts) replaced it in `Reference` and `Semver`. `SourceSearchTest.theEvaluatorUsesNoPlatformRegex` searches for `java.util.regex`, `Pattern.`, `.matches("`, `.replaceAll(` and `.replaceFirst(` and requires every `.split(` hit to be `Text.split(`.
5. Lint and format: checkstyle 14.4.0 with a hand-picked subset of Google's checks over google-java-format 2.0.0 for layout. The header comment of `checkstyle.xml` names the rule set and gives a reason beside each rule left out. Alternatives were Error Prone and SpotBugs, which need the compiler plugin chain and would add runtime to every `bin/lint`. `-Xlint:all -Werror` covers the compiler's own warnings. No JVM notice from the lint tools was seen in `bin/lint` output (only `Starting audit... Audit done.`).
6. Time: a `Clock` seam for every deadline, backoff and watchdog, and real-time request timeouts through `Future.get(timeout)`. The seams are package-private options on `create` (item 14 of the checklist below).
7. Non-positive durations (null, zero or negative) fall back to the default, as contract 2 says in its SHOULD. `Client.positive` does this for `requestTimeout`, `pollInterval`, `flushInterval`, the stream idle limit and the `Retry-After` cap. An earlier draft clamped them to 0; the contract line was re-read in the phase 6 "Bounds" pass and the code changed.
8. Order of `ready`, `status` and `onError` after a failed first load: the SDK records the error code in the snapshot, then settles `ready`, then calls `onError`. The contract fixes no order between `ready` and `onError`, but once `ready` returns false `status().lastError` should already hold the cause. `Sync.refresh` does this; a closing client drops the error entirely (`Sync.record` returns false).
9. A flaky first batch of the ten runs: `aUrlTheSdkCannotUseIsANetworkErrorNotARaise` and `tlsWithALocalCertificate` failed in some runs with `expected: <network> but was: <null>`. The cause was an SDK ordering bug, not a test that raced something it did not control: `ready` settled before `status.lastError` was recorded (decision 8). Fixed in `Sync.refresh`; the ten runs restarted from the first on the fixed tree. The later change of `String.split` to `Text.split` also restarted them. The reported ten and the loaded run are of the final tree.
10. `close` called from one of the SDK's own threads (a callback) cannot join itself, so it delegates to a daemon thread `toggleslash-close` and returns. `readyAndCloseFromTheSdksOwnThreadDoNotWait` covers it.
11. Bounds found in the "Bounds" bullets of the contract: `ready` waits at most `2*timeoutMs+1000` ms as a backstop (the first attempt is itself bounded by `requestTimeout`); `close` joins within `2*timeoutMs+5000` ms; the SSE parser keeps no line or event cap beyond the JVM heap (the contract asks for a 60 s idle watchdog, not a size cap); usage batches are 500 entries; the `Retry-After` cap is 300 s.
12. Threads: both SDK threads are `SdkThread`s with `setDaemon(true)`. Snapshots are immutable and swapped through one `AtomicReference`, so a hook's `etag` and `flagVersion` come from one ruleset.
13. Hooks are plain functions, so there are no futures (checklist line marked `n/a`).
14. Test seams (`httpClient`, `sslContext`, `clock`, `jitter`, `streamIdle`, `retryAfterCap`, `evaluator`) are package-private setters on `Options`, so they are options on `create` that the package's tests set, and no global or environment variable is read.
15. Header audit (checklist, "adds no header that identifies a user or a machine"). One request of each route was captured on a loopback listener by `ClientTest.headerAudit`, which writes `target/header-audit.txt`:

| Route | Set by the SDK | Set by the HttpClient |
| --- | --- | --- |
| GET /sdk/v1/ruleset | authorization | host, user-agent |
| GET /sdk/v1/ruleset (conditional) | authorization, if-none-match | host, user-agent |
| GET /sdk/v1/stream | accept, authorization | host, user-agent |
| POST /sdk/v1/events | authorization, content-type | content-length, host, user-agent |

   - `host`: the server's own name and port from the URL the caller gave; it names the destination, not the sender.
   - `user-agent`: the JDK's default, `Java-http-client/25.0.5` (printed by a loopback capture against the pinned JDK). It names the library and its Java version only; it carries no host name, user name or machine id. The SDK does not set it.
   - `content-length`: the size of the usage body. Not an identity.
   - The JDK client sent no `Accept-Encoding` and no `Connection` header on these requests. `ClientTest.headerAudit` fails if the client adds any header beyond `host`, `user-agent`, `content-length` and `connection`, or if an SDK header is missing.

## Guesses

1. Whether the `json` accessor of the test service takes a non-null fallback of another kind. Where: `docs/sdk-contract.md` section 4, "The mismatch rule", and `docs/sdk-test-service.md`, "Accessors". Closest sentences: the contract says "a JSON accessor, whose type holds every kind, accepts any" (line 113); the test service says "The `json` accessor applies contract 4's mismatch rule like the others: it compares kinds (array and object are distinct, a null fallback accepts any kind)", and the scenario "a value of another kind than the fallback is TYPE_MISMATCH" (`fixtures/sdk/evaluation.json:176`) sends the `json` accessor a fallback `{}` and a served array and expects `TYPE_MISMATCH`. The contract is about an SDK's typed accessor whose type holds every kind; the service text is about the protocol's `json` name. They cannot both be true of one method, and neither sentence says which method the protocol's `json` maps to. Assumed: `Client.jsonVariation` keeps the contract's reading (accepts any kind), and the test service maps the protocol's `json` accessor to the generic `variationDetail`, which applies the mismatch rule against the fallback's kind, so the scenario passes. `DESIGN.md` section 7 records this.
2. Whether `status().lastError` must already be set when `ready` returns false after a failed first load. Where: `docs/sdk-contract.md` sections 5.3 and 8. Closest sentence: 5.3 says `ready` settles after the first attempt; section 8 defines `lastError`; neither orders the two, and no scenario checks the pair under a race. Assumed: the error is recorded first, then `ready` settles, then `onError` is called (decision 8).

## Stuck points

None that blocked the run. The contradiction between the contract and the test-service text on the `json` accessor (guess 1) was worked around without outside help.

## Proposed prompt changes

1. For guess 1, in `docs/sdk-contract.md` section 4, replace "a JSON accessor, whose type holds every kind, accepts any" with: "a JSON accessor, whose type holds every kind, accepts any served value when its own fallback type is the generic one; the test service's `json` accessor is the generic method and applies the mismatch rule". Alternatively, in `docs/sdk-test-service.md`, "Accessors", say that `json` maps to the SDK's generic method and not to a method whose type holds every kind. Aimed at `PROMPT.md` only if the contract is out of scope: in `PROMPT.md` phase 3, after "`json` or `any` (the generic method)", add: "A language with a separate JSON accessor that takes any kind maps the protocol's `json` to its generic method, so that the `evaluation.json` mismatch scenario holds."
2. For guess 2, in `PROMPT.md` phase 3, after the sentence about `ready`, add: "When `ready` returns false after a failed first attempt, `status` already shows the error that caused it: record it before `ready` settles."

## Checklist

- [x] `bin/test` runs every vector but `buckets.json`, and every pin; count equals the phase 2 command's: `bin/test` prints `vectors: 1371 pins: 17`; the phase 2 count command prints `1371 17`. `VectorsTest`.
- [x] `bin/test` passes 10 times in a row and once more under load: ten passes (14, 12, 13, 12, 12, 12, 12, 12, 12, 12 s), and the eleventh with 12 busy loops (`nproc` is 12), 18 s, pass; see Results and Decisions item 9 for the restart after the ordering fix.
- [x] The harness exits 0, with only the 2 named vector skips: `scenarios: 81 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`.
- [x] Both context shapes (contract 3): the harness, `privacy.json`, `kinds.json` vectors; summary line above.
- [x] Results order and mismatch rule: the harness, `evaluation.json`, `usage.json`.
- [x] `integer-accessor`: the harness, `evaluation.json`; advertised.
- [x] No operation raises (contract 5.1): the harness, every scenario.
- [x] Evaluating before the first load answers `NOT_READY`: the harness, `load.json`.
- [x] `ready` settles after the first attempt, bounded, repeatable; false after `close`: the harness, `load.json`, `close.json`.
- [x] An answer that is not a ruleset never replaces the one held: the harness, `status.json`.
- [x] The ruleset request, `If-None-Match`, ETag, URL joining: the harness, `load.json`, `status.json`.
- [x] The stream: the harness, `stream.json`, `load.json`, `status.json`.
- [x] The loop and the jittered backoff: the harness, `backoff.json`, `stream.json`.
- [x] `429` and `Retry-After` on every route: the harness, `rate-limit.json`.
- [x] Status fields, states, `lastError` codes: the harness, `status.json`.
- [x] Usage counts: the harness, `usage.json`, `rate-limit.json`.
- [x] Hook events and `onError`: the harness, `hooks.json`.
- [x] `close` in order, twice at once, during a hold, with a final flush: the harness, `close.json`, `rate-limit.json`, `usage.json`, `status.json`.
- [x] No context, key or attribute in a request, `onError` message or log; no extra header: the harness, `privacy.json`. The source searches are `SourceSearchTest.nothingIsLoggedOrPrinted` (no `System.out`, `System.err`, `java.util.logging`, `printStackTrace`, `Logger` in `src/main`) and `SourceSearchTest.noErrorTheSdkBuildsNamesAContextOrAKey` (every `new ToggleSlashException(` and `report(` line names no context, SDK key or flag key). `grep -rn "System.err\|System.out\|printStackTrace\|Logger" src/main` prints nothing.
- [x] Many threads, `close` racing `variation`, two racing `close` calls: `ConcurrencyTest.closeRacesVariationAndAnotherClose`. The JVM has no race detector; this stress test stands in (many threads, every operation, `close` racing, a bound on the wait).
- [x] SHOULD: TLS with a local certificate: `ClientTest.tlsWithALocalCertificate`.
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a flush hangs: `ClientTest.variationStaysBoundedWhileTheRulesetRequestHangs` and `ClientTest.variationStaysBoundedWhileAFlushHangs`.
- [x] A hook's `etag` and `flagVersion` belong to one ruleset under concurrent swaps: `ClientTest.hookEtagAndFlagVersionComeFromOneSwap`.
- [x] Hooks and `onError` that call back into the client do not deadlock; `ready` from the SDK's own thread returns: `ClientTest.callbacksMayCallBackIntoTheClient`, `readyAndCloseFromTheSdksOwnThreadDoNotWait`, `aFailedFirstLoadReportsToOnErrorOnTheSdkThread`.
- [x] A quiet stream is dropped as `timeout` after the idle limit, and a keep-alive restarts it, on a real quiet loopback socket through the `streamIdle` seam: `ClientTest.aQuietStreamIsDroppedAfterTheIdleLimitAndAKeepAliveRestartsIt`.
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes through the `retryAfterCap` seam: `ClientTest.retryAfterIsCappedOnTheRulesetRoute`, `retryAfterIsCappedOnTheStreamRoute`, `retryAfterIsCappedOnTheEventsRoute`.
- [x] Counts either side of a UTC midnight go out as two days: `ClientTest.usageSplitsAtUtcMidnight`.
- [x] An evaluation that raises inside the SDK gives the fallback, `EVALUATION_FAILED` and one `onError`: `ClientTest.anEvaluatorThatRaisesGivesEvaluationFailedAndOneOnError`.
- [x] Hook futures: `n/a: no futures` (a hook is a plain function).
- [x] A served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `ClientTest.theIntegerAccessorIsStrict`, and the harness `huge` scenario.
- [x] A client never closed does not keep the process from exiting: `ClientTest.aClientNeverClosedDoesNotKeepTheJvmAlive` (a child JVM runs `DaemonProbe`); by construction both SDK threads are `SdkThread`s with `setDaemon(true)`.
- [x] `DESIGN.md` passes the phase 1 row check, and `README.md` links to the mapping: the row check found every row; `README.md` links to the table in `DESIGN.md`.
- [x] Mapping names keep the canonical meaning, defaults and spellings: `DESIGN.md` section 1 and the closed sets under it (`ReasonKind`, `ErrorCode` and `StreamState` keep the contract's spelling).
- [x] The evaluator has no platform regex: `SourceSearchTest.theEvaluatorUsesNoPlatformRegex` (decision 4).
- [x] The lint configuration names its rule set and reasons: the header comment of `checkstyle.xml` (decision 5).
- [x] Every runtime dependency has a line in `DESIGN.md`: there is none; `DESIGN.md` section 4 says so.
- [x] No telemetry; only the allowed headers: the header audit table (decision 15). Client-added headers are `host`, `user-agent` (`Java-http-client/25.0.5`) and `content-length`, each explained there.
- [x] Every extra is one contract 13 allows: the `Options.DEFAULT_*` constants and the typed accessors; none weakens contract 5 or sends anything new.
- [x] Test seams are options on `create`, never a global or an environment variable: decision 14.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK and wrap with `mise exec --`: `ls -l sdks/java/bin` shows mode 755 on each; each starts `#!/bin/sh` and `set -eu`.
- [x] `test-service/README.md` maps every accessor: `bool` to `boolVariationDetail`, `string` to `stringVariationDetail`, `number` to `doubleVariationDetail`, `int` to `longVariationDetail`, `json` and `any` to `variationDetail`.
- [x] The workflow follows the skeleton, filters on its paths and pins every action by commit SHA: `.github/workflows/sdk-java.yml` (checkout, setup-bun and mise-action by SHA).
- [x] No dashes: `bun scripts/check-dashes.ts` clean.
- [x] `bun run format:check` passes.
- [x] Only `sdks/java/` and the one workflow changed: the phase 6 scope check prints `scope ok`.
- [x] No package published, no name reserved.
- [x] Commit subject is a Conventional Commit, not `feat`, with no `Co-Authored-By` and no AI attribution: `chore(sdk-java): the Java server SDK regenerated cold from the authoring prompt`.
- [x] The workflow job is `runs-on: ubuntu-latest` with no `if:`, triggers only `pull_request` and `push`, installs its own toolchain, and `bun scripts/check-workflows.ts .github/workflows/*.yml` passes: `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`: every section is present, with evidence for every line above.
