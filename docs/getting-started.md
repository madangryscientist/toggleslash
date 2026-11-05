# Getting started

FeatureSlash is early: the engine evaluates flags ([`evaluation.md`](evaluation.md)), and the TypeScript server SDK
([`sdk.md`](sdk.md)) evaluates them against a server, and the CLI manages a server over its admin API (see `packages/cli/README.md`). What you can do today:

```bash
bun install
bun run featureslash --version     # featureslash-engine 0.0.0
bun run featureslash --help
```

With a server running (below), point the CLI at it and manage projects, environments, flags and SDK keys:

```bash
bun run featureslash config set-url https://flags.example.com
bun run featureslash config set-token < token-file    # or set FEATURESLASH_URL / FEATURESLASH_TOKEN
bun run featureslash projects create shop --name Shop
bun run featureslash flags create new-checkout --project shop
bun run featureslash flags list --project shop --json
```

The dashboard (`apps/web`) and the site (`apps/site`) each have a `dev` script: run `bun run dev` in the app's directory.
The dashboard reads `FEATURESLASH_API_URL` (the C# server's address) and `FEATURESLASH_ADMIN_TOKEN` (the same admin token the server was started with) from the environment, in `apps/web/.dev.vars` for `dev`; without them it shows a "not configured" page.

The C# server (`apps/server`) needs the .NET SDK: `dotnet run --project apps/server/FeatureSlash.Server`, then `GET /health`.
Set `FEATURESLASH_ADMIN_TOKEN` to use the admin API (projects, environments, flags, their targeting, segments and SDK keys under `/api/v1`);
its OpenAPI document is at `/openapi/v1.json`, and committed as `apps/server/openapi/v1.json`.
To run the server in a container instead, see [`self-hosting.md`](self-hosting.md) (`docker compose up -d --build`).
