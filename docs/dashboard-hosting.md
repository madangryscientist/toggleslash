# Hosting the dashboard

The dashboard (`apps/web`) runs as a Cloudflare Worker at `flags.madangryscientist.com`, a hostname the
maintainer chose. No hosted service runs until the maintainer deploys it: this page is how, and the
deploy is the maintainer's step, never a contributor's or a CI job's.

## What is in front

Two hostnames, each behind its own [Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/policies/access/)
application:

| Hostname | What it serves | Who Access lets in |
| --- | --- | --- |
| `flags.madangryscientist.com` | this Worker, the dashboard | the maintainer's own account, signing in with a one-time PIN |
| `toggleslash-admin.madangryscientist.com` | the C# server's admin API | only a service token, the one this Worker holds |

The admin API hostname also has a reverse proxy behind Access that passes only `/api/v1/*` and `/health`,
and the server behind it still wants the admin token. So every call the dashboard makes carries three
things: `CF-Access-Client-Id` and `CF-Access-Client-Secret` (the service token, for Access) and
`Authorization: Bearer <admin token>` (for the server). Without the service token Access answers 403; with
it but without the bearer the server answers 401. The dashboard shows either as "The admin API refused the
dashboard", which names the variables to check.

The dashboard's own [operator sign-in](self-hosting.md#dashboard-sign-in) stays behind Access: a request
needs both the Access login and the operator session.

`wrangler.jsonc` serves the Worker only at that hostname: `workers_dev` and `preview_urls` are off,
because a `*.workers.dev` URL would reach the Worker without passing Access. The route is a Worker custom
domain, so the first deploy creates its DNS record.

The SDK routes are on a third hostname that this page does not change.

## The Worker's secrets

Six, all set on the Worker, none in the repository:

| Secret | What it is |
| --- | --- |
| `TOGGLESLASH_API_URL` | `https://toggleslash-admin.madangryscientist.com` |
| `TOGGLESLASH_ADMIN_TOKEN` | the admin token the server was started with |
| `TOGGLESLASH_OPERATOR_PASSWORD` | the password on the sign-in page (16 characters or more) |
| `TOGGLESLASH_SESSION_SECRET` | signs the session cookie (32 characters or more) |
| `TOGGLESLASH_ACCESS_CLIENT_ID` | the Access service token's client id |
| `TOGGLESLASH_ACCESS_CLIENT_SECRET` | the Access service token's client secret |

The two Access values go together: with only one of them set, the dashboard refuses to call the admin API
and shows that it is not configured, naming both. With neither set it calls without Access headers, which
is how local runs, `bun run stack` and the tests work.

## Shipping it

Wrangler reads the account from the environment, so there is no `wrangler login`: export
`CLOUDFLARE_API_TOKEN` (a token that can edit Workers and the zone's DNS) and `CLOUDFLARE_ACCOUNT_ID`
in the shell first.

Deploy first, with the `deploy` script, which builds and deploys. It is the one way the dashboard ships:

```bash
cd apps/web
bun run deploy
```

The first deploy creates the Worker from `wrangler.jsonc` (so `workers_dev` and `preview_urls` are off from
the start) and attaches `flags.madangryscientist.com`. Until its secrets are set it fails closed: nobody
can sign in.

Then set each secret from `apps/web`, giving the value on standard input, either typed at wrangler's
prompt or piped from wherever you keep it. Never put a value on the command line, and never print one.
Each `secret put` deploys a new version of the Worker with that secret:

```bash
cd apps/web
bunx wrangler secret put TOGGLESLASH_API_URL
bunx wrangler secret put TOGGLESLASH_ADMIN_TOKEN
bunx wrangler secret put TOGGLESLASH_OPERATOR_PASSWORD
bunx wrangler secret put TOGGLESLASH_SESSION_SECRET
bunx wrangler secret put TOGGLESLASH_ACCESS_CLIENT_ID
bunx wrangler secret put TOGGLESLASH_ACCESS_CLIENT_SECRET
```

Setting the secrets before the first deploy would make `secret put` create a placeholder Worker outside
this configuration, so deploy first. After the secrets are in, check that the hostname asks for the
Access login, and that after the operator sign-in the flag list loads. Later deploys keep the secrets.

To check a change without deploying anything, `bunx wrangler deploy --dry-run --outdir "$TMPDIR/web-dry"`
builds the bundle and stops; it needs no credential, and CI runs it.

Any other route or domain, and every real deploy, waits for the maintainer's yes.
