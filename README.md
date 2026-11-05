# FeatureSlash

Open source feature flags you can run yourself. MIT licensed. Site: https://featureslash.dev (not
live yet).

> **Status: early, and nothing is published.** You can run FeatureSlash on your own machine or server:
> a C# server keeps flags in SQLite and serves an admin API and the SDK routes, a `featureslash` command
> manages flags over that admin API, and a TypeScript server SDK evaluates flags in your process. Every
> admin API write is recorded in an audit trail, and the server counts how often each flag variation is served per day
> (counts only, never an end user's context). Evaluation follows the spec in
> [`docs/evaluation.md`](docs/evaluation.md). There is no hosted service, no browser SDK and no published
> package yet.

- [Quickstart](docs/quickstart.md): start the server, create a flag with the CLI, read it from the SDK. About five minutes.
- [Feature parity](docs/feature-parity.md): what FeatureSlash and the main competing products offer, capability by capability.
- [API overview](docs/api.md): the routes and who may call them. The OpenAPI document is [`apps/server/openapi/v1.json`](apps/server/openapi/v1.json).
- [Getting started](docs/getting-started.md): the parts the quickstart leaves out, including the dashboard.
- [Self-hosting](docs/self-hosting.md): the server as a container, with backups and upgrades.

## Layout

| Path | What it is |
| --- | --- |
| `packages/engine` | flag evaluation |
| `fixtures/evaluation` | the language-neutral test vectors every implementation must pass |
| `packages/cli` | the `featureslash` command: manage projects, environments, flags and SDK keys over the admin API |
| `packages/sdk` | the TypeScript server SDK (private, not published) |
| `apps/web` | the dashboard: TanStack Start on Cloudflare Workers; flag list, toggle, create and archive, a flag detail page (metadata, targeting editor, history, usage), and pages for environments, SDK keys, segments and the audit log, over the server's admin API |
| `apps/site` | the featureslash.dev site, prerendered |
| `apps/server` | the C# server (ASP.NET Core, .NET 10): owns flag data and the API |
| `apps/server/FeatureSlash.Evaluation` | flag evaluation in C#, to the same spec and vectors as the engine; the server's `POST /sdk/v1/evaluate` uses it |
| `packages/e2e` | the cross-language end-to-end test: the real server, CLI and SDK on localhost (`bunx nx test:e2e e2e`; needs the .NET SDK) |

## Develop

```bash
bun install
bun run featureslash --version
bun run typecheck && bun run lint && bun run format:check && bun run test
```

`apps/server` needs the .NET SDK (10.0.402, `global.json`); without one the root scripts skip it and say so.

See [`AGENTS.md`](AGENTS.md) for the full gate and [`CONTRIBUTING.md`](CONTRIBUTING.md) for how to
send a change. [`docs/`](docs) has the rest of the documentation, including the [architecture](docs/architecture.md).

## Licence

MIT, see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
