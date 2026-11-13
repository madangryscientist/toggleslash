# Tracing: the ruleset hash on a trace

`packages/sdk-otel` (`@hackcove/toggleslash-sdk-otel`, moon project `sdk-otel`) is an evaluation hook for the
[TypeScript SDK](sdk.md). Each time the SDK evaluates a flag it adds a `feature_flag.evaluation` event to the
**active OpenTelemetry span**, with the flag's result and the **hash of the ruleset the SDK held**. Later, anyone
holding a trace can ask the server what the flags were under that hash ([below](#from-a-trace-to-the-flags)).

The package is private and unpublished. It depends on `@opentelemetry/api` only: it bundles no exporter and
starts no tracer provider, so the application owns both. The SDK itself has no OpenTelemetry dependency; it only
offers [`hooks.afterEvaluation`](sdk.md#hooks).

```ts
import { createClient } from "@hackcove/toggleslash-sdk";
import { createTracingHooks } from "@hackcove/toggleslash-sdk-otel";

const client = createClient({
  url: "https://flags.example.com",
  sdkKey: process.env.TOGGLESLASH_SDK_KEY ?? "",
  hooks: createTracingHooks({ project: "shop", environment: "production" }),
});
```

`project` and `environment` are required: they are the project and environment keys the SDK key belongs to
(the SDK does not know them), and they are what turns a hash into the lookup URL. An empty one throws a
`TypeError` when the hooks are made, at start-up, never on a request.

With **no active span**, or a span that is not recording, the hook does nothing. It is never an error.
The event is added to whatever span your instrumentation made current, so call `variation` inside a request
handler that is traced.

## The event

Name: `feature_flag.evaluation`, from the OpenTelemetry
[feature flag events](https://opentelemetry.io/docs/specs/semconv/feature-flags/feature-flags-events/)
conventions (status *Development*; its attributes are *Release Candidate*).

| Attribute | Value | |
| --- | --- | --- |
| `feature_flag.key` | the flag key | always |
| `feature_flag.provider.name` | `ToggleSlash` | always |
| `feature_flag.result.variant` | the variation index as a string, `"0"`, `"1"`... | omitted when nothing was served (a fallback, an unknown flag) |
| `feature_flag.result.reason` | see [reasons](#reasons) | always |
| `feature_flag.version` | the **flag's** `version` in the ruleset held | omitted when the flag is not in it |
| `toggleslash.ruleset.etag` | the ruleset's ETag: lowercase hex, no quotes | omitted before the first load |
| `toggleslash.project` | the `project` option | always |
| `toggleslash.environment` | the `environment` option | always |
| `error.type` | `flag_not_found`, `invalid_context`, `provider_not_ready`, `type_mismatch` or `general` | only when the reason is `error` |
| `feature_flag.context.id` | the context's `key`; none for a multi context | **only with `includeContextId: true`** |
| `feature_flag.result.value` | the value the caller got (a JSON object or array as JSON text) | **only with `includeValue: true`** |

- **Nothing identifying is on the span by default.** The context key is an end user's identifier and a value can
  be large or private, so each is off until the option turns it on. A context's other attributes are never
  recorded. Both options are about *your* trace backend's retention, not ToggleSlash's: the server never
  receives them.
- `feature_flag.version` is the flag's version. The conventions describe it as "the version of the ruleset",
  which is `toggleslash.ruleset.etag` here.
- `toggleslash.ruleset.etag` is the ETag the SDK held at that evaluation, the same value as `client.status().etag`
  without its quotes. It is also what `GET /sdk/v1/ruleset` sent as `ETag` and what the `runtime` read reports.

### Reasons

`feature_flag.result.reason` uses the conventions' well-known values where one applies and keeps ToggleSlash's own
lowercase name where none does:

| ToggleSlash reason | `feature_flag.result.reason` |
| --- | --- |
| `OFF` | `disabled` |
| `TARGET_MATCH`, `RULE_MATCH` | `targeting_match` |
| `FALLTHROUGH` | `fallthrough` (a custom value: it may be a fixed variation or a rollout, and the SDK cannot say which) |
| `PREREQUISITE_FAILED` | `prerequisite_failed` (custom) |
| `ERROR` | `error`, with `error.type` |

## From a trace to the flags

A flag can change the next second; the hash says which version of the flags this request saw. The server keeps the
exact ruleset it served for each hash (`ruleset_snapshots`; see [`architecture.md`](architecture.md#admin-api-and-openapi))
and reads it back, **without salts**, on a route that takes a `read` (or `write`) scoped token of the project.
An SDK key cannot call it, and it has no CORS.

Make a read token once (root admin token; `--expires-in-days` is required, 1 to 365):

```bash
toggleslash tokens create --project shop --name trace-lookup --permission read --expires-in-days 30
```

Then, from the trace's `toggleslash.project`, `toggleslash.environment` and `toggleslash.ruleset.etag`:

```bash
curl -s -H "Authorization: Bearer $TOGGLESLASH_READ_TOKEN" \
  "https://flags.example.com/api/v1/projects/shop/environments/production/rulesets/4137a8076dbf...d79e7?flag=new-checkout"
```

```json
{
  "etag": "\"4137a8076dbf...d79e7\"",
  "firstServedAt": "2026-11-05T23:06:11.5231888Z",
  "flags": [{ "key": "new-checkout", "on": true, "fallthrough": { "variation": 0 }, "version": 2, "...": "..." }],
  "segments": []
}
```

The same with the CLI: `toggleslash rulesets show <hash> --project shop --env production --flag new-checkout`.

- `{hash}` is the 64 lowercase hex characters **without quotes**. The quoted `ETag` header value is a 404.
- `?flag=<key>` narrows the answer to that flag and the segments its rules name. Without it, you get every flag.
  Compare the flag's rules with the span's `feature_flag.result.variant`: that is the variation the SDK served.
- **A hash can be a 404 even though it is on a real trace.** The server keeps a ruleset the first time it answers
  `GET /sdk/v1/ruleset` with a `200`. An SDK that only ever received `304`s for a tag (it held it from before
  snapshots existed) never caused one to be kept, and a tag the stream announced but nobody fetched is not kept
  either. A new process starts with a `200`, which is the usual case. The server also deletes snapshots, so an old
  hash can be a 404. A sweep runs at startup and then hourly; per environment it orders the snapshots by
  `first_served_at` (ties by hash), and a snapshot goes when its successor, the next one in that order, was first
  served more than `TOGGLESLASH_RULESET_RETENTION_DAYS` ago (90 by default; `0` keeps every one) and the snapshot is
  not the environment's current ruleset. So a snapshot is kept while it is the newest, while it is current, and while
  its successor was first served within the window. Age counts from when a snapshot stopped being the newest, not from
  when it last answered, and a rollback does not reset it: an old hash can stop answering at the first sweep after it
  stops being current, sooner than the window after its last use.

## Jaeger: a link from the event

Jaeger shows a span event as a log entry whose fields are the event's attributes. Its UI configuration
(`linkPatterns`, [Jaeger frontend UI](https://www.jaegertracing.io/docs/latest/deployment/frontend-ui/)) can
turn one field into a link, and a `#{...}` placeholder may name **any attribute of the same entry**; Jaeger
URL-encodes the value, and skips the link when a placeholder has no value.

**The link cannot carry the token, so point it at the dashboard.** A browser follows a link with a plain `GET` and no
`Authorization` header, and the server's own lookup route answers `401` to that. Do not put a token in the URL. The
dashboard has a page for a hash, behind its normal sign-in, and it reads the server with its own token on the server
side, so the token never reaches the browser or the link:

```json
{
  "linkPatterns": [
    {
      "type": "logs",
      "key": "toggleslash.ruleset.etag",
      "url": "https://dashboard.internal.example.com/rulesets/#{toggleslash.ruleset.etag}?project=#{toggleslash.project}&env=#{toggleslash.environment}",
      "text": "What were the flags under #{toggleslash.ruleset.etag}?"
    }
  ]
}
```

`/rulesets/$hash?project=&env=` shows the flags as they were under that hash, each with its on state and rules; add
`&flag=<key>` to narrow it to one flag. A signed-out visitor is sent to `/sign-in` and, once signed in, returns to
the same URL; the sign-in is the dashboard's one operator password, not a token. A hash the environment never
served shows a not-found page. A `url` that points at the server's own lookup route (`/api/v1/projects/.../rulesets/<hash>`)
needs a token the link cannot carry, so it only works through a proxy of your own that adds
`Authorization: Bearer <read token>`; the dashboard page needs none.

To ask why, not only what, `toggleslash diagnose <flag> --project shop --env production --context '{"key":"u1"}' --etag <hash>`
evaluates a context against that ruleset, as the server evaluated it then. It is the admin route
`POST /api/v1/projects/{project}/environments/{env}/evaluate` with `"etag": "<hash>"` in the body, it records nothing,
and a hash the environment never served is a 404.

## Grafana (Tempo)

In Grafana's Tempo data source, *Trace correlations* can add a link in the span details with **Type: External** and
an **External URL**; its variables include `tags` (span attributes) and `logs` (span events), both objects that
"must be parsed into a value variable with a regular expression transformation"
([trace correlations](https://grafana.com/docs/grafana/latest/datasources/tempo/configure-tempo-data-source/trace-correlations/)).
So the dashboard URL above, with a transformation for each of the three `toggleslash.*` values, is the way to go. The
link carries no token, and the dashboard's own sign-in is what protects the page. No ToggleSlash-specific Grafana
configuration is shipped.

## What was checked, and what was not

Run against a real server built from this tree:

- The `curl` request above, on a hash taken from the `ETag` header of `GET /sdk/v1/ruleset`, with a `read` token:
  `200` with the body shape shown. The same URL with no header is `401`; with an SDK key `403`; with the quoted
  `ETag` value `404`. `toggleslash rulesets show` answers the same.
- `packages/e2e/test/tracing.test.ts` (`bun run test:e2e`): a real evaluation by the real SDK produces a span event whose
  `toggleslash.ruleset.etag` equals `client.status().etag`, and `rulesets show` on it returns a flag whose
  `fallthrough` and `targets` hold the variation the SDK served.
- `packages/sdk-otel/test`: the attributes, the defaults and the options, with an in-memory exporter.

**Not run:**

- The Jaeger configuration. No Jaeger was started. The `type`, `key`, `url` and `text` fields are from Jaeger's
  documentation; that placeholders resolve against the same entry's attributes, are URL-encoded and skip the link
  when missing is from reading `jaeger-ui`'s `link-patterns.ts` on `main` (2026-11-06), not from running it. A
  Jaeger that does not show an OpenTelemetry span event's attributes as the log fields of that event would not
  produce the link. Newer Jaeger UIs call this type `events`; `logs` is still accepted there.
- Grafana. The Tempo correlation steps are from its documentation, with no regular expression written or tested.
- A proxy that adds a token to the server's lookup route: none is shipped and none was run.
- Any trace backend: the e2e test reads spans from an in-memory exporter.
