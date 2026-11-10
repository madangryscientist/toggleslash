# @hackcove/toggleslash-sdk-openfeature

A server-side [OpenFeature](https://openfeature.dev) provider over the ToggleSlash TypeScript SDK. It is a thin adapter:
it evaluates nothing itself, makes no network call and caches nothing. Every answer is `client.variationDetail(...)` from
`@hackcove/toggleslash-sdk`, mapped to OpenFeature's `ResolutionDetails`. Private and unpublished.

```ts
import { createClient } from "@hackcove/toggleslash-sdk";
import { ToggleSlashProvider } from "@hackcove/toggleslash-sdk-openfeature";
import { OpenFeature } from "@openfeature/server-sdk";

const client = createClient({ url: "https://flags.example.com", sdkKey: process.env.TOGGLESLASH_SDK_KEY! });
await OpenFeature.setProviderAndWait(new ToggleSlashProvider(client));

const enabled = await OpenFeature.getClient().getBooleanValue("new-checkout", false, { targetingKey: "user-42" });
```

You make the client, with its own options and hooks (so `@hackcove/toggleslash-sdk-otel` tracing keeps working
underneath). The provider calls `ready()` when OpenFeature initialises it and `close()` when OpenFeature closes it.
`initialize` never throws: if the first load failed, the SDK keeps retrying and evaluations answer the default with
`PROVIDER_NOT_READY` until it succeeds.

## Context

`targetingKey` becomes the SDK context's `key`; every other entry becomes an attribute of the same name. A `Date` becomes
its ISO 8601 string, because the `before` and `after` operators compare instants given as ISO strings
([`docs/evaluation.md`](../../docs/evaluation.md)). A missing or empty `targetingKey` answers the default with reason
`ERROR` and `TARGETING_KEY_MISSING`, without calling the client.

`variant` is the served variation's index as a string (`"0"`), and is left out when the default was served.

## Reasons

| SDK reason | OpenFeature reason |
| --- | --- |
| `OFF` | `DISABLED` |
| `TARGET_MATCH`, `RULE_MATCH` | `TARGETING_MATCH` |
| `FALLTHROUGH` | `DEFAULT` |
| `PREREQUISITE_FAILED` | `PREREQUISITE_FAILED` (the provider's own reason) |
| `ERROR` | `ERROR` |

## Errors

On an `ERROR` reason the `errorMessage` is the SDK's own code.

| SDK code | OpenFeature `errorCode` |
| --- | --- |
| `NOT_READY` | `PROVIDER_NOT_READY` |
| `FLAG_NOT_FOUND` | `FLAG_NOT_FOUND` |
| `TYPE_MISMATCH` | `TYPE_MISMATCH` |
| `INVALID_CONTEXT` | `INVALID_CONTEXT` |
| `MALFORMED_FLAG`, `MALFORMED_SEGMENT` | `PARSE_ERROR` |
| `SEGMENT_NOT_FOUND`, `PREREQUISITE_CYCLE`, `PREREQUISITE_DEPTH_EXCEEDED`, `EVALUATION_FAILED` | `GENERAL` |

## What it does not do

- No provider events: a ruleset change does not emit `ConfigurationChanged`.
- Server side only. There is no web provider and no provider for another language.
- No `flagMetadata`, no `track`, no hooks.
- Private and unpublished.
