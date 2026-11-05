# Getting started

FeatureSlash is early: the engine evaluates flags ([`evaluation.md`](evaluation.md)), but there is no
SDK to serve them yet, so the CLI does not use it. What you can do today:

```bash
bun install
bun run featureslash --version     # featureslash-engine 0.0.0
bun run featureslash --help
```

The dashboard (`apps/web`) and the site (`apps/site`) each have a `dev` script: run `bun run dev` in the app's directory.

The C# server (`apps/server`) needs the .NET SDK: `dotnet run --project apps/server/FeatureSlash.Server`, then `GET /health`.
Set `FEATURESLASH_ADMIN_TOKEN` to use the admin API (projects, environments, flags, their targeting, segments and SDK keys under `/api/v1`);
its OpenAPI document is at `/openapi/v1.json`, and committed as `apps/server/openapi/v1.json`.
