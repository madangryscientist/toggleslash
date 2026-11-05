# API overview

The server (`apps/server`) speaks JSON over HTTP. Its contract is the OpenAPI 3 document it serves at
`/openapi/v1.json`, and the copy committed at [`apps/server/openapi/v1.json`](../apps/server/openapi/v1.json).
Read that file for request and response shapes, error codes and every route; this page says what the
routes are for and who may call them. The committed copy is generated, and a test fails when it differs
from what the server serves (see `AGENTS.md`, invariant 6).

## Authentication

There are two kinds of credential, and each is accepted only where its routes say so. Both go in an
`Authorization: Bearer <credential>` header.

| Credential | Set by | Accepted on |
| --- | --- | --- |
| Admin token | the server's `FEATURESLASH_ADMIN_TOKEN` | `/api/v1` (the admin API) |
| Server SDK key (`fs_server_…`) | `featureslash keys create --kind server`, or the admin API | `/sdk/v1/ruleset`, `/sdk/v1/stream`, `/sdk/v1/evaluate`, `/sdk/v1/events` |
| Client SDK key (`fs_client_…`) | `featureslash keys create --kind client`, or the admin API | `/sdk/v1/evaluate`, `/sdk/v1/events` |

A key is bound to one environment of one project. The server stores only a hash and a prefix of each key,
so a lost key cannot be shown again; create a new one and revoke the old one. The server never generates, stores or logs the admin token. `GET /health` takes no credential.

A client key never reads the ruleset or the stream, and `evaluate` returns only each flag's value, its
variation index and its reason, never the rules, targets or salts behind them.

## SDK read API (`/sdk/v1`)

| Method and path | What it does |
| --- | --- |
| `GET /sdk/v1/ruleset` | the environment's ruleset, with an `ETag`; send `If-None-Match` to get a `304` when it has not changed |
| `GET /sdk/v1/stream` | Server-Sent Events: a `connected` event, then a `changed` event per change, each carrying the ETag |
| `POST /sdk/v1/evaluate` | evaluates the environment's flags for a context and returns each result (value, variation index, reason) |
| `POST /sdk/v1/events` | adds a batch of usage counts for the key's environment; counts only, never a context or a user |

The TypeScript server SDK ([`sdk.md`](sdk.md)) uses the ruleset, the stream and the events route.

## Admin API (`/api/v1`)

All of these take the admin token. They cover projects, environments, flags (including archive and
restore, per-environment targeting and toggling, history and usage), SDK keys, segments and the project's
audit log. The `featureslash` CLI ([`packages/cli/README.md`](../packages/cli/README.md)) calls the routes
for projects, environments, flags and SDK keys; the dashboard (`apps/web`) calls them for the flag list and
detail page, environments, keys, segments and the audit log. The OpenAPI document lists every route.

Every write is recorded in the audit log, newest first, with the actor, the entity, the action and the
`before` and `after` state.

