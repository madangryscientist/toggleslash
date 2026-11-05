# FeatureSlash

Open source feature flags you can run yourself. MIT licensed. Site: https://featureslash.dev (not
live yet).

> **Status: early.** The engine (TypeScript) and a C# evaluator evaluate flags, to the spec in
> [`docs/evaluation.md`](docs/evaluation.md). The server stores flags in SQLite and has an admin API for projects,
> environments, flags, their targeting and segments, and SDK keys, and records every one of those writes in an audit trail (OpenAPI: [`apps/server/openapi/v1.json`](apps/server/openapi/v1.json)). It also serves the SDK read API (`/sdk/v1`): the ruleset with an `ETag` and a Server-Sent Events stream of its changes for server-side SDKs, and server-side evaluate for clients. It counts how often each flag variation is served per day (counts only, never an end user's context) and reports each flag's `lastEvaluated`. A TypeScript server SDK
> (`packages/sdk`, [`docs/sdk.md`](docs/sdk.md)) uses them; it is not published yet.

## Layout

| Path | What it is |
| --- | --- |
| `packages/engine` | flag evaluation |
| `fixtures/evaluation` | the language-neutral test vectors every implementation must pass |
| `packages/cli` | the `featureslash` command: manage projects, environments, flags and SDK keys over the admin API |
| `packages/sdk` | the TypeScript server SDK |
| `apps/web` | the dashboard: TanStack Start on Cloudflare Workers; flag list, toggle, create and archive, a flag detail page (metadata, targeting editor, history, usage), and pages for environments, SDK keys, segments and the audit log, over the server's admin API |
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
send a change. [`docs/`](docs) has the [architecture](docs/architecture.md), the [evaluation spec](docs/evaluation.md), the testing policy, the getting-started page and the [self-hosting guide](docs/self-hosting.md).

## Licence

MIT, see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
