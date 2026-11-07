-- +goose Up
CREATE TABLE games (
    id TEXT PRIMARY KEY,
    board_letters BLOB NOT NULL CHECK (length(board_letters) > 0),
    tile_owners BLOB NOT NULL CHECK (length(tile_owners) = length(board_letters)),
    current_player INTEGER NOT NULL CHECK (current_player IN (1, 2)),
    status TEXT NOT NULL CHECK (status IN ('active', 'completed')),
    version INTEGER NOT NULL DEFAULT 0 CHECK (version >= 0),
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

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

-- +goose Down
DROP TABLE moves;
DROP TABLE players;
DROP TABLE games;
