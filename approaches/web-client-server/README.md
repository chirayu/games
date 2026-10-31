# Letterpress web client-server approach

This directory contains the HTTP-based Letterpress implementation. A Go server
owns the authoritative game state in SQLite and serves a Vue single-page
application. Players may return days later using their private game links.

Development is tracked in the [Letterpress Linear project](https://linear.app/chirayu1/project/letterpress-0eec12ba3679/overview).

## Prerequisites

- Go 1.24 or newer
- Node.js 24 or newer
- pnpm 11

No standalone SQLite installation is required. The backend uses the pure-Go
SQLite driver, applies embedded Goose migrations, and keeps sqlc output in the
repository.

## Layout

```text
backend/
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

## Development status

The pure Go game engine, SQLite persistence layer, secure game creation and
invitation flow, authenticated responsive board view, HTTP service, Vue
application, router, Vite development proxy, and focused backend and frontend
tests are in place. Move submission is intentionally reserved for a later
ticket.

## Documentation

- The repository `README.md` defines the game rules.
- `docs/architecture.md` defines service boundaries and cross-cutting behavior.
- `docs/decisions.md` records accepted decisions and their rationale.
- Linear tickets define the scope and acceptance criteria for each change.
