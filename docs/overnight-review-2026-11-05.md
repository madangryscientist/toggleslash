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
| 11 | FS-13 | medium | audit | |
| 12 | FS-14 | medium | SDK ruleset and evaluate | |
| 13 | FS-15 | complex | SSE | |
| 14 | FS-16 | medium | usage counts | |
| 15 | FS-17 | medium | TS SDK | |
| 16 | FS-18 | medium | CLI | |
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
