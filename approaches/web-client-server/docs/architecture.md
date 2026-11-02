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

The move repository starts an immediate write transaction before loading the
game, capability, and ordered move history. The application reconstructs the
pure domain game inside that transaction, derives the word from stored letters,
and returns the accepted move and resulting state for one atomic write. This
serializes competing writers so a double submission produces one accepted turn
and one version conflict rather than duplicate history.

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

`GET /api/v1/games/{gameID}` authenticates the game-scoped cookie and returns
the current persisted board plus the requesting player's seat. Scores and
protection are derived by the domain package from tile ownership on every read,
so clients receive a deterministic view without duplicating game rules. Game
responses are marked `no-store`; a missing or invalid session is unauthorized,
while an unknown game has a distinct not-found response.

`POST /api/v1/games/{gameID}/moves` accepts ordered tile indexes and an expected
version, never a client-provided word. Successful responses contain the updated
authoritative game view. Rule failures use stable error codes for duplicate or
invalid tiles, repeated or prefix words, turn violations, completed games, and
version conflicts.

Authenticated game views include immutable move history. Completed views also
include the final scores and either a winning seat or a tie. The final move,
completed status, ownership, version increment, and history row are committed
in the same immediate SQLite transaction; later moves are rejected.

## Client recovery

The client treats server state as authoritative. It refreshes on initial load,
window focus, and return to a visible page. While visible and waiting for the
opponent it polls every 30 seconds; it stops the timer while hidden. A selected,
unsubmitted move is saved in browser-local storage with the game ID and version
so a transient network error or browser restart does not discard it. The draft
is removed after success and discarded with an explanation if the server
version changes. Games have no automatic expiry in the MVP, while the
game-scoped HttpOnly player cookie lasts 180 days and can be restored from the
private invitation link.

## Production shape

The production artifact is a provider-neutral OCI container running one Go
service with a mounted persistent directory for SQLite. MY-17 prepares the
container, health checks, environment configuration, backup/restore guidance,
and a GitHub Actions validation workflow. Deploying to a particular hosting
provider is out of scope until one is selected.
