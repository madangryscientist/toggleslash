# ToggleSlash facts

Checked against main at c254941 on 2026-11-21.

## What it is
ToggleSlash is a feature flag service you run yourself: it decides which users see which version of a feature, and you change those decisions from a command line or a web dashboard while the application runs. It is early (alpha), and its npm packages are at version 0.2.0.

## Today
- `server`: a server written in C# (.NET 10) keeps projects, environments, flags and segments in a SQLite file and serves an admin API and the routes the SDKs call.
- `openapi-document`: the server describes its API in an OpenAPI document, committed at `apps/server/openapi/v1.json`.
- `evaluation-spec`: flag evaluation is written down once as a spec (`docs/evaluation.md`) with shared test vectors (`fixtures/evaluation/`), and the TypeScript engine and the C# evaluator both pass every vector.
- `flag-variations`: a flag serves one of its variations, which can be any JSON values; the dashboard creates boolean flags only, and other kinds are made through the API.
- `targeting`: a flag can target individual contexts by key, match rules on context attributes, include reusable segments, depend on other flags (prerequisites) and roll out by percentage, separately in each environment.
- `targeting-operators`: rules compare strings, numbers, semantic versions and dates, and match regular expressions.
- `multi-context`: a context can carry several kinds (for example a user and an organisation), and targets, rules and rollouts can name the kind they use.
- `targeting-concurrency`: saving targeting checks a version number, so a save made on an outdated copy is refused instead of overwriting a newer change.
- `audit-trail`: every write through the admin API records which credential made it (the admin token or a scoped token) and what changed, with before and after, and the trail can be read back by flag, environment and date.
- `usage-counts`: the server counts how often each flag variation is served per day, and stores counts only, never a user's context or key.
- `flag-lifecycle`: each flag shows whether it is new, active, inactive or archived, from its creation date and its recent usage counts.
- `flag-tags`: flags carry tags, and the flag list can be filtered by tag.
- `archive-and-delete`: a flag can be archived, restored and deleted for good.
- `diagnose`: the server can explain why a flag gave a value for a given context, now or under an earlier ruleset it served, without recording anything.
- `ruleset-lookup`: each ruleset the server sends to an SDK has a hash, and the server can show what the flags were under a hash it served.
- `sdk-keys`: server and client SDK keys are made per environment and stored only as a hash and a prefix.
- `scoped-tokens`: API tokens can be limited to one project and to read or write, and always expire.
- `secure-mode`: in an environment with secure mode on, a browser can evaluate flags only for a context the application's own server signed.
- `client-visibility`: a client key (used in browsers) sees only flags marked visible to client keys; a new flag is hidden until marked.
- `live-updates`: SDKs get changes through a stream from the server, with polling when the stream is down; the stream works within one server process.
- `rate-limits`: the server limits requests per key and per IP address in memory, and sets security headers, a body size limit and request timeouts.
- `cli`: the `toggleslash` command manages projects, environments, flags and their targeting, segments, SDK keys and scoped tokens over the admin API, and has `diagnose`, `runtime` and `rulesets` commands.
- `dashboard`: a web dashboard lists, searches, creates, toggles and archives flags, edits targeting, shows each flag's history and usage, and has pages for environments, SDK keys, segments, the audit trail and the flags as they were under a past ruleset hash.
- `dashboard-sign-in`: the dashboard has one operator sign-in, a password and a signed session cookie; there are no user accounts or roles.
- `typescript-server-sdk`: the TypeScript server SDK keeps a copy of an environment's ruleset current and evaluates flags in the application's own process.
- `browser-sdk`: the browser SDK asks the server to evaluate flags for one context, caches the answer and updates it when flags change.
- `openfeature-provider`: an OpenFeature provider for server-side TypeScript wraps the TypeScript server SDK.
- `opentelemetry-tracing`: an SDK hook adds each flag evaluation and the ruleset hash to the active OpenTelemetry trace.
- `other-server-sdks`: server SDKs for Python, Go, Java, C# and Ruby evaluate flags locally and pass the same conformance tests as the TypeScript SDK; none is published, so they are used from a checkout of this repository.
- `example-app`: an example shop checkout uses the server and browser SDKs, and `bun run stack` starts it with the server, seeded flags and the dashboard on one machine.

## Planned
- `user-accounts-and-roles`: users, roles and project scoping, so a team does not share one admin token.
- `approvals`: approvals and required comments before a change goes live.
- `scheduled-changes`: flag and segment changes scheduled for a later time.
- `progressive-rollouts`: rollouts that raise the percentage in automatic steps.
- `webhooks`: flag triggers and webhooks for CI and incident tools.
- `history-restore`: restoring a flag to an earlier version from its history.
- `copy-across-environments`: comparing and copying flags between environments.
- `experiments`: custom events, metrics and experiments.
- `postgres`: Postgres as a database, and running more than one server process.
- `relay`: a relay or cache tier close to the applications.
- `sso`: single sign-on (SAML) and SCIM.
- `terraform-provider`: a Terraform provider.
- `code-references`: finding where flags are used in code.
- `mobile-sdks`: mobile SDKs.

## Licence
- `licence`: Elastic-2.0, source available under the Elastic License 2.0 (not open source).

## Where it runs
- `server-from-source`: the server runs from a checkout with the .NET 10 SDK (`dotnet run`), as `docs/quickstart.md` shows.
- `server-container`: the server runs in Docker from an image you build from this repository with the included `compose.yaml` (`docs/self-hosting.md`); no image is published.
- `cli-npm`: the CLI installs from npm as `@slashsuite/toggleslash` (version 0.2.0) and needs Bun 1.4 or later.
- `typescript-packages-npm`: the TypeScript engine, server SDK, browser SDK, OpenTelemetry hook and OpenFeature provider install from npm under `@slashsuite/` (version 0.2.0).
- `server-sdks-from-checkout`: the Python (3.11 or later), Go (1.26), Java (17 or later), C# (.NET 10) and Ruby (3.3 or later) SDKs run from a checkout of this repository.
- `dashboard-cloudflare-workers`: the dashboard runs on Cloudflare Workers; the maintainer's own copy is at flags.madangryscientist.com behind Cloudflare Access, for the maintainer only, and there is no public hosted service.
- `local-stack`: `bun run stack` runs the server, the dashboard and the example app on one machine, on loopback, with the .NET SDK and Bun.
