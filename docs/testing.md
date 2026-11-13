# Testing

Tests have layers; a behaviour is proved at the lowest layer that can prove it.

| Layer | Where | Runs with |
| --- | --- | --- |
| Unit | `packages/*/test/`, `apps/*/src/**/*.test.ts` | `bun run test` (Vitest under Bun) |
| SDK | `packages/sdk/test/`: a fake server and a fake clock, no network or real timers (see [`sdk.md`](sdk.md)) | `bunx moon run sdk:test` |
| Vectors | `fixtures/evaluation/*.json`, run by `packages/engine/test/vectors.test.ts`: the same inputs and expected results every implementation of [`evaluation.md`](evaluation.md) must pass | `bunx moon run engine:test` |
| Vectors (C#) | the same files, run by `apps/server/ToggleSlash.Evaluation.Tests` against `ToggleSlash.Evaluation`; a change under `fixtures/` re-runs both languages (the `test` inputs of the TypeScript projects and of `server` include `fixtures/**`) | `bunx moon run server:test`, with the .NET SDK |
| Integration | `apps/web/src/db/test-db.test.ts`: the real migrations on an in-memory SQLite | `bun run test` |
| Unit and API (C#) | `apps/server/ToggleSlash.Server.Tests` (xunit, `WebApplicationFactory`, a temp SQLite file); one test fails when `apps/server/openapi/v1.json` drifts from `/openapi/v1.json` (the `test` inputs of the TypeScript projects include it, and so do `server`'s) | `bun run test`, with the .NET SDK |
| e2e / smoke | `apps/*/e2e/` (Playwright, against the production build in workerd; the dashboard's run against `apps/web/e2e/fake-api.ts`, an in-memory stand-in for the admin API, so it needs no .NET SDK; `flag-detail.spec.ts` covers the detail page, a 409 that keeps the typing and a field error; `operator.spec.ts` covers environments, SDK keys including that a new key's plaintext is gone after leaving the page, segments including a 409 that keeps the typing and the audit log) | `bunx moon run web:test-e2e`, `bunx moon run site:test-e2e` |
| End to end (cross-language) | `packages/e2e/test/product.test.ts`: the real C# server (built, started on a free loopback port with a temp SQLite file under `TMPDIR`), the real CLI as a subprocess and the real TS SDK. It creates a project, environment, flag and keys with the CLI, reads the flag with the SDK, flips it with the CLI and waits for the SDK to see it through the stream (the poll interval is ten minutes, and the test counts the stream connections and ruleset fetches), and compares `POST /sdk/v1/evaluate` with the SDK's local evaluation for 64 contexts across flags that use targets, rules, a segment, a prerequisite, rollouts and an off flag. It needs the .NET SDK: with none it prints a line and skips (in CI a missing SDK fails). The process and the temp files are removed even when it fails | `bunx moon run e2e:test-e2e` |

Every TypeScript project's tests go through `scripts/test-tmp-scope.ts`, so a run cannot fill `/tmp`.
The xunit tests do not (they write no files); set `TMPDIR` yourself for a .NET run that does.
No test uses the live network or a real Cloudflare account.
