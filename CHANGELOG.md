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
- A C# evaluator (`apps/server/FeatureSlash.Evaluation`) that passes the same vectors. No endpoint uses it yet.
- The server's storage: EF Core on SQLite, migrated at startup. No endpoint uses it yet.
