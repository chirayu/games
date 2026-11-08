# Decision log

This file records accepted cross-ticket decisions. Update it when a later
ticket intentionally changes one.

## Accepted decisions

1. **Separate implementation approaches.** The web client-server version lives
   under `approaches/web-client-server/`; future approaches get sibling
   directories.
2. **HTTP client-server architecture.** Persistent server state supports games
   played over multiple days. WebSockets and push notifications are not needed
   for the MVP.
3. **Backend stack.** Use Go with the standard `net/http` router, SQLite through
   `modernc.org/sqlite`, sqlc for typed queries, and Goose for migrations.
4. **Frontend stack.** Use Vue 3, TypeScript, Vite, Vue Router, and pnpm. Prefer
   composables and scoped CSS; introduce a global store or component library
   only when a ticket demonstrates a need.
5. **English word validation.** Validate server-derived words against the
   embedded ESDB/SCOWL `rel-2026.02.25` size-80 lexicon with American, British,
   Canadian, and Australian spellings at variant level 1. Normalize the
   generated artifact to uppercase A–Z, preserve duplicate-word and prefix
   restrictions, and make dictionary updates explicit and reproducible.
6. **MVP board generation.** Generate each position independently and uniformly
   from A–Z. Letters behave literally. Alternative generators plug into
   `BoardGenerator`.
7. **Player identity.** Use a stable registered-player record with a unique,
   case-insensitive name. Name entry is intentionally both registration and
   login from any browser, with no password or ownership proof. Each login gets
   an independent application-wide secure cookie backed by a stored token hash;
   logout revokes only that session. Anyone who knows a name can impersonate
   that player, an accepted limitation for the current private deployment.
   Game seats reference registered-player IDs and contain no independent
   credential. Invitation capabilities are removed rather than retained for
   compatibility, and the migration purges invitation-era games.
8. **Asynchronous refresh.** Fetch on load and focus; poll every 30 seconds only
   while visible and waiting. Version checks resolve stale clients.
9. **Retention.** Keep games indefinitely for the MVP and use 180-day
   registered-player sessions. Each browser login is independently revocable.
10. **Deployment target.** Produce a provider-neutral OCI container, durable
    SQLite volume guidance, and GitHub Actions validation. Selecting and
    provisioning a hosting provider is a later decision.
11. **Durable game allocation.** Create games only by completing a persisted,
    idempotent allocation. Allocation modes share one explicit state machine;
    completion assigns the requester seat 1 and the resolved opponent seat 2 in
    the same transaction that creates the game. Public retries identify the
    original allocation with a requester-scoped idempotency key.
12. **Unified start-game experience.** The authenticated lobby is the only
    place to begin a game. Players explicitly choose a specific registered
    player or random human matchmaking. Pending starts are restored from durable
    allocations; there is no invitation link, transferable seat credential,
    direct public game creation, or compatibility route.
