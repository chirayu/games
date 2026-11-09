-- name: CreateRegisteredPlayer :execrows
INSERT OR IGNORE INTO registered_players (id, display_name, normalized_name, created_at, player_type)
VALUES (?, ?, ?, ?, ?);

-- name: GetRegisteredPlayerByNormalizedName :one
SELECT id, display_name, normalized_name, player_type, created_at
FROM registered_players
WHERE normalized_name = ?;

-- name: GetRegisteredPlayerByID :one
SELECT id, display_name, normalized_name, player_type, created_at
FROM registered_players
WHERE id = ?;

-- name: CreatePlayerSession :exec
INSERT INTO player_sessions (token_hash, player_id, expires_at, created_at)
VALUES (?, ?, ?, ?);

-- name: GetRegisteredPlayerBySessionHash :one
SELECT p.id, p.display_name, p.normalized_name, p.player_type, p.created_at
FROM player_sessions AS s
JOIN registered_players AS p ON p.id = s.player_id
WHERE s.token_hash = ? AND s.expires_at > ?;

-- name: DeletePlayerSession :execrows
DELETE FROM player_sessions
WHERE token_hash = ?;

-- name: SearchRegisteredPlayers :many
SELECT id, display_name, normalized_name, player_type, created_at
FROM registered_players
WHERE id != sqlc.arg(excluded_player_id)
  AND player_type = 'human'
  AND instr(normalized_name, sqlc.arg(name_fragment)) > 0
  AND (
      normalized_name > sqlc.arg(cursor_name)
      OR (normalized_name = sqlc.arg(cursor_name) AND id > sqlc.arg(cursor_id))
  )
ORDER BY normalized_name ASC, id ASC
LIMIT sqlc.arg(result_limit);
