# Quickstart

Five minutes, on your own machine: start the server, create a flag with the CLI, and read it from the
TypeScript server SDK. Nothing here is hosted or published.

You need [Bun](https://bun.sh) 1.4.2 and the .NET SDK 10.0.402 (`.tool-versions` and `global.json`). If
you would rather not install .NET, run the server in Docker instead; see [`self-hosting.md`](self-hosting.md).

## 1. Install

From a checkout of this repository:

```bash
bun install
bun run featureslash --version
```

```
featureslash 0.0.0 (featureslash-engine 0.0.0)
```

`bun run` prints the command it ran before the output (`$ bun packages/cli/src/index.ts ...`), and the CLI
prints its notices (such as the reminder to store a new key) on stderr. Both go to stderr, so the samples
below show the command's output on stdout, with its notices where they appear.

## 2. Start the server

Pick an admin token and keep it in your shell. The server reads it from `FEATURESLASH_ADMIN_TOKEN`; the
CLI and the dashboard need the same value. Put the server's database in a file the repo ignores (`*.db`).

```bash
export FEATURESLASH_ADMIN_TOKEN="$(openssl rand -hex 32)"
export FEATURESLASH_DB_PATH="$PWD/quickstart.db"
echo "$FEATURESLASH_ADMIN_TOKEN"
ASPNETCORE_URLS=http://127.0.0.1:8080 dotnet run --project apps/server/FeatureSlash.Server
```

The `echo` prints the token, which step 3 needs in a second terminal. Copy it before the server takes over
this one.

Leave it running. In a second terminal, in the same checkout, check it answers:

```bash
curl http://127.0.0.1:8080/health
```

```json
{"status":"ok","version":"0.0.0"}
```

The first `dotnet run` builds the server and restores NuGet packages, so it takes a while. The database
is created at startup and migrated to the current schema.

## 3. Point the CLI at the server

In the second terminal, export the token that step 2 printed, and save the server's URL and the token in the
CLI's config file (mode 600):

```bash
export FEATURESLASH_ADMIN_TOKEN="<the token printed by echo in step 2>"
bun run featureslash config set-url http://127.0.0.1:8080
printf '%s' "$FEATURESLASH_ADMIN_TOKEN" | bun run featureslash config set-token
```

```
saved the server URL (http://127.0.0.1:8080) in …/featureslash/config.json
saved the admin token in …/featureslash/config.json (mode 600)
```

The file is `~/.config/featureslash/config.json`, or under `$XDG_CONFIG_HOME` when that is set, and the
CLI prints its full path.

`config set-token` reads the token from stdin, so it is not on the command line. You can use the `FEATURESLASH_URL` and
`FEATURESLASH_TOKEN` environment variables instead of the config file.

## 4. Create a project, an environment and a flag

```bash
bun run featureslash projects create shop --name Shop
bun run featureslash envs create production --project shop
bun run featureslash flags create new-checkout --project shop
bun run featureslash flags list --project shop
```

```
created project shop
created environment production in shop
created flag new-checkout in shop
KEY           NAME          VARIATIONS    TAGS  STATE   LAST EVALUATED
new-checkout  new-checkout  [true,false]  -     active  -
```

A new flag is a boolean (`[true, false]`) and is off in every environment. `flags show new-checkout
--project shop --env production` prints its targeting there.

## 5. Create an SDK key

An SDK reads one environment. The server key is the one that reads rules; the server shows it once:

```bash
bun run featureslash keys create --project shop --env production --kind server --name quickstart
```

```
id:    1
kind:  server
name:  quickstart
key:   fs_server_…
featureslash: store the key now; the server keeps only a hash and will not show it again.
```

Copy the `key:` value into your shell (the `…` above stands for the rest of it):

```bash
export FEATURESLASH_SDK_KEY="fs_server_…"
```

Keep it out of the repo: a server key reads the whole ruleset of its environment.

## 6. Read the flag from the SDK

Save this as `read-flag.ts` in the repo root. It imports the SDK from the workspace, so it runs from
there and nowhere else. Delete it when you are done.

```ts
import { createClient } from "./packages/sdk/src/index.ts";

const client = createClient({ url: "http://127.0.0.1:8080", sdkKey: process.env.FEATURESLASH_SDK_KEY ?? "" });
console.log("ready:", await client.ready());
console.log("variation:", client.variation("new-checkout", { key: "user-42", attributes: { plan: "pro" } }, false));
console.log("detail:", JSON.stringify(client.variationDetail("new-checkout", { key: "user-42" }, false)));
await client.close();
```

Run it:

```bash
bun read-flag.ts
```

```
ready: true
variation: false
detail: {"value":false,"variationIndex":1,"reason":{"kind":"OFF"}}
```

The flag is off, so the SDK serves `false`. `reason` says why (`OFF`, `FALLTHROUGH`, and so on); the
fallback you pass (`false` here) is used only when the SDK cannot evaluate the flag at all.

## 7. Turn the flag on

```bash
bun run featureslash flags on new-checkout --project shop --env production
```

```
flag new-checkout is on in production (version 2)
```

Run `bun read-flag.ts` again:

```
ready: true
variation: true
detail: {"value":true,"variationIndex":0,"reason":{"kind":"FALLTHROUGH"}}
```

A long-running SDK client does not need a restart: it keeps a stream open to the server and applies
the change when it arrives. `flags off` turns it back off.

## Clean up

Stop the server with Ctrl-C and delete `read-flag.ts`. To start over from nothing, delete `quickstart.db`
(and `quickstart.db-wal` and `quickstart.db-shm`, if they exist). Remove `~/.config/featureslash/config.json`
too if you do not want the URL and token kept there.

