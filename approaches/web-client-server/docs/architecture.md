# Web client-server architecture

## System boundary

The Vue application communicates with one Go HTTP service. The Go service owns
all rule validation and stores authoritative state in SQLite. In production,
the same service serves the compiled frontend so browser and API requests share
an origin.

```text
Browser (Vue) -> HTTP API (Go) -> game service -> SQLite
```

There are no WebSockets in the MVP. The client fetches current state on load,
after a move, and when the page becomes visible or focused. While visible and
waiting for the opponent, it may poll every 30 seconds.

## Domain boundaries

The game package has no HTTP or database dependencies. It owns board creation,
move validation, word derivation, ownership, protection, turns, scoring, and
completion. Replaceable `WordValidator` and `BoardGenerator` interfaces isolate
future dictionary and board-generation strategies.

HTTP handlers translate requests and errors. An application service coordinates
authentication, domain operations, and transactions. The SQLite store uses
sqlc-generated queries.

## State and concurrency

Each game stores its board letters, tile owners, active player, status, and a
monotonically increasing version. Moves store the ordered tile indexes and the
word derived by the server. Protected status is derived from current ownership.

Move submission includes `expectedVersion`. In one SQLite transaction, the
server authenticates the player, checks turn and version, validates and applies
the move, records it, and updates game state. A stale version returns a conflict
without changing state.

SQLite schema changes are ordered Goose migrations embedded in the service.
sqlc generates the typed query layer from those migrations and hand-written
queries. Connections enable foreign keys, use a five-second busy timeout, and
put file databases in WAL mode. Games contain the current board state while
moves form the immutable turn history; inserting a move and advancing the
versioned state share one transaction.

## Invitations and sessions

Game creation generates separate 32-byte random secrets for the two player
seats. Only SHA-256 hashes of those secrets are stored. Each invitation places
the raw secret in the URL fragment so it is not sent in ordinary HTTP requests.
The frontend exchanges it at `POST /api/v1/games/{gameID}/sessions`, then removes
the fragment from the visible URL.

The exchange sets an `HttpOnly`, `Secure`, `SameSite=Lax` cookie whose name and
path are scoped to the game. This supports multiple concurrent games without
accounts. Mutating requests verify the request origin. The cookie lasts 180
days and may be re-established by reopening the private invitation link.

For the MVP, games do not expire automatically. Retention and deletion controls
can be added when operating requirements are known.

`POST /api/v1/games` creates the authoritative 5×5 game and returns the two raw
capabilities once; the client immediately exchanges Player 1's capability and
places Player 2's capability only in the invitation URL fragment. Session
exchange returns the authenticated seat, never the capability. API successes
use a `data` envelope and failures use an `error` envelope with stable `code`
and `message` fields. All state-changing endpoints require a matching browser
origin. Secure cookies are the default, with an explicit insecure override for
local HTTP development.

## Client recovery

The client treats server state as authoritative. A selected, unsubmitted move
may be saved locally with the game ID and version so a transient network error
does not discard it. The draft is removed after success and discarded if the
server version changes.

## Production shape

The production artifact is a provider-neutral OCI container running one Go
service with a mounted persistent directory for SQLite. MY-17 prepares the
container, health checks, environment configuration, backup/restore guidance,
and a GitHub Actions validation workflow. Deploying to a particular hosting
provider is out of scope until one is selected.
