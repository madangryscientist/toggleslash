# FeatureSlash overnight review, 2026-11-05

MORNING SUMMARY goes here when the queue is done. The run is in progress until it does.

## Queue

One builder at a time, in this order. FS-2 is held and not in the queue.

| # | Ticket | Tier | What | Outcome |
| --- | --- | --- | --- | --- |
| 1 | FS-1 | simple | globalStore in bunfig | ✅ gate and CI green (#3, 9f85aa2) |
| 2 | FS-4 | simple | stale AGENTS.md line | ✅ gate and CI green (#4, d3a978b) |
| 3 | FS-3 | simple | root build/test:e2e scripts | ✅ gate and CI green (#5, dd6a9ad) |
| 4 | FS-6 | medium | C# server skeleton, Nx, CI, architecture doc | ✅ gate, 2 xunit, CI green (#6, c81cc75) |
| 4a | FS-25 | simple | e2e ports collide with PixelSlash (added tonight) | ✅ gate and CI green (#7, bc6d4cb) |
| 5 | FS-7 | complex | evaluation spec, vectors, TS engine | ✅ full gate, engine 316, 294 vectors, CI green (#9, d3cdf11) |
| 6 | FS-8 | medium | C# evaluator | ✅ full gate, C# 310 cases, CI green (#10, 544fa9e) |
| 7 | FS-9 | complex | EF Core on SQLite | ✅ gate, server 11, CI green (#11, dc4cb4c) |
| 8 | FS-10 | complex | auth | ✅ gate, server 39, CI green (#12, f75c603) |
| 9 | FS-11 | medium | admin API catalogue | ✅ PR #13, 137 server tests, gate and CI green (#13, b815f65) |
| 10 | FS-12 | medium | targeting and segments | ✅ PR #14, 275 server tests, gate and CI green (#14, 3b14029) |
| 11 | FS-13 | medium | audit | ✅ PR #15, 311 server tests, gate and CI green (#15, e79e538) |
| 12 | FS-14 | medium | SDK ruleset and evaluate | ✅ PR #16, 586 server tests, gate and CI green (#16, 86b1cee) |
| 13 | FS-15 | complex | SSE | ✅ PR #17, 607 server tests twice, gate and CI green, mutation check not run (#17, 595417a) |
| 14 | FS-16 | medium | usage counts | ✅ PR #18, 664 server tests, gate and CI green (#18, f885e90) |
| 15 | FS-17 | medium | TS SDK | ✅ PR #19, sdk 43 tests, gate and CI green (#19, 77a003f) |
| 15a | FS-26 | simple | site build cache outputs (added by the planner) | ✅ PR #20, full gate and CI green (#20, b102761) |
| 16 | FS-18 | medium | CLI | ✅ PR #21, full gate and CI green (#21, a39550d) |
| 17 | FS-19 | medium | dashboard list (feel-review) | |
| 18 | FS-20 | medium | flag detail (feel-review) | |
| 19 | FS-21 | medium | operator pages (feel-review) | |
| 20 | FS-22 | simple | Dockerfile and compose, local only | |
| 21 | FS-23 | medium | cross-language e2e | |
| 22 | FS-24 | simple | README and quickstart | |

## Decision log

Each entry: what · why · ⚠ if unsure · how to undo.

### Decisions already made before the run

1. **C# server at `apps/server`, ASP.NET Core minimal API on .NET 10, inferred by `@nx/dotnet` as PixelSlash does.** Why: Jacobus's instruction on going to bed, "using C# for the server". ⚠ This departs from the Cloudflare/D1 standard stack. His C# instruction is the yes. It cannot run on Workers, so it is self-hosted and hosting is undecided. Undo: revert the server tickets and rebuild the API on Workers and D1, which is most of the queue.
2. **Storage is EF Core on SQLite.** Why: no database server to run, fits a self-hosted single process. ⚠ Postgres is a later ticket if needed. SSE is single-process for the same reason. Undo: change the EF provider and regenerate migrations; SSE fan-out needs a broker once there is more than one process.
3. **`apps/web` becomes a front end to the C# API; the D1 placeholder is unused.** Why: the server owns the data. ⚠ The dashboard has no login yet and uses the admin token from env. Undo: add a login ticket; delete the unused D1 binding if the decision stands.
4. **Evaluation is one spec with shared vectors in both languages.** Why: the C# and TS evaluators must agree, and shared vectors prove it. Undo: drop one evaluator and its vector runner.
5. **FS-2 held.** Why: the C# server now owns accounts, so an accounts table in `apps/web`'s D1 would have the wrong owner. Undo: re-scope and dispatch it after the dashboard exists.
6. **Rules gap on `~/dev/omnibus`.** `ob project rules featureslash` prints nothing tonight because the on-PATH `ob` reads `~/dev/omnibus`, which is 12 commits behind origin. That checkout stays where it is, because other work uses it. Every brief carries the landing rules instead. Undo: nothing to undo; pull that checkout in the morning.
7. **Dependabot PR #1 left unmerged.** Why: instruction in the handover. Undo: gate and merge it in the morning.

### During the run

- **`ob issue landed` replaced by `git log --grep` for the pre-dispatch check.** What: before each dispatch I check `git log origin/main --grep 'FS-n'` in the FeatureSlash checkout. Why: `ob issue landed FS-1` refuses from this repo ("not the tracker repo") and answering from omnibus would read the wrong history. ⚠ Low risk: squash subjects carry the `(FS-n)` scope, so the grep finds a landed ticket. Undo: nothing to undo; use `ob issue landed` once it reads FeatureSlash.
- **Watchdog scheduled.** Hourly task bound to this thread, clientRequestId `fs-overnight-watchdog-2026-11-05`. Deleted when the queue is done.
- **FS-25 added and dispatched ahead of FS-7.** What: new ticket moving FeatureSlash's e2e ports from 4310/4320 to 4410/4420. Why: FS-6's site e2e failed in my gate because PixelSlash's `ps-316` builder held 4310, the port the scaffold inherited. It would keep failing gates overnight. ⚠ Low risk, a two-line change outside the planned queue. Undo: revert the FS-25 PR.
- **FS-6 builder choices accepted.** What: `scripts/nx.ts` fails rather than skips when `CI` is set and no SDK runs; `setup-dotnet` is in the gate job only; the apps job sets `NX_DOTNET_DISABLE=true` and its filter is now `apps/(web|site)/`, so a server-only PR skips Playwright; `release.yml` also disables dotnet for the CLI build. Why: keeps the server in the gate without giving every job an SDK. ⚠ FS-23's cross-language e2e needs an SDK in whichever job runs it. Undo: edit `.github/workflows/ci.yml`.
- **xunit tests sit outside `test-tmp-scope.ts`.** What: the FS-6 builder left the Vitest and Playwright temp-dir scope as TypeScript only and documented that. Why: it is a Vitest and Playwright `globalSetup`. ⚠ A later server test that writes scratch must use `$TMPDIR` and clean up itself. Undo: add a test fixture in `apps/server`.
- **FS-7 evaluation semantics accepted.** What: the spec sets a missing attribute to fail a clause whatever `negate` says; the kill switch runs before body validation, so an off flag with a bad body still serves its off variation; a flag is validated whole, not per path; loading a ruleset never fails; prerequisite depth is limited to 32; there is no regex op; strings compare by UTF-16 code unit. Why: each is stated in `docs/evaluation.md`, pinned by vectors, and reads sensibly. ⚠ The negate rule differs from a plain logical NOT, and customers will meet it ("not on pro" does not match a context with no plan). Changing any of these later is a breaking change for every SDK, so this is the best moment to look. Undo: edit the spec and vectors together, before FS-8 and the SDKs ship if possible.
- **FS-7 skeptical review ran on Opus alone.** What: the second and third reviewers were paused, and the builder's last fix (load vectors from its third pass) was gated but not re-reviewed. I read the spec and checked the worked example's hash independently. ⚠ Low risk: the fix is vectors and the gate covers it. Undo: run a review on `packages/engine` in the morning.
- **FS-8 changed the spec for lone surrogates.** What: a lone surrogate is kept as is in keys, attributes and clause values, equal only to itself, and becomes U+FFFD only when hashed. Why: `JsonElement.GetString()` throws on one, which would have broken the never-throws rule in C#. Two new shared vectors pin it, and both languages pass them. ⚠ Low risk, an edge case. Undo: edit the spec and those vectors together.
- **FS-8 builder choices accepted.** What: the evaluator takes `JsonElement` input, and its tests are in a sibling project, `FeatureSlash.Evaluation.Tests`, so they do not build the web host. Why: the spec's validity rules are defined on JSON. ⚠ FS-14 has to hand it JSON from whatever EF Core stores. Undo: add a typed overload.
- **FS-9 storage choices accepted.** What: one explicit transaction around `Migrate()`, with EF's user-transaction warning ignored on purpose; WAL set before that transaction; `salt` added to `flags` though the ticket omitted it; audit events keep `entity_type` and `entity_id` with no foreign keys; simple foreign keys, so the API must check that a segment or config stays in its project. Why: the builder's probes showed `Migrate()` alone commits per migration and can hang on a stale lock row; two real processes starting on one fresh file both came up, ten times out of ten. ⚠ A migration SQLite cannot run in a transaction will fail at startup. The cross-project check is now FS-11's job, so I watch for it there. Undo: remove the transaction in `ServerDatabase.cs`.
- **FS-10 auth choices accepted.** What: one bearer scheme for all three credentials, with the key kind read from the database row; the 401 does not say why; `{project}` and `{env}` are keys, not ids; revoking twice succeeds; policies are strict, so a server key does not pass `client-key`. I read the handler, policies and key endpoints myself because this is security code. Why: 401 versus 403 comes out right, and nothing leaks which check failed. ⚠ One shared admin token and no rate limiting on a failed token, as the ticket said. A public deployment needs a limit at the proxy at least. Undo: add a ticket for rate limiting.

### FS-11 admin API choices (builder calls, accepted)

- **What:** `/openapi/v1.json` takes no token, like `/health`. A new flag gets an off config row in every environment (off variation 1, fallthrough 0 unless named), and a new environment gets a default row for every existing flag, archived ones included. Variations may not be null although the spec allows it, and are fixed at creation. An archived flag keeps its key, so re-creating it is a 409. Tag filter runs in memory.
- **Why:** the document holds nothing private and a client generator should not need the admin token. "Every flag has a config in every environment" stays true in both creation orders. An all-null flag is a mistake the API can catch.
- **⚠** Null variations are narrower than `docs/evaluation.md` allows, so the API cannot author a flag the evaluator accepts. A new environment cannot copy a flag's chosen indexes, because they are not stored per flag. The in-memory tag filter is fine at this size and will need a real query later.
- **⚠** The builder did not run a skeptical review (its brief's AGENTS.md has no such gate). I gated it locally and in CI; I did not read all 2,588 lines.
- **Undo:** allow null in `Catalogue/Validation.cs`; require a token by moving `/openapi` behind the admin policy in `Catalogue/OpenApiSetup.cs`.

### FS-12 targeting choices (builder calls, accepted)

- **What:** the version travels in the body or `If-Match`, and a save is one SQL compare-and-set. An in-process lock (`TargetingLock`) serialises config and segment writes and the segment-delete check. Stricter than the spec on purpose: unknown fields, a lone surrogate, and a prerequisite variation out of that flag's range are all 400. Toggle takes an optional version. A segment PUT has no version, so last write wins. I told the builder to refuse references across projects or environments, which closes the FS-9 ⚠; tests cover both.
- **Why:** the checks read other rows before writing, and SQLite would let two such checks both pass. Client input must never be a 500.
- **⚠ The lock assumes one server process per database file.** A second server on the same file could save a cycle or delete a segment still in use. That fits SQLite today and has to change if hosting (still undecided) ever runs more than one instance.
- **⚠** Two editors of one segment overwrite each other silently, unlike flag configs. Worth a version when the dashboard edits segments (FS-20/FS-21).
- **⚠ Small known mismatch:** the OpenAPI schema marks `offVariation` required on the PUT body while the server tolerates its absence, so a generated client is stricter than the server.
- **Undo:** the lock is `Targeting/TargetingLock.cs`; a segment version needs an additive column and a generated migration.

### FS-13 audit choices (builder calls, accepted)

- **What:** added scope columns (`project_key`, `environment_key`, `flag_key`) by additive migration. Before and after are the API's response shapes. Actor is always `admin`. A no-op write still writes a row, with before equal to after. A new flag or environment is one row, not one per default config. The environment filter drops flag-level events. Flag PATCH, archive and restore now take `TargetingLock`. `IsUniqueViolation` narrowed to the unique and primary-key codes, so a failed audit write is a 500 rather than a false 409.
- **Why:** keys outlive deleted entities where ids would not. Response shapes cannot carry a secret, and a test checks every column.
- **⚠** No-op rows are a judgment call: they record that someone asked, but they add noise to a flag's history. Filtering `before = after` on read would hide them.
- **⚠** Deleting a segment or an entity leaves its audit rows in place by design, so an unknown flag on the project feed is an empty page, not a 404.
- **Undo:** in `Audit/AuditLog.cs`, skip the write when before equals after.

### FS-14 SDK read API choices (builder calls, accepted)

- **What:** a new `sdk-key` policy (server or client) gates evaluate. The ETag hashes the exact response bytes, with no version counter or cache. An invalid context is a 400 before any flag is read, an unknown or archived `flagKey` is a 404, and a flag that cannot evaluate reports an ERROR reason in its own entry. Evaluate's CORS allows any origin, without credentials. Seven vectors are excluded from the endpoint test by name.
- **Why:** hashing the bytes makes the ETag right by construction. A client key is public by design and goes in a header, so no cookie is at risk from any origin. Deferred read transactions keep SDK reads off SQLite's write lock (the skeptical review caught the default taking it).
- **⚠** Every ruleset request reads and hashes the whole environment, even for a 304. Fine for now; FS-15 streaming and real traffic may want a cached version.
- **⚠** A client key can evaluate any context and read every non-archived flag's value for it, with no rate limiting. That is the usual client-key trade, and it is worth deciding before anything is public.
- **Undo:** tighten the origins in `Sdk/SdkEndpoints.cs`.

### FS-15 stalled on a permission prompt (planner call)

- **What:** the builder sat from 03:45 to 04:45 UTC on a permission prompt for a scripted mutation check (back up three source files, sed-edit them, run the stream tests, restore). The hourly watchdog found it. I could not approve it and did not try to. I interrupted the run and sent it on without that step, telling it not to retry the check in another form and to say in the commit body that it was not run.
- **Why:** the prompt was waiting for a human, and the check is the builder's own extra proof, not an acceptance criterion. An hour lost is the cost; the alternative was waiting for morning with the queue stopped. Not re-dispatched at a higher tier: it already runs on Opus and its work was intact (26 stream tests passing, no mutation left in the tree).
- **⚠** Nothing has shown the revocation and after-commit tests fail on a broken build. I will read them at the gate; a mutation run is worth doing by hand in the morning.
- **⚠** Runs in `full-access` mode can still stop on a prompt, and nothing tells the planner except the watchdog. The tell is `pendingRequestCount` on the thread. Worth an `ob` ticket if it recurs.
- **Undo:** nothing to undo; to run the check, flip `KeyIsLive` to always true in `Sdk/RulesetStream.cs` and run `SdkStreamTests`.

### FS-15 SDK stream choices (builder calls, accepted)

- **One server process only.** What: the notifier is in-process, per environment; `docs/architecture.md` says a multi-instance deployment needs another bus. Why: the ticket asks for this and the server is SQLite on one host. **⚠** The same assumption as `TargetingLock` (FS-12). Undo: put a bus behind `RulesetNotifier`.
- **Revoking does not wake a stream.** What: a revoked key's stream ends at its next event or keep-alive, so up to 25 seconds later, and it sends nothing in between. Why: the ticket says so. Undo: raise a signal from the revoke route.
- **The key is read from the database before every event and keep-alive**, and each subscriber costs one ruleset read per wake. Why: a stream outlives any one read; bursts coalesce to one wake-up. **⚠** Cost grows with subscribers times writes. Fine for now.
- **No signal for flag PATCH.** Checked at the gate: `UpdateFlagRequest` is metadata only (name, description, temporary, owner, tags). I did not read every field the ruleset query selects.
- **The mutation check was not run (see the stall entry above).** I read the tests instead. `A_key_revoked_*` assert the stream ends with no further line, so a broken key check would surface a keep-alive or `changed` line and fail. `Each_ruleset_write_signals_after_its_commit_and_only_then` makes each of the eight writes fail at commit and asserts no signal, then succeed and asserts one, so it catches both an early signal and a missing one. **⚠** That is reading, not a run. `StreamHeartbeat.Next`, the real 25-second delay, is run by no test; only the constant is asserted.
- **⚠ Thread left unsettled.** `ob close` could not settle the FS-15 thread because the old permission prompt is still pending on it; it auto-settles in 3 days. Do not approve that prompt: it would sed-edit files in the `featureslash.fs-15` worktree. The landed code is not affected.

### FS-16 usage count choices (builder calls, accepted)

- **CORS now covers `POST /sdk/v1/events` as well as evaluate.** What: invariant 7 in `AGENTS.md` changed to say so. Why: client keys run in browsers, and a browser SDK has to report usage. Undo: drop the CORS policy from the route and restore the invariant's wording.
- **⚠ A client key can write counts.** A client key ships in the browser, so anyone holding it can inflate a flag's counts, and the server keeps no record of who sent them. With no rate limiting this is unbounded, and the later safe-to-remove verdict (OB-1780) will read these numbers. A false "in use" is the safe direction, but a verdict should know the data is client-writable. Undo: restrict events to server keys.
- **⚠ Evaluate takes SQLite's write lock once per call** to count what it served. A counting failure is logged and never fails the evaluation. The ticket requires counting; batching in memory waits for a measured need.
- **A bad entry rejects the whole batch (400); an unknown flag does not** (ignored, counted in `unknownFlags`). Why: an SDK can outlive a deleted flag, while a malformed entry is a bug. Archived flags still count.
- **Limits:** 128 KiB and 1000 events (413), count 1 to 1e9, day from 366 back to one ahead of UTC today. The numbers are the builder's. Undo: constants in `Usage/UsageEndpoints.cs`.
- **`lastEvaluated` is a UTC day, the latest across environments,** and is left out of audit snapshots so a usage count does not read as a change. The usage read breaks each day down by variation, more than the ticket asked; kept because the table is keyed that way and the later work needs it.

### FS-17 SDK choices (builder calls, accepted)

- **One loop is both the reconnect and the poll cadence.** After a stream ends or fails it waits 1 s, doubling up to 30 s, polls once, then tries the stream again. A stream that is up needs no poll. `stream: false` polls only.
- **A stream silent for 60 s is dropped** (the server keeps alive every 25 s), and every request has a 10 s timeout, so `ready()` and `close()` cannot hang.
- **A value of the wrong JSON type returns the fallback** with `TYPE_MISMATCH` and counts nothing. A `null` fallback accepts any type.
- **⚠ Backoff resets once a stream connects,** so a server that says `connected` and then hangs up is retried every second. The builder kept it: one ETag read each, and a deploy reconnects quickly. Undo: reset only after a stream has lived a while.
- **The package stays private.** Publishing waits for you, per the floor.

### FS-26 added by the planner: the site's cached build

- **What:** a new one-line ticket, dispatched next, ahead of FS-18. `apps/site` declares only `dist` as its build output, but `vite preview` and `wrangler` also read `.wrangler/deploy/config.json`, so a fresh worktree with a cached build fails site e2e and the dry run.
- **Why now:** FS-19 to FS-21 run this gate, and it would be red on each one for a reason unrelated to the work. It first went red gating FS-17; the same head passed with `--skip-nx-cache`, and CI's apps job passed.
- **⚠** I landed FS-17 on that evidence, not on a clean run of my gate script.
- **Undo:** revert the outputs line.

### FS-18 choices (CLI)

- **What:** exit 1 is a server or network failure, 2 stays usage. No `--token` flag and `config set-token` reads stdin, so the token stays out of shell history and `ps`. Output goes through a writer that masks a known secret of 8 or more characters. Redirects are refused so the token cannot follow one to another host. **Why:** the ticket asked to pick the exit code and keep the token out of output. **⚠** masking is a backstop only for secrets of 8+ characters; a shorter token would not be masked, though error messages never carry the request. **Undo:** `packages/cli/src/errors.ts`, `command.ts`.
- **What:** the builder kept three things the ticket did not name: `--url`, `--json` on writes, `flags show --env`. **Why:** `keys create --json` is how a script captures a key shown once. **Undo:** remove from `packages/cli/src/commands/`.
