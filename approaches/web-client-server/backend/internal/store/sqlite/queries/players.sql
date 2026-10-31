-- name: CreatePlayer :exec
INSERT INTO players (game_id, seat, token_hash, created_at)
VALUES (?, ?, ?, ?);

-- name: ListPlayers :many
SELECT game_id, seat, token_hash, created_at
FROM players
WHERE game_id = ?
ORDER BY seat;

-- name: GetPlayerByTokenHash :one
SELECT game_id, seat, token_hash, created_at
FROM players
WHERE game_id = ? AND token_hash = ?;
