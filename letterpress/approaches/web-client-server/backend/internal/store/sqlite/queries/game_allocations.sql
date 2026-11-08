-- name: CreateGameAllocation :execrows
INSERT OR IGNORE INTO game_allocations (
    id, requester_id, opponent_id, mode, state, idempotency_key,
    game_id, failure_reason, expires_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetGameAllocation :one
SELECT * FROM game_allocations WHERE id = ?;

-- name: GetGameAllocationByRequesterKey :one
SELECT * FROM game_allocations WHERE requester_id = ? AND idempotency_key = ?;

-- name: CancelGameAllocation :execrows
UPDATE game_allocations
SET state = 'cancelled', updated_at = ?
WHERE id = ? AND requester_id = ? AND state IN ('pending_acceptance', 'queued');

-- name: CompleteGameAllocation :execrows
UPDATE game_allocations
SET state = 'allocated', opponent_id = ?, game_id = ?, updated_at = ?
WHERE id = ? AND state IN ('pending_acceptance', 'queued');
