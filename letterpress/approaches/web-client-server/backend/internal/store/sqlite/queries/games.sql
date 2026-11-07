-- name: CreateGame :exec
INSERT INTO games (
    id, board_letters, tile_owners, current_player, status, version, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetGame :one
SELECT id, board_letters, tile_owners, current_player, status, version, created_at, updated_at
FROM games
WHERE id = ?;

-- name: UpdateGameState :execrows
UPDATE games
SET tile_owners = ?,
    current_player = ?,
    status = ?,
    version = version + 1,
    updated_at = ?
WHERE id = ? AND version = ?;
