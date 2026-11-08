-- +goose Up
CREATE TABLE registered_players (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 24),
    normalized_name TEXT NOT NULL UNIQUE CHECK (length(normalized_name) BETWEEN 1 AND 24),
    created_at DATETIME NOT NULL
);

CREATE TABLE player_sessions (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    player_id TEXT NOT NULL REFERENCES registered_players(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX player_sessions_player_id_idx ON player_sessions(player_id);
CREATE INDEX player_sessions_expires_at_idx ON player_sessions(expires_at);

-- +goose Down
DROP TABLE player_sessions;
DROP TABLE registered_players;
