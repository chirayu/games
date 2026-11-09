-- +goose Up
CREATE UNIQUE INDEX game_allocations_one_bot_queue_per_player_idx
    ON game_allocations(requester_id, mode)
    WHERE mode = 'bot' AND state = 'queued';

-- +goose Down
DROP INDEX game_allocations_one_bot_queue_per_player_idx;
