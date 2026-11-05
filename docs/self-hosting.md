# Self-hosting

The C# server (`apps/server`) ships as a container image you build yourself. Nothing is published:
there is no image on any registry, and `featureslash.dev` is not routed to anything. The dashboard
(`apps/web`) is not containerised; it runs on Cloudflare Workers and its hosting is undecided
(see [`architecture.md`](architecture.md#hosting)). This page is about the server, plus
[what the dashboard needs before anyone can sign in](#dashboard-sign-in).

What you get: one container running one process, a SQLite file in a Docker volume, and the admin
API and SDK routes on port 8080. The image runs as a non-root user (the base image's `app`, UID
1654), has a `HEALTHCHECK` on `/health`, and holds no secret: the admin token is read from the
environment when the container starts.

## Run

You need Docker with the Compose plugin and this repository.

```bash
export FEATURESLASH_ADMIN_TOKEN="$(openssl rand -hex 32)"   # keep it; the dashboard and CLI need the same value
docker compose up -d --build --wait
curl http://127.0.0.1:8080/health                            # {"status":"ok","version":"0.0.0"}
```

`compose.yaml` refuses to start without `FEATURESLASH_ADMIN_TOKEN`. Instead of exporting it you can
put `FEATURESLASH_ADMIN_TOKEN=...` in a `.env` file next to `compose.yaml`; `.env` is git-ignored.
The server never generates, stores or logs the token. Without it the whole admin API answers 503
`admin_token_unset`, and that includes scoped tokens that are otherwise live.

Terraform and CI should not hold that token. Give each one a scoped token instead: one project, `read` or `write`,
expiring in 1 to 365 days (`featureslash tokens create`, which takes the root token; see
[the architecture](architecture.md#auth) for what each may reach). A `write` token can create and revoke its project's SDK
keys, and a `read` token can list them, so treat both as you would the admin token, within that project. The server stores
only a hash and a prefix; the plain token is shown once. There is no rotation: create a new one, then revoke the old.

Compose publishes the port on `127.0.0.1` only, because the server speaks plain HTTP and the admin
token and SDK keys travel in headers. To expose it, put a TLS-terminating reverse proxy in front and
leave the published port on loopback.

Point the CLI at it:

```bash
bun run featureslash config set-url http://127.0.0.1:8080
bun run featureslash config set-token < token-file    # or FEATURESLASH_URL / FEATURESLASH_TOKEN
```

The image's build context is the repository root (`docker build -f apps/server/Dockerfile .`), and `.dockerignore` keeps `node_modules`, `bin`, `obj`,
every `*.db` file and the other apps out of it.

| Setting | Where | Meaning |
| --- | --- | --- |
| `FEATURESLASH_ADMIN_TOKEN` | environment, required by `compose.yaml` | the root admin token for `/api/v1`; scoped tokens are created through the API and live in the database |
| `FEATURESLASH_DB_PATH` | environment, `/data/featureslash.db` in the image | the SQLite file; keep it on the `/data` volume |

To set anything else, such as the rate limits below, add a line under `environment:` in `compose.yaml`.

Stop it with `docker compose down`, which keeps the data. `docker compose down -v` also deletes the
volume, and with it every flag: do that only when you mean it.

## Backup

Everything the server knows is in one SQLite file, `/data/featureslash.db`, in the `featureslash-data`
volume. The database runs in WAL mode, so a copy taken while the server is writing can miss recent
changes. Stop the server, copy the volume's contents, start it again:

```bash
backup="featureslash-backup-$(date +%Y%m%d-%H%M%S)"
docker compose stop server
docker compose cp server:/data/. "./$backup"
docker compose start server
```

`$backup` holds `featureslash.db` (and, only after an unclean stop, its `-wal` and `-shm` files, which belong
with it). It contains SDK key hashes, so treat it as sensitive. Each backup gets its own directory, so
a later one never mixes files with an earlier one.

**Restore** into the stopped server's volume. `docker compose cp` would write the files as root, and
the server runs as UID 1654, so copy and `chown` in one throwaway container instead:

```bash
docker compose stop server
docker compose run --rm --no-deps --user 0 --entrypoint bash \
  -v "$PWD/$backup:/backup:ro" server \
  -c 'rm -f /data/featureslash.db* && cp -a /backup/. /data/ && chown -R 1654 /data'
docker compose start server
```

## Upgrade

Migrations apply when the server starts. All pending migrations run in a single transaction, so a
failed upgrade leaves the file as it was and the server does not start; read the container's logs
(`docker compose logs server`). Migrations are additive, and there is no downgrade: an older image
is not promised to run against a newer database.

```bash
# back up first (see Backup), then:
git pull
docker compose up -d --build --wait
curl http://127.0.0.1:8080/health
```

If an upgrade has to be undone, restore the backup (above) and start the previous version of the code.

## Dashboard sign-in

The dashboard has one operator and no user table. It keeps nothing about a session: sign-in needs two
Worker secrets, set by hand on the Worker.

```bash
cd apps/web
password="$(openssl rand -base64 24)"
echo "$password"                                             # keep it; it is what you type at sign-in
printf '%s' "$password" | bunx wrangler secret put FEATURESLASH_OPERATOR_PASSWORD
openssl rand -hex 32 | bunx wrangler secret put FEATURESLASH_SESSION_SECRET
```

| Secret | Minimum length | Meaning |
| --- | --- | --- |
| `FEATURESLASH_OPERATOR_PASSWORD` | 16 characters | the password on the sign-in page |
| `FEATURESLASH_SESSION_SECRET` | 32 characters | signs the session cookie (HMAC-SHA256) |

**It fails closed.** With either one unset or too short, nobody can sign in: the sign-in page names the
variable (never its value), every other page redirects to it, and every server function answers 401.

**A session lasts 12 hours**, then you sign in again. The cookie holds an expiry and a signature, is
`HttpOnly` and `SameSite=Lax`, and is `Secure` everywhere except a loopback host.

- **Signing out** clears the cookie in that browser. A copy of the cookie taken earlier stays valid until
  it expires, because the server keeps no list of sessions.
- **Rotating `FEATURESLASH_SESSION_SECRET` ends every session at once.** That is the way to cut off a
  cookie you think has leaked. Changing the password alone does not end a session.

**Nothing counts failed attempts**, so sign-in itself does not slow down someone guessing. Two things
stand against that: the 16-character minimum (a long random password, as above, is out of reach of
guessing), and a rate limit you add yourself. When you route the dashboard publicly, add a
[Cloudflare rate limiting rule](https://developers.cloudflare.com/waf/rate-limiting-rules/) on `POST`
requests to the path `/sign-in`. It is an operator step: this repository creates no Cloudflare resource.

## Limits

**Run exactly one server process on a database file.** The server is built for it, and the current
limits all follow from that:

- Do not scale the service (`docker compose up --scale server=2`) and do not start a second
  container on the same volume. SQLite's write lock keeps the file consistent, but the
  Server-Sent Events stream (`GET /sdk/v1/stream`) is held in memory by the process that took the
  write, so SDKs connected to another process would not hear of a change until they poll.
- There is no high availability. A restart is a short outage, during which SDKs keep serving from
  the last ruleset they fetched.
- The volume is local to one Docker host. Back it up as above.
- Postgres is a later option, not part of this version.

## Headers, body size and timeouts

The server and the dashboard set these themselves; there is nothing to configure.

**Response headers.** Every answer from the server, errors and the event stream included, carries
`X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and `X-Frame-Options: DENY`. Admin API answers
(`/api/...`) also carry `Cache-Control: no-store`. The ruleset keeps its `ETag` and its `304`. The dashboard sends the
same three on every answer, redirects and the sign-in page included, `Cache-Control: no-store` on everything but its
hashed static files, and a `Content-Security-Policy`: `default-src 'self'`, scripts and styles from the page itself
plus a nonce made fresh for each response, and `frame-ancestors 'none'`, `base-uri 'self'`, `form-action 'self'`,
`object-src 'none'`. No inline script or style runs without that nonce.

**HSTS is yours to set.** Neither the server nor the dashboard sends `Strict-Transport-Security`, because neither
terminates TLS. Set it at the proxy or on the Cloudflare zone, once you are sure the host will serve HTTPS for good.

**Body size.** A request body over 1 MiB is refused with a `413` and the usual error body, before the route sees it.
`POST /sdk/v1/events` is held to 128 KiB. A chunked body is counted as it arrives, so it cannot get past the cap.

**Timeouts.**

| What | Limit | When it is hit |
| --- | --- | --- |
| a request, from routing to the end of its handler | 30 seconds | `504` with code `request_timeout` |
| reading the request headers | 30 seconds | `408`, and the connection is closed |
| an idle keep-alive connection | 2 minutes | the connection is closed |

`GET /sdk/v1/stream` has **no** request timeout: it is open for hours and says nothing between changes except a
keep-alive comment every 25 seconds. The data-rate floor for a request body is ASP.NET's default (240 bytes a second
after a five second grace).

A request timeout is cooperative. It cancels the request's token, and a handler that does not look at it runs to
completion and answers as it would have: no `504` is sent. The SDK routes pass the token to the database; most admin
routes do not yet, so a slow admin query is not cut off at 30 seconds.

## Rate limits

The limits are always on, and there is no setting that turns them off. A variable takes a positive integer; anything
else, an empty value included, stops the server at startup with an error naming the variable.

| Limit | Default | Variable | Counted per |
| --- | --- | --- | --- |
| `POST /sdk/v1/evaluate` | 6000 a minute | `FEATURESLASH_RATE_LIMIT_EVALUATE_PER_MINUTE` | SDK key |
| `POST /sdk/v1/events` | 600 a minute | `FEATURESLASH_RATE_LIMIT_EVENTS_PER_MINUTE` | SDK key |
| wrong credentials, on any route that takes one | 30 a minute | `FEATURESLASH_RATE_LIMIT_AUTH_FAILURES_PER_MINUTE` | client IP |
| open `GET /sdk/v1/stream` connections | 100 at once | `FEATURESLASH_RATE_LIMIT_STREAMS_PER_KEY` | SDK key |

Over a limit the answer is a `429` with `Retry-After` in whole seconds and the usual error body, code `rate_limited`.
A client key is shared by everyone using your app, so raise the evaluate and events numbers if a busy app reaches them.
The per-minute limits are token buckets: a quiet client can spend a whole minute's allowance at once, then gets it
back evenly.

Once an IP has used its failures, it gets the `429` before its credential is looked at, even a correct one, on the
admin routes too. A request with no `Authorization` header is not a failure, and neither is the `503` for an unset
admin token. `GET /health` is never limited. A service still sending a revoked key, or a revoked or expired scoped
token, counts as failing, so it can lock out everything else on its IP: replace the credential before it expires.

Counts live in the server's memory and a restart resets them. The server tracks up to 100,000 IPs and 10,000 keys per
limit and forgets the stalest first, so rotating addresses cannot grow its memory without limit, though an address
pushed out that way starts again from a full allowance.

### Behind a proxy

The client IP is the address of the connection. **Behind a proxy that is not listed, that is the proxy's address, so
every client shares one IP, and one attacker sending wrong credentials locks out all of them.** List your proxies:

```bash
FEATURESLASH_TRUSTED_PROXIES=10.0.0.5,10.0.0.6
```

It is a comma-separated list of IP addresses, not ranges, empty by default, and a value that is not an address stops the server. `X-Forwarded-For` is
believed only from a listed address, read from the right, stopping at the first address that is not listed, so a client
cannot name an IP of its choice. Your proxy must append to the header rather than pass along a client's alone. IPv6
clients are grouped by /64. Leave `ASPNETCORE_FORWARDEDHEADERS_ENABLED` unset: it makes ASP.NET believe the header from
anyone.

Docker can do the same to you with no proxy in sight: depending on the host's network setup, a published port can show
every client as the bridge's gateway address. If the limits trip for everyone together, that is the first thing to check.

## How this is tested

The `container` job in `.github/workflows/ci.yml` builds the image, brings the stack up, checks `/health`, that
the container runs as a non-root user and enforces the admin token, and that a project survives a restart and
the backup and restore above, then runs `docker compose down -v`. It pushes nothing and uses a throwaway
token, not a repository secret.
