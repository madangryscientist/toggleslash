# FeatureSlash: repo conventions

FeatureSlash is an open source feature flag service, a LaunchDarkly competitor: evaluate flags in an
engine, manage them from a CLI and a hosted dashboard. The product domain is `featureslash.dev`
(not registered yet, so nothing is routed to it).

**This repository is a skeleton.** It has the structure, the gate and the CI, and no flag product
features. The engine exports a name and a version; the dashboard and the site each render one page,
and the C# server answers `GET /health`. Flag evaluation, storage and the API arrive as their own
tickets. The design they build on is in [`docs/architecture.md`](docs/architecture.md).

This file is the operating agreement for contributors and agents working here: the gate, the landing
rule and the invariants. Keep it accurate.

## The gate

A small Nx workspace on Bun workspaces:

| Project | Path | What it is |
| --- | --- | --- |
| `engine` | `packages/engine` | flag evaluation (a stub today) |
| `cli` | `packages/cli` | the `featureslash` bin: argv, stdout and exit codes, over `@hackcove/featureslash-engine` |
| `web` | `apps/web` | the dashboard: TanStack Start on Cloudflare Workers, D1 via Drizzle (one placeholder table) |
| `site` | `apps/site` | featureslash.dev: TanStack Start on a Worker, every page prerendered; no D1, no secrets |
| `server` | `apps/server` | the C# server: ASP.NET Core minimal API on .NET 10 (`FeatureSlash.Server`, xunit in `FeatureSlash.Server.Tests`); owns all flag data and the API |
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
  `dotnet SDK not found: skipping apps/server`; the TS projects run as before and the two dotnet
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
bun run build                     # nx run-many -t build  (every project's build target: web, site, cli, and the two C# projects)
(cd apps/web && bunx wrangler deploy --dry-run --outdir "$TMPDIR/fs-web-dry")
(cd apps/site && bunx wrangler deploy --dry-run --outdir "$TMPDIR/fs-site-dry")
```

One app: `bunx nx test:e2e web`, `bunx nx build site`.

First time on a machine: `bunx playwright install chromium-headless-shell`.

**Generated files are not formatted.** `oxfmt` rewrites what it is given, so anything a tool writes
is excluded in `.oxfmtrc.json`'s `ignorePatterns`: the route tree, `worker-configuration.d.ts` and
the drizzle migrations. Add a pattern when a new generator appears. `**/*.md` is ignored too.

**/tmp is a quota'd tmpfs; do not fill it.**

- Every TypeScript project's test run goes through `scripts/test-tmp-scope.ts` (a Vitest and Playwright
  `globalSetup`): `TMPDIR` is a fresh dir for the run and is removed after it. A new project's
  `vitest.config.ts` must list the same `globalSetup`.
- `nx.json` sets `"useDaemonProcess": false`; the daemon installs `nx@latest` into `/tmp` and does
  not clean it. Do not turn it on and do not run `nx daemon`.
- Scratch goes under `$TMPDIR`, never a hardcoded `/tmp`.

## Cloudflare, domains and publishing are human steps

The skeleton adds no Worker route and no custom domain: `featureslash.dev` is not registered. The
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
4. **A secret is read from the environment, never from the tree.** The site holds none.
5. **Migrations are additive.** `drizzle-kit generate` writes the `.sql` and its `meta/` snapshot
   together (`bun run db:generate` in `apps/web`); never hand-write one.
