-- +goose Up
ALTER TABLE registered_players
ADD COLUMN player_type TEXT NOT NULL DEFAULT 'human'
    CHECK (player_type IN ('human', 'bot'));

CREATE TABLE bot_profiles (
    player_id TEXT NOT NULL REFERENCES registered_players(id) ON DELETE CASCADE,
    configuration_version INTEGER NOT NULL CHECK (configuration_version > 0),
    style TEXT NOT NULL CHECK (length(style) BETWEEN 1 AND 32),
    english_knowledge_level TEXT NOT NULL CHECK (length(english_knowledge_level) BETWEEN 1 AND 32),
    skill_level TEXT NOT NULL CHECK (length(skill_level) BETWEEN 1 AND 32),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at DATETIME NOT NULL,
    PRIMARY KEY (player_id, configuration_version)
);

CREATE TABLE bot_runtime_credentials (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    player_id TEXT NOT NULL UNIQUE REFERENCES registered_players(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL
);

CREATE UNIQUE INDEX game_seats_snapshot_parent_idx
    ON game_seats(game_id, seat, player_id);

CREATE TABLE game_seat_bot_profiles (
    game_id TEXT NOT NULL,
    seat INTEGER NOT NULL CHECK (seat IN (1, 2)),
    player_id TEXT NOT NULL,
    configuration_version INTEGER NOT NULL CHECK (configuration_version > 0),
    style TEXT NOT NULL,
    english_knowledge_level TEXT NOT NULL,
    skill_level TEXT NOT NULL,
    PRIMARY KEY (game_id, seat),
    FOREIGN KEY (game_id, seat, player_id)
        REFERENCES game_seats(game_id, seat, player_id) ON DELETE CASCADE,
    FOREIGN KEY (player_id, configuration_version)
        REFERENCES bot_profiles(player_id, configuration_version) ON DELETE RESTRICT
);

INSERT INTO registered_players (
    id, display_name, normalized_name, created_at, player_type
) VALUES (
    'bot-balanced-v1', 'Balanced Bot', 'balanced bot', CURRENT_TIMESTAMP, 'bot'
);

INSERT INTO bot_profiles (
    player_id, configuration_version, style,
    english_knowledge_level, skill_level, enabled, created_at
) VALUES (
    'bot-balanced-v1', 1, 'balanced',
    'full_dictionary', 'baseline', 1, CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE game_seat_bot_profiles;
DROP INDEX game_seats_snapshot_parent_idx;
DROP TABLE bot_runtime_credentials;
DROP TABLE bot_profiles;
DELETE FROM games
WHERE id IN (
    SELECT s.game_id
    FROM game_seats AS s
    JOIN registered_players AS p ON p.id = s.player_id
    WHERE p.player_type = 'bot'
);
DELETE FROM registered_players WHERE player_type = 'bot';
ALTER TABLE registered_players DROP COLUMN player_type;
