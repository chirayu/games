-- +goose Up
CREATE INDEX game_seats_player_game_idx ON game_seats(player_id, game_id);
CREATE INDEX games_active_activity_idx ON games(status, updated_at DESC, id DESC);

-- +goose Down
DROP INDEX games_active_activity_idx;
DROP INDEX game_seats_player_game_idx;
