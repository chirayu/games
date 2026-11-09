# Letterpress web client-server approach

This directory contains the HTTP-based Letterpress implementation. A Go server
owns the authoritative game state in SQLite and serves a Vue single-page
application. Players authenticate by name and may return to assigned games.

Development is tracked in the [Letterpress Linear project](https://linear.app/chirayu1/project/letterpress-0eec12ba3679/overview).

## Prerequisites

- Go 1.24 or newer
- Node.js 24 or newer
- pnpm 11

No standalone SQLite installation is required. The backend uses the pure-Go
SQLite driver, applies embedded Goose migrations, and keeps sqlc output in the
repository.

## English dictionary

The server embeds a generated 222,023-word English dictionary from the official
English Speller Database (ESDB/SCOWL) release `rel-2026.02.25`. It uses size 80,
variant level 1, and American, British, Canadian, and Australian spellings. The
generator deaccents, uppercases, filters to A–Z, sorts, and deduplicates the
source. Production validation performs immutable in-memory lookups and never
calls a dictionary service or reads the artifact per move.

Source revision, checksums, generation instructions, and the complete upstream
notice live in `backend/internal/game/dictionary/`. To intentionally reproduce
the pinned artifact:

```sh
cd approaches/web-client-server/backend
./internal/game/dictionary/generate.sh
```

## Player identity

The backend supports name-only registered-player sessions. Entering a new name
registers it; entering an existing name from any browser logs in to that same
player. There is deliberately no password or ownership verification yet, so
anyone who knows a player name can access that identity. Each browser receives
an independent secure session and can log out without affecting other sessions.

The UI registers or logs in a player by name, restores their session on reload,
and provides application-wide logout. Game seats reference registered players
directly. The old invitation tokens,
game-scoped cookies, and public creation/session-exchange endpoints have been
removed; the migration intentionally purges invitation-era games. The game
allocation REST API and new player-facing UI are tracked separately.

## Layout

```text
backend/
  cmd/bot-runtime/
  cmd/server/
  internal/app/
  internal/game/
  internal/httpapi/
  internal/store/sqlite/
frontend/
  src/api/
  src/components/
  src/router/
  src/views/
docs/
  architecture.md
  decisions.md
```

## Local development

Start the backend in one terminal:

```sh
cd approaches/web-client-server/backend
go run ./cmd/server -insecure-cookies -allowed-origin http://localhost:5173
```

The server stores local game data in `letterpress.db`. Secure cookies are the
default; `-insecure-cookies` is the explicit override for local HTTP
development. The Vite origin is allowed explicitly because its development
proxy forwards API requests to a different host. Use `-db` to select a
different SQLite path.

The service listens on `http://localhost:8080` by default. Confirm it is ready:

```sh
curl http://localhost:8080/healthz
```

To run managed bot turns locally, generate a 32-byte base64url secret once and
start the durable worker against the same SQLite file in another terminal:

```sh
export LETTERPRESS_BOT_TOKEN="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=')"
cd approaches/web-client-server/backend
go run ./cmd/bot-runtime -api-base-url http://localhost:8080
```

The worker provisions only the secret's SHA-256 hash for the configured managed
bot, discovers pending turns from durable assignments, and reads/submits moves
through the shared REST game endpoints. Reuse the same secret after restart.

Start the frontend in another terminal:

```sh
cd approaches/web-client-server/frontend
pnpm install --frozen-lockfile
pnpm dev
```

Open the URL printed by Vite. During development, Vite forwards `/api` and
`/healthz` requests to the Go service at `http://localhost:8080`.

## Verification

Format and test the backend:

```sh
cd approaches/web-client-server/backend
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate
gofmt -w .
go test ./...
go build ./...
```

Format, test, type-check, and build the frontend:

```sh
cd approaches/web-client-server/frontend
pnpm format
pnpm test
pnpm build
```

Use `pnpm format:check` when checking formatting without changing files.

Run the deterministic two-player browser journey with:

```sh
cd approaches/web-client-server/frontend
pnpm exec playwright install chromium # first run only
pnpm test:e2e
```

Playwright starts isolated backend and frontend processes, uses separate browser
contexts for each player, and retains traces, screenshots, and video under
`test-results/` when a test fails. The backend's `-fixed-board` option exists
only for deterministic development and test runs; normal startup continues to
use cryptographically secure random letters.

## Development status

The pure Go game engine, SQLite persistence layer, name-only registered-player
sessions and UI, registered game membership, participant-scoped active-game
lobby and player discovery, authenticated responsive board, transactional move submission,
multi-day focus/visibility refresh and polling, recoverable local move drafts,
server-authoritative English dictionary validation, final results and move
history, HTTP service, Vue application, router, Vite development proxy, and
focused backend and frontend tests are in place.
The deterministic two-player Playwright journey is also in place. Production
packaging, one-origin static serving, structured operations, and deployment
documentation are also complete.
The durable allocation resource, atomic game-creation boundary, and complete
specified-player challenge journey are also in place. Players can challenge a
search result, accept or decline incoming challenges, cancel outgoing ones, and
enter the allocated game. Persisted random-human matchmaking is also available:
players may join or cancel the queue, and compatible oldest-first requests are
atomically assigned one shared game.
The lobby's unified Start game flow is the only new-game journey and restores
pending allocation state after reload. It contains no share-link or invitation
compatibility path.
Managed bots now have stable registered-player identities, immutable versioned
profiles, hash-only runtime credentials, and historical profile snapshots on
ordinary game seats. Legal move search, the versioned balanced policy, and a
restart-safe REST bot worker are in place. Explicit bot allocation remains the
next ticket.

## Documentation

- The repository `README.md` defines the game rules.
- `docs/architecture.md` defines service boundaries and cross-cutting behavior.
- `docs/decisions.md` records accepted decisions and their rationale.
- `docs/deployment.md` defines the production runtime, build, backup, restore,
  migration, and rollback procedures.
- Linear tickets define the scope and acceptance criteria for each change.
