-- +goose Up
CREATE TABLE game_allocations (
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

CREATE INDEX game_allocations_requester_state_idx
    ON game_allocations(requester_id, state, created_at DESC, id DESC);
CREATE INDEX game_allocations_opponent_state_idx
    ON game_allocations(opponent_id, state, created_at DESC, id DESC);
CREATE INDEX game_allocations_queue_idx
    ON game_allocations(mode, state, created_at ASC, id ASC);

-- +goose Down
DROP TABLE game_allocations;
