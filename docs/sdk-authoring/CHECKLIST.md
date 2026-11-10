# SDK checklist: the definition of done

Every line is a check you can run or point at. Each names where its rule comes from. "Contract" is
[`docs/sdk-contract.md`](../sdk-contract.md); "the harness" is `bun run conformance -- --service
"$L/bin/test-service"`, whose scenarios are in `fixtures/sdk/`. A line that says "the harness" is checked
when phase 4's check passes; every other line needs its own evidence in `RUN.md` (a test name, a command, or
a file and line). A line marked SHOULD comes from a contract SHOULD: missing it is a decision in `RUN.md`
with its reason, not a stuck point. A line that does not apply to your language says why, in `RUN.md`.

The harness is necessary and not sufficient: the lines under "Native tests" and "Code and documents" are
what it cannot see.

## The two executable checks

- [ ] `$L/bin/test` runs every vector in `fixtures/evaluation/*.json` but `buckets.json`, read from the
  repository at test time, and every pin in `buckets.json` (`uint32` and `bucket`), and all pass; its count
  equals the phase 2 count command's (evaluation.md, "The vectors").
- [ ] The harness exits 0, with the only skips the 2 named vectors and, without `integer-accessor`, the 2
  scenarios that require it (sdk-test-service.md, "Running it").

## What the harness checks (contract MUSTs it covers)

- [ ] Both context shapes, single and multi, are accepted (contract 3): the harness, `privacy.json`, and the
  `kinds.json` vectors run through the service.
- [ ] Results follow the order of contract 4 and the mismatch rule, with the SDK's own error codes: the
  harness, `evaluation.json`, `usage.json`.
- [ ] With `integer-accessor`, a served fraction is `TYPE_MISMATCH` and a served `2.0` is `2` (contract 4,
  "Typed languages"): the harness, `evaluation.json`.
- [ ] No failure of the server, the network or a context makes an operation raise (contract 5.1): the
  harness, every scenario (the service answers 500 on anything raised, and the scenario fails).
- [ ] Evaluating before the first load answers `NOT_READY` at once (contract 5.2, 5.4): the harness,
  `load.json`.
- [ ] `ready` settles after the first attempt, bounded, and may be called again; false after `close` if it
  had not settled (contract 5.3, 11): the harness, `load.json`, `close.json`.
- [ ] An answer that is not a ruleset never replaces the one held (contract 5.5): the harness, `status.json`.
- [ ] The ruleset request, `If-None-Match`, the ETag kept exactly, URL joining (contract 7, 7.1): the
  harness, `load.json`, `status.json`.
- [ ] The stream: events, SSE parsing, refresh on change, a failed refresh ends the stream (contract 7.2):
  the harness, `stream.json`, `load.json`, `status.json`.
- [ ] The loop and the jittered backoff (contract 7.3): the harness, `backoff.json`, `stream.json`.
- [ ] `429` and `Retry-After` on every route (contract 7.4, 9): the harness, `rate-limit.json`.
- [ ] Status fields, states and `lastError` codes (contract 8): the harness, `status.json`, and the shape
  check on every status read.
- [ ] Usage counts: what counts, summing, batches of 500, keep or drop by answer (contract 9): the harness,
  `usage.json`, `rate-limit.json`.
- [ ] Hook events and `onError`, a hook that raises and an `onError` that raises (contract 10): the harness,
  `hooks.json`.
- [ ] `close` in order, twice at once, during a hold, with a final flush (contract 11): the harness,
  `close.json`, `rate-limit.json`, `usage.json`, `status.json`.
- [ ] No context, context key or attribute in any request or `onError` message, and no header beyond the
  protocol's (contract 12): the harness, `privacy.json` and the privacy and protocol checks after every
  scenario. The harness cannot see logs: no log line the SDK writes and no error value it builds carries a
  context, a context key or an attribute (contract 12), by a source search named in `RUN.md`.

## Native tests (what the harness cannot reach)

Each line is a native test in `$L`, named in `RUN.md`.

- [ ] Every operation of contract 2 called from many threads at once, `close` racing `variation`, and two
  `close` calls racing, with no error, deadlock or data race; under the race detector where the language has
  one (contract 6.1). A single-threaded runtime instead tests that `variation` returns without yielding to
  the event loop (contract 6, "Single-threaded runtimes").
- [ ] `variation` and `variationDetail` return within a bound while a ruleset request hangs, and while a
  usage flush hangs (contract 5.2, 6.2).
- [ ] A reader sees one ruleset or the other across a swap: a hook's `etag` and `flagVersion` belong to the
  same ruleset under concurrent swaps (contract 6.3; the swap is a MUST, the `etag` pairing a SHOULD).
- [ ] An `afterEvaluation` hook and an `onError` that call back into the client (`status`, `variation`) do
  not deadlock (contract 6.4, SHOULD).
- [ ] A stream that gets no bytes for 60 s is dropped as `timeout`, and a keep-alive restarts the 60 s,
  tested through a seam, not by waiting: the seam sets the watchdog's length, and the test
  reads a real loopback socket that goes quiet, because a seam-only test can pass while the timeout never
  reaches the socket (contract 7.2, "Bounds").
- [ ] A `Retry-After` above 300 is held for 300 s, on the ruleset, stream and events routes, tested through a
  seam (contract 7.4).
- [ ] Counts recorded either side of a UTC midnight go out as two days (contract 9, "What is kept").
- [ ] An evaluation that raises inside the SDK gives the fallback, `ERROR EVALUATION_FAILED` and one
  `onError` call (contract 4, step 5).
- [ ] Where hooks may return a future, one that fails later goes to `onError` and changes neither the
  answer nor the count (contract 10; a hook that raises at once is the harness's `hooks.json`).
- [ ] With `integer-accessor`, a served number outside signed 64 bits is `TYPE_MISMATCH` on the integer
  accessor, in a language with unbounded integers too (contract 4, "Typed languages"; the `huge` scenario).
- [ ] A client that is never closed does not keep the process from exiting, where the runtime has daemon
  threads or background tasks (contract 6.6, SHOULD).

## Code and documents

- [ ] `DESIGN.md` passes the phase 1 row check, and `README.md` has the mapping table or links to it
  (contract 2: the mapping is documented in a table).
- [ ] The names in the mapping table keep the canonical meaning and defaults, and the reason kinds, error
  codes, stream states and `lastError` codes keep their spelling in the public API (contract 2).
- [ ] The evaluator has no regular expression from the platform: a search of the evaluator's source for the
  language's regex module or type finds nothing (evaluation.md, "Patterns", "Cost").
- [ ] Every runtime dependency has a line in `DESIGN.md` saying why the standard library will not do
  (PROMPT.md, phase 1).
- [ ] The SDK sends no telemetry and adds no header that identifies a user or a machine: a search of the
  source for request headers finds only `Authorization`, `Accept`, `If-None-Match` and `Content-Type`,
  and any other header set is shown in `RUN.md` not to identify a user or a machine (contract 12).
- [ ] Every extra beyond the contract is one contract 13 allows, and none weakens contract 5 or sends
  anything new (contract 13).
- [ ] Test seams are options on `create` (which may be internal to the package) or the language's
  standard fake-time test facility, never a global or an environment variable the SDK reads (contract 13,
  PROMPT.md phase 3).
- [ ] `bin/lint`, `bin/test` and `bin/test-service` exist, are executable, `cd` to the SDK, and run with only
  the toolchain `$L/mise.toml` pins (PROMPT.md, "Entry points").
- [ ] `test-service/README.md` maps every accessor of the protocol to an SDK method
  (sdk-test-service.md, "Accessors").
- [ ] `.github/workflows/sdk-<lang>.yml` follows the skeleton, filters on its paths, and pins every action by
  commit SHA (PROMPT.md, phase 5).

## Repository

- [ ] No em dash or en dash in any file you wrote: `bun scripts/check-dashes.ts` (PROMPT.md, rule 1).
- [ ] `bun run format:check` passes (PROMPT.md, phase 5).
- [ ] Only `$L/` and the one workflow file changed: the phase 6 scope check prints `scope ok` (PROMPT.md,
  rule 2).
- [ ] No package was published and no name reserved (PROMPT.md, rule 3).
- [ ] Commit subjects are Conventional Commits, none `feat`, with no `Co-Authored-By` or AI attribution
  (PROMPT.md, rules 5 and 6).
- [ ] `RUN.md` follows `RUN-TEMPLATE.md`, with evidence for every line of this list (PROMPT.md, phase 6).
