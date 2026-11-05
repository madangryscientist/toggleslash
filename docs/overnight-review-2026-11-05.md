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
| 5 | FS-7 | complex | evaluation spec, vectors, TS engine | |
| 6 | FS-8 | medium | C# evaluator | |
| 7 | FS-9 | complex | EF Core on SQLite | |
| 8 | FS-10 | complex | auth | |
| 9 | FS-11 | medium | admin API catalogue | |
| 10 | FS-12 | medium | targeting and segments | |
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
