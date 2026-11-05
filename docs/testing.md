# Testing

Tests have layers; a behaviour is proved at the lowest layer that can prove it.

| Layer | Where | Runs with |
| --- | --- | --- |
| Unit | `packages/*/test/`, `apps/*/src/**/*.test.ts` | `bun run test` (Vitest under Bun) |
| Vectors | `fixtures/evaluation/*.json`, run by `packages/engine/test/vectors.test.ts`: the same inputs and expected results every implementation of [`evaluation.md`](evaluation.md) must pass | `bunx nx test engine` |
| Vectors (C#) | the same files, run by `apps/server/FeatureSlash.Evaluation.Tests` against `FeatureSlash.Evaluation`; a change under `fixtures/` re-runs both languages (the Nx `test` inputs include `fixtures/**`) | `bunx nx test FeatureSlash.Evaluation.Tests`, with the .NET SDK |
| Integration | `apps/web/src/db/test-db.test.ts`: the real migrations on an in-memory SQLite | `bun run test` |
| Unit and API (C#) | `apps/server/FeatureSlash.Server.Tests` (xunit, `WebApplicationFactory`, a temp SQLite file); one test fails when `apps/server/openapi/v1.json` drifts from `/openapi/v1.json` (the Nx `test` inputs include it) | `bun run test`, with the .NET SDK |
| e2e / smoke | `apps/*/e2e/` (Playwright, against the production build in workerd) | `bunx nx test:e2e web`, `bunx nx test:e2e site` |

Every TypeScript project's tests go through `scripts/test-tmp-scope.ts`, so a run cannot fill `/tmp`.
The xunit tests do not (they write no files); set `TMPDIR` yourself for a .NET run that does.
No test uses the live network or a real Cloudflare account.
