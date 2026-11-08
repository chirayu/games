-- +goose Up
UPDATE game_allocations
SET state = 'cancelled', updated_at = CURRENT_TIMESTAMP
WHERE id IN (
    SELECT id FROM (
        SELECT id, ROW_NUMBER() OVER (
            PARTITION BY requester_id, opponent_id
            ORDER BY created_at ASC, id ASC
        ) AS duplicate_number
        FROM game_allocations
        WHERE mode = 'specified' AND state = 'pending_acceptance'
    )
    WHERE duplicate_number > 1
);

CREATE UNIQUE INDEX game_allocations_one_pending_specified_pair_idx
    ON game_allocations(requester_id, opponent_id)
    WHERE mode = 'specified' AND state = 'pending_acceptance';

-- +goose Down
DROP INDEX game_allocations_one_pending_specified_pair_idx;
