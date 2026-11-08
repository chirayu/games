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
