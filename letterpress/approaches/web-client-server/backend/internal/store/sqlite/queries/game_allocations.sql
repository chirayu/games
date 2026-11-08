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

-- name: ExpireGameAllocation :execrows
UPDATE game_allocations
SET state = 'expired', updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND state IN ('pending_acceptance', 'queued')
  AND expires_at IS NOT NULL
  AND expires_at <= sqlc.arg(now);

-- name: ExpireDueGameAllocations :execrows
UPDATE game_allocations
SET state = 'expired', updated_at = sqlc.arg(now)
WHERE state IN ('pending_acceptance', 'queued')
  AND expires_at IS NOT NULL
  AND expires_at <= sqlc.arg(now);

-- name: DeclineSpecifiedAllocation :execrows
UPDATE game_allocations
SET state = 'declined', updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND opponent_id = sqlc.arg(opponent_id)
  AND mode = 'specified'
  AND state = 'pending_acceptance'
  AND expires_at > sqlc.arg(updated_at);

-- name: ListPendingSpecifiedChallenges :many
SELECT
    a.id, a.requester_id, requester.display_name AS requester_name,
    a.opponent_id, opponent.display_name AS opponent_name,
    a.mode, a.state, a.expires_at, a.created_at, a.updated_at
FROM game_allocations AS a
JOIN registered_players AS requester ON requester.id = a.requester_id
JOIN registered_players AS opponent ON opponent.id = a.opponent_id
WHERE a.mode = 'specified'
  AND a.state = 'pending_acceptance'
  AND a.expires_at > sqlc.arg(now)
  AND (a.requester_id = sqlc.arg(player_id) OR a.opponent_id = sqlc.arg(player_id))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(result_limit);
