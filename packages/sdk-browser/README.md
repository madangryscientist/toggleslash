# @hackcove/toggleslash-sdk-browser

The ToggleSlash browser SDK. The server evaluates: the client sends its context to `POST /sdk/v1/evaluate`, caches the
answer, serves it synchronously, and re-evaluates when `GET /sdk/v1/changes` signals a change. It holds no ruleset and
imports no engine. Private and unpublished; the full description is in [`docs/sdk-browser.md`](../../docs/sdk-browser.md).

```ts
import { createBrowserClient } from "@hackcove/toggleslash-sdk-browser";

const client = createBrowserClient({ url: "https://flags.example.com", clientKey: "fs_client_...", context: { key: "user-42" } });
await client.ready();
client.variation("new-checkout", false);
client.on("change", (flags) => console.log("changed:", flags));
await client.close();
```

Tests (`bun run test` from the repository root, or `bunx moon run sdk-browser:test`) use a fake `fetch` and a fake clock; no network.
