# Letterpress web client-server approach

This directory contains the HTTP-based Letterpress implementation. A Go server
owns the authoritative game state in SQLite and serves a Vue single-page
application. Players may return days later using their private game links.

Development is tracked in the [Letterpress Linear project](https://linear.app/chirayu1/project/letterpress-0eec12ba3679/overview).

## Prerequisites

- Go 1.24 or newer
- Node.js 24 or newer
- pnpm 11

No database tools are required for this foundation ticket. SQLite, Goose, and
sqlc setup will be added with the persistence work.

## Layout

```text
backend/
  cmd/server/
  internal/httpapi/
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
go run ./cmd/server
```

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

The Go HTTP service, health endpoint, Vue application, router, Vite development
proxy, and focused backend and frontend tests are in place. Game rules,
persistence, and gameplay flows are intentionally reserved for later tickets.

## Documentation

- The repository `README.md` defines the game rules.
- `docs/architecture.md` defines service boundaries and cross-cutting behavior.
- `docs/decisions.md` records accepted decisions and their rationale.
- Linear tickets define the scope and acceptance criteria for each change.
