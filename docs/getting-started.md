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
It reads `TOGGLESLASH_API_URL` (the C# server's address) and `TOGGLESLASH_ADMIN_TOKEN` (the same admin
token the server was started with) For `dev`, put them in `apps/web/.dev.vars`,
which is git-ignored. Without them it shows a "The dashboard is not configured" page.

Sign-in needs two more in the same file: `TOGGLESLASH_OPERATOR_PASSWORD` (at least 16 characters, what you
type at sign-in) and `TOGGLESLASH_SESSION_SECRET` (at least 32). Without them nobody can sign in, and the
sign-in page names what is missing. There is one operator and no accounts; see
[`self-hosting.md`](self-hosting.md#dashboard-sign-in).

The site (`apps/site`) also has a `dev` script (port 3100). It has no configuration.
