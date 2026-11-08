package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"letterpress/backend/internal/game"
	"letterpress/backend/internal/store/sqlite"
)

const allocationIDBytes = 16

var (
	ErrInvalidAllocation      = errors.New("invalid game allocation")
	ErrAllocationNotFound     = errors.New("game allocation not found")
	ErrIdempotencyConflict    = errors.New("idempotency key conflicts with existing allocation")
	ErrInvalidAllocationState = errors.New("invalid game allocation state")
)

// GameAllocation is the public durable allocation resource.
type GameAllocation struct {
	ID            string     `json:"id"`
	RequesterID   string     `json:"requesterId"`
	OpponentID    string     `json:"opponentId,omitempty"`
	Mode          string     `json:"mode"`
	State         string     `json:"state"`
	GameID        string     `json:"gameId,omitempty"`
	FailureReason string     `json:"failureReason,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// CreateAllocation creates an idempotent durable request for the authenticated player.
func (service *Service) CreateAllocation(
	ctx context.Context,
	token, idempotencyKey, mode, opponentID string,
) (GameAllocation, bool, error) {
	requester, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return GameAllocation{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		return GameAllocation{}, false, ErrInvalidAllocation
	}
	state := ""
	switch mode {
	case "specified":
		if opponentID == "" || opponentID == requester.ID {
			return GameAllocation{}, false, ErrInvalidAllocation
		}
		state = "pending_acceptance"
	case "random", "bot":
		if opponentID != "" {
			return GameAllocation{}, false, ErrInvalidAllocation
		}
		state = "queued"
	default:
		return GameAllocation{}, false, ErrInvalidAllocation
	}
	allocationID, err := service.randomToken(allocationIDBytes)
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("generate allocation ID: %w", err)
	}
	now := service.now().UTC()
	stored, created, err := service.store.CreateGameAllocation(ctx, sqlite.GameAllocation{
		ID: allocationID, RequesterID: requester.ID, OpponentID: opponentID,
		Mode: mode, State: state, IdempotencyKey: idempotencyKey, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("persist game allocation: %w", err)
	}
	if stored.Mode != mode || stored.OpponentID != opponentID {
		return GameAllocation{}, false, ErrIdempotencyConflict
	}
	return allocationView(stored), created, nil
}

// GetAllocation returns a private allocation only to its participants.
func (service *Service) GetAllocation(ctx context.Context, token, allocationID string) (GameAllocation, error) {
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return GameAllocation{}, err
	}
	stored, err := service.store.GetGameAllocation(ctx, allocationID)
	if errors.Is(err, sql.ErrNoRows) {
		return GameAllocation{}, ErrAllocationNotFound
	}
	if err != nil {
		return GameAllocation{}, fmt.Errorf("load game allocation: %w", err)
	}
	if player.ID != stored.RequesterID && player.ID != stored.OpponentID {
		return GameAllocation{}, ErrAllocationNotFound
	}
	return allocationView(stored), nil
}

// CancelAllocation cancels only the requester's pending or queued allocation.
func (service *Service) CancelAllocation(ctx context.Context, token, allocationID string) (GameAllocation, error) {
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return GameAllocation{}, err
	}
	stored, err := service.store.GetGameAllocation(ctx, allocationID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && stored.RequesterID != player.ID) {
		return GameAllocation{}, ErrAllocationNotFound
	}
	if err != nil {
		return GameAllocation{}, fmt.Errorf("authorize allocation cancellation: %w", err)
	}
	stored, err = service.store.CancelGameAllocation(ctx, allocationID, player.ID, service.now().UTC())
	if errors.Is(err, sqlite.ErrAllocationTransition) {
		return GameAllocation{}, ErrInvalidAllocationState
	}
	if err != nil {
		return GameAllocation{}, fmt.Errorf("cancel game allocation: %w", err)
	}
	return allocationView(stored), nil
}

// CompleteAllocation atomically creates one game with requester in seat one and opponent in seat two.
// It is an internal allocation-worker boundary, not a public HTTP action in this ticket.
func (service *Service) CompleteAllocation(ctx context.Context, allocationID, opponentID string) (GameAllocation, error) {
	stored, err := service.store.GetGameAllocation(ctx, allocationID)
	if errors.Is(err, sql.ErrNoRows) {
		return GameAllocation{}, ErrAllocationNotFound
	}
	if err != nil {
		return GameAllocation{}, fmt.Errorf("load allocation for completion: %w", err)
	}
	if stored.State == "allocated" {
		return allocationView(stored), nil
	}
	if stored.State != "pending_acceptance" && stored.State != "queued" {
		return GameAllocation{}, ErrInvalidAllocationState
	}
	if stored.OpponentID != "" {
		if opponentID != "" && opponentID != stored.OpponentID {
			return GameAllocation{}, ErrInvalidAllocation
		}
		opponentID = stored.OpponentID
	}
	if opponentID == "" || opponentID == stored.RequesterID {
		return GameAllocation{}, ErrInvalidAllocation
	}
	domainGame, err := game.New(game.DefaultWidth, game.DefaultHeight, service.generator, service.validator)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("generate allocated board: %w", err)
	}
	gameID, err := service.randomToken(gameIDBytes)
	if err != nil {
		return GameAllocation{}, fmt.Errorf("generate allocated game ID: %w", err)
	}
	now := service.now().UTC()
	board := domainGame.Board()
	gameRow := sqlite.Game{
		ID: gameID, BoardLetters: board.Letters(), TileOwners: make([]byte, board.Len()),
		CurrentPlayer: int64(domainGame.CurrentPlayer()), Status: string(domainGame.Status()),
		Version: 0, CreatedAt: now, UpdatedAt: now,
	}
	seats := [2]sqlite.GameSeat{
		{GameID: gameID, Seat: 1, PlayerID: stored.RequesterID, CreatedAt: now},
		{GameID: gameID, Seat: 2, PlayerID: opponentID, CreatedAt: now},
	}
	completed, err := service.store.CompleteGameAllocation(ctx, allocationID, opponentID, gameRow, seats)
	if errors.Is(err, sqlite.ErrAllocationTransition) {
		return GameAllocation{}, ErrInvalidAllocationState
	}
	if errors.Is(err, sqlite.ErrAllocationParticipant) {
		return GameAllocation{}, ErrInvalidAllocation
	}
	if err != nil {
		return GameAllocation{}, fmt.Errorf("complete allocation: %w", err)
	}
	return allocationView(completed), nil
}

func allocationView(stored sqlite.GameAllocation) GameAllocation {
	return GameAllocation{
		ID: stored.ID, RequesterID: stored.RequesterID, OpponentID: stored.OpponentID,
		Mode: stored.Mode, State: stored.State, GameID: stored.GameID,
		FailureReason: stored.FailureReason, ExpiresAt: stored.ExpiresAt,
		CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
	}
}
