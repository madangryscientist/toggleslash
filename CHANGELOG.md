# Changelog

All notable changes to the `@hackcove/featureslash` npm package (`packages/cli`) are listed here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). The version is `packages/cli/package.json`'s.

## [Unreleased]

### Added

- The repository skeleton: an engine stub, the `featureslash` CLI (`--version`, `--help`), a
  dashboard and a marketing site, each with its gate.
- Flag evaluation in the engine, with its spec (`docs/evaluation.md`) and shared test vectors
  (`fixtures/evaluation/`). The CLI does not use it yet.
- A C# evaluator (`apps/server/FeatureSlash.Evaluation`) that passes the same vectors.
- The server's storage: EF Core on SQLite, migrated at startup.
- Server auth: the admin token (`FEATURESLASH_ADMIN_TOKEN`), hashed server and client SDK keys per
  environment, and the admin API that creates, lists and revokes them.
- The admin API for projects, environments and flags (create, list, read; update, archive and restore
  for flags), and the OpenAPI document at `/openapi/v1.json`, committed as `apps/server/openapi/v1.json`.
- The admin API for a flag's targeting in each environment (targets, prerequisites, rules, fallthrough, on
  and off) with optimistic concurrency (`version`, `ETag`, `If-Match`; a stale save is 409) and a `toggle`
  route, and for segments, validated against `docs/evaluation.md`.
- An audit trail for the server: every admin write records one `audit_events` row in the same transaction
  (when, actor, entity, action, before and after, never a key or its hash), read back with
  `GET /api/v1/projects/{project}/audit` and a flag's `.../history`, filtered and paged by cursor.
- The SDK read API: `GET /sdk/v1/ruleset` (server key) returns an environment's flags and segments with a strong
  `ETag` and `304` on `If-None-Match`, and `POST /sdk/v1/evaluate` (server or client key, with CORS) evaluates a
  context for every flag or one, through the C# evaluator, without exposing rules, targets or segments.
- Usage counts: `POST /sdk/v1/events` (server or client key, with CORS, size-limited) adds daily per-flag,
  per-variation counts to the key's environment, evaluate counts what it serves, `GET /api/v1/projects/{project}/flags/{flag}/usage`
  reads them per environment, and flags carry `lastEvaluated`. Counts only: no context or end-user data is stored.
