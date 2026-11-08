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
dictionary and board-generation strategies.

The production `WordValidator` is an embedded, immutable ESDB/SCOWL size-80
lexicon covering American, British, Canadian, and Australian spellings. The
build embeds the checksum-verified, sorted A–Z artifact; startup validates it
once and move transactions use concurrency-safe in-memory exact lookups. No
runtime filesystem or network access is involved. The server still derives the
candidate word from ordered board tiles before dictionary, repeated-word, and
prefix validation.

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
registered-player seat, game, and ordered move history. The application reconstructs the
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

## Registered players and sessions

A registered player has a stable random ID, a preserved display name, and a
case-insensitive normalized name. Names contain 1–24 ASCII letters, numbers,
spaces, hyphens, or underscores; surrounding spaces are removed and repeated
spaces collapse to one. The normalized name is unique.

`POST /api/v1/player-sessions` is intentionally both registration and login. A
new normalized name creates the player, while an existing normalized name logs
in to the original player from any browser. There is no password, verification,
or proof of ownership: anyone who enters a known player name can act as that
player. This limitation is accepted for the current private deployment and
must be stated in the UI. A later identity system can strengthen login without
changing game rules.

Each successful name entry creates a separate 32-byte random session secret.
Only its SHA-256 hash is stored. The raw value is set in the application-wide
`letterpress_player_session` cookie with `HttpOnly`, `Secure`, `SameSite=Lax`,
and a 180-day lifetime. `GET /api/v1/players/me` restores the current identity.
`DELETE /api/v1/player-sessions/current` revokes only the presented session and
clears its cookie, so logging out one browser does not affect another browser
logged in with the same name. State-changing session requests require a
matching browser origin.

The Vue router restores this session before entering protected routes. Visitors
without a valid session see name entry; authenticated players go directly to
the lobby. The lobby header always identifies the current player and provides
logout. Network, validation, and expired-session states remain recoverable, and
the UI explicitly warns that knowing a name is sufficient to use that identity.

## Registered game membership

Each game has exactly two `game_seats`. A seat links its numbered game role to
a durable registered-player ID; it contains no credential. Game allocation is
the only creator of games and assigns two distinct registered players when it
calls the application service. There is no public direct game-creation endpoint.

The migration from invitation capabilities is deliberately destructive. It
purges all invitation-era games and move history, drops the legacy `players`
table, and replaces it with registered `game_seats`. The legacy creation and
session-exchange routes are absent and old credentials cannot authorize or
reveal a game.

For the MVP, newly allocated games do not expire automatically. Retention and
deletion controls can be added when operating requirements are known. API
successes use a `data` envelope and failures use an `error` envelope with
stable `code` and `message` fields. State-changing endpoints require a matching
browser origin.

## Lobby and player discovery

`GET /api/v1/games` authenticates the application-wide player session and
returns only active games in which that player holds a seat. Compact summaries
contain the opponent's public ID, display name, and player type; the requesting
seat, active seat, status, scores, version, and last activity. Scores are
derived from ownership by the application service. Board letters, move history,
other memberships, and credentials are not part of the lobby response.

Games are ordered by `updated_at DESC, id DESC`, so accepted moves place a game
at the top deterministically. Opaque keyset cursors continue from that pair.
The default page size is 20 and the hard maximum is 50. Completed games are not
included in this active-game endpoint.

`GET /api/v1/players?q=...` performs bounded, case-insensitive matching against
normalized registered-player names. It excludes the requesting player and
returns only public ID, display name, and type, ordered by normalized name and
ID. It uses the same page-size bounds and an opaque keyset cursor. Empty or
invalid search fragments are rejected rather than exposing an unfiltered player
directory.

The lobby groups summaries by whether the active seat is the player's seat,
links each row to the authoritative game view, and provides empty, loading,
expired-session, and retry states. Player search results can create specified
challenges. Incoming and outgoing challenges expose only the actions authorized
for that player, and the lobby refreshes them on load, focus, and visibility.
Successful acceptance navigates directly to the authoritative game.

## Durable game allocation

Games are created behind a durable `game_allocations` resource rather than a
public game-creation endpoint. `POST /api/v1/game-allocations` authenticates the
requester exclusively from the application session and requires an
`Idempotency-Key` of 1–128 characters. A key is unique per requester. Reusing it
with the same mode and opponent returns the original allocation; reusing it for
different input returns a conflict.

Allocation modes are `specified`, `random`, and the reserved `bot` mode. Their
durable states are `pending_acceptance`, `queued`, `allocated`, `declined`,
`cancelled`, `expired`, and `failed`. Terminal states cannot be reopened.
Specified requests begin pending acceptance; random and reserved bot requests
begin queued. Bot selection is not implemented yet.

`GET /api/v1/game-allocations/{id}` is private to the requester and intended
opponent. Unrelated players receive the same not-found response as an unknown
allocation. `DELETE` lets only the requester cancel a pending or queued
allocation. State-changing requests retain same-origin enforcement.

Specified-player allocations expire seven days after creation. Expiry is
applied lazily and atomically whenever an allocation is read, listed, cancelled,
declined, or accepted, so an expired challenge cannot race into a game.
`GET /api/v1/game-allocations` lists the authenticated player's live incoming
and outgoing specified challenges. Only the intended opponent may `POST` to
`/{id}/accept` or `/{id}/decline`; accepting creates exactly one game, while the
requester may cancel with `DELETE /{id}` until another terminal transition wins.

Completion runs in an immediate SQLite transaction. It locks and rechecks the
allocation, creates the game and both seats, and attaches the game ID before
commit. The requester deterministically receives seat 1 and the resolved
opponent seat 2. Repeated or concurrent completion returns the single existing
game. The domain engine receives only those resolved seats and remains unaware
of allocation mode.

`GET /api/v1/games/{gameID}` authenticates the application-wide player session,
requires that registered player to hold a game seat, and returns the current
persisted board plus the requesting player's seat. Scores and
protection are derived by the domain package from tile ownership on every read,
so clients receive a deterministic view without duplicating game rules. Game
responses are marked `no-store`. A missing or invalid session, a non-member,
and an unknown game for which the player has no seat all return unauthorized,
so membership checks do not disclose game existence.

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
version changes. Games have no automatic expiry in the MVP. Registered-player
sessions last 180 days and authorize every game in which the player has a seat.

## Production shape

The production artifact is a provider-neutral OCI container running one Go
service with compiled Vue assets and a mounted persistent directory for SQLite.
The Go router serves the SPA and `/api/v1` from one origin. Environment values
configure the listener, canonical public URL, database, static assets, cookie
security, and JSON log level. The process handles `SIGTERM` with a bounded
graceful shutdown; `/healthz` reports liveness and `/readyz` checks SQLite.

The container runs one non-root application process and expects one writable
`/data` volume. Startup applies embedded migrations before listening. Request
logs include method, path, status, response bytes, and duration but omit query
strings, headers, cookies, and bodies so player session credentials are not exposed.
The deployment runbook defines backup, restore, migration, and rollback
procedures. Deploying to a particular hosting provider remains out of scope.

## Browser validation

The Playwright suite starts an isolated local service with the explicit
development-only `-fixed-board` option and uses separate browser contexts for
registered players. Production startup omits that option and retains secure
random generation. The browser journey will move to registration and allocation
as those UI tickets land. Traces, screenshots, and video are retained for failed
runs.
