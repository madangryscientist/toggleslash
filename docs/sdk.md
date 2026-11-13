# Server-side SDK (TypeScript)

`packages/sdk` (`@hackcove/toggleslash-sdk`, moon project `sdk`) is the server-side SDK for TypeScript: it keeps
a copy of one environment's ruleset current and evaluates flags **locally** with `@hackcove/toggleslash-engine`,
to [`evaluation.md`](evaluation.md). It needs a **server** key (`ruleset` and `stream` refuse a client key). The
package is private and unpublished; publishing waits for the maintainer. Code that runs in a browser uses the
[browser SDK](sdk-browser.md) instead, where the server evaluates.

This page is the TypeScript binding of the language-neutral [SDK contract](sdk-contract.md), which says what
every server SDK must do; where the two differ in wording, the contract is the rule. In this SDK the contract's
canonical names are:

| Contract | TypeScript |
| --- | --- |
| `create` | `createClient(options)` |
| `ready` | `client.ready(): Promise<boolean>` |
| `variation`, `variationDetail` | `client.variation<T>(flagKey, context, fallback)`, `client.variationDetail<T>(...)`: one generic method each, over the fallback's type |
| `status` | `client.status()` |
| `close` | `client.close(): Promise<void>` |
| `url`, `sdkKey`, `stream` | `url`, `sdkKey`, `stream` |
| `pollInterval`, `flushInterval`, `requestTimeout` | `pollIntervalMs`, `flushIntervalMs`, `requestTimeoutMs` (milliseconds) |
| `hooks.afterEvaluation`, `onError` | `hooks.afterEvaluation`, `onError` |

```ts
import { createClient } from "@hackcove/toggleslash-sdk";

const client = createClient({ url: "https://flags.example.com", sdkKey: process.env.TOGGLESLASH_SDK_KEY ?? "" });
await client.ready(); // optional: true once a ruleset has loaded, false if the server could not be reached

client.variation("new-checkout", { key: user.id, attributes: { plan: "pro" } }, false);
client.variationDetail("new-checkout", { key: user.id }, false); // { value, variationIndex, reason }

await client.close(); // stops the loops and sends the last usage counts
```

A context is one of two shapes, as [`evaluation.md`](evaluation.md) defines them. A single context is
`{ kind?, key, attributes? }`, and no `kind` is `"user"`, so the calls above evaluate a user. A multi context
carries several kinds at once, each keyed by its kind; a clause, target or rollout with a `contextKind`
reads that kind:

```ts
client.variation(
  "new-checkout",
  { kind: "multi", contexts: { user: { key: user.id }, org: { key: org.id, attributes: { seats: 40 } } } },
  false,
);
```

## Guarantees

- **It never throws at a caller.** `createClient`, `ready`, `status`, `variation`, `variationDetail` and `close` do not
  throw or reject, whatever the server, the network, the context, an `onError` hook or an evaluation hook does. A failure comes back
  as the caller's fallback with an `ERROR` reason (`variationDetail`), or goes to `onError`.
- **It never blocks a request path.** `variation` is synchronous and reads memory. `ready()` is for a startup
  that wants to wait; it settles after the first attempt (bounded by `requestTimeoutMs`), not when the server
  comes back.
- **Server unreachable:** at start, every call returns the fallback (reason `ERROR` / `NOT_READY`) and the client
  keeps trying; later, it keeps serving the last ruleset it held. Something that is not a ruleset (a bad
  gateway page, `{}`) is an error and never replaces the one held.

## How it stays current

1. `GET /sdk/v1/ruleset` with `Authorization: Bearer <key>`; later requests send `If-None-Match`, so an unchanged
   ruleset is a `304`.
2. `GET /sdk/v1/stream` (Server-Sent Events). `connected` and `changed` carry the ETag; the client fetches only
   when it differs from the one held. A stream that goes 60 s without a byte (the server sends a keep-alive
   every 25 s) is dropped.
3. When the stream ends or will not open, the client waits (1 s, doubling, capped at `pollIntervalMs`, jittered),
   polls once, and tries the stream again. So a server without a stream is polled at about `pollIntervalMs`. A proxy that
   buffers the stream is noticed only when the 60 s watchdog drops it, so such a deployment should set `stream: false`. `stream: false` polls only.
4. A `429` on the ruleset or the stream with a `Retry-After` of whole seconds holds both until it has passed, capped at
   5 minutes, or for the wait above if that is longer. With the header missing or anything else in it, the wait above is all there is. Evaluation keeps answering
   from the last ruleset, and the `429` goes to `onError`.

## Status

`client.status()` says what this SDK has loaded and how its stream is doing. It is synchronous, reads memory,
makes no request and never throws:

```ts
{ etag: string | null; lastSyncAt: number | null; stream: StreamState; lastError: SyncErrorCode | null }
```

- `etag` is the ETag of the ruleset held; `null` before the first load.
- `lastSyncAt` is epoch milliseconds from the SDK's clock: when it last confirmed its ruleset matches the server.
  It moves on a `200`, a `304` or a stream event naming the tag held. **It does not move on a keep-alive, so an old
  `lastSyncAt` with `stream: "connected"` is normal: nothing has changed.**
- `stream` is one of: `off` (`stream: false`), `connecting` (opening it, or the first load), `connected` (the
  server's `connected` event arrived), `disconnected` (down, waiting to retry, polling meanwhile), `closed` (after
  `close()`).
- `lastError` is why the most recent ruleset or stream attempt failed, and `null` if it did not. It clears on the
  next successful fetch or a stream `connected`. Its codes are `http_<status>` (for example `http_401`), `timeout`,
  `bad_ruleset` (an answer that is not a ruleset) and `network`.
- Usage count failures are not in it; they still go to `onError`.
- It is local: the server does not know any of this. Nothing in it is a URL, a header, a key or an error message.

## Usage counts

`variation` and `variationDetail` count what they served, per flag, variation index and UTC day, and
`POST /sdk/v1/events` is sent every `flushIntervalMs` and on `close()`. **Counts only**: the body is
`{ flag, variationIndex, count, day }`, never a context or a key. Only an evaluation the caller got the flag's
value from is counted (not a fallback, a type mismatch or an unknown flag), and nothing is counted after
`close()`. Batches are at most 500 entries (the server takes 1000). A `5xx`, `408`, `429` or network failure keeps
the counts for the next flush; any other `4xx` drops them, since the server would refuse them again.
After a `429` with a valid `Retry-After` (capped at 5 minutes) no flush sends anything until it has passed. `close()` does not wait it
out and does not ignore it: counts still held back then are lost, and `onError` says so.

## Hooks

`hooks.afterEvaluation` is called synchronously after every `variation` and `variationDetail`, a fallback included,
with what the caller is about to get:

```ts
createClient({
  url, sdkKey,
  hooks: {
    afterEvaluation: ({ flagKey, context, value, variationIndex, reason, etag, flagVersion }) => {},
  },
});
```

- `value`, `variationIndex` and `reason` are what `variationDetail` returns (`variationIndex` is `null` for a
  fallback). `etag` is the ETag of the ruleset held at that evaluation, quoted as the server sent it: the same string
  as `status().etag` at that moment, or `null` before the first load. `flagVersion` is the flag's `version` in that
  ruleset, or `null` when the flag is not in it or is malformed.
- **A hook never reaches the caller.** An error it throws, or a promise it rejects, goes to `onError`; the caller gets
  the same answer, and the evaluation is counted, with or without a hook. A hook that is not a function is ignored.
- The event holds the `context` the caller passed, so what a hook does with it is the application's decision. The SDK
  sends none of it anywhere. It takes no OpenTelemetry dependency; [`tracing.md`](tracing.md) describes the package
  that puts the result and the ruleset hash on a span.

## Options

| Option | Default | |
| --- | --- | --- |
| `url`, `sdkKey` | required | the server's base URL and a server key |
| `stream` | `true` | `false` polls only |
| `pollIntervalMs` | 30000 | poll cadence, and the cap on the stream's reconnect backoff |
| `flushIntervalMs` | 30000 | usage flush cadence |
| `requestTimeoutMs` | 10000 | per request, body included, so no single request outlasts it (`close()` waits for at most the in-flight flush and the final one) |
| `hooks` | none | `{ afterEvaluation }`, see [Hooks](#hooks) |
| `onError` | ignore | called with each failure; it may throw, and it is ignored if it does |
| `fetch`, `clock`, `random` | the platform's | injected by the tests |

## OpenFeature

`packages/sdk-openfeature` is a server-side OpenFeature provider over this client: `new ToggleSlashProvider(client)`.
It evaluates nothing itself; see its [README](../packages/sdk-openfeature/README.md) for the context, reason and error mapping.

## Tests

`packages/sdk/test` runs against a fake server (`fake-server.ts`) and a fake clock (`fake-clock.ts`), so there is
no network and no real timer: first load, `304`, a stream change, a reconnect, the server down at start and later,
the flush on close, and a check after every test that no promise rejected unhandled.

`packages/sdk/test-service/main.ts` is the SDK's conformance test service: it wraps `createClient` in the
protocol of [`sdk-test-service.md`](sdk-test-service.md), and `bun run conformance -- --service "bun
packages/sdk/test-service/main.ts"` runs every scenario in `fixtures/sdk` and every evaluation vector
through it, against a real HTTP server on real time. `bunx moon run sdk-conformance:test` does the same in the gate.
