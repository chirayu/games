# Production deployment

Letterpress ships one OCI image containing the Go HTTP service, compiled Vue
app, and optional managed-bot worker. The supported deployment uses one HTTP
service replica, one bot worker replica, and one persistent SQLite directory.
This guide does not provision a hosting provider.

## Runtime contract

Run exactly one application replica against one writable persistent volume.
Mount that volume at `/data`; the default database is
`/data/letterpress.db`. The container listens on port 8080 as a non-root user.
Terminate TLS at the platform load balancer or reverse proxy and forward the
original `Host` and scheme.

The ESDB/SCOWL attribution notice ships at `/app/licenses/ESDB.txt`; native
builds place the same notice under `build/licenses/`.

| Environment variable | Default in container | Purpose |
| --- | --- | --- |
| `LETTERPRESS_ADDR` | `:8080` | HTTP listen address |
| `LETTERPRESS_PUBLIC_BASE_URL` | unset | Canonical HTTPS origin, for example `https://play.example.com` |
| `LETTERPRESS_DB_PATH` | `/data/letterpress.db` | SQLite file on the persistent volume |
| `LETTERPRESS_STATIC_DIR` | `/app/frontend` | Compiled Vue assets |
| `LETTERPRESS_INSECURE_COOKIES` | `false` | Local HTTP override; never enable in production |
| `LETTERPRESS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` JSON logs |

The bot worker uses these additional values:

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `LETTERPRESS_BOT_TOKEN` | none | Required base64url-encoded 32-byte runtime secret; never store it in source control |
| `LETTERPRESS_BOT_API_BASE_URL` | `http://127.0.0.1:8080` | Internal URL of the HTTP service |
| `LETTERPRESS_BOT_PLAYER_ID` | `bot-balanced-v1` | Managed bot identity to operate |

Set `LETTERPRESS_PUBLIC_BASE_URL` to the externally visible HTTPS origin. The
service rejects an HTTP public URL while secure cookies are enabled. Player
cookies are `HttpOnly`, `Secure`, and `SameSite=Lax`; therefore production must
be accessed through HTTPS. Raw player tokens are never included in structured
request logs.

`GET /healthz` is the process liveness endpoint. `GET /readyz` verifies that
SQLite can serve requests and is the readiness/container-health endpoint.

## Build from a clean checkout

Requirements are Go 1.24+, Node.js 24+, pnpm 11, and either Docker or another
OCI-compatible builder.

Build the OCI image:

```sh
cd approaches/web-client-server
docker build -f Containerfile -t letterpress:local .
```

Or build the native distribution (binary plus frontend directory):

```sh
cd approaches/web-client-server
./scripts/build-production.sh
```

The native output is under `build/`. Run it with the compiled frontend:

```sh
LETTERPRESS_PUBLIC_BASE_URL=https://play.example.com \
LETTERPRESS_DB_PATH=/srv/letterpress/letterpress.db \
LETTERPRESS_STATIC_DIR="$PWD/build/frontend" \
./build/letterpress
```

The same build contains `build/letterpress-bot` for the separately configured
worker process.

## Run the container

Create a durable volume and start one service:

```sh
docker volume create letterpress-data
docker run --name letterpress --restart unless-stopped \
  -p 8080:8080 \
  -v letterpress-data:/data \
  -e LETTERPRESS_PUBLIC_BASE_URL=https://play.example.com \
  letterpress:local
```

Run the bot worker from the same image and volume after the HTTP service is
ready. Supply its secret through the platform's secret manager; the worker
stores only its SHA-256 hash and uses the raw value only for bearer requests:

```sh
docker run --name letterpress-bot --restart unless-stopped \
  -v letterpress-data:/data \
  -e LETTERPRESS_BOT_TOKEN="$LETTERPRESS_BOT_TOKEN" \
  -e LETTERPRESS_BOT_API_BASE_URL=http://letterpress:8080 \
  --entrypoint /app/letterpress-bot \
  letterpress:local
```

Place both containers on the same private network so the `letterpress` hostname
resolves. Keep exactly one bot worker replica for routine operation; correctness
still relies on expected-version conflicts if workers overlap during a restart.

On startup the service opens SQLite, enables foreign keys and WAL mode, and
applies embedded Goose migrations before reporting ready. A restart with the
same mounted volume preserves games and player sessions. Graceful `SIGTERM`
handling drains HTTP requests for up to ten seconds before closing SQLite.

## Back up

Take a backup before every deployment that introduces a migration. For an
online backup, run the SQLite CLI on a host that can access the mounted volume;
SQLite's backup command produces a consistent snapshot while WAL mode is
active:

```sh
sqlite3 /path/to/letterpress.db \
  ".timeout 5000" \
  ".backup '/backups/letterpress-YYYYMMDD-HHMMSS.db'"
sqlite3 /backups/letterpress-YYYYMMDD-HHMMSS.db "PRAGMA integrity_check;"
```

The integrity check must print `ok`. Store backups outside the application
volume and apply the retention/encryption policy of the selected platform. A
volume snapshot is also valid when the service is stopped cleanly.

## Restore

Restoration replaces live state, so first stop the application and retain the
current database as a rollback copy. Copy the verified backup to the configured
`LETTERPRESS_DB_PATH`, ensure it is writable by the container user, and remove
stale `letterpress.db-wal` and `letterpress.db-shm` sidecars before restarting.
The service reapplies any migrations missing from the restored backup. Confirm
`/readyz`, then load a known game from both player perspectives.

Never copy a raw database file over the live file while the service is running.

## Migration and rollback

Migrations are ordered, embedded artifacts and run automatically before the
HTTP listener starts. Deployment should fail rather than receive traffic if a
migration cannot complete. Keep one service replica so two processes never
race over the same SQLite volume.

To roll back application code without a schema change, start the previous image
against the existing volume. If the release applied a schema change that the
previous image does not understand, stop the service, restore the pre-deploy
backup, and then start the previous image. Goose down-migrations are not run
automatically in production.

## Logs and validation

Logs are newline-delimited JSON on standard output. Request completion records
include method, path, status, response bytes, and duration; they intentionally
exclude query strings, headers, cookies, and bodies.

The GitHub Actions workflow checks Go formatting/tests/race/vet/build, sqlc
reproducibility, frontend formatting/tests/type checking/build, the isolated
two-player Playwright journey, and the OCI image build. The repository README
lists the equivalent local verification commands.
