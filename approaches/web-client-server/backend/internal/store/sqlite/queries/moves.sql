-- name: CreateMove :exec
INSERT INTO moves (game_id, player, turn_number, tile_indexes, word, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListMoves :many
SELECT id, game_id, player, turn_number, tile_indexes, word, created_at
FROM moves
WHERE game_id = ?
ORDER BY turn_number;
