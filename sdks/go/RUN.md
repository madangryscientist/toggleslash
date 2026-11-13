# Go SDK: run report

## Prompt version

- Prompt: 3a95c4ac1ebe3b279c06bcaa80efab98346ca45f
- Model: opencode-go/deepseek-v4.1-flash
- Date: 2026-11-13
- Input: language Go; runtime chosen: the gc toolchain that mise installs; minimum version chosen: 1.26; package manager chosen: the go command with modules
- Toolchain: go 1.27.4 (`go version` prints `go version go1.27.4 linux/amd64`); golangci-lint 2.15.0; zig 0.17.1 (the C compiler the race detector needs). `sdks/go/go.mod` declares `go 1.26` (decision 17); it requires nothing, so there is no `go.sum`.

## Results

- Vectors: 1371 vectors and 17 bucket pins in `fixtures/evaluation/`, run by `sdks/go/eval/vector_test.go`; all pass,
  under `-race`. `bin/test` prints `vectors: 1371 pins: 17`, and phase 2's count command prints `1371 17`.
- Conformance: `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`, from
  `bun run conformance -- --service sdks/go/bin/test-service` (exit 0).
- Capabilities advertised: `integer-accessor`.
- Native tests: 47 passed of 47 top-level tests (42 in the root package, 4 in `eval`, 1 in `jsonval`), with 11
  subtests. `bin/test` builds every package and runs the tests under `-race`; the test service has no tests of
  its own and the harness is its test.

## Decisions

1. Go version: 1.27.4, over 1.26 and every earlier release, because it is the newest stable one. Looked up with `mise ls-remote go`, whose newest line is `1.27.4`, and cross-checked against the vendor index `https://go.dev/dl/?mode=json`, whose newest stable entry is `go1.27.4`. The two agree, so there is no disagreement to resolve between `PROMPT.md` phase 0 and the Go notes.
2. Minimum version: 1.26, over 1.25, because upstream supports each major Go release until two newer major releases exist (`go.dev/doc/devel/release`: "Each major Go release is supported until there are two newer major releases"). With 1.27 current, 1.26 and 1.27 are in support and 1.25 is not, so 1.26 is the oldest supported one. It goes in `go.mod`'s `go` directive when that file arrives, and `DESIGN.md` states why.
3. Runtime: the standard `gc` toolchain that mise installs, over `gccgo`, because Go has no runtime input in the brief and `gc` is its mainstream one.
4. Package manager and build tool: the `go` command with modules, over any third-party build tool, because it is Go's mainstream choice and the toolchain mise pins sits underneath it.
5. Lint and format: `golangci-lint` 2.15.0 plus `gofmt -l`, over `go vet` alone, because the Go notes' "Lint" fact records an earlier run using `go vet`, `golangci-lint` and `gofmt -l`, and 2.15.0 is the newest version `mise ls-remote golangci-lint` lists.
6. C compiler: `zig` 0.17.1, pinned in `mise.toml`, because `-race` needs cgo (Go notes, "`-race` needs cgo"), the CI runner is not assumed to have a C compiler, and `bin/test` calls it by path.
7. No version was copied from the Go notes, from an earlier tree or from a document: `mise.toml` holds only what the lookups above returned.
8. Accessor shape: five typed accessors (`BoolVariation`, `StringVariation`, `IntVariation`, `FloatVariation`, `JSONVariation`), each with a `...Detail` sibling, over one generic method. Contract section 4 makes the JSON kind comparison per accessor, and Go's type parameters cannot switch on the fallback's static type; `JSONVariation` covers the every-kind case with a `jsonval.Value` fallback.
9. `Options` is a plain struct, not functional options or a builder, with `Stream *bool` (nil is not given, which is on) and the three durations zero meaning not given. The zero `bool` cannot say "not given", and a zero `time.Duration` is not a valid interval, so no option that means two things is ambiguous; `New` replaces a zero or negative duration with its default before it starts anything.
10. The hook event carries its value as `jsonval.Value`, not as the accessor's own type, so one `Event` type serves all five accessors. For an `int64` value above 2^53 the event carries the nearest binary64, which contract section 10 allows where the event's type holds a double, and the caller still gets the exact integer.
11. `onError` runs on one notify goroutine the client starts, never the caller's and never the sync or flush goroutine. Go cannot identify the goroutine that calls `Close`, so contract section 6.4's "a close on the SDK's own thread does not wait for its own work" is met structurally: `Close` joins the sync and flush goroutines and never the goroutine that runs user callbacks. Contract section 10 leaves the thread to the SDK.
12. JSON: a hand written reader and writer in package `jsonval`, not `encoding/json`. The standard decoder replaces a lone surrogate escape with U+FFFD and rejects `1e999` as out of range, and `docs/evaluation.md` "Reading JSON" and "The vectors" need both kept; Go's `string` is a byte slice, so it holds UTF-8, or WTF-8 where the input had a lone surrogate. A non-finite number is written as `1e999` or `-1e999`, the only JSON text that reads back as the value held.
13. HTTP bounds: a per request `context.WithTimeout(ctx, requestTimeout)` for the ruleset and events requests, which covers reading the body because Go's transport aborts a body read when the request's context ends; and for the stream a `time.AfterFunc(requestTimeout, cancel)` stopped once the response headers arrive, then the same `cancel` armed as the 60 s watchdog. `http.Client.Timeout` would bound the body but cannot be dropped after the headers, which contract 7.2 needs, and one cancel covers the open, the watchdog and the reconnect.
14. The SSE parser is hand written, over a library. Contract 7.2's rules are few and a dependency that keeps all of them would be more code to trust than a byte scanner that keeps the split line ending rule.
15. Redirects are not followed (`http.Client.CheckRedirect` returns `http.ErrUseLastResponse`): a 3xx is
    `http_<status>`. Go re-sends the request, and a 301/302/303 on the usage POST becomes a bodyless GET whose
    2xx would read as sent and lose the counts; contract 7.1 permits either. This replaces the follow-the-default
    choice of phase 3 (decision 72).
16. Package layout: module `github.com/madangryscientist/toggleslash/sdks/go`, with the public API in the module root as package `toggleslash` (`go` is a keyword), and the subpackages `jsonval` (public, because `Context` is its `Value` and `internal` cannot appear in a public signature), `eval` and `testservice`.
17. Minimum runtime in `go.mod`: `go 1.26`, not 1.27. It is the oldest release still supported (decision 2) and nothing in the design needs a feature newer than 1.19's `atomic.Pointer`; the toolchain that builds it is still 1.27.4 from `mise.toml`.
18. Lint: `golangci-lint` 2.15.0 with a committed `sdks/go/.golangci.yml` (the default linters plus `errcheck`, `govet`, `staticcheck`, `ineffassign`, `unused`), and `gofmt -l`, with no separate `go vet` because `golangci-lint` runs `govet`.
19. The vectors test reads `fixtures/evaluation` by walking up from `sdks/go/eval` instead of a copy under `sdks/go`. The fixtures are the spec's executable form, so a copy could drift from them.
20. `bin/test` captures `go test -race -count=1 -v ./...`, prints it, then prints the `vectors: <n> pins: <n>` line it greps from that output, so the check's last line is the counts line. On failure it prints the captured output and exits 1, so nothing is swallowed, and `-count=1` keeps go from reporting a cached result.
21. `bin/test` sets `CC="$(mise where zig)/zig cc"` itself, over leaving it to the caller, so `-race` works on a machine whose default C compiler is not the pinned one (decision 6).
22. Phase 2's `bin/test` runs `go build ./...` and the tests and does not build a test service, because `testservice` arrives with phase 4. Phase 4 adds `build/testservice` as the last step, where `bin/test` has it in the design.
23. `jsonval` carries `Read` now and `Write` with phase 3, over writing both now: phase 2 writes no JSON, and `Write` is needed only for the events body and the test service's replies.
24. An escape such as `\D` inside a class contributes its complement as code point ranges, over a nested negation, so `[^\S]` means space. A class is a list of ranges and one outer `^`, and the six escape sets are expanded once at parse time.
25. A `matches` pattern is parsed per evaluation, over caching it on the loaded ruleset: the pattern is a clause value, not a ruleset field, and the whole vector run takes under a second with it. A cache is a client-side decision if a profile ever asks for one.
26. The vectors test compares the object the client will return: `Result.ToJSON` writes `value`, `variationIndex` and `reason`, and the test compares deeply with numbers by IEEE-754, so `-0` equals `0` and `1.0` equals `1`.
27. The pattern size table distinguishes a bare quantifier from a braced one (`x*` is `size(x) + 1`, `x{0,}` is `size(x)`), because the table is the rule and the two spellings are not equivalent for an empty body.
28. The vectors test is the module's only test, over a unit test per package: the vectors cover the evaluator and the JSON reader they run through, and a second test of the same rules would add no evidence. `jsonval`'s own edge cases (a lone surrogate, `1e999`, the depth limit) are the ones to name when phase 6 walks the checklist.

29. Layout: the client is the module root's `api.go`, `client.go`, `stream.go`, `events.go`, `ruleset.go` and `context.go`
    as `DESIGN.md` section 7 lays them out, plus `clock.go`, which holds the clock seam and the Retry-After hold because
    both are the SDK's only sources of time.
30. Test seams are unexported fields on `Options` (`httpClient`, `clock`, `jitter`, `watchdog`, `evaluate`), over a
    second constructor or exported seam options: the phase allows a seam internal to the package, and `Options` stays
    one struct a caller fills in as a literal.
31. The clock seam is an interface (`Now`, `AfterFunc` answering a timer with `Stop` and `Reset`), and production is
    `time.AfterFunc`. Poll, flush and Retry-After waits are stepped by a fake clock in the tests; the stream watchdog
    keeps the real clock with a shortened length, because the checklist wants a real loopback socket that goes quiet.
32. The ruleset and its ETag live in one `atomic.Pointer[snapshot]`, and status in one `atomic.Pointer[statusSnapshot]`
    whose writers take a mutex, so `Close` can force `closed` without a concurrent write losing it. Readers never lock,
    which keeps `Status` and evaluation off any lock a request holds.
33. `eval` gains `(*Ruleset).FlagVersion(key)` and stores a flag's `version` at load, over the client re-reading the raw
    flags: contract 10's malformed-at-load rule (`MALFORMED_FLAG`, or a segment the flag names missing or malformed) is
    the evaluator's, and a second copy in the client would drift from it. A version that is absent, fractional or
    outside signed 64 bits is none.
34. `Reason` carries `Kind` and `Code` only, as `DESIGN.md` section 1 says; the evaluator's `ruleIndex` and `flagKey`
    are not exposed. Contract 4 asks for the kind and, for `ERROR`, the code. If the harness's result shape wants a
    rule index, phase 4 adds it.
35. `Ready` settles at the end of the first load, and that load's failure is reported to `onError` only after that, so
    no callback on an SDK goroutine ever runs while `Ready` is unsettled. That is the order the checklist's
    "the unsettled case cannot arise" line asks to be named.
36. A usage request is made with `context.Background()` and `requestTimeout`, not the client's context: contract 11
    waits for a flush already running instead of abandoning it, so a flush has to survive `Close`.
37. The integer accessor's rule is `-2^63 <= f < 2^63` and integral, so the literal `2^63 - 1`, which reads as `2^63`,
    is a `TYPE_MISMATCH`, as contract 4 says. The hook's value for a served integer is the nearest binary64
    (decision 10).
38. The JSON accessor accepts a served value of any kind when its fallback is `jsonval.Null` or nil; any other
    fallback compares JSON kinds, and a null served value against a non-null fallback is a `TYPE_MISMATCH`.
39. A usage flush sorts its entries by flag, then variation, then day, and sends at most 500 per request: the order is
    not specified, and a stable one lets a test read the body and makes two runs comparable.
40. A `5xx`, `408`, `429`, network failure or timeout keeps the batch and every entry after it for the next flush, and
    merges them back with the counts recorded since; any other answer (a `3xx`, or a `4xx` such as `400`, `401`, `413`
    or `404`) drops the batch and the flush goes on with the next request. The status alone decides, as contract 9
    says. A `429` with a valid `Retry-After` also raises the usage hold.
41. At `close`, counts that cannot be sent are counted as entries (one per flag, variation and day) and reported once:
    with the TypeScript SDK's wording when a `Retry-After` hold is in force, and a shorter message when a kept
    request failed at close.
42. The stream watchdog is reset on every byte read, not once per read call, so a keep-alive comment restarts it; and a
    `connected` event sets the stream state and clears `lastError` in one status store (contract 6.5).
43. The HTTP client is one per client, the default transport cloned with `MaxIdleConnsPerHost` raised and `Timeout` at
    zero, because the bounds are per request (decision 13); redirects are Go's default (decision 15).
44. The test service is `sdks/go/test-service/` (`package main`, the directory name carries the hyphen), not the
    `testservice/` of decision 16 and `DESIGN.md` section 7: `PROMPT.md` phase 4 and `docs/sdk-test-service.md`
    both name `test-service`, and the returned-proposal decision would rather match the prompt than a draft name.
    `DESIGN.md` section 7 was corrected to match, and `build/testservice` keeps the binary name the design gave it.
45. `toggleslash.Reason` gains `RuleIndex *int` and `FlagKey string`, filled by `reasonOf` from the evaluator's
    reason. Phase 3's open item anticipated this: the vectors compare a reason field by field and 11 of them expect
    `flagKey` on `PREREQUISITE_FAILED` while others expect `ruleIndex` on `RULE_MATCH`, so a service answering only
    `kind` and `code` fails them. The hook event's reason carries the same fields.
46. The service reads each body with `jsonval.Read` and answers with `jsonval.Write`, over `encoding/json`, so a
    context passes through as the JSON it arrived as (lone surrogates included) and a non-finite number is written
    back as JSON `null`, which the protocol page asks for and `encoding/json` refuses.
47. `any` maps to `JSONVariationDetail` with the fallback as a `jsonval.Value`, the closest Go has to a generic
    accessor; `json` maps to the same method. The `int` accessor takes a `jsonval.Number` fallback, passes `int64`,
    and writes the answer back through a `float64`, which the protocol page allows and no vector or scenario needs
    beyond.
48. The service uses `net/http`'s own mux and goroutine per request, so scenarios and a waiting `ready` run
    concurrently with nothing to configure; a per-client mutex covers only the hook events and the error list. A
    `recover` at the handler turns a panic, which `net/http` would answer by dropping the connection, into the 500
    the protocol wants. Clients are never removed from the registry, so a closed client still answers status and
    errors.
49. Nothing is in the answer that the protocol does not name: `etag` is `null` before the first load and the string
    as received after it, in `status` and in a hook event; `lastSyncAt` is whole epoch milliseconds or `null`;
    `lastError`, `variationIndex` and `flagVersion` are `null` when they have no value. A reason carries `kind`,
    plus `ruleIndex` or `flagKey` or `code` only when they apply.
50. `bin/test` builds `build/testservice` from `./test-service` after the test run and before the counts line, plain,
    without `-race`: the race detector is for the tests, and the harness's real-time windows would only get longer.
    `bin/test-service` rebuilds it only when the binary is missing, because the harness waits 30 s for `listening`.
51. `sdks/go/.gitignore` is written now with `build/` only, so `bin/test`'s output never shows as work. Phase 5
    extends it; the rest of phase 5's files are still absent.
52. `.gitignore` gains `*.test`, `coverage.out` and `coverage.html` beside phase 4's `build/`. Go's build and
    module caches live outside the tree (`GOCACHE`, `GOMODCACHE`), so the only output left in it is a test binary
    built by hand and coverage files. Nothing else is named: a pattern that matches nothing is noise, and phase 6
    stages with `git add -A`.
53. `.golangci.yml` names five linters with `default: none` (`errcheck`, `govet`, `ineffassign`, `staticcheck`,
    `unused`): the set golangci-lint 2.15.0 enables by default today, pinned by name so a later release cannot turn
    the gate red by growing its defaults. The formatter is deliberately not in the config: `bin/lint` runs
    `gofmt -l` itself, so there is one formatter and one answer (DESIGN section 6, decision 18).
54. `bin/lint` runs `gofmt -l .`, which recurses over the module root and every package under it
    (./eval, ./jsonval, ./test-service), and `golangci-lint run ./...`, which covers the same tree. A package
    added later comes with both. The tests and the test service are linted rather than exempted, because the
    standard set is already clean over them, so an exemption would buy nothing and could hide a real finding
    later.
55. The workflow's toolchain step is `jdx/mise-action`, not `actions/setup-go`: `bin/lint` calls `golangci-lint`
    and `bin/test` gives cgo `zig` as its C compiler for `-race` (decision 21), so three tools have to be
    installed, and `sdks/go/mise.toml` already names all three. `install_args: go golangci-lint zig` with
    `working_directory: sdks/go` limits the action to them, so the root `.tool-versions` (.NET and a second Bun)
    is not installed. This is the phase's open choice "which tool to use".
56. `jdx/mise-action` is pinned to `b44d12e7d0d8ee7461bb893e1b1993500433185b # v5.2.7`, the pin `sdk-java.yml`,
    `sdk-python.yml` and `sdk-ruby.yml` already carry. The phase's Reads list allows reading a sibling workflow
    when the toolchain step needs the action's inputs, and it was read for the `install_args` form; the pin was
    then reused from it rather than resolved again with `gh api`, which this run's brief forbids, and one version
    across every SDK workflow is what a shared runner should have. This is the phase's open choice "how to look
    up a version".
57. `BUN_VERSION: "1.4.2"`, the `actions/checkout` pin (`# v7.0.1`), the `oven-sh/setup-bun` pin (`# v2.2.0`) and
    the checkout's `persist-credentials: false` are copied from `ci.yml` as it stands, found with `grep -n`, as
    item 4 says.
58. `README.md` links the mapping table (DESIGN section 1) instead of copying it, which item 2 allows, and adds a
    short API table so the file answers "what do I call" without a second window. The guarantees are contract
    section 5's five items, shortened, and `Reason` is described as `{ kind, ruleIndex, flagKey, code }` (phase
    4's open item). The quickstart installs from a checkout through `go mod edit --replace`, because the module
    has no tag and no registry and the file names neither.
59. `TestNotModifiedIsASync` was fixed, not the SDK: the loaded run of the checklist's ten failed it with
    "lastSyncAt did not move on a 304". The test waited for the server to receive the conditional request and
    then read status, but the client's sync store follows the request a moment later, and load widens that gap.
    It now waits for the assertion (a `lastSyncAt` later than the first) on the real clock, which is the
    checklist's "a test that asserts something it did not control" case. The ten runs then started again from
    the first.
60. `TestRetryAfterHoldsTheUsageReports` was fixed the same way: the next loaded run failed it with "a flush
    after the hold passed: not reached within 3s". It advanced the fake clock once and expected the flush
    loop's timer to fire, but when the loop had not armed its wait yet the step fired nothing and no second
    step came. It now steps once past the hold and then through `advanceUntil`, which steps only while a timer
    is armed. Both fixes are test bugs, not guesses.
61. The contract 6.5 line has its own native test, `TestStatusNeverPairsAFailureWithConnected`: a reader
    goroutine samples `Status` while three stream attempts each send `connected` and then go quiet, so each
    ends as a watchdog `timeout` and the next attempt reconnects. It keeps the real clock for the watchdog with
    a 30 ms length, over stepping a fake one, because a fake watchdog and a fake backoff have to be stepped
    from the test goroutine, which is the shape decision 59 removed.
62. The header table is produced by `TestRequestHeaderAudit`, which captures one request of each route (the
    first ruleset, a conditional ruleset, the stream, the events flush), prints every header it saw with the
    `Host` the server read outside the header map, and fails on a header the audit does not allow. The table
    below is that test's output, over a table read off the source, because the point of the line is what the
    client actually added: `Accept-Encoding`, `User-Agent` and `Content-Length` come from Go's transport, not
    from the SDK.
63. The three substring ops were byte comparisons, and `docs/evaluation.md` says they are ordinal over UTF-16
    code units. A needle that is a lone surrogate can match half of a pair, which bytes cannot see
    (`containsUTF16`, `startsWithUTF16`, `endsWithUTF16` in `eval/clause.go`);
    `TestSubstringOpsUseUTF16CodeUnits` pins it. String order and equality already went through `compareUTF16`.
64. `nodeSize` multiplied and summed plain `int`s, so a 32 level repeat of `{100}` wrapped to a size of zero,
    passed the 10000 limit and reached the compiler, which multiplies the body out. Every step now saturates at
    one past the limit (`sizeSum`, `sizeProduct`), and `TestPatternSizeSaturates` pins that the pattern is
    rejected.
65. `jsonval`'s truncated-input branch discarded every remaining byte as one U+FFFD. A body ending in an
    incomplete sequence followed by the string's quote lost the quote and failed. It now consumes only the
    valid continuation bytes of the maximal subpart and leaves the rest; a test in `jsonval/jsonval_test.go`
    covers it beside the lone surrogate round trip, `1e999` and the depth limit.
66. `onError`'s text now carries the fixed code only. Appending the transport's cause put a `*url.Error`'s whole
    URL in it, which the Go notes rule out ("A network error's text carries the URL"); the cause is still
    reachable through `Unwrap`. `TestNetworkErrorMessageCarriesNoURL` pins it.
67. `Ready` checks the settled answer before its select. With a context that was already done, the select could
    pick the done case at random and answer false after the first load had settled true, which contract 5.3
    fixes. `TestReadyStaysFixedWithACancelledContext` calls it 200 times with a cancelled context.
68. Three smaller fixes from the same pass: a usage count now checks `closed` under `usageMu`, so a count cannot
    land after `close`'s final drain and go unreported; `parseRetryAfter` trims only the header's optional
    whitespace (space or tab), not every Unicode space; and a stream whose open timer fires in the moment before
    its `Stop` is classified `timeout`, not `network`.
69. `newHTTPClient` no longer asserts that `http.DefaultTransport` is a `*http.Transport`: an application may
    have replaced it with its own `RoundTripper`, and `New` must not panic on that (contract 5.1). Only a
    `*http.Transport` is cloned for the raised idle limit; anything else is used as it stands.
70. A served `Array` or `Object` is shared storage, so `JSONVariationDetail` and the hook event carry a
    `jsonval.Clone` copy, never the ruleset's own node: a caller or a hook that mutates what it was given can no
    longer change what the next evaluation returns.
71. Two more from the second review round: the BOM check reads a byte at a time and restarts the stream's
    watchdog on each byte, because the watchdog's rule is about bytes arriving, not events; and `bin/test` fails
    when its `vectors: <n> pins: <n>` line is missing instead of printing nothing and exiting 0.
72. Redirects are not followed (decision 15's revision): `CheckRedirect` returns `http.ErrUseLastResponse`. A
    `301`/`302`/`303` on the usage `POST` becomes a bodyless `GET`, and a `2xx` on that would be read as sent,
    losing the batch; contract 7.1 allows either answer, and contract 9's keep-or-drop rule wants the `3xx` seen.
73. `onError` still reaches the caller after `close`: once the notify goroutine has finished, `report` calls the
    handler on the caller's goroutine, with the same panic recovery. Evaluation keeps answering after `close` and
    a hook that raises is still a failure the SDK absorbed (contract 10). Before `close` it stays the one notify
    goroutine of decision 11.
74. The hook event carries copies of its three pointers (`copyInt` on `variationIndex`, `ruleIndex` and
    `flagVersion`), so a hook that writes through one cannot change the caller's result or the ruleset's version.
75. The sync loop rechecks `closed` after each wait, so no ruleset or stream request starts once `close` has
    begun; only a request already in flight finishes (contract 11).
76. The keep-alive test holds its stream open until the request context ends, so a slow scheduler cannot make
    its assertion race a stream the handler ended cleanly.

## Guesses

None.

## Stuck points

None.

## Proposed prompt changes

1. For decision 1, in `PROMPT.md` phase 0, after "and the vendor's release or support page", add: "prefer a compact page or an index that prints only version names; `https://go.dev/dl/?mode=json` for Go answers in tens of thousands of lines, which is a whole-file read in disguise".
2. Phase 1's "Reads" list omits contract section 8, but its own section 2 (which the list names) points at section 8 for the stream states and the `lastError` codes, and the mapping table has to say how those closed sets appear. Section 8 was read on its own (lines 308 to 348, found by grep) and the rest of the contract was not. Suggest adding "8 (status)" to the list.
3. Phase 1's item 5 says the JSON answer must keep what "Reading JSON" **and "The vectors"** in the spec require, while "Reads" names only "Reading JSON". "The vectors" was read on its own (lines 578 to 617, found by grep). Suggest adding it to the list, or dropping it from item 5.
4. Phase 1's "Reads" list omits `docs/sdk-authoring/language-notes/<lang>.md`, but the phase 0 entry's "Next phase reads first" tells phase 1 to carry facts from it, and item 5's JSON answer turns on the fact named "`encoding/json`" there. The facts were not read from the notes in this session: the phase 0 entry carries them verbatim with their labels, and those were used, so this is a pointer, not a breach. Suggest adding the language notes to phase 1's list (read by bold label, as phase 0 does), or having phase 1 read the notes itself rather than inherit them from phase 0.
5. Phase 2's "Reads" list names `fixtures/evaluation/clauses.json` and `fixtures/evaluation/buckets.json`, but its own test must read every file in that directory ("The vectors" lists twelve), so a port cannot be written from the two named shapes alone. Suggest naming the directory: "every `fixtures/evaluation/*.json`, one file at a time, for the shape of each", which is what this session did.
6. Phase 2 says "Create `$L/bin/test` now", while "Entry points" requires `bin/test` to build the test service as its last step, and no test service exists until phase 4. Suggest: "Create `$L/bin/test` now with the build and the test run; phase 4 adds the test service's build."
7. Neither `PROMPT.md` nor phase 2 says where the `vectors: <n> pins: <n>` line has to appear in `bin/test`'s output, yet "How a run is split" asks for the check's last output line. Suggest: "`bin/test` ends with the `vectors: <n> pins: <n>` line, so it is the check's last output line."
8. "How a run is split" says the entry holds "the check it ran and that check's last output line", and phase 2's check prints two results: the `bin/test` count line and the two numbers the `bun -e` line prints. Suggest saying that every result line a check prints goes in the entry, not only the last one.
9. Rule 9 bars reading a whole file, but a source file can be one function long: this session read the whole of nine reference files (15 to 142 lines each) because each holds a single function or a single loop and there is nothing smaller to read. Suggest a size floor in rule 9, such as "a file under 150 lines may be read whole; above that, a section or a function at a time".

10. Phase 3's "Reads" list names the contract, `docs/api.md`, `CHECKLIST.md` and `DESIGN.md`, but a native test has to
    serve a ruleset, and a ruleset's JSON shape is `docs/evaluation.md` "Flag", with "Loading a ruleset" for what
    loading does. The list leaves both out. They were read on their own (lines 29 to 58 and 153 to 159, found by
    grep) and the rest of the spec was not. Suggest adding those two sections to phase 3's list.
11. Phase 3 says the test seams "are options on `create`", which reads as exported options, while the same sentence
    allows them to be "internal to the package" and contract 13 allows them only "for its own tests". Suggest "a field
    on the options struct, exported or not".
12. The checklist's "Native tests" lines are contract rules, not test names, and the run's rule only says each line is
    "named in `RUN.md`". A later phase reading the entry cannot tell which test covers which line. Suggest the phase
    entry carry that mapping, which phase 3 did under "Native tests".
13. Rule 9 says "never read two files in one command", while its own first step is a `grep -n` symbol search, which in
    a package naturally spans that package's files. Suggest: "a search may span a directory; a read of a file may
    not". This session searched across directories a few times (`grep -rn "reasonOf" sdks/go/*.go`,
    `grep -rn '"requires"' fixtures/sdk/`), and each printed only its matching lines.
14. Phase 3's check is one `"$L/bin/test"` run, and these tests lean on a clock seam where a flake is likely. Suggest
    running it twice for the phase, as the gate's twice rule does for DB work.
15. Phase 4's "Reads" list names `fixtures/sdk/*.json` for "the scenario names only", but a test service cannot be
    written from names: the answer's shape is the scenario's expectation, and three of this session's first failures
    were a field the name cannot show (`etag` and `flagVersion` are `null`, not `""`, before the first load, in
    `status` and in a hook event). The failures printed those expectations, so the reading stayed inside the list.
    Suggest adding: "each failing scenario prints its expectation; the shape is read from that, not from the file".
16. Phase 4 names no place for the test service, while `DESIGN.md` section 7 names one (`testservice/`) that differs
    from the prompt's own `test-service/`. Suggest naming the directory in the phase: "`$L/test-service/`, as this
    section and the protocol page spell it".
17. Phase 4 says a test service's answer to a non-finite number is JSON `null` in the "The protocol" page, which
    `jsonval.Write` does not do (it writes `1e999`), so the service has to convert before answering. Suggest a
    sentence in phase 4's own text, which a cold reader will otherwise miss in the protocol page's long accessors
    paragraph.
18. Phase 5's item 4 asks for "every tool the `bin/` scripts call", and its Reads list names neither `$L/bin/test`
    nor `$L/bin/test-service`, which is where that list lives. This session took the three tools from `mise.toml`
    and the phase log's carried facts (`bin/test` gives cgo zig, `bin/lint` runs golangci-lint), so no file outside
    the list was read. Suggest adding: "`$L/bin/test` and `$L/bin/test-service`: the tools each one calls, for the
    install line".
19. Phase 5 resolves an action pin with `gh api repos/<owner>/<repo>/commits/<tag> --jq .sha`, while a sibling
    workflow that already uses the action carries a resolved pin, and the list already allows reading that
    sibling for its inputs. Suggest: "a pin a sibling `sdk-<lang>.yml` already carries may be reused as it is; say
    which you did and why".
20. Phase 5's check stages the whole worktree with `git add -A` before the dash, format and workflow checks, while
    rule 2 confines a run to `$L/` and `.github/workflows/sdk-<lang>.yml`. A stray file anywhere else would be
    staged too. Suggest `git add -A "$L" .github/workflows/sdk-<lang>.yml`, which still makes the run's new files
    visible to `check-dashes.ts` (it reads only tracked files).
21. Phase 6's "Reads" list names the checklist, the template and the contract's "Bounds" bullets, while the
    checklist's own lines cite contract 6.5, contract 13, `DESIGN.md` sections 1 and 4, `README.md`,
    `.golangci.yml`, the three `bin/` scripts, the workflow and `test-service/README.md`. This session read each
    of those, one at a time, found by heading or grep, and the rest of each was not read. Suggest adding to the
    list: "the section or file each checklist line cites, found by heading, grep or a read with an offset and a
    limit".
22. The ten-run line says "the ten are of the tree that is committed", and phase 6 is the only session that
    commits, after the runs. It reads as if the commit must come first. Suggest: "the ten are of the final tree;
    any change after them starts them again, and writing `RUN.md` is not one".
23. The loaded-run paragraph says a failure there is a bug in the test but not what the shape looks like. Both
    failures this run were a test that observed something other than the client's state after an asynchronous
    step: the server's request count, and one fake-clock step that landed before the loop armed its wait.
    Suggest adding: "the usual shape is a test that waits for a server-side record, or steps a fake clock without
    waiting for the SDK to arm its wait; wait for the assertion instead".

## Checklist

Every line of [`CHECKLIST.md`](../sdk-authoring/CHECKLIST.md), in its order, walked in this session. The harness
lines name the scenarios its run passed; the rest carry a test, a command and what it printed, or a file and
line. `n/a` means the line names itself that way.

**The two executable checks**

- [x] `bin/test` runs every vector in `fixtures/evaluation/*.json` but `buckets.json`, and every pin, and all
  pass; its count equals phase 2's: `vectors: 1371 pins: 17`, and phase 2's `bun -e` line prints `1371 17`.
- [x] `bin/test` passes 10 times in a row, and an eleventh under CPU load (`nproc` busy loops): ten runs each
  printing `vectors: 1371 pins: 17`, then the loaded run, also `vectors: 1371 pins: 17`. Two flakes were found
  and fixed first (decisions 59 and 60), and the ten started again from the first after them.
- [x] The harness exits 0, with only the 2 named vector skips: `scenarios: 83 passed, 0 failed, 0 skipped;
  vectors: 1369 passed, 0 failed, 2 skipped`. The service advertises `integer-accessor`, so neither required
  scenario was skipped.

**What the harness checks**

- [x] Both context shapes, single and multi (contract 3): the harness, `privacy.json`, and the `kinds.json`
  vectors through the service; the summary above is the run.
- [x] Results follow contract 4's order and the mismatch rule: the harness, `evaluation.json`, `usage.json`.
- [x] With `integer-accessor`, a served fraction is `TYPE_MISMATCH` and a served `2.0` is `2`: the harness,
  `evaluation.json`; `TestIntVariationTypedRules` also pins both.
- [x] No failure of the server, the network or a context raises: the harness, every scenario (the service
  answers 500 on anything raised, and a scenario fails on it).
- [x] Before the first load, evaluation answers `NOT_READY` at once (5.2, 5.4): the harness, `load.json`;
  `TestNotReadyBeforeTheFirstLoad`.
- [x] `ready` settles after the first attempt, bounded, and may be called again; false after `close` if it had
  not settled (5.3, 11): the harness, `load.json`, `close.json`; `TestCloseBeforeReadySettles`.
- [x] An answer that is not a ruleset never replaces the one held (5.5): the harness, `status.json`;
  `TestBadRulesetDoesNotReplaceTheOneHeld` and `TestBodyThatIsNotARulesetKeepsTheRuleset`.
- [x] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining (7, 7.1): the harness,
  `load.json`, `status.json`; `TestNotModifiedIsASync`, and `TestRequestHeaderAudit`'s conditional row.
- [x] The stream: events, SSE parsing, refresh on change, a failed refresh ends the stream (7.2): the harness,
  `stream.json`, `load.json`, `status.json`; `TestStreamFollowsAChangedTag`, `TestSSEParser`,
  `TestStreamCleanEndIsNotAnError`.
- [x] The loop and the jittered backoff (7.3): the harness, `backoff.json`, `stream.json`; `TestDelayFollowsTheContract`
  (the cap, the doubling and the zero jitter), `TestVariationNeverWaits` (a hung request never reaches an
  evaluation) and the three Retry-After tests, which read the armed waits rather than sleeping.
- [x] `429` and `Retry-After` on every route (7.4, 9): the harness, `rate-limit.json`;
  `TestRetryAfterCapsTheRulesetHoldAt300`, `TestRetryAfterCapsTheStreamHoldAt300`,
  `TestRetryAfterHoldsTheUsageReports`.
- [x] Status fields, states and `lastError` codes (8): the harness, `status.json` and the shape check on every
  status read; `TestReadyAndStatusAfterTheFirstLoad`, `TestStreamWatchdogDropsASilentStream`.
- [x] Usage counts: what counts, summing, batches of 500, keep or drop by answer (9): the harness, `usage.json`,
  `rate-limit.json`; `TestUsageFlushBatchesAt500`, `TestUsageRequestOutcomes`, `TestUsageCountsAcrossUTCMidnight`.
- [x] Hook events and `onError`, a hook that raises and an `onError` that raises (10): the harness, `hooks.json`;
  `TestEvaluationThatRaises`, `TestHookValueForTheIntegerAccessor`.
- [x] `close` in order, twice at once, during a hold, with a final flush (11): the harness, `close.json`,
  `rate-limit.json`, `usage.json`, `status.json`; `TestCloseReportsCountsLostToARetryAfterHold`,
  `TestConcurrentOperations`.
- [x] No context, context key or attribute in any request or `onError` message, and no header beyond the
  protocol's (12): the harness, `privacy.json` and the post-scenario checks; `TestRequestHeadersAndPrivacy`
  fails if the context key appears in any recorded request or path. No log line: the source search
  `grep -rn 'log\.\|Println\|Printf\|Fprint' sdks/go --include=*.go`, tests and generated files aside, prints
  only `test-service/main.go`'s `listening` line and its two stderr lines, which carry the listen error and
  nothing else. No error value is built from a context: `failureError` and `networkError` (`client.go`) carry a
  code and the transport's cause, and `unsentError` a count.

**Native tests**

- [x] Every operation of contract 2 from many threads, `close` racing `variation`, two `close` calls racing, no
  error, deadlock or data race, under `-race` (6.1): `TestConcurrentOperations`.
- [x] SHOULD: an `https` URL reaches the TLS path with a local certificate: `TestTLSURLReachesTheTLSPath`
  (`httptest`'s certificate through the `httpClient` seam).
- [x] `variation` and `variationDetail` return within a bound while a ruleset request hangs and while a usage
  flush hangs (5.2, 6.2): `TestVariationNeverWaits`, its two subtests.
- [x] A reader sees one ruleset or the other across a swap, and the hook's `etag` and `flagVersion` belong to
  the same ruleset (6.3): `TestSwapKeepsEtagAndVersionPaired`.
- [x] A hook and an `onError` that call back into the client do not deadlock, and `ready` from a callback
  returns at once (6.4): `TestCallbacksCallBackIntoTheClient`, `TestCloseFromInsideOnError`. `Ready` settles
  before the first failure is reported, so the unsettled case cannot arise (decision 35).
- [x] A stream that gets no bytes is dropped as `timeout`, and a keep-alive restarts it, through a seam and a
  real loopback socket (7.2, "Bounds"): `TestStreamWatchdogDropsASilentStream` (80 ms) and
  `TestStreamKeepAliveRestartsTheWatchdog` (80 ms watchdog, a comment every 25 ms).
- [x] A `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam
  (7.4): `TestRetryAfterCapsTheRulesetHoldAt300`, `TestRetryAfterCapsTheStreamHoldAt300`,
  `TestRetryAfterHoldsTheUsageReports`, each reading the armed hold instead of waiting.
- [x] Counts either side of a UTC midnight go out as two days (9): `TestUsageCountsAcrossUTCMidnight`.
- [x] An evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` (4, step 5):
  `TestEvaluationThatRaises`.
- [x] A hook that fails later: `n/a: no futures` (the hook is a plain `func(Event)`); a hook that raises at once
  is the harness's `hooks.json`.
- [x] With `integer-accessor`, a served number outside signed 64 bits is `TYPE_MISMATCH`: `TestIntVariationTypedRules`
  (also a fraction, `2.0` as `2`, and `2^53` in range).
- [x] A client never closed does not keep the process alive: met by construction, because Go's goroutines never
  keep `main` from returning. The work is started by `New`: `syncLoop`, `flushLoop` and `notifyLoop`.
- [x] A typed accessor whose type admits null: `n/a: the accessor's type cannot hold null` for the five typed
  accessors (`StringVariation` takes a `string`); the JSON accessor's null fallback is
  `TestTypeMismatchAndNullFallback`.
- [x] A generic accessor with an integer fallback: `n/a: no generic method` (decision 8); the integer rule itself
  is `TestIntVariationTypedRules`.
- [x] The hook event's `value` for the integer accessor has the mapping table's type: `TestHookValueForTheIntegerAccessor`
  (a served `2^60`: the event carries the nearest double, the caller the exact integer).
- [x] A context attribute of each numeric type matches as the JSON number of its value (3):
  `TestContextNumericTypesMatch` (`int`, `int8`, `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16`,
  `uint32`, `uint64`, `float32`, `float64`).

**Code and documents**

- [x] `RUN.md` has a phase log entry for each of phases 0 to 6: `## Phase log` below.
- [x] `DESIGN.md` passes the phase 1 row check (the 16-row loop printed nothing, `test "$missing" = 0` exited
  0), and `README.md` links the mapping table at lines 72 to 73.
- [x] The names in the mapping table keep the canonical meaning and defaults, and the closed sets their spelling
  (2): `DESIGN.md` section 1, lines 12 to 58.
- [x] The evaluator has no platform regular expression: `grep -rn regexp sdks/go --include=*.go` printed
  nothing.
- [x] The lint configuration names the rule set and gives every ignored rule a reason: `.golangci.yml` sets
  `default: none` and enables `errcheck`, `govet`, `ineffassign`, `staticcheck` and `unused`; it has no ignore
  list, so no ignored rule needs a reason.
- [x] Every runtime dependency has a line in `DESIGN.md`: `DESIGN.md` section 4, "Runtime: none." (lines 174 to
  177).
- [x] No telemetry, and no header that identifies a user or a machine: the requests set only `Authorization`,
  `Accept`, `If-None-Match` and `Content-Type` (`grep -rn 'req.Header.Set' sdks/go --include=*.go`).
  `TestRequestHeaderAudit` captured one request of each route on the loopback fake and printed this table
  (Host read from `r.Host`, the rest from the header map):

  | Route | The SDK sets | Go's client adds | Why it identifies nobody |
  | --- | --- | --- | --- |
  | `GET /sdk/v1/ruleset` (first) | `Authorization: Bearer <key>` | `Host: 127.0.0.1:<port>`, `Accept-Encoding: gzip`, `User-Agent: Go-http-client/1.1` | the host is the operator's own URL; the agent names the library, not the host or the user; gzip has no value |
  | `GET /sdk/v1/ruleset` (`If-None-Match`) | `Authorization`, `If-None-Match: "<etag>"` | the same three | the ETag names the ruleset, never a context |
  | `GET /sdk/v1/stream` | `Authorization`, `Accept: text/event-stream` | the same three | the same |
  | `POST /sdk/v1/events` | `Authorization`, `Content-Type: application/json` | the same three plus `Content-Length: 79` (the body's length) | the body is counts only (`TestUsageBodyCarriesOnlyTheFourFields`) |

- [x] Every extra beyond the contract is one contract 13 allows, and none weakens contract 5 or sends anything
  new: the extras are the five typed accessors and their `...Detail` siblings (contract 13, typed accessors)
  and the test seams (contract 13, options for its own tests, decision 30). `jsonval` is not an extra: contract
  2 and `DESIGN.md` section 5 make the context the SDK's own JSON value type. There is no logger adapter, no
  OpenFeature package and no tracing package in `sdks/go`.
- [x] Test seams are options on `create` or an internal field of the SDK's config, never a global or an
  environment variable: `Options` carries `httpClient`, `clock`, `jitter`, `watchdog` and `evaluate` as
  unexported fields (decision 30); no source file reads `os.Getenv`.
- [x] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK, and run with only
  the pinned toolchain: `git ls-files -s sdks/go/bin` prints mode `100755` for all three; each does
  `cd "$(dirname "$0")/.."` and calls its tools through `mise exec --` when `mise` is on `PATH`;
  `bin/test-service` printed `listening http://127.0.0.1:43349`.
- [x] `test-service/README.md` maps every accessor of the protocol to an SDK method: its table, lines 20 to 33.
- [x] `.github/workflows/sdk-<lang>.yml` follows the skeleton, filters on its paths, and pins every action by
  commit SHA: `sdk-go.yml` has the skeleton's triggers, `permissions: contents: read`, concurrency group and
  job shape, its paths include `sdks/go/**`, and its three actions carry `# v7.0.1`, `# v2.2.0` and `# v5.2.7`
  beside full SHAs.
- [x] A `status` read on another thread racing a stream failure never shows that failure's `lastError` beside
  `connected` (6.5), under `-race`: `TestStatusNeverPairsAFailureWithConnected` (three timeout attempts, a
  reader sampling `Status` across them).

**Repository**

- [x] No em dash or en dash in any file this run wrote: `bun scripts/check-dashes.ts` printed nothing and exited
  0.
- [x] `bun run format:check` passes: `All matched files use the correct format.` (368 files).
- [x] Only `$L/` and the one workflow file changed: the phase 6 check printed only `scope ok`.
- [x] No package was published and no name reserved: no registry call, upload or tag was made in this run; the
  module path is the repository path and nothing is registered under it.
- [x] Commit subjects are Conventional Commits, none `feat`, with no `Co-Authored-By` or AI attribution: the one
  commit is `chore(sdk-go): the Go server SDK regenerated cold from the authoring prompt`.
- [x] The workflow's job is `runs-on: ubuntu-latest` with no `if:` guard, triggers only `pull_request` and
  `push`, installs its own toolchain, and the workflow check passes: `bun scripts/check-workflows.ts
  .github/workflows/*.yml` printed `8 workflow(s) read, 0 failure(s)`.
- [x] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of this list: the phase 6 check's
  heading loop printed nothing.

## Phase log

One entry per phase, 0 to 6, written by the session that ran it (`PROMPT.md`, "How a run is split").
Each entry is all the next session gets, so write it to be read cold.

### Phase 0: read and set up

- Check: `(cd sdks/go && go version)` -> `go version go1.27.4 linux/amd64`. It prints exactly the version `sdks/go/mise.toml` pins (1.27.4, `linux/amd64`).
- Files written: `sdks/go/mise.toml` (go 1.27.4, golangci-lint 2.15.0, zig 0.17.1) and `sdks/go/RUN.md` (this report). The previous Go tree was removed first (`sdks/go/` and `.github/workflows/sdk-go.yml`), so `sdks/go` holds only these two files. `bun install --frozen-lockfile` ran clean, and `mise trust` then `mise install` in `sdks/go` reported every pin installed.
- Decisions: go 1.27.4, minimum 1.26, `gc` runtime, the `go` command with modules, `golangci-lint` 2.15.0 with `gofmt -l`, and zig 0.17.1 for cgo. Reasons are in Decisions above.
- Open items: `go.mod`, the packages, `bin/lint`, `bin/test`, `bin/test-service` and the workflow do not exist yet; the phases after this one create them. `bin/test` must print one `vectors: <n> pins: <n>` line and must run gofmt and golangci-lint through `mise exec --`.
- Next phase reads first: this entry, then the Decisions above, then `PROMPT.md` "Phase 1: design". Carry these Go facts from `docs/sdk-authoring/language-notes/go.md` (one heading, `# Go`; cite each by its bold label): "`encoding/json`" (write the JSON reader, hold strings as WTF-8), "Headers `net/http` adds itself", "Redirects", "A network error's text carries the URL", "Bounding a body read", "A hung test handler must drain the request body first", "`-race` needs cgo" (name it `CC="$(mise where zig)/zig cc"`), "`mise ls-remote go` can hide the newest releases", "Lint", "Stopping a test", "Printing the vector and pin counts", "Errors collected by `onError`", "A test callback that needs the client", "The SDK's own goroutine (contract 6.4)", "A recovered panic is a raised error", "The test fake's stream", "A test never writes to a client's config after `New`", "Name a finite check for what it checks", "A fake clock shared by two loops".

### Phase 1: design

- Check: the phase 1 check of `PROMPT.md`, run from the repository root with `L=sdks/go` (the 16 row `grep -qF "| \`<name>\` |"` loop, then `test "$missing" = 0`). Its last output line is none, because it prints only a missing row: no line was printed and the final `test "$missing" = 0` exited 0, so all sixteen required rows are in `sdks/go/DESIGN.md`.
- Files written: `sdks/go/DESIGN.md` (new, the whole design, items 1 to 7) and `sdks/go/RUN.md` (this entry, Decisions 8 to 19, Proposed prompt changes 2 to 4). No other file changed in this session.
- Decisions: 8, the five typed accessors with Detail siblings and no generic method; 9, a plain `Options` struct with `Stream *bool` and zero durations meaning not given; 10, the hook event's value as `jsonval.Value`; 11, `onError` on one notify goroutine that `Close` never joins; 12, a hand written JSON reader and writer with strings as UTF-8 or WTF-8; 13, per request context deadlines and, for the stream, an open timer stopped at the headers plus a 60 s watchdog on one cancel; 14, a hand written SSE parser; 15, redirects followed; 16, the module, root package `toggleslash` and the `jsonval`, `eval` and `testservice` subpackages; 17, `go 1.26` in `go.mod`; 18, `golangci-lint` with a committed config plus `gofmt -l`; 19, the vectors test reads the repository fixtures rather than a copy. Reasons are in Decisions above.
- Open items for the phases after this one:
  - `go.mod` does not exist yet. Phase 2 creates it (`go mod init github.com/madangryscientist/toggleslash/sdks/go`, then `go 1.26`), and with it `jsonval` and `eval`.
  - `bin/lint`, `bin/test`, `bin/test-service`, `.golangci.yml` and `.gitignore` do not exist yet. When they do, `bin/test` must print `vectors: <n> pins: <n>`, run under `-race` with `CC` set to zig's `cc`, and build `build/testservice` last; `bin/lint` runs `gofmt -l` and `golangci-lint`, each through `mise exec --` when `mise` is on `PATH`.
  - The concurrency, HTTP and SSE sections of `DESIGN.md` (2 and 3) are for phases 3 and 4 to implement; nothing in phase 1 depends on them being revisited.
  - No number in this entry is a guess and no question was asked in this phase: every open point had a decision to make, and it was made and recorded.
- Next phase reads first: this entry, then `sdks/go/RUN.md` Decisions 8, 12, 16, 17 and 19, then `sdks/go/DESIGN.md` sections 5 (JSON), 7 (layout) and 1 (the accessor shape row and the closed sets), and then `PROMPT.md` "Phase 2: evaluator" with the sections its Reads list names. Carry these facts cold: the module path is `github.com/madangryscientist/toggleslash/sdks/go`; the JSON value type is `jsonval.Value`, a sealed interface of `Null`, `Bool`, `Number` (binary64), `String` (UTF-8 or WTF-8), `Array` and `Object`, with `jsonval.Read(r, maxDepth)` and `jsonval.Write(w, v)`; the evaluator is package `eval` and imports only `jsonval`; the integer accessor is `int64`; the vectors test lives in `sdks/go/eval` and walks up to `fixtures/evaluation`; and no file this run writes may carry an em dash or an en dash.

### Phase 2: evaluator

- Check: from the repository root with `L=sdks/go`, `"$L/bin/test"` and then the `bun -e` line of "Phase 2: evaluator". `bin/test` passed and its last output line is `vectors: 1371 pins: 17`; the `bun -e` line printed `1371 17`. The two match, and both commands were run from this worktree.
- Files written: `sdks/go/go.mod` (module `github.com/madangryscientist/toggleslash/sdks/go`, `go 1.26`, no requirements, so no `go.sum`); `sdks/go/jsonval/jsonval.go`, `sdks/go/jsonval/decode.go`, `sdks/go/jsonval/wtf8.go`; `sdks/go/eval/ruleset.go`, `sdks/go/eval/evaluate.go`, `sdks/go/eval/context.go`, `sdks/go/eval/clause.go`, `sdks/go/eval/bucket.go`, `sdks/go/eval/semver.go`, `sdks/go/eval/instant.go`, `sdks/go/eval/pattern.go`, `sdks/go/eval/vector_test.go`; `sdks/go/bin/test` (executable). `sdks/go/RUN.md` holds this entry, Decisions 20 to 28 and proposed changes 5 to 8. Nothing outside `sdks/go/` changed.
- Decisions: 20, `bin/test` ends with the count line it greps from the captured test output; 21, it sets `CC` to zig's `cc` itself; 22, no test service build until phase 4; 23, `jsonval.Write` waits for phase 3; 24, a class escape like `\D` is expanded to complement ranges so `[^\S]` means space; 25, a pattern is parsed per evaluation rather than cached; 26, the test compares `Result.ToJSON` deeply by IEEE-754; 27, the pattern size table tells a bare quantifier from a braced one; 28, the vectors test is the only test. Reasons are in Decisions above.
- Reads: the sections of `docs/evaluation.md` that phase 2 names, one section at a time (the whole page is named, so every section was read); `fixtures/evaluation/clauses.json` and `buckets.json` for their shapes, one file at a time; `sdks/go/DESIGN.md` items 5 and 7; and, as the optional references, nine files of `packages/engine/src` (`evaluate`, `validate`, `ruleset`, `clause`, `context`, `bucket`, `reference`, `date`, `semver`), each read whole because each is 15 to 142 lines and holds one function or one loop (proposed change 9). Nothing else was read, and no other vector file was opened.
- Open items for the phases after this one:
  - `jsonval.Write(w, v)` does not exist yet: phase 3 adds it for the events body and phase 4 for the test service's replies. `jsonval.Read(r, maxDepth)` and `CodePoints(s)` are there.
  - `bin/lint`, `.golangci.yml` and `.gitignore` still do not exist; phase 5 creates them. `golangci-lint run ./...` with the default configuration reports 0 issues today, and `gofmt -l` is empty.
  - `bin/test` has no test service step: phase 4 adds `build/testservice` after the test run (decision 22).
  - No question was asked in this phase and no number in this entry is a guess: every open point had a decision to make, and it was made.
- Next phase reads first: this entry, then `sdks/go/RUN.md` Decisions 8 (accessor shape), 9 (`Options`), 10 (the hook's value), 11 (`onError`'s goroutine), 12 (JSON), 13 (HTTP bounds), 14 (the SSE parser), 15 (redirects), 16 (layout) and 20 to 28, then `sdks/go/DESIGN.md` sections 2 (concurrency), 3 (HTTP and SSE), 4 (dependencies), 5 (JSON) and 7 (layout), then `PROMPT.md` "Phase 3: client" with the sections its Reads list names. Carry these facts cold: the module is `github.com/madangryscientist/toggleslash/sdks/go` with `go 1.26` in `go.mod` and no dependencies; `jsonval` holds `Value` (`Null`, `Bool`, `Number`, `String`, `Array`, `Object`), `Read(r io.Reader, maxDepth int)` and `CodePoints(s string) []rune`, and gains `Write(w io.Writer, v Value) error` in phase 3; the evaluator is package `eval`, imports only `jsonval`, and its surface is `eval.DefaultKind`, `eval.BucketCount`, the code constants (`CodeInvalidContext` and the six others), `eval.NewRuleset(flags, segments jsonval.Value) *eval.Ruleset` (never fails, non-array or absent counts as empty), `(*Ruleset).Evaluate(flagKey string, context jsonval.Value) eval.Result`, `eval.Result{Value jsonval.Value, VariationIndex *int, Reason eval.Reason}`, `eval.Reason{Kind, RuleIndex *int, FlagKey, Code string}` with `Kind` one of `ReasonOff`, `ReasonPrerequisiteFailed`, `ReasonTargetMatch`, `ReasonRuleMatch`, `ReasonFallthrough`, `ReasonError`, and `Result.ToJSON() jsonval.Object`; `bin/test` runs `go build ./...` then `go test -race -count=1 -v ./...` through `mise exec --` and ends with the counts line; the vectors test is `sdks/go/eval/vector_test.go` and walks up from the package directory to `fixtures/evaluation`; and no file this run writes may carry an em dash or an en dash.

### Phase 3: client

- Check: from the repository root with `L=sdks/go`, `"$L/bin/test"`, the phase's only check. Its last output line is
  `vectors: 1371 pins: 17`. It printed that line on three runs in a row, the vectors (1371) and pins (17) unchanged
  from phase 2. Every native test below ran under `-race`, inside that one command.
- Files written: `sdks/go/options.go`, `api.go`, `client.go`, `stream.go`, `events.go`, `ruleset.go`, `context.go` and
  `clock.go` (new, package `toggleslash`); `sdks/go/jsonval/write.go` (new: `Write`, which decision 23 deferred to this
  phase); `sdks/go/eval/ruleset.go` (a flag's `version` kept at load and `(*Ruleset).FlagVersion`, decision 33);
  `sdks/go/client_test.go`, `stream_test.go`, `events_test.go`, `concurrency_test.go`, `testserver_test.go` and
  `fakeclock_test.go` (new); `sdks/go/RUN.md` (this entry, Decisions 29 to 43, proposed changes 10 to 14). No other
  file changed, `go.mod` kept `go 1.26` and no dependency was added.
- Decisions: 29, the module root layout and `clock.go`; 30, seams as unexported `Options` fields; 31, the clock seam and
  which waits the fake clock steps; 32, the snapshot and status atomics with a writers' mutex; 33, `eval.FlagVersion`;
  34, `Reason` as kind and code; 35, `Ready` settles before the first failure is reported; 36, usage requests outside
  the client's context; 37, the integer accessor's range rule; 38, the JSON accessor's null fallback; 39, a stable
  order for a flush's entries; 40, the flush outcome table; 41, counts lost at close; 42, the watchdog reset and the
  connected store; 43, the HTTP client and redirects. Reasons are in Decisions above.
- Native tests, each against the `CHECKLIST.md` "Native tests" line it covers:
  - many threads, `close` racing `variation`, two closes racing, under `-race` (6.1): `TestConcurrentOperations`.
  - an `https` URL reaches the TLS path with a local certificate: `TestTLSURLReachesTheTLSPath` (`httptest`'s own
    certificate through the `httpClient` seam).
  - `variation` returns within a bound while a ruleset request hangs and while a usage flush hangs (5.2, 6.2):
    `TestVariationNeverWaits`, its two subtests.
  - a reader sees one ruleset or the other across a swap, and the hook's `etag` and `flagVersion` pair (6.3):
    `TestSwapKeepsEtagAndVersionPaired`.
  - a hook and an `onError` that call back into the client do not deadlock, and `ready` from those callbacks returns at
    once (6.4): `TestCallbacksCallBackIntoTheClient`, with `TestCloseFromInsideOnError` for a `close` from a callback.
    The order that rules the unsettled case out is decision 35.
  - a stream that gets no bytes for the watchdog's length is dropped as `timeout`, and a keep-alive restarts it, on a
    real loopback socket through the seam: `TestStreamWatchdogDropsASilentStream` (80 ms) and
    `TestStreamKeepAliveRestartsTheWatchdog` (80 ms watchdog, a comment every 25 ms).
  - a `Retry-After` above 300 is held for 300 s on the ruleset, stream and events routes, through a seam:
    `TestRetryAfterCapsTheRulesetHoldAt300`, `TestRetryAfterCapsTheStreamHoldAt300` and
    `TestRetryAfterHoldsTheUsageReports`, each reading the armed timer or the hold rather than waiting.
  - counts either side of a UTC midnight go out as two days: `TestUsageCountsAcrossUTCMidnight`.
  - an evaluation that raises gives the fallback, `ERROR EVALUATION_FAILED` and one `onError` call:
    `TestEvaluationThatRaises`.
  - a hook that fails later: `n/a: no futures` (the hook is a plain `func(Event)`). A hook that raises at once is the
    harness's `hooks.json`; this phase has no native test for it, because the recovery path is the one
    `TestEvaluationThatRaises` proves for the evaluator.
  - a served number outside signed 64 bits is `TYPE_MISMATCH` on the integer accessor: `TestIntVariationTypedRules`
    (also a fraction, `2.0` as `2`, and `2^53` in range).
  - a client never closed does not keep the process alive: met by construction, because Go's goroutines never keep
    `main` from returning. The work is started by `New` (`syncLoop`, `flushLoop`, `notifyLoop`).
  - a typed accessor whose type admits null, given a null fallback: `n/a: the accessor's type cannot hold null` for
    the five typed accessors (`StringVariation` takes a `string`); the JSON accessor's null fallback is
    `TestTypeMismatchAndNullFallback`.
  - a generic accessor with an integer fallback type: `n/a: no generic method` (decision 8); the integer rule itself is
    `TestIntVariationTypedRules`.
  - the hook event's value for the integer accessor has the type the mapping table names: `TestHookValueForTheIntegerAccessor`
    (a served `2^60`, the event carrying the nearest double, the caller the exact integer).
  - a context attribute of each of the language's numeric type widths matches as the JSON number of its value:
    `TestContextNumericTypesMatch` (`int`, `int8`, `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16`, `uint32`,
    `uint64`, `float32`, `float64`, each converted as a caller converts it, `jsonval.Number(float64(v))`).
- Supporting tests, not claimed against a checklist line: `TestNotReadyBeforeTheFirstLoad`, `TestReadyAndStatusAfterTheFirstLoad`,
  `TestNotModifiedIsASync`, `TestBadRulesetDoesNotReplaceTheOneHeld`, `TestBodyThatIsNotARulesetKeepsTheRuleset`,
  `TestRequestHeadersAndPrivacy` (the header audit and the context key absent from every request),
  `TestStreamRequestHeaders`, `TestStreamFollowsAChangedTag`, `TestStreamCleanEndIsNotAnError`, `TestSSEParser` (line
  endings, comments, joined data lines, a dropped event, an incomplete event at EOF), `TestUsageFlushBatchesAt500`,
  `TestUsageRequestOutcomes`, `TestCloseReportsCountsLostToARetryAfterHold`, `TestUsageBodyCarriesOnlyTheFourFields`
  and `TestCloseBeforeReadySettles`.
- Reads: the sections of `docs/sdk-contract.md` that phase 3 names, 2 to 12, one section at a time, then section 13
  alone by grep (the brief's phase 3 note says the phase text points at it); the "SDK read API (`/sdk/v1`)" section of
  `docs/api.md`; `CHECKLIST.md` "Native tests"; `DESIGN.md` sections 1, 2, 3, 5 and 7; `sdks/go/RUN.md`'s phase 2
  entry and Decisions; `eval`'s and `jsonval`'s declarations by grep, for the symbols the client calls; and, for the
  shape a native test serves, `docs/evaluation.md` "Flag" and "Loading a ruleset" (proposed change 10). Nothing else,
  no whole file, and one file per read.
- Open items for the phases after this one:
  - The test service is phase 4's: `bin/test` still has no `build/testservice` step (decision 22), and
    `testservice/` does not exist. `docs/sdk-test-service.md` is phase 4's to read.
  - `Reason` has no `ruleIndex` (decision 34). If the harness's result shape wants one, phase 4 adds it to `Reason`,
    `reasonOf`, the test service and the mapping table.
  - `README.md`, `bin/lint`, `bin/test-service`, `.golangci.yml` and `.gitignore` are phase 5's. `gofmt` is clean now
    and `golangci-lint run ./...` with the default configuration reports no issue, but neither is wired into a script.
  - The fake server (`newFakeServer`, `testserver_test.go`), the fake clock (`newFakeClock`, `fakeclock_test.go`) and
    the ruleset builder (`flagSpec`, `rulesetOf`, `boolFlag`, `client_test.go`) are reusable by phase 4's test service
    tests.
  - A delivered `onError` can arrive after `Close` returns (the notify goroutine drains the queue it was given), which
    contract 10 allows because the thread is the SDK's choice. Nothing in phase 4 should wait for `onError` after
    `Close` without a timeout.
  - No question was asked in this phase and no number in this entry is a guess: every open point had a decision to
    make, and it was made and recorded.
- Next phase reads first: this entry, then `sdks/go/RUN.md` Decisions 8 (accessor shape), 9 (`Options`), 29 to 43,
  then `sdks/go/DESIGN.md` section 7 (layout) and section 2 (goroutines), then `PROMPT.md` "Phase 4: test service" with
  the sections its Reads list names. Carry these facts cold: the module is `github.com/madangryscientist/toggleslash/sdks/go`
  with `go 1.26` and no dependencies; the public package is `toggleslash` at the module root with
  `New(opts Options) (*Client, error)` (an error only for a missing `URL` or `SDKKey`), `Options` with `URL`, `SDKKey`,
  `Stream *bool`, `PollInterval`, `FlushInterval`, `RequestTimeout`, `Hooks{AfterEvaluation func(Event)}` and
  `OnError func(error)`; the five accessors `BoolVariation`, `StringVariation`, `IntVariation` (int64), `FloatVariation`
  and `JSONVariation`, each with a `...Detail` sibling returning `Detail[T]{Value, VariationIndex *int, Reason}`;
  `Ready(ctx context.Context) bool`, `Status() Status{ETag string, LastSyncAt *time.Time, Stream StreamState,
  LastError *LastError}` and `Close()`; `Context` is `jsonval.Value`; the event is
  `Event{FlagKey, Context, Value jsonval.Value, VariationIndex *int, Reason, ETag string, FlagVersion *int64}`; the
  ruleset is `eval.NewRuleset(flags, segments)` behind an atomic snapshot with the ETag, and `eval` now also has
  `(*Ruleset).FlagVersion(key) *int64`; `jsonval` has `Read`, `Write`, `CodePoints`, and `Write` writes a lone
  surrogate as its `\uXXXX` escape and an infinity as `1e999`; `bin/test` runs `go build ./...` then
  `go test -race -count=1 -v ./...` through `mise exec --`, ends with the counts line, and does not build a test
  service yet; and no file this run writes may carry an em dash or an en dash.

### Phase 4: test service

- Check: from the repository root with `L=sdks/go`, the phase's only check,
  `"$L/bin/test" && bun run conformance -- --service "$L/bin/test-service"`. It exited 0. It prints two result
  lines and both are here: `bin/test` printed `vectors: 1371 pins: 17` (1371 vectors less the 2 named `load`
  skips is the harness's 1369), and the check's last output line is
  `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`. The service advertised
  `integer-accessor` (the harness printed `service: toggleslash-sdk (Go); capabilities: integer-accessor`), so no
  scenario was skipped, and the vectors show exactly the 2 named skips. The harness was then run a second time on
  the same binary and printed the same summary line.
- Files written: `sdks/go/test-service/main.go` and `sdks/go/test-service/README.md` (new: the service, and the
  accessor mapping phase 4 requires); `sdks/go/bin/test-service` (new, executable); `sdks/go/.gitignore` (new,
  `build/` only); `sdks/go/bin/test` (edited: builds `build/testservice` after the test run and before the counts
  line); `sdks/go/api.go` (edited: `Reason` gains `RuleIndex *int` and `FlagKey string`); `sdks/go/client.go`
  (edited: `reasonOf` fills them); `sdks/go/DESIGN.md` (edited: section 1's `Reason` row and section 7's
  `test-service/` directory); `sdks/go/RUN.md` (this entry, Decisions 44 to 51, proposed changes 15 to 17). Nothing
  outside `sdks/go/` changed.
- Decisions: 44, the service is `test-service/` (the prompt's spelling) and not decision 16's `testservice/`;
  45, `Reason` gains `RuleIndex` and `FlagKey` because the vectors compare them; 46, bodies and answers go through
  `jsonval`, and a non-finite number is answered as JSON `null`; 47, the accessor mapping (`any` and `json` are
  `JSONVariationDetail`, `int` answers through a `float64`); 48, `net/http`'s mux and goroutine per request, a
  recover-to-500 at the handler, clients kept after `close`; 49, `etag`, `lastSyncAt`, `lastError`,
  `variationIndex` and `flagVersion` are `null` when they have no value; 50, `bin/test` builds the service binary
  plain and last, and `bin/test-service` rebuilds only when it is missing; 51, `.gitignore` holds `build/` now.
  Reasons are in Decisions above.
- What the first harness run caught, worth knowing before phase 6: 7 scenarios and 11 vectors failed, and both
  kinds were a shape the scenario *names* cannot show. `etag` is `null`, not `""`, before the first load, in
  `status` and in a hook event; and a reason must carry `flagKey` on `PREREQUISITE_FAILED` (11 vectors) and
  `ruleIndex` on `RULE_MATCH`, which the public `Reason` had dropped in phase 3. The failing scenario printed its
  expected status and hook event, which is how the shapes were read without opening a fixture body.
- Open items for the phases after this one:
  - `bin/lint`, `.golangci.yml`, `README.md` and `.github/workflows/sdk-go.yml` do not exist; phase 5 writes them.
    `.gitignore` now holds `build/` alone and phase 5 extends it.
  - `gofmt -l` is empty, `go vet ./...` is clean and `golangci-lint run ./...` with the default configuration
    reports 0 issues on this tree (run under `mise exec --`), so phase 5 has a clean starting point.
  - No native test covers the test service: the harness is its test, and phase 6's checklist walk should name that
    rather than claim a unit test.
  - The test service's binary is `build/testservice`, built by `bin/test` and ignored by git. A tree that has never
    run `bin/test` gets a cold compile from `bin/test-service`, which is why it rebuilds only when the binary is
    missing; the harness's 30 s wait covers the build in practice (a build is about 1 s warm here).
  - `Reason` gained two fields, so the phase 5 README and any mapping table there should describe
    `{ kind, ruleIndex, flagKey, code }`, and `DESIGN.md` section 1 already does.
  - No question was asked in this phase and no number in this entry is a guess: every open point had a decision to
    make, and it was made and recorded.
- Next phase reads first: this entry, then `sdks/go/RUN.md` Decisions 44 to 51, then `sdks/go/DESIGN.md` section 7
  (layout) and section 1's `Reason` row, then `PROMPT.md` "Phase 5: package" with the sections its Reads list
  names. Carry these facts cold: the module is `github.com/madangryscientist/toggleslash/sdks/go` with `go 1.26`
  and no dependencies; the test service is `package main` in `sdks/go/test-service/`, speaks
  `docs/sdk-test-service.md`, listens on `127.0.0.1:<os-picked port>`, prints `listening http://127.0.0.1:<port>`
  and flushes it, advertises `integer-accessor`, maps `bool`/`string`/`number`/`int`/`json`/`any` onto the five
  `...VariationDetail` methods (`any` and `json` both to `JSONVariationDetail`), answers a non-finite number as
  JSON `null`, and turns a panic at a handler into a 500; `bin/test` runs `go build ./...`, then
  `go test -race -count=1 -v ./...`, then `go build -o build/testservice ./test-service`, then prints the counts
  line; `bin/test-service` runs `build/testservice` and builds it only when missing; `Reason` is
  `{ Kind, RuleIndex *int, FlagKey string, Code }`; `build/` is ignored; and no file this run writes may carry an
  em dash or an en dash.

### Phase 5: package

- Check: phase 5's one check line, run from the repository root with `L=sdks/go`:
  `"$L/bin/lint" && "$L/bin/test" && git add -A && bun run format:check && bun scripts/check-dashes.ts && bun scripts/check-workflows.ts .github/workflows/*.yml`.
  It exited 0. It prints several result lines and all of them are here: `0 issues.` then
  `lint: gofmt and golangci-lint clean` (bin/lint); `vectors: 1371 pins: 17` (bin/test's last line, the same line
  its verbose vectors test prints just above); `All matched files use the correct format.` (oxfmt, 368 files);
  `8 workflow(s) read, 0 failure(s)` (the check's last output line). `check-dashes.ts` printed nothing, which is
  its clean answer.
- Files written: `sdks/go/.gitignore` (extended: `*.test`, `coverage.out`, `coverage.html`), `sdks/go/.golangci.yml`
  (new), `sdks/go/bin/lint` (new, executable), `sdks/go/README.md` (new), `.github/workflows/sdk-go.yml` (new) and
  `sdks/go/RUN.md` (this entry, Decisions 52 to 58, Proposed prompt changes 18 to 20). Nothing else changed:
  `git status --short` before the commit listed exactly those six paths.
- Decisions: 52, what `.gitignore` holds and why the Go caches need no entry; 53, the five linters pinned by name
  with `default: none` and the formatter left out; 54, `bin/lint`'s four roots and no test exemption; 55,
  `jdx/mise-action` rather than `actions/setup-go`, because `bin/lint` calls golangci-lint and `bin/test` gives
  cgo zig; 56, the pin reused from three sibling workflows instead of resolved with `gh api`; 57, the `ci.yml`
  lines and `BUN_VERSION` copied as they stand; 58, the README's shape and the `--replace` quickstart. Reasons are
  in Decisions above.
- Open items for the phases after this one:
  - The package is complete: `bin/lint` and `bin/test` are the entry points from "Entry points", all three exist
    (`bin/test-service` since phase 4), and the workflow installs exactly the three tools they call.
  - This check does not run the harness. Phase 4's result still stands and phase 6 runs it: `scenarios: 83 passed,
    0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`.
  - `bun run format:check` passes over every file this phase wrote, `mise.toml` included, and oxfmt rewrote
    nothing in the two new YAML files.
  - The action pin may lag: a tag newer than `v5.2.7` may exist, and this run deliberately pinned the one the other
    three SDK workflows carry (decision 56). A refresh of `jdx/mise-action` should move all four together.
  - The `Results` section above still reads as phases 3 and 4 left it (conformance "None yet"); phase 6 writes the
    report's final numbers and walks the checklist.
  - No question was asked in this phase and no number in this entry is a guess: the two choices the brief leaves
    open (how to look up a version, which tool for the toolchain step) were decided and recorded as 55 and 56.
- Next phase reads first: this entry, then `sdks/go/RUN.md` Decisions 44 to 58, then `PROMPT.md` "Phase 6: review
  and report" with the sections its Reads list names, and `CHECKLIST.md`, which it walks. Carry these facts cold:
  the gate is `sdks/go/bin/lint` (gofmt over `. ./eval ./jsonval ./test-service`, then `golangci-lint run ./...`
  with the five pinned linters, ending `lint: gofmt and golangci-lint clean`), `sdks/go/bin/test` (ending
  `vectors: 1371 pins: 17`), `bun run conformance -- --service sdks/go/bin/test-service`, `bun run format:check`,
  `bun scripts/check-dashes.ts` and `bun scripts/check-workflows.ts .github/workflows/*.yml`; the workflow is
  `.github/workflows/sdk-go.yml`, named `SDK (Go)`, on `pull_request` and `push` to `main` with the skeleton's
  paths, `permissions: contents: read`, `BUN_VERSION: "1.4.2"`, and a `jdx/mise-action` step
  (`b44d12e7d0d8ee7461bb893e1b1993500433185b # v5.2.7`) whose `working_directory: sdks/go` and
  `install_args: go golangci-lint zig` install the pinned toolchain before `bun install --frozen-lockfile`,
  `bin/lint`, `bin/test` and the conformance run; `sdks/go/README.md` holds the quickstart, the API summary, the
  five guarantees and the three commands; `sdks/go/.golangci.yml` pins errcheck, govet, ineffassign, staticcheck
  and unused; and no file this run writes may carry an em dash or an en dash.


### Phase 6: review and report

- Check: phase 6's check of `PROMPT.md`, run from the repository root with `L=sdks/go` (the heading loop, then
  `git add -A` and the scope `grep` against `git merge-base HEAD origin/main`). It printed only `scope ok`: the
  heading loop found every section and nothing outside `sdks/go/` and `.github/workflows/sdk-go.yml` is staged.
  The run's other checks, each a result line of its own: `bin/lint` -> `lint: gofmt and golangci-lint clean`;
  `bin/test` eleven times (ten in a row, then one under `nproc` busy loops) -> `vectors: 1371 pins: 17` every
  time; `bun run conformance -- --service sdks/go/bin/test-service` ->
  `scenarios: 83 passed, 0 failed, 0 skipped; vectors: 1369 passed, 0 failed, 2 skipped`; `bun run format:check`
  -> `All matched files use the correct format.` (368 files); `bun scripts/check-dashes.ts` printed nothing;
  `bun scripts/check-workflows.ts .github/workflows/*.yml` -> `8 workflow(s) read, 0 failure(s)`.
- Files written: `sdks/go/client_test.go` (`TestNotModifiedIsASync` now waits for the sync it asserts, and the
  new `TestRequestHeaderAudit`, `TestReadyStaysFixedWithACancelledContext` and
  `TestNetworkErrorMessageCarriesNoURL`), `sdks/go/concurrency_test.go` (new
  `TestStatusNeverPairsAFailureWithConnected`), `sdks/go/events_test.go`
  (`TestRetryAfterHoldsTheUsageReports` steps again only while a timer is armed), `sdks/go/testserver_test.go`
  (the request record keeps `r.Host`), `sdks/go/eval/clause_test.go` and `sdks/go/jsonval/jsonval_test.go`
  (new), `sdks/go/jsonval/clone.go` (new), and the review's source fixes: `eval/clause.go` (the three substring
  ops over UTF-16 units), `eval/pattern.go` (a saturating pattern size), `jsonval/decode.go` (a truncated
  sequence consumes only its maximal subpart), `client.go` (a fixed `onError` text, optional whitespace only in
  `Retry-After`, and a custom default transport that no longer panics), `api.go` (a settled `Ready` wins over a
  done context, and the JSON accessor returns a copy), `events.go` (`closed` checked under `usageMu`),
  `stream.go` (an open-timer cancel is `timeout`, and the BOM check restarts the watchdog per byte), `bin/lint`
  (`gofmt -l .` recurses on its own), `bin/test` (a missing count line fails) and `eval/instant.go` (epoch
  milliseconds in int64, for 32 bit builds). `sdks/go/RUN.md` carries Results, Decisions 59 to 76, Proposed
  prompt changes 21 to 23, the checklist walk and this entry.
- Decisions: 59 and 60, the two flaky tests and their fixes; 61, the contract 6.5 test keeps a real watchdog on
  a real socket; 62, the header table is the header-audit test's output; 63 to 76, the defects three skeptical
  review rounds found (UTF-16 substring matching, the pattern size's overflow, the truncated JSON sequence,
  `onError`'s text carrying the URL, a settled `Ready` losing to a cancelled context, the usage-count/close
  race, the BOM watchdog, the shared-array mutation, the replaced default transport, the redirect that loses a
  usage batch, post-close `onError` delivery, the hook's shared pointers, the poll a close could still start,
  and three smaller ones). All of them also sit under Decisions.
- Open items: None for a phase after this one. The run's remaining steps are outside `RUN.md`: the one commit,
  the branch push, the PR and the verdict. Phase 5's open item about `jdx/mise-action` possibly having a newer
  tag stands and is not phase 6's.
- Next phase reads first: nothing: phase 6 is the last phase, and the run ends with the commit, the PR and the
  verdict.
