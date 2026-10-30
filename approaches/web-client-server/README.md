# Letterpress web client-server approach

This directory contains the HTTP-based Letterpress implementation. A Go server
owns the authoritative game state in SQLite and serves a Vue single-page
application. Players may return days later using their private game links.

Development is tracked in the [Letterpress Linear project](https://linear.app/chirayu1/project/letterpress-0eec12ba3679/overview).

## Planned layout

```text
backend/
  cmd/server/
  internal/game/
  internal/httpapi/
  internal/service/
  internal/store/
  db/migrations/
  db/queries/
frontend/
  src/api/
  src/components/
  src/composables/
  src/router/
  src/views/
docs/
  architecture.md
  decisions.md
```

## Development status

Implementation has not started. Linear ticket MY-8 will scaffold the backend
and frontend and replace this section with exact prerequisites and commands for
development, formatting, tests, code generation, builds, and end-to-end checks.

## Documentation

- The repository `README.md` defines the game rules.
- `docs/architecture.md` defines service boundaries and cross-cutting behavior.
- `docs/decisions.md` records accepted decisions and their rationale.
- Linear tickets define the scope and acceptance criteria for each change.
