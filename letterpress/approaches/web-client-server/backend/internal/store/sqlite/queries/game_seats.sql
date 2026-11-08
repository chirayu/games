-- name: CreateGameSeat :exec
INSERT INTO game_seats (game_id, seat, player_id, created_at)
VALUES (?, ?, ?, ?);

-- name: ListGameSeats :many
SELECT game_id, seat, player_id, created_at
FROM game_seats
WHERE game_id = ?
ORDER BY seat;

-- name: GetGameSeatByPlayer :one
SELECT game_id, seat, player_id, created_at
FROM game_seats
WHERE game_id = ? AND player_id = ?;

-- name: ListActiveGamesForPlayer :many
SELECT
    g.id,
    g.tile_owners,
    g.current_player,
    g.status,
    g.version,
    g.updated_at,
    own.seat AS player_seat,
    opponent.player_id AS opponent_id,
    opponent_player.display_name AS opponent_name
FROM game_seats AS own
JOIN games AS g ON g.id = own.game_id
JOIN game_seats AS opponent ON opponent.game_id = g.id AND opponent.seat != own.seat
JOIN registered_players AS opponent_player ON opponent_player.id = opponent.player_id
WHERE own.player_id = sqlc.arg(player_id)
  AND g.status = 'active'
  AND (
      g.updated_at < sqlc.arg(cursor_updated_at)
      OR (g.updated_at = sqlc.arg(cursor_updated_at) AND g.id < sqlc.arg(cursor_id))
  )
ORDER BY g.updated_at DESC, g.id DESC
LIMIT sqlc.arg(result_limit);
