-- +goose Up
CREATE TABLE game_allocations_v2 (
    id TEXT PRIMARY KEY,
    requester_id TEXT NOT NULL REFERENCES registered_players(id) ON DELETE RESTRICT,
    opponent_id TEXT REFERENCES registered_players(id) ON DELETE RESTRICT,
    mode TEXT NOT NULL CHECK (mode IN ('specified', 'random', 'bot')),
    allowed_opponent_type TEXT NOT NULL CHECK (allowed_opponent_type IN ('human', 'bot')),
    state TEXT NOT NULL CHECK (state IN ('pending_acceptance', 'queued', 'allocated', 'declined', 'cancelled', 'expired', 'failed')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    game_id TEXT REFERENCES games(id) ON DELETE RESTRICT,
    failure_reason TEXT,
    expires_at DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE (requester_id, idempotency_key),
    CHECK (opponent_id IS NULL OR opponent_id != requester_id),
    CHECK ((state = 'allocated' AND game_id IS NOT NULL) OR (state != 'allocated' AND game_id IS NULL))
);

INSERT INTO game_allocations_v2 (
    id, requester_id, opponent_id, mode, allowed_opponent_type, state,
    idempotency_key, game_id, failure_reason, expires_at, created_at, updated_at
)
SELECT id, requester_id, opponent_id, mode,
       CASE WHEN mode = 'bot' THEN 'bot' ELSE 'human' END,
       state, idempotency_key, game_id, failure_reason, expires_at, created_at, updated_at
FROM game_allocations;

DROP TABLE game_allocations;
ALTER TABLE game_allocations_v2 RENAME TO game_allocations;

CREATE INDEX game_allocations_requester_state_idx
    ON game_allocations(requester_id, state, created_at DESC, id DESC);
CREATE INDEX game_allocations_opponent_state_idx
    ON game_allocations(opponent_id, state, created_at DESC, id DESC);
CREATE INDEX game_allocations_queue_idx
    ON game_allocations(mode, allowed_opponent_type, state, created_at ASC, id ASC);
CREATE UNIQUE INDEX game_allocations_one_random_queue_per_player_idx
    ON game_allocations(requester_id, mode, allowed_opponent_type)
    WHERE mode = 'random' AND state = 'queued';

-- +goose Down
CREATE TABLE game_allocations_v1 (
    id TEXT PRIMARY KEY,
    requester_id TEXT NOT NULL REFERENCES registered_players(id) ON DELETE RESTRICT,
    opponent_id TEXT REFERENCES registered_players(id) ON DELETE RESTRICT,
    mode TEXT NOT NULL CHECK (mode IN ('specified', 'random', 'bot')),
    state TEXT NOT NULL CHECK (state IN ('pending_acceptance', 'queued', 'allocated', 'declined', 'cancelled', 'expired', 'failed')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    game_id TEXT UNIQUE REFERENCES games(id) ON DELETE RESTRICT,
    failure_reason TEXT,
    expires_at DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE (requester_id, idempotency_key),
    CHECK (opponent_id IS NULL OR opponent_id != requester_id),
    CHECK ((state = 'allocated' AND game_id IS NOT NULL) OR (state != 'allocated' AND game_id IS NULL))
);

INSERT OR IGNORE INTO game_allocations_v1 (
    id, requester_id, opponent_id, mode, state, idempotency_key,
    game_id, failure_reason, expires_at, created_at, updated_at
)
SELECT id, requester_id, opponent_id, mode, state, idempotency_key,
       game_id, failure_reason, expires_at, created_at, updated_at
FROM game_allocations
ORDER BY created_at ASC, id ASC;

DROP TABLE game_allocations;
ALTER TABLE game_allocations_v1 RENAME TO game_allocations;
CREATE INDEX game_allocations_requester_state_idx
    ON game_allocations(requester_id, state, created_at DESC, id DESC);
CREATE INDEX game_allocations_opponent_state_idx
    ON game_allocations(opponent_id, state, created_at DESC, id DESC);
CREATE INDEX game_allocations_queue_idx
    ON game_allocations(mode, state, created_at ASC, id ASC);
