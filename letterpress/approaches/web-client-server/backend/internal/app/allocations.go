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

const specifiedChallengeLifetime = 7 * 24 * time.Hour

var (
	ErrInvalidAllocation      = errors.New("invalid game allocation")
	ErrAllocationNotFound     = errors.New("game allocation not found")
	ErrIdempotencyConflict    = errors.New("idempotency key conflicts with existing allocation")
	ErrInvalidAllocationState = errors.New("invalid game allocation state")
)

// GameAllocation is the public durable allocation resource.
type GameAllocation struct {
	ID                   string     `json:"id"`
	RequesterID          string     `json:"requesterId"`
	OpponentID           string     `json:"opponentId,omitempty"`
	Mode                 string     `json:"mode"`
	AllowedOpponentTypes []string   `json:"allowedOpponentTypes"`
	State                string     `json:"state"`
	GameID               string     `json:"gameId,omitempty"`
	FailureReason        string     `json:"failureReason,omitempty"`
	ExpiresAt            *time.Time `json:"expiresAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

// BotProfilePreference selects a supported managed profile by safe labels, never player ID.
type BotProfilePreference struct {
	Style                 string `json:"style"`
	EnglishKnowledgeLevel string `json:"englishKnowledgeLevel"`
	SkillLevel            string `json:"skillLevel"`
}

// ChallengeSummary is a participant-scoped pending specified challenge.
type ChallengeSummary struct {
	AllocationID string       `json:"allocationId"`
	Direction    string       `json:"direction"`
	Requester    PublicPlayer `json:"requester"`
	Opponent     PublicPlayer `json:"opponent"`
	ExpiresAt    time.Time    `json:"expiresAt"`
	CreatedAt    time.Time    `json:"createdAt"`
}

// LobbyAllocations contains every pending allocation needed to restore start-game UI.
type LobbyAllocations struct {
	Items            []ChallengeSummary `json:"items"`
	RandomAllocation *GameAllocation    `json:"randomAllocation,omitempty"`
	BotAllocation    *GameAllocation    `json:"botAllocation,omitempty"`
}

// CreateAllocation creates an idempotent durable request for the authenticated player.
func (service *Service) CreateAllocation(
	ctx context.Context, token, idempotencyKey, mode, opponentID string,
	allowedOpponentTypes []string, botPreference BotProfilePreference,
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
	allowedOpponentType := ""
	var expiresAt *time.Time
	switch mode {
	case "specified":
		if opponentID == "" || opponentID == requester.ID {
			return GameAllocation{}, false, ErrInvalidAllocation
		}
		opponent, err := service.store.GetRegisteredPlayerByID(ctx, opponentID)
		if errors.Is(err, sql.ErrNoRows) {
			return GameAllocation{}, false, ErrInvalidAllocation
		} else if err != nil {
			return GameAllocation{}, false, fmt.Errorf("resolve allocation opponent: %w", err)
		}
		if opponent.PlayerType != "human" {
			return GameAllocation{}, false, ErrInvalidAllocation
		}
		state = "pending_acceptance"
		allowedOpponentType = "human"
	case "random", "bot":
		if opponentID != "" {
			return GameAllocation{}, false, ErrInvalidAllocation
		}
		state = "queued"
		if mode == "random" {
			if len(allowedOpponentTypes) != 1 || allowedOpponentTypes[0] != "human" {
				return GameAllocation{}, false, ErrInvalidAllocation
			}
			allowedOpponentType = "human"
		} else {
			bot, selectErr := service.selectManagedBot(ctx, botPreference)
			if selectErr != nil {
				return GameAllocation{}, false, selectErr
			}
			opponentID = bot.Player.ID
			allowedOpponentType = "bot"
		}
	default:
		return GameAllocation{}, false, ErrInvalidAllocation
	}
	allocationID, err := service.randomToken(allocationIDBytes)
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("generate allocation ID: %w", err)
	}
	now := service.now().UTC()
	if mode == "specified" {
		expires := now.Add(specifiedChallengeLifetime)
		expiresAt = &expires
	}
	stored, created, err := service.store.CreateGameAllocation(ctx, sqlite.GameAllocation{
		ID: allocationID, RequesterID: requester.ID, OpponentID: opponentID,
		Mode: mode, AllowedOpponentType: allowedOpponentType,
		State: state, IdempotencyKey: idempotencyKey,
		ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return GameAllocation{}, false, fmt.Errorf("persist game allocation: %w", err)
	}
	if stored.Mode != mode || stored.OpponentID != opponentID || stored.AllowedOpponentType != allowedOpponentType {
		return GameAllocation{}, false, ErrIdempotencyConflict
	}
	if mode == "random" {
		matched, _, err := service.matchRandomAllocation(ctx, stored)
		if err != nil {
			return GameAllocation{}, false, err
		}
		stored = matched
	} else if mode == "bot" {
		completed, completeErr := service.CompleteAllocation(ctx, stored.ID, stored.OpponentID)
		if completeErr != nil {
			return GameAllocation{}, false, completeErr
		}
		return completed, created, nil
	}
	return allocationView(stored), created, nil
}

func (service *Service) selectManagedBot(ctx context.Context, preference BotProfilePreference) (sqlite.ManagedBot, error) {
	if preference.Style == "" || preference.EnglishKnowledgeLevel == "" || preference.SkillLevel == "" {
		return sqlite.ManagedBot{}, ErrInvalidAllocation
	}
	bots, err := service.store.ListLatestEnabledBotProfiles(ctx)
	if err != nil {
		return sqlite.ManagedBot{}, fmt.Errorf("list selectable bots: %w", err)
	}
	for _, bot := range bots {
		if bot.Profile.Style == preference.Style &&
			bot.Profile.EnglishKnowledgeLevel == preference.EnglishKnowledgeLevel &&
			bot.Profile.SkillLevel == preference.SkillLevel {
			return bot, nil
		}
	}
	return sqlite.ManagedBot{}, ErrInvalidAllocation
}

func (service *Service) matchRandomAllocation(ctx context.Context, allocation sqlite.GameAllocation) (sqlite.GameAllocation, bool, error) {
	if allocation.State == "allocated" {
		return allocation, true, nil
	}
	domainGame, err := game.New(game.DefaultWidth, game.DefaultHeight, service.generator, service.validator)
	if err != nil {
		return sqlite.GameAllocation{}, false, fmt.Errorf("generate matched board: %w", err)
	}
	gameID, err := service.randomToken(gameIDBytes)
	if err != nil {
		return sqlite.GameAllocation{}, false, fmt.Errorf("generate matched game ID: %w", err)
	}
	now := service.now().UTC()
	board := domainGame.Board()
	matched, allocated, err := service.store.MatchRandomAllocation(ctx, allocation.ID, sqlite.Game{
		ID: gameID, BoardLetters: board.Letters(), TileOwners: make([]byte, board.Len()),
		CurrentPlayer: int64(domainGame.CurrentPlayer()), Status: string(domainGame.Status()),
		Version: 0, CreatedAt: now, UpdatedAt: now,
	})
	if errors.Is(err, sqlite.ErrAllocationTransition) {
		return sqlite.GameAllocation{}, false, ErrInvalidAllocationState
	}
	if err != nil {
		return sqlite.GameAllocation{}, false, fmt.Errorf("match random allocation: %w", err)
	}
	return matched, allocated, nil
}

// GetAllocation returns a private allocation only to its participants.
func (service *Service) GetAllocation(ctx context.Context, token, allocationID string) (GameAllocation, error) {
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return GameAllocation{}, err
	}
	stored, err := service.store.RefreshGameAllocation(ctx, allocationID, service.now().UTC())
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
	stored, err := service.store.RefreshGameAllocation(ctx, allocationID, service.now().UTC())
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

// AcceptSpecifiedChallenge lets only the intended opponent allocate the game.
func (service *Service) AcceptSpecifiedChallenge(ctx context.Context, token, allocationID string) (GameAllocation, error) {
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return GameAllocation{}, err
	}
	stored, err := service.store.RefreshGameAllocation(ctx, allocationID, service.now().UTC())
	if errors.Is(err, sql.ErrNoRows) || (err == nil && stored.OpponentID != player.ID) {
		return GameAllocation{}, ErrAllocationNotFound
	}
	if err != nil {
		return GameAllocation{}, fmt.Errorf("authorize challenge acceptance: %w", err)
	}
	if stored.Mode != "specified" || stored.State != "pending_acceptance" {
		return GameAllocation{}, ErrInvalidAllocationState
	}
	return service.CompleteAllocation(ctx, allocationID, player.ID)
}

// DeclineSpecifiedChallenge lets only the intended opponent decline a live challenge.
func (service *Service) DeclineSpecifiedChallenge(ctx context.Context, token, allocationID string) (GameAllocation, error) {
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return GameAllocation{}, err
	}
	stored, err := service.store.RefreshGameAllocation(ctx, allocationID, service.now().UTC())
	if errors.Is(err, sql.ErrNoRows) || (err == nil && stored.OpponentID != player.ID) {
		return GameAllocation{}, ErrAllocationNotFound
	}
	if err != nil {
		return GameAllocation{}, fmt.Errorf("authorize challenge decline: %w", err)
	}
	if stored.Mode != "specified" || stored.State != "pending_acceptance" {
		return GameAllocation{}, ErrInvalidAllocationState
	}
	stored, err = service.store.DeclineSpecifiedAllocation(ctx, allocationID, player.ID, service.now().UTC())
	if errors.Is(err, sqlite.ErrAllocationTransition) {
		return GameAllocation{}, ErrInvalidAllocationState
	}
	if err != nil {
		return GameAllocation{}, fmt.Errorf("decline specified challenge: %w", err)
	}
	return allocationView(stored), nil
}

// ListPendingChallenges returns incoming and outgoing live specified challenges.
func (service *Service) ListPendingChallenges(ctx context.Context, token string) ([]ChallengeSummary, error) {
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return nil, err
	}
	rows, err := service.store.ListPendingSpecifiedChallenges(ctx, player.ID, service.now().UTC(), 50)
	if err != nil {
		return nil, fmt.Errorf("list pending challenges: %w", err)
	}
	items := make([]ChallengeSummary, 0, len(rows))
	for _, row := range rows {
		direction := "incoming"
		if row.RequesterID == player.ID {
			direction = "outgoing"
		}
		items = append(items, ChallengeSummary{
			AllocationID: row.AllocationID, Direction: direction,
			Requester: PublicPlayer{ID: row.RequesterID, Name: row.RequesterName, Type: "human"},
			Opponent:  PublicPlayer{ID: row.OpponentID, Name: row.OpponentName, Type: "human"},
			ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt,
		})
	}
	return items, nil
}

// GetLobbyAllocations restores pending specified challenges and a queued random search.
func (service *Service) GetLobbyAllocations(ctx context.Context, token string) (LobbyAllocations, error) {
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return LobbyAllocations{}, err
	}
	items, err := service.ListPendingChallenges(ctx, token)
	if err != nil {
		return LobbyAllocations{}, err
	}
	result := LobbyAllocations{Items: items}
	random, err := service.store.GetQueuedRandomAllocation(ctx, player.ID, "human", service.now().UTC())
	if err == nil {
		view := allocationView(random)
		result.RandomAllocation = &view
	} else if !errors.Is(err, sql.ErrNoRows) {
		return LobbyAllocations{}, fmt.Errorf("load queued random allocation: %w", err)
	}
	queuedBot, err := service.store.GetQueuedBotAllocation(ctx, player.ID)
	if err == nil {
		allocation, completeErr := service.CompleteAllocation(ctx, queuedBot.ID, queuedBot.OpponentID)
		if completeErr != nil {
			return LobbyAllocations{}, fmt.Errorf("recover queued bot allocation: %w", completeErr)
		}
		result.BotAllocation = &allocation
	} else if !errors.Is(err, sql.ErrNoRows) {
		return LobbyAllocations{}, fmt.Errorf("load queued bot allocation: %w", err)
	}
	return result, nil
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
	if stored.Mode == "bot" {
		managed, profileErr := service.store.GetLatestEnabledBotProfile(ctx, opponentID)
		if profileErr != nil || managed.Player.PlayerType != "bot" {
			return GameAllocation{}, ErrInvalidAllocation
		}
		seats[1].BotProfile = &sqlite.BotProfileSnapshot{
			ConfigurationVersion: managed.Profile.ConfigurationVersion,
			Style:                managed.Profile.Style, EnglishKnowledgeLevel: managed.Profile.EnglishKnowledgeLevel,
			SkillLevel: managed.Profile.SkillLevel,
		}
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
		AllowedOpponentTypes: []string{stored.AllowedOpponentType},
		FailureReason:        stored.FailureReason, ExpiresAt: stored.ExpiresAt,
		CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
	}
}
