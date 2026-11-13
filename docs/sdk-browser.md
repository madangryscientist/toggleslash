# Browser SDK (TypeScript)

`packages/sdk-browser` (`@hackcove/toggleslash-sdk-browser`, moon project `sdk-browser`) is the SDK for code that runs
in a browser. **The server evaluates.** The browser never receives a ruleset and never evaluates: it sends its context to
`POST /sdk/v1/evaluate`, keeps the whole answer in memory, serves it synchronously, and asks again when
`GET /sdk/v1/changes` says something changed (see [`api.md`](api.md), and section 13 of
[`launchdarkly-gap-analysis.md`](launchdarkly-gap-analysis.md) for why). It needs a **client** key. The package is private and
unpublished; publishing waits for the maintainer. The server-side SDK is in [`sdk.md`](sdk.md).

```ts
import { createBrowserClient } from "@hackcove/toggleslash-sdk-browser";

const client = createBrowserClient({
  url: "https://flags.example.com",
  clientKey: "tgl_client_...",
  context: { key: user.id, attributes: { plan: "pro" } },
});
await client.ready(); // optional: settles after the first answer or failure

client.variation("new-checkout", false); // synchronous, from the cache
client.detail("new-checkout", false); // { value, variationIndex, reason }
const stop = client.on("change", (flags) => render(flags)); // the sorted keys that changed
await client.identify({ key: otherUser.id }); // a new context
await client.close();
```

## Options

| Option | Default | Meaning |
| --- | --- | --- |
| `url` | required | the server, as the server SDK's `url` |
| `clientKey` | required | a client SDK key; it goes in an `Authorization` header and never in a URL |
| `context` | required | a single or a multi context, the two shapes in [`api.md`](api.md) and [`evaluation.md`](evaluation.md#context) |
| `fetch`, `clock` | the global `fetch`, the system clock | injectable, for tests |
| `bootstrap` | none | values served before the first answer |
| `pollIntervalMs` | `60000` | how often to evaluate again, only while the change signal is down |
| `requestTimeoutMs` | `10000` | the longest any one request may take |
| `sendUsage` | `true` | send usage counts to `POST /sdk/v1/events` |
| `onError` | nothing | told about every failure the client absorbs |

## API

- `ready()` settles after the first evaluate answer or failure. It never rejects.
- `variation(flag, fallback)` and `detail(flag, fallback)` read the cache and never touch the network. A flag the
  cache lacks gives the fallback with `{ kind: "ERROR", code }`: `NOT_READY` before any answer, `FLAG_NOT_FOUND` after one.
  A value of another JSON type than the fallback's (a `null` fallback accepts any) gives the fallback with `TYPE_MISMATCH`.
  `reason` is the discriminated union the server SDK uses, not the server's flat wire object.
- `allFlags()` is every cached value by key.
- `identify(context)` evaluates for a new context. Until its answer arrives the previous context's values keep serving, so
  nothing flickers to the fallback; the answer then replaces the cache whole.
- `on("change", listener)` returns the unsubscribe function. After each answer the client compares it with the cache and
  calls the listener with the sorted keys whose value or variation index changed, appeared or disappeared, and with
  nothing when nothing changed. The first answer reports every key as appeared. A listener that throws goes to `onError`.
- `close()` stops the signal and the timers and sends the pending usage counts.
- Usage is counted twice by design: `POST /sdk/v1/evaluate` counts the flags it evaluated on the server (docs/architecture.md), and the
  client adds a count for each `variation` or `detail` read that hit the cache with a variation index. Set `sendUsage: false` to
  keep only the server's count.

No method throws or rejects. A network or parse failure goes to `onError` and the cache keeps its last good values.

## How it stays current

1. On start, and on every `connected` and `changed` event, it calls `evaluate` with the current context. Signals that
   arrive during an evaluation cause one more evaluation, not one each. Each evaluation carries a sequence number, and an
   answer older than the newest applied, or for an earlier context, is dropped, so `identify` racing a `changed` ends on the
   newest context's values.
2. The signal is `fetch` on `GET /sdk/v1/changes` with the key in the `Authorization` header, read as a stream. It is not
   `EventSource`, which cannot send a header, and a key must not go in a URL. A stream silent for 60 s is dropped.
3. When the signal ends or fails it reconnects after 1 s, doubling to 30 s, and resets on `connected`. A `429` waits its
   `Retry-After`. While it is down, the client evaluates every `pollIntervalMs`. A failed evaluate is not retried by
   itself: the next signal or poll does it.
4. A `401` or `403` on any route means the key will not work. The signal, the polling and usage stop for good, and
   `onError` is told once.

## Usage and storage

Each `variation` or `detail` answered from the cache with a variation index records a count, sent to
`POST /sdk/v1/events` every 30 s and on `close()`. A fallback, a bootstrap value and a type mismatch record nothing.
The counts hold a flag, a variation index and a day, never the context. The browser has no page-hide hook yet, so a page
that is closed without `close()` loses up to 30 s of counts.

The client uses no `localStorage`, cookie or IndexedDB. The context is sent only in the evaluate body. It uses only
`fetch`, `TextDecoder`, `AbortController` and the clock, so it runs in a browser and in Bun.

## Shared code

The signal parser, the usage reporter, the clock and the HTTP helpers are the server SDK's own modules, exported as
`@hackcove/toggleslash-sdk/shared`. That entry imports neither the engine, `client.ts` nor `sync.ts`, and
`packages/sdk-browser/test/shared-imports.test.ts` fails if it ever does, so a browser bundle never carries an evaluator.

## Not yet

- **No secure mode.** A client key is public by design, so anyone who has it can evaluate as any context.
- **No per-flag client availability.** Every flag that is not archived is visible to a client key.
- No React binding, no persistence between page loads, no flush on page hide, no published package.
