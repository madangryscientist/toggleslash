# Self-hosting

The C# server (`apps/server`) ships as a container image you build yourself. Nothing is published:
there is no image on any registry, and `featureslash.dev` is not routed to anything. The dashboard
(`apps/web`) is not containerised; it runs on Cloudflare Workers and its hosting is undecided
(see [`architecture.md`](architecture.md#hosting)). This page is about the server alone.

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
The server never generates, stores or logs the token. Without it the admin API answers 503
`admin_token_unset`.

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
| `FEATURESLASH_ADMIN_TOKEN` | environment, required by `compose.yaml` | the admin token for `/api/v1` |
| `FEATURESLASH_DB_PATH` | environment, `/data/featureslash.db` in the image | the SQLite file; keep it on the `/data` volume |

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

## How this is tested

The `container` job in `.github/workflows/ci.yml` builds the image, brings the stack up, checks `/health`, that
the container runs as a non-root user and enforces the admin token, and that a project survives a restart and
the backup and restore above, then runs `docker compose down -v`. It pushes nothing and uses a throwaway
token, not a repository secret.
