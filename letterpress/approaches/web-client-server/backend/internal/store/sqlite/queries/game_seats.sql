-- name: CreateGameSeat :exec
INSERT INTO game_seats (game_id, seat, player_id, created_at)
VALUES (?, ?, ?, ?);

-- name: CreateGameSeatBotProfile :exec
INSERT INTO game_seat_bot_profiles (
    game_id, seat, player_id, configuration_version,
    style, english_knowledge_level, skill_level
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetGameSeatBotProfile :one
SELECT game_id, seat, player_id, configuration_version,
       style, english_knowledge_level, skill_level
FROM game_seat_bot_profiles
WHERE game_id = ? AND seat = ?;

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
    opponent_player.display_name AS opponent_name,
    opponent_player.player_type AS opponent_type,
    opponent_profile.configuration_version AS opponent_profile_version,
    opponent_profile.style AS opponent_style,
    opponent_profile.english_knowledge_level AS opponent_english_knowledge_level,
    opponent_profile.skill_level AS opponent_skill_level
FROM game_seats AS own
JOIN games AS g ON g.id = own.game_id
JOIN game_seats AS opponent ON opponent.game_id = g.id AND opponent.seat != own.seat
JOIN registered_players AS opponent_player ON opponent_player.id = opponent.player_id
LEFT JOIN game_seat_bot_profiles AS opponent_profile
    ON opponent_profile.game_id = opponent.game_id AND opponent_profile.seat = opponent.seat
WHERE own.player_id = sqlc.arg(player_id)
  AND g.status = 'active'
  AND (
      g.updated_at < sqlc.arg(cursor_updated_at)
      OR (g.updated_at = sqlc.arg(cursor_updated_at) AND g.id < sqlc.arg(cursor_id))
  )
ORDER BY g.updated_at DESC, g.id DESC
LIMIT sqlc.arg(result_limit);
