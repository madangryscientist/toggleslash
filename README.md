# FeatureSlash

Open source feature flags you can run yourself. MIT licensed. Site: https://featureslash.dev (not
live yet).

> **Status: early.** The engine (TypeScript) and a C# evaluator evaluate flags, to the spec in
> [`docs/evaluation.md`](docs/evaluation.md). The server stores flags in SQLite and manages SDK keys, but has no flag API yet and there is no
> SDK, so nothing can use them from outside this repo.

## Layout

| Path | What it is |
| --- | --- |
| `packages/engine` | flag evaluation |
| `fixtures/evaluation` | the language-neutral test vectors every implementation must pass |
| `packages/cli` | the `featureslash` command |
| `apps/web` | the dashboard: TanStack Start on Cloudflare Workers, D1 via Drizzle |
| `apps/site` | the marketing and docs site, prerendered |
| `apps/server` | the C# server (ASP.NET Core, .NET 10): owns flag data and the API |
| `apps/server/FeatureSlash.Evaluation` | flag evaluation in C#, to the same spec and vectors as the engine; no endpoint uses it yet |

## Develop

```bash
bun install
bun run featureslash --version
bun run typecheck && bun run lint && bun run format:check && bun run test
```

`apps/server` needs the .NET SDK (10.0.402, `global.json`); without one the root scripts skip it and say so.

See [`AGENTS.md`](AGENTS.md) for the full gate and [`CONTRIBUTING.md`](CONTRIBUTING.md) for how to
send a change. [`docs/`](docs) has the [architecture](docs/architecture.md), the [evaluation spec](docs/evaluation.md), the testing policy and the getting-started page.

## Licence

MIT, see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
