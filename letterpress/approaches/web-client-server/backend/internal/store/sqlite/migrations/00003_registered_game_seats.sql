-- +goose Up
-- Invitation-era games cannot be assigned to registered identities safely.
-- Purge them before replacing capability seats with registered memberships.
DELETE FROM games;

DROP TABLE moves;
DROP TABLE players;

CREATE TABLE game_seats (
    game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    seat INTEGER NOT NULL CHECK (seat IN (1, 2)),
    player_id TEXT NOT NULL REFERENCES registered_players(id) ON DELETE RESTRICT,
    created_at DATETIME NOT NULL,
    PRIMARY KEY (game_id, seat),
    UNIQUE (game_id, player_id)
);

CREATE INDEX game_seats_player_id_idx ON game_seats(player_id);

CREATE TABLE moves (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id TEXT NOT NULL,
    player INTEGER NOT NULL,
    turn_number INTEGER NOT NULL CHECK (turn_number > 0),
    tile_indexes TEXT NOT NULL CHECK (length(tile_indexes) > 2),
    word TEXT NOT NULL CHECK (length(word) > 0),
    created_at DATETIME NOT NULL,
    UNIQUE (game_id, turn_number),
    FOREIGN KEY (game_id, player) REFERENCES game_seats(game_id, seat) ON DELETE CASCADE
);

CREATE INDEX moves_game_id_turn_number_idx ON moves(game_id, turn_number);

-- +goose Down
DELETE FROM games;

DROP TABLE moves;
DROP TABLE game_seats;

CREATE TABLE players (
    game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    seat INTEGER NOT NULL CHECK (seat IN (1, 2)),
    token_hash BLOB NOT NULL CHECK (length(token_hash) = 32),
    created_at DATETIME NOT NULL,
    PRIMARY KEY (game_id, seat),
    UNIQUE (game_id, token_hash)
);

CREATE TABLE moves (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id TEXT NOT NULL,
    player INTEGER NOT NULL,
    turn_number INTEGER NOT NULL CHECK (turn_number > 0),
    tile_indexes TEXT NOT NULL CHECK (length(tile_indexes) > 2),
    word TEXT NOT NULL CHECK (length(word) > 0),
    created_at DATETIME NOT NULL,
    UNIQUE (game_id, turn_number),
    FOREIGN KEY (game_id, player) REFERENCES players(game_id, seat) ON DELETE CASCADE
);

CREATE INDEX moves_game_id_turn_number_idx ON moves(game_id, turn_number);
