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
| `signature` | none | the context's signature, for an environment with [secure mode](#secure-mode) on; sent with every evaluate for this context |
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
- `identify(context, signature?)` evaluates for a new context, sending `signature` with it in secure mode (the old
  context's signature is dropped). Until its answer arrives the previous context's values keep serving, so nothing
  flickers to the fallback; the answer then replaces the cache whole.
- `on("change", listener)` returns the unsubscribe function. After each answer the client compares it with the cache and
  calls the listener with the sorted keys whose value, variation index or reason changed, appeared or disappeared, and with
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
   `onError` is told once. The exception is a secure-mode `403` from evaluate (`signature_required` or `bad_signature`):
   it is about this context's signature, not the key, so it is an error like any other. The cache (or the fallback)
   keeps serving, `onError` is told, and the client goes on, so an `identify` with the right signature recovers.

## Usage and storage

Each `variation` or `detail` answered from the cache with a variation index records a count, sent to
`POST /sdk/v1/events` every 30 s and on `close()`. A fallback, a bootstrap value and a type mismatch record nothing.
The counts hold a flag, a variation index and a day, never the context. The browser has no page-hide hook yet, so a page
that is closed without `close()` loses up to 30 s of counts.

The client uses no `localStorage`, cookie or IndexedDB. The context is sent only in the evaluate body. It uses only
`fetch`, `TextDecoder`, `AbortController` and the clock, so it runs in a browser and in Bun.

## Which flags a client key sees

A client key ships in the page, so anyone can read it. The server answers it only the flags marked **visible to client
keys** (`clientVisible` on the flag, `--client-visible` on `toggleslash flags create`, "Visible to client keys" in the
dashboard). A new flag is hidden until it is marked. A hidden flag is left out of the evaluate answer, so
`variation` serves the fallback and `detail` answers `ERROR` with `FLAG_NOT_FOUND`, the same as for a key that does
not exist; asking for it by `flagKey` is the same `404` as a missing key, and a usage count for it is one of
`unknownFlags`, counted nowhere. A server key sees every flag.

Two things still reach a page, by design:

- **A hidden prerequisite's key.** Hidden flags stay in the ruleset the server evaluates with, so a visible flag
  with a hidden prerequisite evaluates as it should. When that prerequisite fails, the visible flag's reason is
  `PREREQUISITE_FAILED` with `flagKey` naming it.
- **That something changed.** The change signal fires on any change to the environment's ruleset, hidden flags
  included, and on a flag being marked visible or hidden. A page can tell that something changed, not what. Firing
  only for visible flags would miss a change that reaches one through a hidden prerequisite or a segment.

## Secure mode

A client key is public, so without more anyone who has it could evaluate as any context key and read what another
user would see. **Secure mode**, set per environment, closes that: with it on, a client-key evaluate must carry a
signature over the context's keys, made by the application's server, which the browser cannot forge.

1. Make the environment's signing key: `toggleslash envs signing-key <env> --project <p>` (or
   `POST /api/v1/projects/{project}/environments/{env}/signing-key`). It is an ECDSA P-256 key pair. The server answers the
   private key **once**, as a PKCS#8 PEM, and stores only the public half (SubjectPublicKeyInfo DER), so nothing the
   server holds can sign. Keep the private key on the application's server, in its secrets, and never in a page.
2. Turn it on: `toggleslash envs secure-mode <env> on --project <p>` (or `PATCH .../environments/{env}` with
   `{ "secureMode": true }`). Turning it on with no signing key is a `409`.
3. On the application's server, sign each user's context with `signContext` from the server SDK
   ([`sdk.md`](sdk.md#secure-mode-signcontext)) and hand the signature to the page with the context.
4. In the page, pass it: `createBrowserClient({ ..., context, signature })`, and `identify(context, signature)` for a new
   context.

**What is signed.** The context normalised as [`evaluation.md`](evaluation.md) does it (a map from kind to its key and
attributes; an absent kind is `user`), **keys only**. Take the entries in ordinal (byte) order of the kind, and write
each as two netstrings, its kind and then its key, where a netstring is `<byte length of the UTF-8>:<UTF-8 bytes>,`
(the length in decimal ASCII). Attributes are not signed: the browser sets them, as LaunchDarkly's secure mode allows,
so a rule on an attribute is not protected by secure mode. There is no expiry and no nonce: a signature holds for its
context until the key is rotated.

The signature is ECDSA P-256 with SHA-256 over those bytes, as the 64-byte IEEE P1363 `r || s` (not DER), encoded
base64url with no padding (86 characters). In Node: `crypto.sign("sha256", message, { key, dsaEncoding: "ieee-p1363" })`.

Test vectors for the message:

| Context | Signed bytes |
| --- | --- |
| `{ "key": "user-1" }` | `4:user,6:user-1,` |
| `{ "kind": "multi", "contexts": { "user": { "key": "u" }, "org": { "key": "acme" } } }` | `3:org,4:acme,4:user,1:u,` |

A key is counted in bytes: `{ "key": "é" }` signs `4:user,2:é,`.

**What the server answers.** In a secure-mode environment a client-key `POST /sdk/v1/evaluate` checks the context first
(a bad one is still a `400`), then the `signature` in the body: none is a `403` with error code `signature_required`, and
one that does not verify against the environment's public key (another context's, the wrong key's, or not a 64-byte
base64url value) is a `403` with `bad_signature`. Neither body says anything about the key. A server key, the admin
diagnostic route and every other route ignore secure mode; `events` and `changes` carry no context, so they need no
signature. With secure mode off, a signature sent is ignored.

**Rotation.** Running `envs signing-key` again makes a new pair and replaces the old one. Signatures made with the old
key fail from that moment, so in alpha rotation is a cut-over: deploy the new private key to the application's server
as you rotate, and pages re-identify with fresh signatures. Turning secure mode off keeps the key.

## Shared code

The signal parser, the usage reporter, the clock and the HTTP helpers are the server SDK's own modules, exported as
`@hackcove/toggleslash-sdk/shared`. That entry imports neither the engine, `client.ts` nor `sync.ts`, and
`packages/sdk-browser/test/shared-imports.test.ts` fails if it ever does, so a browser bundle never carries an evaluator.

## Not yet

- No React binding, no persistence between page loads, no flush on page hide, no published package.
