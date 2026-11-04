# FeatureSlash

Open source feature flags you can run yourself. MIT licensed. Site: https://featureslash.dev (not
live yet).

> **Status: skeleton.** The repo has its structure, gate and CI, and no flag features yet.

## Layout

| Path | What it is |
| --- | --- |
| `packages/engine` | flag evaluation (stub) |
| `packages/cli` | the `featureslash` command |
| `apps/web` | the dashboard: TanStack Start on Cloudflare Workers, D1 via Drizzle |
| `apps/site` | the marketing and docs site, prerendered |

## Develop

```bash
bun install
bun run featureslash --version
bun run typecheck && bun run lint && bun run format:check && bun run test
```

See [`AGENTS.md`](AGENTS.md) for the full gate and [`CONTRIBUTING.md`](CONTRIBUTING.md) for how to
send a change. [`docs/`](docs) has the testing policy and the getting-started page.

## Licence

MIT, see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
