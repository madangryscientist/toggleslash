# @slashsuite/toggleslash-sdk

The ToggleSlash server-side SDK for TypeScript. It keeps a copy of one environment's ruleset current (an ETag poll and
the server's stream) and evaluates flags locally with `@slashsuite/toggleslash-engine`, so `variation` is synchronous,
never throws and never waits on the network. It needs a server key; code in a browser uses
`@slashsuite/toggleslash-sdk-browser`. Published on npm by `release.yml`.

```sh
npm install @slashsuite/toggleslash-sdk
```

```ts
import { createClient } from "@slashsuite/toggleslash-sdk";

const client = createClient({ url: "https://flags.example.com", sdkKey: process.env.TOGGLESLASH_SDK_KEY ?? "" });
await client.ready(); // optional: true once a ruleset has loaded, false if the server could not be reached

client.variation("new-checkout", { key: "user-42", attributes: { plan: "pro" } }, false);
client.variationDetail("new-checkout", { key: "user-42" }, false); // { value, variationIndex, reason }

await client.close(); // stops the loops and sends the last usage counts
```

`signContext(privateKeyPem, context)` signs a context for an environment in secure mode. It uses `node:crypto`, which
the main entry imports, so a Cloudflare Worker that imports this package should set the `nodejs_compat` compatibility flag (on a
compatibility date before 2026-09-01 it does not start without it). The options, the guarantees and Workers are in
[`docs/sdk.md`](https://github.com/slashsuite/toggleslash/blob/main/docs/sdk.md); what evaluation does is [`docs/evaluation.md`](https://github.com/slashsuite/toggleslash/blob/main/docs/evaluation.md).
