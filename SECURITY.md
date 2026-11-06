# Security

## Reporting a vulnerability

Report it privately, not in a public issue: use GitHub's private vulnerability reporting,
**Security > Report a vulnerability** on the repository (https://github.com/madangryscientist/featureslash).
If you cannot use GitHub, email hello@hack-cove.com. Only the maintainers can read the report. Say
what you found, the version or commit, and the steps to reproduce it; a proof of concept helps.

This is a one-maintainer project, so these are targets, not guarantees: an acknowledgement within 5
working days, and a fix or a plan within 30 days for anything in scope. We will credit you in the
advisory unless you ask us not to. Please give us that time before you disclose, and do not test
against someone else's deployment or data.

## What is in scope

- **The CLI** (`packages/cli`, the `@hackcove/toggleslash` npm package) and **the engine** it runs
  (`packages/engine`).
- **The server** (`apps/server`): its admin token and SDK key handling. A key or token reaching a
  log, a client key reading a ruleset, and a revoked key still working are all in scope.
- **The dashboard** (`apps/web`): it lists and changes flags over the server's admin API. It has no
  sign-in of its own, so anyone who can reach it can act with the admin token it was given; how it
  handles that token is in scope.

Out of scope: a deployment you configured weakly, a vulnerability in a dependency that does not
affect ToggleSlash (report it upstream), and findings that need a stolen token or a machine you
already control.

## The threat model, short

The code is public, so assume an attacker has read it. Nothing here depends on the code being
secret: every secret is read from the environment, never from the tree. The server's database holds
only hashes of SDK keys, so reading the file does not yield a working key. A client key is assumed
public. This section grows as the product does.
