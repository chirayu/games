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
5. **MVP word validation.** Accept every non-empty selected sequence while
   retaining duplicate-word and prefix restrictions. A later dictionary
   implementation plugs into `WordValidator`.
6. **MVP board generation.** Generate each position independently and uniformly
   from A–Z. Letters behave literally. Alternative generators plug into
   `BoardGenerator`.
7. **Player identity.** Use separate capability links and game-scoped secure
   cookies rather than user accounts. Store hashes of invitation secrets only.
8. **Asynchronous refresh.** Fetch on load and focus; poll every 30 seconds only
   while visible and waiting. Version checks resolve stale clients.
9. **Retention.** Keep games indefinitely for the MVP and use 180-day player
   cookies. Reopening a private link can restore a session.
10. **Deployment target.** Produce a provider-neutral OCI container, durable
    SQLite volume guidance, and GitHub Actions validation. Selecting and
    provisioning a hosting provider is a later decision.
