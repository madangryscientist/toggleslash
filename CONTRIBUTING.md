# Contributing to ToggleSlash

Thanks for helping. This page is the short version of [`AGENTS.md`](AGENTS.md), the full operating
agreement for people and coding agents working here. Read it before a large change.

By taking part you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). A security problem goes to
[`SECURITY.md`](SECURITY.md), not to a public issue. Your contribution is licensed under the Elastic License 2.0, as the
rest of the repo is (see [`LICENSE`](LICENSE)); there is no CLA.

## Start with an issue

For a bug, open an issue with the version and the steps. For anything bigger than a fix, open an
issue first so we can agree on it before you build it.

## The gate

A change is ready when all of these pass from a fresh checkout:

```bash
bun install
bun run typecheck       # tsc, strict, on every project
bun run lint            # oxlint (type-aware, --deny-warnings) and check-slop
bun run format:check    # oxfmt; `bun run format` fixes it
bun run test            # Vitest under Bun
```

A change to `apps/web` or `apps/site` also runs `bunx moon run <app>:build` and `bunx moon run <app>:test-e2e`
(Playwright; `bunx playwright install chromium-headless-shell` once per machine).

A change to the server, the CLI, the SDK or the engine also runs `bun run test:e2e` (or `bunx moon run e2e:test-e2e` with the SDK): the cross-language
end-to-end test (the real server, CLI and SDK on localhost; it needs the .NET SDK and skips with a printed line without one).

A change to `apps/server` needs the .NET SDK (10.0.402, see `global.json`; `mise install` gets it).
Without an SDK the root scripts skip the server and print that they did.

Lint rules are errors: fix the code, do not disable a rule.

## Pull requests

- Branch from `main`, keep the change small, and rebase on `origin/main` before you push.
- Write the commit subject as a [Conventional Commit](https://www.conventionalcommits.org/)
  (`feat(scope): …`, `fix(scope): …`; `docs`, `chore`, `refactor`, `test`, `perf`, `build`, `ci`,
  `style`, `revert`). The scope is the area (`cli`, `engine`, `sdk`, `web`, `site`, `server`, `ci`) and the issue goes
  at the end: `fix(engine): handle an empty rule list (#12)`. The squash keeps your subject.
- Stage files by name. Never commit straight to `main`.
- No AI attribution trailers are required or expected.
