# ToggleSlash example: a shop checkout

A fictional shop whose checkout is steered by ToggleSlash flags, written the way a customer would
write it: the [TypeScript server SDK](../../docs/sdk.md) in a small `Bun.serve` app. Private, and
not published.

- `GET /?user=<key>` is one HTML page: a shopper picker (`free-user`, `pro-user`, `beta-user`), each
  flag's value and reason for that shopper, and the SDK's status (ruleset ETag, last sync, stream).
  Its script reads `/state` once a second, so a flag changed anywhere shows up within a second.
- `GET /browser.js` is the page's browser bundle (see below).
- `GET /state?user=<key>` is the same data as JSON. It reads the SDK's memory and makes no request.
- Either route takes `&flag=<key>` to read one more flag than the five the shop uses (a key of up to
  64 letters, digits, `-` or `_`; anything else is a 400). The page keeps it across the picker and its
  refresh. The journey test uses it to watch a flag it creates in the dashboard.
- The **In the browser (client key)** section runs the [browser SDK](../../docs/sdk-browser.md) on the page. At
  startup `src/server.ts` bundles `src/browser.ts` with `Bun.build` (in memory, nothing written, no extra
  dependency) and serves it at `GET /browser.js`. The module reads the page's config, creates a browser client with
  the **client key** and the shopper's context, and repaints a table of `new-checkout` and the `?flag=` flag on
  `ready` and on every `change`. A flag changed anywhere reaches it through `GET /sdk/v1/changes` and a fresh
  evaluate, with no reload (the SDK's `change` event fires when a value or variation index changes, so a reason that changes
  alone shows after the next such change or a reload). The server key stays in this process: it is never in the page, the bundle or
  `/state`, and `test/` holds that.
- SIGTERM and SIGINT close the client, which sends the last usage counts.

The page is plain semantic HTML with no stylesheet and no component library, so AGENTS.md invariant 3
("UI is composed from `@hackcove/ui-react`") has nothing to compose here. Every value has a stable
`data-testid` for a browser test (`flag-<flag>-value` for the server SDK's table, `browser-flag-<flag>-value` for the browser SDK's).

## Configuration

| Variable | |
| --- | --- |
| `TOGGLESLASH_URL` | the server, for example `http://127.0.0.1:5080` |
| `TOGGLESLASH_SDK_KEY` | a server key (`tgl_server_...`) |
| `TOGGLESLASH_CLIENT_KEY` | a client key (`tgl_client_...`), sent to the browser |
| `PORT` | default `4441`, bound to `127.0.0.1` |
| `TOGGLESLASH_FLUSH_INTERVAL_MS` | how often usage counts are sent (optional) |

## Run it with the whole product

```bash
bun run stack
```

This needs the .NET SDK (`mise install`; if another `dotnet` (say `/usr/bin/dotnet`) comes first on your PATH
and cannot run `global.json`'s SDK, put the mise one first:
`PATH="$(dirname "$(mise which dotnet)"):$PATH" bun run stack`). It builds and starts the real server on a port the OS picks, seeds
a project `shop` (environments `dev` and `prod`, a `pro-customers` segment and five flags), builds
the dashboard, and starts this app. Then it prints:

- the dashboard on <http://127.0.0.1:4440> with its sign-in password,
- this app on <http://127.0.0.1:4441>,
- the server's URL and the admin token, with the `TOGGLESLASH_URL` and `TOGGLESLASH_TOKEN` exports
  for the CLI.

Every secret is made fresh for the run, written nowhere, and thrown away when you press Ctrl-C.
Everything listens on loopback only.

## What to try

1. Open the app as `free-user`, then `pro-user`: `new-checkout` is on only for the `pro-customers` segment.
2. Open it as `beta-user`: `banner-text` differs (a target).
3. Flip `legacy-cart` with the CLI and watch the app follow within a second, and the ETag change:
   `bun run toggleslash flags on legacy-cart --project shop --env dev`
4. Do the same from the dashboard (sign in, pick `shop` and `dev`, toggle the flag).
5. Flip a flag and watch the **In the browser** table follow with no reload: the browser SDK is told by the change signal and evaluates again.
6. After a few seconds, open `new-checkout` in the dashboard: its usage shows the counts this app sent.
