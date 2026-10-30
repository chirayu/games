# Repository guidance

## Project

Letterpress is a two-player, asynchronous word game. The root `README.md` is
the source of truth for product rules. Keep separate implementation approaches
under `approaches/`; the active approach is `approaches/web-client-server/`.

The Linear Letterpress project owns ticket scope, ordering, and acceptance
criteria. Implement only the ticket named in the task and do not begin the next
ticket automatically.

## Web client-server approach

- Backend: Go, standard `net/http`, SQLite via `modernc.org/sqlite`, sqlc, and
  SQL migrations managed with Goose.
- Frontend: Vue 3, TypeScript, Vite, Vue Router, and pnpm.
- Tests: Go's `testing` package, Vitest with Vue Test Utils, and Playwright for
  end-to-end coverage.
- The server is authoritative for boards, moves, ownership, protection, turns,
  versions, and results.
- Use HTTP for the MVP. Do not add WebSockets, push notifications, accounts, or
  dictionary validation unless a ticket explicitly requests them.

Read `approaches/web-client-server/docs/architecture.md` when changing service
boundaries, HTTP APIs, authentication, persistence, concurrency, or deployment.
Read `approaches/web-client-server/docs/decisions.md` when proposing a change to
an accepted technical or product decision. Do not require unrelated tasks to
read every document.

## MVP invariants

- The default board is 5×5. Generate each tile independently and uniformly
  from the English alphabet A–Z using cryptographically secure randomness.
- Letters behave literally; `Q` is not `Qu`.
- Every non-empty selected sequence is dictionary-valid for the MVP. Repeated
  words and words that are prefixes of previously played words remain invalid.
- A tile may appear only once in a move, and selected tiles need not be
  adjacent.
- Recalculate protection across the whole board after every accepted move.
- The game ends when no neutral tiles remain.

## Working agreement

- Keep domain rules independent of HTTP and database packages.
- Prefer clear standard-library code over additional frameworks or layers.
- Add or update focused tests with behavior changes. Run the affected tests,
  formatting, linting, type checking, and builds defined by the approach
  README or package manifests before finishing.
- Treat migrations and generated sqlc code as reproducible artifacts; commit
  both the source SQL and generated Go output.
- Keep secrets and raw player tokens out of source control, database rows, and
  logs.
- Update architecture or decision documentation when a ticket changes an
  accepted boundary. Do not duplicate the Linear backlog in repository files.
