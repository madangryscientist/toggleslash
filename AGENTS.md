# ToggleSlash: repo conventions

ToggleSlash is a source-available feature flag service, a LaunchDarkly competitor: evaluate flags in an
engine, manage them from a CLI and a dashboard. The product has no domain yet, so nothing
is routed anywhere.

**This repository is early, and nothing is published.** The C# server (`apps/server`) migrates its SQLite database, answers `GET /health`, authenticates the admin token, hashed SDK keys and hashed scoped API tokens (one project, read or write, always expiring), and serves the admin API (SDK keys, scoped tokens, projects, environments, flags, their per-environment targeting and segments (both versioned), with each write recorded in an audit trail you can read back, a diagnostic evaluation route that answers "why is this flag on for this context" (now, or under a ruleset hash it served) and records nothing, a runtime read of an environment's ruleset tag, flag count and last change, and a lookup of what the flags were under a ruleset hash it served, described by `/openapi/v1.json`) and the SDK read API (`GET /sdk/v1/ruleset` with an `ETag`, `GET /sdk/v1/stream` for live updates in one server process, `GET /sdk/v1/changes` for a client key's change signal, `POST /sdk/v1/evaluate` through the C# evaluator, and `POST /sdk/v1/events` for usage counts, which store counts and nothing about an end user), with in-memory rate limits per key and per IP (`apps/server/ToggleSlash.Server/Limits`; there is no off switch), security headers on every answer, a 1 MiB body cap and request timeouts that skip the stream (`apps/server/ToggleSlash.Server/Hardening`). The TypeScript server SDK (`packages/sdk`) calls the ruleset, stream and events routes, and `packages/sdk-otel` puts its evaluations and the ruleset hash on OpenTelemetry traces, and `packages/sdk-openfeature` is an OpenFeature provider over it, and `packages/sdk-browser` is the browser SDK, which asks the server to evaluate for one context, caches the answer and follows `GET /sdk/v1/changes`; the `toggleslash` CLI (`packages/cli`) calls the admin API for projects, environments, flags, SDK keys and scoped tokens, and for `diagnose`, `runtime` and `rulesets show`. The dashboard (`apps/web`) calls the admin API for the flag list, the flag detail page, environments, keys, segments, the audit log and a page for a past ruleset hash, behind a sign-in for one operator (a password and a signed session cookie, both from Worker secrets), with security headers and a nonce-based content security policy on every response. The site (`apps/site`) is prerendered. An example client app (`apps/example`, a fictional shop checkout on the SDK) and `bun run stack` (the real server, a seeded shop, the dashboard and the example, on loopback with throwaway secrets) show the product running on one machine. No hosted service or published package exists. The server builds into a container for self-hosting (`apps/server/Dockerfile`, `compose.yaml`, [`docs/self-hosting.md`](docs/self-hosting.md)), proven by a CI job and never pushed. The design is in [`docs/architecture.md`](docs/architecture.md).

**Evaluation is specified once and implemented per language.** [`docs/evaluation.md`](docs/evaluation.md)
is normative and `fixtures/evaluation/` is its executable form. A change to evaluation changes the spec,
the vectors and every implementation together, and nothing in it may depend on one language's runtime.

This file is the operating agreement for contributors and agents working here: the gate, the landing
rule and the invariants. Keep it accurate.

## The gate

A small moon workspace (moon 2.6.5) on Bun workspaces. `.moon/workspace.yml` lists the projects, `.moon/tasks/*.yml` holds what they inherit (shared inputs, the Vitest `test` task) and each project has a `moon.yml`. There is no toolchain file: every task runs on the system toolchain, and moon never downloads Bun or runs `bun install`:

| Project | Path | What it is |
| --- | --- | --- |
| `engine` | `packages/engine` | flag evaluation, to `docs/evaluation.md`; `test/vectors.test.ts` runs every shared vector |
| `cli` | `packages/cli` | the `toggleslash` bin: argv, stdout and exit codes; `config`, `projects`, `envs`, `flags`, `keys`, `tokens`, `diagnose`, `runtime` and `rulesets` commands over the server's admin API (injected `fetch`, tested against a fake server); `--version` from the engine. Exit 0, 1 (server or network failure), 2 (usage); see its `README.md` |
| `sdk` | `packages/sdk` | the TypeScript server SDK (`createClient`): ruleset with ETags, the stream with reconnect and a polling fallback, local evaluation with the engine, batched usage counts, and an optional `hooks.afterEvaluation`; it never throws at a caller. Private and unpublished; see `docs/sdk.md`, the TypeScript binding of the language-neutral `docs/sdk-contract.md` |
| `sdk-otel` | `packages/sdk-otel` | `createTracingHooks`: the SDK's `hooks.afterEvaluation` that adds a `feature_flag.evaluation` event to the active OpenTelemetry span, with the flag result and `toggleslash.ruleset.etag` (the hash the lookup route answers); the context id and the value are off unless an option turns each on. Depends on `@opentelemetry/api` only: no exporter, no tracer provider. Private and unpublished; see `docs/tracing.md` |
| `sdk-openfeature` | `packages/sdk-openfeature` | `ToggleSlashProvider`: a server-side OpenFeature provider over the SDK's `Client` (`new ToggleSlashProvider(client)`): `targetingKey` becomes the context key, other entries attributes, and the SDK's reason and error code map to OpenFeature's. It evaluates nothing itself and emits no provider events. Depends on `@openfeature/server-sdk` and the SDK. Private and unpublished; see its `README.md` |
| `sdk-browser` | `packages/sdk-browser` | `createBrowserClient`: the browser SDK. The server evaluates: it sends its context to `POST /sdk/v1/evaluate`, caches the whole answer, serves `variation`/`detail` synchronously from it, re-evaluates on `connected`/`changed` from `GET /sdk/v1/changes` (read with `fetch`, key in a header), polls only while that is down, and fires `change` with the changed keys. It has no ruleset and no engine import; it imports the server SDK only through `@hackcove/toggleslash-sdk/shared` (`packages/sdk/src/shared.ts`), and a test holds that. No storage. Private and unpublished; see `docs/sdk-browser.md` |
| `sdk-conformance` | `packages/sdk-conformance` | the SDK conformance harness: `bun run conformance -- --service "<command>"` (or `--service-url`) starts a server SDK's test service, plays the application and the ToggleSlash server around it, and runs the scenarios in `fixtures/sdk/*.json` and every evaluation vector but `buckets.json` (two named skips) through it, on real time with wide windows; every scenario also checks that no request carries the context and that every request is one the protocol allows. Its `test` runs the TypeScript SDK's service (`packages/sdk/test-service/main.ts`), in about 12 s. Private; see `docs/sdk-test-service.md`, which holds the protocol and the coverage table |
| `web` | `apps/web` | the dashboard: TanStack Start on Cloudflare Workers; `/sign-in` and a stateless session (`src/api/auth.server.ts`; fails closed without `TOGGLESLASH_OPERATOR_PASSWORD` and `TOGGLESLASH_SESSION_SECRET`), with `requireSession` as global function middleware in `src/start.ts`, so every server function is protected by default and a test holds that; a server-only client for the C# admin API (`src/api`), a project and environment switcher and the flag list (search, toggle, create, archive, and an archived view with restore and delete) with each flag's lifecycle stage, and the flag detail page (`/flags/$flag`: metadata, targeting editor with context kinds on targets, clauses and rollouts, history, usage; the editor's pure logic is `src/targeting`), and the operator pages `/environments` (list, create, rename, delete), `/keys` (create shows the plaintext once, in component state only; revoke asks first), `/segments` (list, create, edit with members of other context kinds, delete), `/audit` (flag, environment and date filters, cursor paging) and `/rulesets/$hash` (the flags as they were under a ruleset hash a trace carries: read only, no salt, needs `?project=&env=`, a not-found state for a hash never served, sign-in returns to the same URL). D1 via Drizzle stays a placeholder table, unused. Its e2e run against a fake API (`e2e/fake-api.ts`), so its gate needs no .NET SDK, and sign in through the real form with throwaway values from `playwright.config.ts`: there is no test mode that skips sign-in |
| `example` | `apps/example` | a fictional shop checkout on `Bun.serve` that uses the TS SDK against a server (`TOGGLESLASH_URL`, `TOGGLESLASH_SDK_KEY`, `TOGGLESLASH_CLIENT_KEY`): a page with a user picker, `/state` for its values, and a section that runs the browser SDK (`@hackcove/toggleslash-sdk-browser`) with the client key: `src/server.ts` bundles `src/browser.ts` with `Bun.build` at startup and serves it, in memory, at `GET /browser.js`, so the page loads one module from the example server. Plain HTML with no stylesheet and no component, so invariant 3 has nothing to compose here. The server key never reaches the page, and a test holds that. Private; see its `README.md` |
| `site` | `apps/site` | the marketing site: TanStack Start on a Worker, every page prerendered; no D1, no secrets |
| `server` | `apps/server` | the C# server (one moon project, tagged `dotnet`, over the four .NET projects below): ASP.NET Core minimal API on .NET 10 (`ToggleSlash.Server`, xunit in `ToggleSlash.Server.Tests`); owns all flag data and the API. `apps/server/openapi/v1.json` is its generated OpenAPI document; `apps/server/Dockerfile` and the root `compose.yaml` containerise it (below) |
| `evaluation` | `apps/server/ToggleSlash.Evaluation` | part of the `server` moon project, not one of its own: the C# implementation of `docs/evaluation.md`: a class library with no ASP.NET or database dependency (xunit in `ToggleSlash.Evaluation.Tests`, which runs every vector); the server references it for `POST /sdk/v1/evaluate` |
| `e2e` | `packages/e2e` | the cross-language end-to-end test (`test:e2e`, not `test`): builds and starts the real C# server on a port the OS picks with a SQLite file under `$TMPDIR`, drives it with the real CLI (a subprocess) and the real TS SDK, and checks a CLI flip arrives through the stream and that `POST /sdk/v1/evaluate` and the SDK's local evaluation agree over a set of contexts. It also holds the seed (`src/seed.ts`: a shop with five flags and a segment) and `bun run stack` (`src/stack.ts`, `src/stack-cli.ts`): the server, the seed, the dashboard on 4440 and the example app on 4441, with secrets made per run; `test/example.test.ts` proves the example against the real server. `bun run test:stack` (`test:stack`, not `test:e2e`; `playwright.config.ts`, `stack/journey.spec.ts`) starts that stack and walks one journey in a browser: sign in to the dashboard, two shoppers on the example, a flag created through the UI, a toggle that reaches both shoppers through the stream and, in the browser SDK's table, through the change signal, `runtime` and `diagnose` from the CLI, the usage count, the audit log, and a read-scoped token refused a write; on a failure it prints the stack's logs. Loopback only; skips with a printed line when no `dotnet` runs (in CI, where `CI` is set, a missing SDK fails instead). Private |
| `scripts` | `scripts` | `check-slop.ts`, `check-dashes.ts`, `check-workflows.ts`, `test-tmp-scope.ts`, `moon.ts` and `dotnet.ts` (typecheck + lint; `check-workflows.ts` also has a `bun test` test) |

`bunfig.toml` sets `[install] linker = "isolated"` and `globalStore = true` (the shared package store that
makes a warm worktree install fast): a package sees its own declared dependencies and the root's, not
another package's. Declare what a package imports in that package's `package.json`.

CI runs on GitHub-hosted `ubuntu-latest`, so a pull request from a fork runs with a read-only token. `scripts/check-workflows.ts` (run by the `gate` job) forbids a self-hosted runner in any workflow and any trigger a fork can drive beyond `pull_request`: a workflow takes only `push`, `pull_request`, `workflow_dispatch` and `workflow_call`.

A change is green when all of these pass **from the worktree**:

```bash
bun install                       # FIRST: a fresh worktree has no node_modules
bun run typecheck                 # moon run :typecheck  (tsc, strict)
bun run lint                      # moon run :lint  (oxlint --type-aware --deny-warnings + check-slop), then check-dashes over every tracked file
bun run format:check              # oxfmt --check  (`bun run format` fixes)
bun run parity:check              # the feature parity matrix matches this tree
bun run test                      # moon run :test  (Vitest under the Bun runtime, and xunit for the server)
```

`bun run parity:check` validates [`docs/feature-parity.json`](docs/feature-parity.json) against the
repository: every `yes` or `partial` has evidence, every evidence target exists, and every API route,
CLI command and dashboard route is claimed by a capability. `bun run parity` regenerates
[`docs/feature-parity.md`](docs/feature-parity.md); commit both. A `feat` commit must change the JSON,
and CI enforces that with `bun run parity:check:commits`. Competitor cells are a coarse, dated
reading of public pages, not a claim of vendor parity; refresh them when a release or comparison
matters.

**The .NET SDK is needed for `apps/server`.** A change to it needs the SDK to be checked. The root
scripts (`typecheck`, `lint`, `build`, `test`, `test:e2e`, `test:stack`) go through `scripts/moon.ts`,
which asks `scripts/dotnet.ts` (`findDotnet`) for a `dotnet` that *runs*, from the repo root so
`global.json` applies. It tries every `dotnet` on PATH in order, because a `dotnet` that exists can
still fail: here `/usr/bin/dotnet` is a host with no SDK, and a mise shim fails where no SDK is
active. `packages/e2e/src/dotnet.ts` uses the same function, so the two cannot disagree. The four .NET
projects are one moon project, `server`, tagged `dotnet`: `server:test` runs `dotnet test` on both test
projects (dotnet restores and builds on its own) and `server:build` builds them without moon's cache,
since dotnet's incremental build is its own. Two paths:

- **With a working `dotnet`** (anywhere on PATH, `/usr/bin/dotnet` first or not): `bun run test`
  runs `server:test` with the TS projects, `bun run build` builds the server, and `bun run test:e2e`
  runs the `e2e` project. The version (10.0.402) is pinned in `.tool-versions` (mise activates it here;
  `mise install` installs it) and in `global.json` (what `dotnet` itself reads). The first restore
  fetches NuGet packages, so it needs the network once.
- **Without one**: the wrapper prints a `!!` banner to stderr before and after the run, saying
  `apps/server` is NOT in the run, so a green there does not cover the server, and adds
  `--query "tag!=dotnet"` to `moon run`, which leaves the `dotnet`-tagged project out; the TS projects
  run as before. In CI (`CI` set) a missing SDK fails instead. Setting `TOGGLESLASH_DOTNET_DISABLE`
  yourself skips the check and the banner and leaves the server out.

Why the wrapper does more than check: moon puts nothing of its own runtime ahead of PATH. It prepends
its own and proto's bin directories and every `node_modules/.bin` from the workspace upward, and
leaves the rest of the caller's PATH in order, so a task runs the first `dotnet` on the caller's PATH.
Here that is `/usr/bin/dotnet`, a host with no SDK, ahead of the mise one (`dotnet restore` fails with
"No .NET SDKs were found"). The wrapper puts the directory of the `dotnet` it found first on PATH. **A
bare `bunx moon ...` skips that**, so on a machine like this one use the root scripts, or
`bun scripts/moon.ts run <target>` for one project. Moon's project graph needs no SDK, so a bare `bunx moon`
command works without one for the TS projects. Both
`.csproj` files set `Nullable`, `TreatWarningsAsErrors` and `InvariantGlobalization`;
`apps/server/Directory.Build.props` and `.rsp` leave no compiler server or MSBuild node running after a
build. `bin/` and `obj/` are ignored, and so are they in the `server` project's inputs.

One project: `bunx moon run engine:test`, `bunx moon run cli:lint`, `bunx moon run server:test`. moon
caches in `.moon/cache` (gitignored; it cannot be moved); add `--force` to rerun a cached task. The root
scripts run three tasks at a time (`--concurrency 3`): moon's default of one per core
starved the timing-sensitive suites. There is no remote cache. moon has no `^default`: without a
toolchain it does not hash the files of a project a task depends on, so every TS project's tasks
(tag `ts`, `.moon/tasks/ts.yml`) take all of `packages/**` as inputs, and a change to the engine
re-runs them all. Every `test` also takes `fixtures/**` and `apps/server/openapi/*`. A generated file a
task rewrites (`routeTree.gen.ts`) must stay out of the inputs or the hash changes under the task.
Task ids cannot hold a colon, so the Playwright and end-to-end tasks are `test-e2e` and `test-stack`
(the root script names stay `test:e2e` and `test:stack`). Lint rules are all errors; fix the code, do not disable a
rule. A TODO carries an issue: `TODO(#123)`. Tests live in `packages/*/test/` and
`apps/*/src/**/*.test.ts` and run with `bun --bun vitest`; the server's tests are xunit, in
`apps/server/ToggleSlash.Server.Tests`.

A change to `apps/web` or `apps/site` also runs their builds, Playwright and Worker dry-runs (from the
worktree root). `test:e2e` runs before `build`: web's e2e run leaves its own build behind, and `build` replaces it.

```bash
bun run test:e2e                  # moon run :test-e2e  (Playwright: web and site; web builds with the e2e env on a fresh local D1; and the `e2e` project, which needs the .NET SDK)
bun run build                     # moon run :build  (every project's build task: web, site, cli, and the C# projects)
(cd apps/web && bunx wrangler deploy --dry-run --outdir "$TMPDIR/fs-web-dry")
(cd apps/site && bunx wrangler deploy --dry-run --outdir "$TMPDIR/fs-site-dry")
```

One app: `bunx moon run web:test-e2e`, `bunx moon run site:build`. The cross-language test alone: `bunx moon run e2e:test-e2e` (with the SDK on the machine; `bun run test:e2e` runs it too and skips it without one; a `dotnet build` of the server, then about five seconds). Any change to the server, the CLI, the SDK or the engine can break it, so run it then; CI has its own `e2e` job. `bun run test:stack` (about fifteen seconds after a build; it needs the SDK and `bunx playwright install chromium-headless-shell`, and it holds ports 4440 and 4441) is the whole product in a browser: run it after a change to `apps/web`, `apps/example`, the SDK's stream, or anything the dashboard and the server must agree on. It is not part of `bun run test:e2e`; CI has its own `stack` job, which uploads nothing because a report could hold the run's secrets.

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
- moon has no daemon and installs nothing into `/tmp`: no toolchain is declared, so it downloads
  nothing and runs no install. Keep it that way (no `.moon/toolchains.yml`, `pipeline.installDependencies: false`).
- Scratch goes under `$TMPDIR`, never a hardcoded `/tmp`.

## SDKs in `sdks/`

`sdks/<lang>` is where a server SDK per language goes (Python, Go, Java, C#, Ruby to start), each built from
[`docs/sdk-authoring/PROMPT.md`](docs/sdk-authoring/PROMPT.md); see [`sdks/README.md`](sdks/README.md).
They are **not** in the gate above: `.moon/workspace.yml` lists the projects explicitly, so nothing under
`sdks/` is a moon project. Each has its own workflow, `.github/workflows/sdk-<lang>.yml`,
which runs on a change to the fixtures, the contract, the test-service protocol or the harness too. So such a
change must keep every `sdks/*` green, and a pull request with one red is not merged.

## Cloudflare, domains and publishing are human steps

Pushing a container image to a registry is a human step too. The repo adds no Worker route and no custom domain: the product has no domain yet. The
`D1` binding in `apps/web/wrangler.jsonc` carries a placeholder id. Creating the D1 database, the
Worker routes, and the first npm publish each wait for the maintainer's yes.
Registering a domain is a human step: until one is bought and the maintainer says yes, no file names an address for the product.

## Landing

- Code changes land through pull requests; nobody pushes to `main`. Branch, gate, rebase on
  `origin/main`, push, open the PR.
- Commit subjects are Conventional Commits: `<type>(<scope>): <subject>`, with type one of `feat fix
  docs chore refactor test perf build ci style revert`. The squash keeps the subject.
- Stage files by name or `git add -A`; never `git commit -am` (it drops new files).
- No `Co-Authored-By` trailer and no AI attribution line, in a commit or a pull request.

## Invariants

1. **Exit codes are a contract.** `toggleslash` exits 0 on success, 2 on a usage error.
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
   `ToggleSlash.Server/Data/Migrations`, from the worktree root:
   `dotnet tool restore`, then `dotnet ef migrations add <Name> --project apps/server/ToggleSlash.Server`.
   The tool is pinned in `.config/dotnet-tools.json`. The server applies them at startup in one
   transaction, so a change SQLite cannot make inside one (a table rebuild) fails there, which is one
   more reason to stay additive. A model change with no migration fails startup.
6. **`apps/server/openapi/v1.json` is generated, never hand-edited.** A test fails when it differs
   from what the server serves at `/openapi/v1.json`. After an API change, regenerate it with
   `TOGGLESLASH_UPDATE_OPENAPI=1 dotnet test apps/server/ToggleSlash.Server.Tests` and commit the
   result. The TypeScript clients generate from or check against this file.
7. **A client key never reads the ruleset, and `evaluate` never returns rule data.** `GET /sdk/v1/ruleset` is
   server-key only and carries rules, targets and salts; `GET /sdk/v1/stream` is server-key only and carries ETags; a client key may open `GET /sdk/v1/changes`, which carries no ETag and no flag data (every event's data is `{}`); `POST /sdk/v1/evaluate` (either key, with `POST /sdk/v1/events` and `GET /sdk/v1/changes`, the only routes with
   CORS) answers `value`, `variationIndex` and `reason` and nothing it was configured with. `events` stores counts only
   (`flag_usage_daily`), never a context, a context key or any end-user data.
