# Getting started

The [quickstart](quickstart.md) is the way in: it starts the server, creates a flag with the CLI and reads it
from the SDK. This page covers the parts it leaves out.

- The engine and the evaluation spec: [`evaluation.md`](evaluation.md).
- The TypeScript server SDK: [`sdk.md`](sdk.md).
- The HTTP API and its OpenAPI document: [`api.md`](api.md).
- The CLI's commands and exit codes: [`packages/cli/README.md`](../packages/cli/README.md).
- Running the server in a container, with backups and upgrades: [`self-hosting.md`](self-hosting.md).

## The dashboard

The dashboard (`apps/web`) has a `dev` script: run `bun run dev` in `apps/web` (it serves on port 3000).
It reads `FEATURESLASH_API_URL` (the C# server's address) and `FEATURESLASH_ADMIN_TOKEN` (the same admin
token the server was started with) For `dev`, put them in `apps/web/.dev.vars`,
which is git-ignored. Without them it shows a "The dashboard is not configured" page.

The dashboard has no sign-in. Anyone who can reach it can act with the admin token it was given, so run it
on your own machine or behind something you trust.

The site (`apps/site`) also has a `dev` script (port 3100). It has no configuration.
