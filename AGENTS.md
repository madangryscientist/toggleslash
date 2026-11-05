# FeatureSlash: repo conventions

FeatureSlash is an open source feature flag service, a LaunchDarkly competitor: evaluate flags in an
engine, manage them from a CLI and a hosted dashboard. The product domain is `featureslash.dev`
(not registered yet, so nothing is routed to it).

**This repository is early.** The engine evaluates flags; the dashboard lists, toggles, creates and archives flags and edits one flag's details and per-environment
targeting (with its history and usage), lists and creates environments, creates and revokes SDK keys, edits segments and reads the project audit log, all through the server's admin API, the site renders one page, and the C# server migrates its SQLite database, answers `GET /health`, authenticates (the admin token and hashed SDK keys) and serves the admin API: SDK keys, projects, environments, flags, their per-environment targeting (versioned) and segments, each write recorded in an audit trail you can read back, described by `/openapi/v1.json`. It serves the SDK read API (`GET /sdk/v1/ruleset` with an `ETag`, `GET /sdk/v1/stream` for live updates in one server process, `POST /sdk/v1/evaluate`, through the C# evaluator, and `POST /sdk/v1/events` for usage counts, which store counts and nothing about an end user). The TypeScript server SDK (`packages/sdk`) calls the ruleset, stream and events routes; the `featureslash` CLI (`packages/cli`) calls the admin API (projects, environments, flags and SDK keys); no browser SDK exists. The server builds into a container for self-hosting (`apps/server/Dockerfile`, `compose.yaml`, [`docs/self-hosting.md`](docs/self-hosting.md)), proven by a CI job and never pushed. The design is in [`docs/architecture.md`](docs/architecture.md).

**Evaluation is specified once and implemented per language.** [`docs/evaluation.md`](docs/evaluation.md)
is normative and `fixtures/evaluation/` is its executable form. A change to evaluation changes the spec,
the vectors and every implementation together, and nothing in it may depend on one language's runtime.

This file is the operating agreement for contributors and agents working here: the gate, the landing
rule and the invariants. Keep it accurate.

## The gate

A small Nx workspace on Bun workspaces:

| Project | Path | What it is |
| --- | --- | --- |
| `engine` | `packages/engine` | flag evaluation, to `docs/evaluation.md`; `test/vectors.test.ts` runs every shared vector |
| `cli` | `packages/cli` | the `featureslash` bin: argv, stdout and exit codes; `config`, `projects`, `envs`, `flags` and `keys` commands over the server's admin API (injected `fetch`, tested against a fake server); `--version` from the engine. Exit 0, 1 (server or network failure), 2 (usage); see its `README.md` |
| `sdk` | `packages/sdk` | the TypeScript server SDK (`createClient`): ruleset with ETags, the stream with reconnect and a polling fallback, local evaluation with the engine, batched usage counts; it never throws at a caller. Private and unpublished; see `docs/sdk.md` |
| `web` | `apps/web` | the dashboard: TanStack Start on Cloudflare Workers; a server-only client for the C# admin API (`src/api`), a project and environment switcher and the flag list (search, toggle, create, archive) and the flag detail page (`/flags/$flag`: metadata, targeting editor, history, usage; the editor's pure logic is `src/targeting`), and the operator pages `/environments` (list, create), `/keys` (create shows the plaintext once, in component state only; revoke asks first), `/segments` (list, edit) and `/audit` (flag, environment and date filters, cursor paging). D1 via Drizzle stays a placeholder table, unused. Its e2e run against a fake API (`e2e/fake-api.ts`), so its gate needs no .NET SDK |
| `site` | `apps/site` | featureslash.dev: TanStack Start on a Worker, every page prerendered; no D1, no secrets |
| `server` | `apps/server` | the C# server: ASP.NET Core minimal API on .NET 10 (`FeatureSlash.Server`, xunit in `FeatureSlash.Server.Tests`); owns all flag data and the API. `apps/server/openapi/v1.json` is its generated OpenAPI document; `apps/server/Dockerfile` and the root `compose.yaml` containerise it (below) |
| `evaluation` | `apps/server/FeatureSlash.Evaluation` | the C# implementation of `docs/evaluation.md`: a class library with no ASP.NET or database dependency (xunit in `FeatureSlash.Evaluation.Tests`, which runs every vector); only the server's tests reference it |
| `scripts` | `scripts` | `check-slop.ts`, `test-tmp-scope.ts`, `nx.ts` (typecheck + lint only) |

`bunfig.toml` sets `[install] linker = "isolated"` and `globalStore = true` (the shared package store that
makes a warm worktree install fast): a package sees its own declared dependencies and the root's, not
another package's. Declare what a package imports in that package's `package.json`.

A change is green when all of these pass **from the worktree**:

```bash
bun install                       # FIRST: a fresh worktree has no node_modules
bun run typecheck                 # nx run-many -t typecheck  (tsc, strict)
bun run lint                      # nx run-many -t lint  (oxlint --type-aware --deny-warnings + check-slop)
bun run format:check              # oxfmt --check  (`bun run format` fixes)
bun run test                      # nx run-many -t test  (Vitest under the Bun runtime)
```

**The .NET SDK is needed for `apps/server`.** A change to it needs the SDK to be checked. The root
scripts (`typecheck`, `lint`, `build`, `test`, `test:e2e`) go through `scripts/nx.ts`, which runs
`dotnet --version` first. Two paths:

- **With a working `dotnet`**: nothing changes. `bun run test` restores, builds and tests
  `FeatureSlash.Server.Tests` with xunit. The version (10.0.402) is pinned in `.tool-versions` (mise
  activates it here; `mise install` installs it) and in `global.json` (what `dotnet` itself reads).
  The first restore fetches NuGet packages, so it needs the network once.
- **Without one**: the wrapper sets `NX_DOTNET_DISABLE=true` and prints
  `dotnet SDK not found: skipping apps/server`; the TS projects run as before and the dotnet
  projects are not in the graph. Setting `NX_DOTNET_DISABLE` yourself skips the check and the line.

The reason for the wrapper: the `@nx/dotnet` plugin in `nx.json` reads the .NET projects when Nx
builds its graph, so without an SDK **every** `nx` command fails. A bare `bunx nx ...` skips the
wrapper, so on a machine without an SDK set `NX_DOTNET_DISABLE=true` yourself. Both `.csproj` files
set `Nullable`, `TreatWarningsAsErrors` and `InvariantGlobalization`; `apps/server/Directory.Build.props`
and `.rsp` leave no compiler server or MSBuild node running after a build. `bin/` and `obj/` are
ignored.

One project: `bunx nx test engine`, `bunx nx lint cli`, `bunx nx test FeatureSlash.Server.Tests`. Nx caches in `.nx/`; add `--skip-nx-cache` to
force a rerun. There is no Nx Cloud here. Lint rules are all errors; fix the code, do not disable a
rule. A TODO carries an issue: `TODO(#123)`. Tests live in `packages/*/test/` and
`apps/*/src/**/*.test.ts` and run with `bun --bun vitest`; the server's tests are xunit, in
`apps/server/FeatureSlash.Server.Tests`.

A change to `apps/web` or `apps/site` also runs their builds, Playwright and Worker dry-runs (from the
worktree root). `test:e2e` runs before `build`: web's e2e run leaves its own build behind, and `build` replaces it.

```bash
bun run test:e2e                  # nx run-many -t test:e2e  (Playwright: web and site; web builds with the e2e env on a fresh local D1)
bun run build                     # nx run-many -t build  (every project's build target: web, site, cli, and the C# projects)
(cd apps/web && bunx wrangler deploy --dry-run --outdir "$TMPDIR/fs-web-dry")
(cd apps/site && bunx wrangler deploy --dry-run --outdir "$TMPDIR/fs-site-dry")
```

One app: `bunx nx test:e2e web`, `bunx nx build site`.

First time on a machine: `bunx playwright install chromium-headless-shell`.

**The server's container is checked in CI, not by the gate.** The `container` job in `.github/workflows/ci.yml`
runs `docker build -f apps/server/Dockerfile`, `docker compose up -d`, waits for `/health` and runs
`docker compose down -v`; it pushes nothing, logs in to no registry and uses a throwaway token. You need Docker to
run the same commands locally, and the gate does not. Both base images in the Dockerfile are pinned by digest
(the multi-arch index digest from `mcr.microsoft.com`); a bump changes the tag and the digest together, and
the SDK image matches `global.json`. `.dockerignore` keeps `node_modules`, `bin`, `obj` and any `*.db` out of the
context. `apps/web` is not containerised.

**Generated files are not formatted.** `oxfmt` rewrites what it is given, so anything a tool writes
is excluded in `.oxfmtrc.json`'s `ignorePatterns`: the route tree, `worker-configuration.d.ts`,
the drizzle migrations and the `dotnet-tools.json` manifest. Add a pattern when a new generator appears. `**/*.md` is ignored too.

**/tmp is a quota'd tmpfs; do not fill it.**

- Every TypeScript project's test run goes through `scripts/test-tmp-scope.ts` (a Vitest and Playwright
  `globalSetup`): `TMPDIR` is a fresh dir for the run and is removed after it. A new project's
  `vitest.config.ts` must list the same `globalSetup`.
- `nx.json` sets `"useDaemonProcess": false`; the daemon installs `nx@latest` into `/tmp` and does
  not clean it. Do not turn it on and do not run `nx daemon`.
- Scratch goes under `$TMPDIR`, never a hardcoded `/tmp`.

## Cloudflare, domains and publishing are human steps

Pushing a container image to a registry is a human step too. The repo adds no Worker route and no custom domain: `featureslash.dev` is not registered. The
`D1` binding in `apps/web/wrangler.jsonc` carries a placeholder id. Creating the D1 database, the
Worker routes, and the first npm publish each wait for the maintainer's yes.
`featureslash.dev` is used for canonical URLs and copy only.

## Landing

- Code changes land through pull requests; nobody pushes to `main`. Branch, gate, rebase on
  `origin/main`, push, open the PR.
- Commit subjects are Conventional Commits: `<type>(<scope>): <subject>`, with type one of `feat fix
  docs chore refactor test perf build ci style revert`. The squash keeps the subject.
- Stage files by name or `git add -A`; never `git commit -am` (it drops new files).
- No `Co-Authored-By` trailer and no AI attribution line, in a commit or a pull request.

## Invariants

1. **Exit codes are a contract.** `featureslash` exits 0 on success, 2 on a usage error.
2. **The CLI writes through `process.stdout` / `process.stderr`**, not `console` (the lint bans it
   outside `scripts/`).
3. **UI is composed from `@hackcove/ui-react`.** A custom style or component is a flag, not a fix; a
   reusable gap is filed against the hackcove repo. Keep the Catppuccin credit comment in each
   `styles.css`.
4. **A secret is read from the environment, never from the tree.** The site holds none. The server
   never generates, stores or logs the admin token, and stores an SDK key only as a hash and a prefix.
5. **Migrations are additive and generated; never hand-write one.** In `apps/web`, `drizzle-kit
   generate` writes the `.sql` and its `meta/` snapshot together (`bun run db:generate`). In
   `apps/server`, `dotnet ef` writes the migration, its designer file and the model snapshot in
   `FeatureSlash.Server/Data/Migrations`, from the worktree root:
   `dotnet tool restore`, then `dotnet ef migrations add <Name> --project apps/server/FeatureSlash.Server`.
   The tool is pinned in `.config/dotnet-tools.json`. The server applies them at startup in one
   transaction, so a change SQLite cannot make inside one (a table rebuild) fails there, which is one
   more reason to stay additive. A model change with no migration fails startup.
6. **`apps/server/openapi/v1.json` is generated, never hand-edited.** A test fails when it differs
   from what the server serves at `/openapi/v1.json`. After an API change, regenerate it with
   `FEATURESLASH_UPDATE_OPENAPI=1 dotnet test apps/server/FeatureSlash.Server.Tests` and commit the
   result. The TypeScript clients generate from or check against this file.
7. **A client key never reads the ruleset, and `evaluate` never returns rule data.** `GET /sdk/v1/ruleset` is
   server-key only and carries rules, targets and salts; `GET /sdk/v1/stream` is server-key only and carries ETags; `POST /sdk/v1/evaluate` (either key, with `POST /sdk/v1/events`, the only routes with
   CORS) answers `value`, `variationIndex` and `reason` and nothing it was configured with. `events` stores counts only
   (`flag_usage_daily`), never a context, a context key or any end-user data.
