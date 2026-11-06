# FeatureSlash: repo conventions

FeatureSlash is an open source feature flag service, a LaunchDarkly competitor: evaluate flags in an
engine, manage them from a CLI and a dashboard. The product domain is `featureslash.dev`
(not registered yet, so nothing is routed to it).

**This repository is early, and nothing is published.** The C# server (`apps/server`) migrates its SQLite database, answers `GET /health`, authenticates the admin token, hashed SDK keys and hashed scoped API tokens (one project, read or write, always expiring), and serves the admin API (SDK keys, scoped tokens, projects, environments, flags, their per-environment targeting and segments (both versioned), with each write recorded in an audit trail you can read back, a diagnostic evaluation route that answers "why is this flag on for this context" (now, or under a ruleset hash it served) and records nothing, a runtime read of an environment's ruleset tag, flag count and last change, and a lookup of what the flags were under a ruleset hash it served, described by `/openapi/v1.json`) and the SDK read API (`GET /sdk/v1/ruleset` with an `ETag`, `GET /sdk/v1/stream` for live updates in one server process, `POST /sdk/v1/evaluate` through the C# evaluator, and `POST /sdk/v1/events` for usage counts, which store counts and nothing about an end user), with in-memory rate limits per key and per IP (`apps/server/FeatureSlash.Server/Limits`; there is no off switch), security headers on every answer, a 1 MiB body cap and request timeouts that skip the stream (`apps/server/FeatureSlash.Server/Hardening`). The TypeScript server SDK (`packages/sdk`) calls the ruleset, stream and events routes, and `packages/sdk-otel` puts its evaluations and the ruleset hash on OpenTelemetry traces; the `featureslash` CLI (`packages/cli`) calls the admin API for projects, environments, flags, SDK keys and scoped tokens, and for `diagnose`, `runtime` and `rulesets show`. The dashboard (`apps/web`) calls the admin API for the flag list, the flag detail page, environments, keys, segments, the audit log and a page for a past ruleset hash, behind a sign-in for one operator (a password and a signed session cookie, both from Worker secrets), with security headers and a nonce-based content security policy on every response. The site (`apps/site`) is prerendered. An example client app (`apps/example`, a fictional shop checkout on the SDK) and `bun run stack` (the real server, a seeded shop, the dashboard and the example, on loopback with throwaway secrets) show the product running on one machine. No browser SDK, hosted service or published package exists. The server builds into a container for self-hosting (`apps/server/Dockerfile`, `compose.yaml`, [`docs/self-hosting.md`](docs/self-hosting.md)), proven by a CI job and never pushed. The design is in [`docs/architecture.md`](docs/architecture.md).

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
| `cli` | `packages/cli` | the `featureslash` bin: argv, stdout and exit codes; `config`, `projects`, `envs`, `flags`, `keys`, `tokens`, `diagnose`, `runtime` and `rulesets` commands over the server's admin API (injected `fetch`, tested against a fake server); `--version` from the engine. Exit 0, 1 (server or network failure), 2 (usage); see its `README.md` |
| `sdk` | `packages/sdk` | the TypeScript server SDK (`createClient`): ruleset with ETags, the stream with reconnect and a polling fallback, local evaluation with the engine, batched usage counts, and an optional `hooks.afterEvaluation`; it never throws at a caller. Private and unpublished; see `docs/sdk.md` |
| `sdk-otel` | `packages/sdk-otel` | `createTracingHooks`: the SDK's `hooks.afterEvaluation` that adds a `feature_flag.evaluation` event to the active OpenTelemetry span, with the flag result and `featureslash.ruleset.etag` (the hash the lookup route answers); the context id and the value are off unless an option turns each on. Depends on `@opentelemetry/api` only: no exporter, no tracer provider. Private and unpublished; see `docs/tracing.md` |
| `web` | `apps/web` | the dashboard: TanStack Start on Cloudflare Workers; `/sign-in` and a stateless session (`src/api/auth.server.ts`; fails closed without `FEATURESLASH_OPERATOR_PASSWORD` and `FEATURESLASH_SESSION_SECRET`), with `requireSession` as global function middleware in `src/start.ts`, so every server function is protected by default and a test holds that; a server-only client for the C# admin API (`src/api`), a project and environment switcher and the flag list (search, toggle, create, archive) and the flag detail page (`/flags/$flag`: metadata, targeting editor, history, usage; the editor's pure logic is `src/targeting`), and the operator pages `/environments` (list, create), `/keys` (create shows the plaintext once, in component state only; revoke asks first), `/segments` (list, edit), `/audit` (flag, environment and date filters, cursor paging) and `/rulesets/$hash` (the flags as they were under a ruleset hash a trace carries: read only, no salt, needs `?project=&env=`, a not-found state for a hash never served, sign-in returns to the same URL). D1 via Drizzle stays a placeholder table, unused. Its e2e run against a fake API (`e2e/fake-api.ts`), so its gate needs no .NET SDK, and sign in through the real form with throwaway values from `playwright.config.ts`: there is no test mode that skips sign-in |
| `example` | `apps/example` | a fictional shop checkout on `Bun.serve` that uses the TS SDK against a server (`FEATURESLASH_URL`, `FEATURESLASH_SDK_KEY`, `FEATURESLASH_CLIENT_KEY`): a page with a user picker, `/state` for its values, a button that evaluates in the browser with the client key. Plain HTML with no stylesheet and no component, so invariant 3 has nothing to compose here. The server key never reaches the page, and a test holds that. Private; see its `README.md` |
| `site` | `apps/site` | featureslash.dev: TanStack Start on a Worker, every page prerendered; no D1, no secrets |
| `server` | `apps/server` | the C# server: ASP.NET Core minimal API on .NET 10 (`FeatureSlash.Server`, xunit in `FeatureSlash.Server.Tests`); owns all flag data and the API. `apps/server/openapi/v1.json` is its generated OpenAPI document; `apps/server/Dockerfile` and the root `compose.yaml` containerise it (below) |
| `evaluation` | `apps/server/FeatureSlash.Evaluation` | the C# implementation of `docs/evaluation.md`: a class library with no ASP.NET or database dependency (xunit in `FeatureSlash.Evaluation.Tests`, which runs every vector); the server references it for `POST /sdk/v1/evaluate` |
| `e2e` | `packages/e2e` | the cross-language end-to-end test (`test:e2e`, not `test`): builds and starts the real C# server on a port the OS picks with a SQLite file under `$TMPDIR`, drives it with the real CLI (a subprocess) and the real TS SDK, and checks a CLI flip arrives through the stream and that `POST /sdk/v1/evaluate` and the SDK's local evaluation agree over a set of contexts. It also holds the seed (`src/seed.ts`: a shop with five flags and a segment) and `bun run stack` (`src/stack.ts`, `src/stack-cli.ts`): the server, the seed, the dashboard on 4440 and the example app on 4441, with secrets made per run; `test/example.test.ts` proves the example against the real server. `bun run test:stack` (`test:stack`, not `test:e2e`; `playwright.config.ts`, `stack/journey.spec.ts`) starts that stack and walks one journey in a browser: sign in to the dashboard, two shoppers on the example, a flag created through the UI, a toggle that reaches both shoppers through the stream, `runtime` and `diagnose` from the CLI, the usage count, the audit log, and a read-scoped token refused a write; on a failure it prints the stack's logs. Loopback only; skips with a printed line when no `dotnet` runs (in CI, where `CI` is set, a missing SDK fails instead). Private |
| `scripts` | `scripts` | `check-slop.ts`, `test-tmp-scope.ts`, `nx.ts` and `dotnet.ts` (typecheck + lint only) |

`bunfig.toml` sets `[install] linker = "isolated"` and `globalStore = true` (the shared package store that
makes a warm worktree install fast): a package sees its own declared dependencies and the root's, not
another package's. Declare what a package imports in that package's `package.json`.

A change is green when all of these pass **from the worktree**:

```bash
bun install                       # FIRST: a fresh worktree has no node_modules
bun run typecheck                 # nx run-many -t typecheck  (tsc, strict)
bun run lint                      # nx run-many -t lint  (oxlint --type-aware --deny-warnings + check-slop)
bun run format:check              # oxfmt --check  (`bun run format` fixes)
bun run parity:check              # the feature parity matrix matches this tree
bun run test                      # nx run-many -t test  (Vitest under the Bun runtime)
```

`bun run parity:check` validates [`docs/feature-parity.json`](docs/feature-parity.json) against the
repository: every `yes` or `partial` has evidence, every evidence target exists, and every API route,
CLI command and dashboard route is claimed by a capability. `bun run parity` regenerates
[`docs/feature-parity.md`](docs/feature-parity.md); commit both. A `feat` commit must change the JSON,
and CI enforces that with `bun run parity:check:commits`. Competitor cells are a coarse, dated
reading of public pages, not a claim of vendor parity; refresh them when a release or comparison
matters.

**The .NET SDK is needed for `apps/server`.** A change to it needs the SDK to be checked. The root
scripts (`typecheck`, `lint`, `build`, `test`, `test:e2e`, `test:stack`) go through `scripts/nx.ts`,
which asks `scripts/dotnet.ts` (`findDotnet`) for a `dotnet` that *runs*, from the repo root so
`global.json` applies. It tries every `dotnet` on PATH in order, because a `dotnet` that exists can
still fail: here `/usr/bin/dotnet` is a host with no SDK, and a mise shim fails where no SDK is
active. `packages/e2e/src/dotnet.ts` uses the same function, so the two cannot disagree. Two paths:

- **With a working `dotnet`** (anywhere on PATH, `/usr/bin/dotnet` first or not): `bun run test`
  restores, builds and tests `FeatureSlash.Server.Tests` with xunit, and `bun run test:e2e` runs the
  `e2e` project. The version (10.0.402) is pinned in `.tool-versions` (mise activates it here;
  `mise install` installs it) and in `global.json` (what `dotnet` itself reads). The first restore
  fetches NuGet packages, so it needs the network once.
- **Without one**: the wrapper sets `NX_DOTNET_DISABLE=true` and prints a `!!` banner to stderr
  saying `apps/server` is NOT in the run, so a green there does not cover the server; the TS projects
  run as before and the dotnet projects are not in the graph. In CI (`CI` set) a missing SDK fails
  instead. Setting `NX_DOTNET_DISABLE` yourself skips the check and the banner.

Why the wrapper does more than check: Nx puts the directory of its own runtime ahead of PATH for
every task (npm-run-path), and `nx` run through its `#!/usr/bin/env node` shebang is `/usr/bin/node`
here, so `/usr/bin` went first and the host-only `/usr/bin/dotnet` hid the SDK even with the mise one
first on the caller's PATH (`dotnet restore` failed with "No .NET SDKs were found"). The wrapper runs
nx under Bun (`~/.bun/bin` has no `dotnet`) and puts the directory of the `dotnet` it found first on
PATH. **A bare `bunx nx ...` skips all of that**, so on a machine like this one use the root scripts,
or `bun scripts/nx.ts <nx args>` for one project, and on a machine without an SDK set
`NX_DOTNET_DISABLE=true` yourself. The `@nx/dotnet` plugin in `nx.json` reads the .NET projects when
Nx builds its graph, so without an SDK **every** `nx` command fails unless it is disabled. Both
`.csproj` files set `Nullable`, `TreatWarningsAsErrors` and `InvariantGlobalization`;
`apps/server/Directory.Build.props` and `.rsp` leave no compiler server or MSBuild node running after a
build. `bin/` and `obj/` are ignored.

One project: `bunx nx test engine`, `bunx nx lint cli`, `bunx nx test FeatureSlash.Server.Tests`. Nx caches in `.nx/`; add `--skip-nx-cache` to
force a rerun. There is no Nx Cloud here. Lint rules are all errors; fix the code, do not disable a
rule. A TODO carries an issue: `TODO(#123)`. Tests live in `packages/*/test/` and
`apps/*/src/**/*.test.ts` and run with `bun --bun vitest`; the server's tests are xunit, in
`apps/server/FeatureSlash.Server.Tests`.

A change to `apps/web` or `apps/site` also runs their builds, Playwright and Worker dry-runs (from the
worktree root). `test:e2e` runs before `build`: web's e2e run leaves its own build behind, and `build` replaces it.

```bash
bun run test:e2e                  # nx run-many -t test:e2e  (Playwright: web and site; web builds with the e2e env on a fresh local D1; and the `e2e` project, which needs the .NET SDK)
bun run build                     # nx run-many -t build  (every project's build target: web, site, cli, and the C# projects)
(cd apps/web && bunx wrangler deploy --dry-run --outdir "$TMPDIR/fs-web-dry")
(cd apps/site && bunx wrangler deploy --dry-run --outdir "$TMPDIR/fs-site-dry")
```

One app: `bunx nx test:e2e web`, `bunx nx build site`. The cross-language test alone: `bunx nx test:e2e e2e` (with the SDK on the machine; `bun run test:e2e` runs it too and skips it without one; a `dotnet build` of the server, then about five seconds). Any change to the server, the CLI, the SDK or the engine can break it, so run it then; CI has its own `e2e` job. `bun run test:stack` (about fifteen seconds after a build; it needs the SDK and `bunx playwright install chromium-headless-shell`, and it holds ports 4440 and 4441) is the whole product in a browser: run it after a change to `apps/web`, `apps/example`, the SDK's stream, or anything the dashboard and the server must agree on. It is not part of `bun run test:e2e`; CI has its own `stack` job, which uploads nothing because a report could hold the run's secrets.

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
   never generates, stores or logs the admin token, and stores an SDK key or a scoped API token only as a hash and a prefix.
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
