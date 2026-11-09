// Package app coordinates Letterpress domain operations and persistence.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"letterpress/backend/internal/game"
	"letterpress/backend/internal/store/sqlite"
)

const (
	gameIDBytes   = 16
	playerIDBytes = 16
	secretBytes   = 32
)

var (
	// ErrGameNotFound means the requested game does not exist.
	ErrGameNotFound = errors.New("game not found")
	// ErrUnauthorized means the registered player is not authenticated or does
	// not hold a seat in the requested game.
	ErrUnauthorized = errors.New("unauthorized game access")
	// ErrVersionConflict means the client submitted against stale game state.
	ErrVersionConflict = errors.New("game version conflict")
)

// Store is the persistence needed by the application service.
type Store interface {
	Health(context.Context) error
	CreateRegisteredPlayerSession(context.Context, sqlite.RegisteredPlayer, sqlite.PlayerSession) (sqlite.RegisteredPlayer, bool, error)
	GetRegisteredPlayerBySessionHash(context.Context, [32]byte, time.Time) (sqlite.RegisteredPlayer, error)
	AuthenticateBotRuntime(context.Context, [32]byte) (sqlite.RegisteredPlayer, error)
	GetRegisteredPlayerByID(context.Context, string) (sqlite.RegisteredPlayer, error)
	GetLatestEnabledBotProfile(context.Context, string) (sqlite.ManagedBot, error)
	ListLatestEnabledBotProfiles(context.Context) ([]sqlite.ManagedBot, error)
	DeletePlayerSession(context.Context, [32]byte) error
	CreateGameAllocation(context.Context, sqlite.GameAllocation) (sqlite.GameAllocation, bool, error)
	GetGameAllocation(context.Context, string) (sqlite.GameAllocation, error)
	RefreshGameAllocation(context.Context, string, time.Time) (sqlite.GameAllocation, error)
	CancelGameAllocation(context.Context, string, string, time.Time) (sqlite.GameAllocation, error)
	DeclineSpecifiedAllocation(context.Context, string, string, time.Time) (sqlite.GameAllocation, error)
	ListPendingSpecifiedChallenges(context.Context, string, time.Time, int) ([]sqlite.SpecifiedChallenge, error)
	GetQueuedRandomAllocation(context.Context, string, string, time.Time) (sqlite.GameAllocation, error)
	GetQueuedBotAllocation(context.Context, string) (sqlite.GameAllocation, error)
	MatchRandomAllocation(context.Context, string, sqlite.Game) (sqlite.GameAllocation, bool, error)
	CompleteGameAllocation(context.Context, string, string, sqlite.Game, [2]sqlite.GameSeat) (sqlite.GameAllocation, error)
	CreateGame(context.Context, sqlite.Game, [2]sqlite.GameSeat) error
	GetGame(context.Context, string) (sqlite.Game, error)
	ListMoves(context.Context, string) ([]sqlite.Move, error)
	GetGameSeatByPlayer(context.Context, string, string) (sqlite.GameSeat, error)
	GetGameSeatByNumber(context.Context, string, int64) (sqlite.GameSeat, error)
	ListActiveGamesForPlayer(context.Context, string, time.Time, string, int) ([]sqlite.ActiveGameSummary, error)
	SearchRegisteredPlayers(context.Context, string, string, string, string, int) ([]sqlite.RegisteredPlayer, error)
	TransactMove(context.Context, string, string, int64, sqlite.MoveOperation) error
}

// GameView is the authenticated, derived state returned to a player.
type GameView struct {
	ID                   string               `json:"id"`
	Width                int                  `json:"width"`
	Height               int                  `json:"height"`
	Tiles                []GameTile           `json:"tiles"`
	Scores               Scores               `json:"scores"`
	ActivePlayer         int64                `json:"activePlayer"`
	ActivePlayerMetadata ActivePlayerMetadata `json:"activePlayerMetadata"`
	PlayerSeat           int64                `json:"playerSeat"`
	Version              int64                `json:"version"`
	Status               string               `json:"status"`
	Moves                []MoveView           `json:"moves"`
	Result               *GameResult          `json:"result,omitempty"`
}

// ActivePlayerMetadata lets every client represent a normal bot-thinking turn.
type ActivePlayerMetadata struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	PlayerType  string `json:"playerType"`
	Thinking    bool   `json:"thinking"`
}

// GameCredential distinguishes public browser sessions from managed bot runtime credentials.
type GameCredential struct {
	browserToken string
	botToken     string
}

// BrowserGameCredential authenticates the existing session-cookie principal.
func BrowserGameCredential(token string) GameCredential { return GameCredential{browserToken: token} }

// BotGameCredential authenticates a managed bot bearer principal.
func BotGameCredential(token string) GameCredential { return GameCredential{botToken: token} }

// MoveView is an accepted move in turn order. The server derives every word.
type MoveView struct {
	Player      int64  `json:"player"`
	TurnNumber  int64  `json:"turnNumber"`
	TileIndexes []int  `json:"tileIndexes"`
	Word        string `json:"word"`
}

// GameResult describes the immutable outcome of a completed game.
type GameResult struct {
	PlayerOneScore int   `json:"playerOneScore"`
	PlayerTwoScore int   `json:"playerTwoScore"`
	Winner         int64 `json:"winner"`
	Tie            bool  `json:"tie"`
}

// GameTile is one row-major tile in an authenticated game response.
type GameTile struct {
	Index     int    `json:"index"`
	Letter    string `json:"letter"`
	Owner     int64  `json:"owner"`
	Protected bool   `json:"protected"`
}

// Scores contains the current number of tiles owned by each player.
type Scores struct {
	PlayerOne int `json:"playerOne"`
	PlayerTwo int `json:"playerTwo"`
}

// Service coordinates registered players and authoritative game operations.
type Service struct {
	store     Store
	random    io.Reader
	now       func() time.Time
	generator game.BoardGenerator
	validator game.WordValidator
}

// NewService returns the application service with production-safe randomness.
func NewService(store Store) *Service {
	return NewServiceWithDependencies(store, game.NewPlayableBoardGenerator(), game.EnglishDictionaryValidator())
}

// NewServiceWithBoardGenerator supplies a deterministic generator for tests.
// Production callers should use NewService.
func NewServiceWithBoardGenerator(store Store, generator game.BoardGenerator) *Service {
	return NewServiceWithDependencies(store, generator, game.EnglishDictionaryValidator())
}

// NewServiceWithDependencies allows focused tests to supply deterministic
// generation and validation strategies.
func NewServiceWithDependencies(store Store, generator game.BoardGenerator, validator game.WordValidator) *Service {
	return NewServiceWithDependenciesAndClock(store, generator, validator, time.Now)
}

// NewServiceWithDependenciesAndClock allows HTTP and persistence tests to use
// one deterministic clock for sessions and game operations.
func NewServiceWithDependenciesAndClock(
	store Store,
	generator game.BoardGenerator,
	validator game.WordValidator,
	now func() time.Time,
) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, random: rand.Reader, now: now, generator: generator, validator: validator}
}

// Ready checks whether the authoritative store can serve requests.
func (service *Service) Ready(ctx context.Context) error {
	return service.store.Health(ctx)
}

// CreateGame creates a default game assigned to two registered players. Public
// callers receive games through the allocation service rather than HTTP game
// creation.
func (service *Service) CreateGame(ctx context.Context, playerOneID, playerTwoID string) (string, error) {
	if playerOneID == "" || playerTwoID == "" || playerOneID == playerTwoID {
		return "", errors.New("two distinct registered players are required")
	}
	domainGame, err := game.New(game.DefaultWidth, game.DefaultHeight, service.generator, service.validator)
	if err != nil {
		return "", fmt.Errorf("generate game board: %w", err)
	}

	gameID, err := service.randomToken(gameIDBytes)
	if err != nil {
		return "", fmt.Errorf("generate game ID: %w", err)
	}

	createdAt := service.now().UTC()
	board := domainGame.Board()
	storedGame := sqlite.Game{
		ID:            gameID,
		BoardLetters:  board.Letters(),
		TileOwners:    make([]byte, board.Len()),
		CurrentPlayer: int64(domainGame.CurrentPlayer()),
		Status:        string(domainGame.Status()),
		Version:       0,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}
	seats := [2]sqlite.GameSeat{
		{GameID: gameID, Seat: int64(game.PlayerOne), PlayerID: playerOneID, CreatedAt: createdAt},
		{GameID: gameID, Seat: int64(game.PlayerTwo), PlayerID: playerTwoID, CreatedAt: createdAt},
	}
	if err := service.store.CreateGame(ctx, storedGame, seats); err != nil {
		return "", fmt.Errorf("persist game: %w", err)
	}
	return gameID, nil
}

// GetGame returns the current authoritative state for an authenticated player.
func (service *Service) GetGame(ctx context.Context, gameID, token string) (GameView, error) {
	return service.GetGameWithCredential(ctx, gameID, BrowserGameCredential(token))
}

// GetGameWithCredential serves both human and bot principals through one game contract.
func (service *Service) GetGameWithCredential(ctx context.Context, gameID string, credential GameCredential) (GameView, error) {
	playerID, err := service.authenticateGamePrincipal(ctx, credential)
	if err != nil {
		return GameView{}, err
	}
	seat, err := service.store.GetGameSeatByPlayer(ctx, gameID, playerID)
	if errors.Is(err, sql.ErrNoRows) {
		return GameView{}, ErrUnauthorized
	}
	if err != nil {
		return GameView{}, fmt.Errorf("authorize game player: %w", err)
	}
	stored, err := service.store.GetGame(ctx, gameID)
	if errors.Is(err, sql.ErrNoRows) {
		return GameView{}, ErrGameNotFound
	}
	if err != nil {
		return GameView{}, fmt.Errorf("get game: %w", err)
	}
	activeSeat, err := service.store.GetGameSeatByNumber(ctx, gameID, stored.CurrentPlayer)
	if err != nil {
		return GameView{}, fmt.Errorf("get active game seat: %w", err)
	}
	activeIdentity, err := service.store.GetRegisteredPlayerByID(ctx, activeSeat.PlayerID)
	if err != nil {
		return GameView{}, fmt.Errorf("get active player identity: %w", err)
	}

	if len(stored.BoardLetters) != game.DefaultWidth*game.DefaultHeight || len(stored.TileOwners) != len(stored.BoardLetters) {
		return GameView{}, fmt.Errorf("stored game %q has invalid board dimensions", gameID)
	}
	if stored.CurrentPlayer != int64(game.PlayerOne) && stored.CurrentPlayer != int64(game.PlayerTwo) {
		return GameView{}, fmt.Errorf("stored game %q has invalid active player", gameID)
	}
	if stored.Status != string(game.StatusActive) && stored.Status != string(game.StatusCompleted) {
		return GameView{}, fmt.Errorf("stored game %q has invalid status", gameID)
	}
	owners := make([]game.Player, len(stored.TileOwners))
	for index, owner := range stored.TileOwners {
		owners[index] = game.Player(owner)
	}
	storedMoves, err := service.store.ListMoves(ctx, gameID)
	if err != nil {
		return GameView{}, fmt.Errorf("list game moves: %w", err)
	}
	domainMoves, moveViews, err := restoreMoves(storedMoves)
	if err != nil {
		return GameView{}, fmt.Errorf("read game moves: %w", err)
	}
	restored, err := game.Restore(game.Snapshot{
		Width: game.DefaultWidth, Height: game.DefaultHeight,
		Letters: stored.BoardLetters, Owners: owners,
		CurrentPlayer: game.Player(stored.CurrentPlayer), Status: game.Status(stored.Status), Moves: domainMoves,
	}, service.validator)
	if err != nil {
		return GameView{}, fmt.Errorf("restore stored game: %w", err)
	}
	analysis, err := game.AnalyzeOwnership(game.DefaultWidth, game.DefaultHeight, owners)
	if err != nil {
		return GameView{}, fmt.Errorf("analyze stored game: %w", err)
	}

	tiles := make([]GameTile, len(stored.BoardLetters))
	for index, letter := range stored.BoardLetters {
		if letter < 'A' || letter > 'Z' {
			return GameView{}, fmt.Errorf("stored game %q has invalid letter at tile %d", gameID, index)
		}
		tiles[index] = GameTile{
			Index: index, Letter: string([]byte{letter}), Owner: int64(owners[index]), Protected: analysis.Protected[index],
		}
	}
	view := GameView{
		ID: gameID, Width: game.DefaultWidth, Height: game.DefaultHeight, Tiles: tiles,
		Scores:       Scores{PlayerOne: analysis.PlayerOneScore, PlayerTwo: analysis.PlayerTwoScore},
		ActivePlayer: stored.CurrentPlayer,
		ActivePlayerMetadata: ActivePlayerMetadata{
			ID: activeIdentity.ID, DisplayName: activeIdentity.DisplayName, PlayerType: activeIdentity.PlayerType,
			Thinking: activeIdentity.PlayerType == "bot" && stored.Status == string(game.StatusActive),
		},
		PlayerSeat: seat.Seat, Version: stored.Version, Status: stored.Status,
		Moves: moveViews,
	}
	if outcome, ok := restored.Outcome(); ok {
		view.Result = &GameResult{
			PlayerOneScore: outcome.PlayerOneScore, PlayerTwoScore: outcome.PlayerTwoScore,
			Winner: int64(outcome.Winner), Tie: outcome.Tie,
		}
	}
	return view, nil
}

// SubmitMove authenticates and applies one versioned move transaction.
func (service *Service) SubmitMove(
	ctx context.Context,
	gameID, token string,
	tileIndexes []int,
	expectedVersion int64,
) (GameView, error) {
	return service.SubmitMoveWithCredential(ctx, gameID, BrowserGameCredential(token), tileIndexes, expectedVersion)
}

// SubmitMoveWithCredential sends human and bot moves through the same authoritative transaction.
func (service *Service) SubmitMoveWithCredential(
	ctx context.Context,
	gameID string,
	credential GameCredential,
	tileIndexes []int,
	expectedVersion int64,
) (GameView, error) {
	playerID, err := service.authenticateGamePrincipal(ctx, credential)
	if err != nil {
		return GameView{}, err
	}
	err = service.store.TransactMove(ctx, gameID, playerID, expectedVersion, func(
		stored sqlite.Game,
		seat sqlite.GameSeat,
		storedMoves []sqlite.Move,
	) (sqlite.Move, sqlite.GameState, error) {
		owners := make([]game.Player, len(stored.TileOwners))
		for index, owner := range stored.TileOwners {
			owners[index] = game.Player(owner)
		}
		moves, _, err := restoreMoves(storedMoves)
		if err != nil {
			return sqlite.Move{}, sqlite.GameState{}, err
		}
		restored, err := game.Restore(game.Snapshot{
			Width: game.DefaultWidth, Height: game.DefaultHeight,
			Letters: stored.BoardLetters, Owners: owners,
			CurrentPlayer: game.Player(stored.CurrentPlayer), Status: game.Status(stored.Status), Moves: moves,
		}, service.validator)
		if err != nil {
			return sqlite.Move{}, sqlite.GameState{}, fmt.Errorf("restore game for move: %w", err)
		}
		accepted, err := restored.Play(game.Player(seat.Seat), tileIndexes)
		if err != nil {
			return sqlite.Move{}, sqlite.GameState{}, err
		}
		persistedIndexes := make([]int64, len(accepted.TileIndexes))
		for index, tileIndex := range accepted.TileIndexes {
			persistedIndexes[index] = int64(tileIndex)
		}
		ownersAfter := make([]byte, restored.Board().Len())
		for index, tile := range restored.Board().Tiles() {
			ownersAfter[index] = byte(tile.Owner)
		}
		playedAt := service.now().UTC()
		return sqlite.Move{
				GameID: gameID, Player: int64(accepted.Player), TurnNumber: int64(accepted.TurnNumber),
				TileIndexes: persistedIndexes, Word: accepted.Word, CreatedAt: playedAt,
			}, sqlite.GameState{
				TileOwners: ownersAfter, CurrentPlayer: int64(restored.CurrentPlayer()),
				Status: string(restored.Status()), UpdatedAt: playedAt,
			}, nil
	})
	switch {
	case errors.Is(err, sqlite.ErrGameNotFound):
		return GameView{}, ErrGameNotFound
	case errors.Is(err, sqlite.ErrPlayerNotFound):
		return GameView{}, ErrUnauthorized
	case errors.Is(err, sqlite.ErrVersionConflict):
		return GameView{}, ErrVersionConflict
	case err != nil:
		return GameView{}, err
	}
	return service.GetGameWithCredential(ctx, gameID, credential)
}

func (service *Service) authenticateGamePrincipal(ctx context.Context, credential GameCredential) (string, error) {
	if credential.botToken != "" && credential.browserToken == "" {
		hash, ok := credentialHash(credential.botToken)
		if !ok {
			return "", ErrUnauthorized
		}
		player, err := service.store.AuthenticateBotRuntime(ctx, hash)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrUnauthorized
		}
		if err != nil {
			return "", fmt.Errorf("authenticate bot runtime: %w", err)
		}
		return player.ID, nil
	}
	if credential.browserToken != "" && credential.botToken == "" {
		player, err := service.CurrentPlayer(ctx, credential.browserToken)
		if errors.Is(err, ErrInvalidPlayerSession) {
			return "", ErrUnauthorized
		}
		return player.ID, err
	}
	return "", ErrUnauthorized
}

func restoreMoves(storedMoves []sqlite.Move) ([]game.Move, []MoveView, error) {
	domainMoves := make([]game.Move, 0, len(storedMoves))
	views := make([]MoveView, 0, len(storedMoves))
	for _, storedMove := range storedMoves {
		indexes := make([]int, len(storedMove.TileIndexes))
		for index, tileIndex := range storedMove.TileIndexes {
			indexes[index] = int(tileIndex)
			if int64(indexes[index]) != tileIndex {
				return nil, nil, fmt.Errorf("stored tile index %d overflows int", tileIndex)
			}
		}
		domainMoves = append(domainMoves, game.Move{
			Player: game.Player(storedMove.Player), TileIndexes: append([]int(nil), indexes...),
			Word: storedMove.Word, TurnNumber: int(storedMove.TurnNumber),
		})
		views = append(views, MoveView{
			Player: storedMove.Player, TurnNumber: storedMove.TurnNumber,
			TileIndexes: indexes, Word: storedMove.Word,
		})
	}
	return domainMoves, views, nil
}

func (service *Service) playerToken() (string, [32]byte, error) {
	encoded, raw, err := service.randomTokenBytes(secretBytes)
	if err != nil {
		return "", [32]byte{}, err
	}
	return encoded, sha256.Sum256(raw), nil
}

func (service *Service) randomToken(size int) (string, error) {
	encoded, _, err := service.randomTokenBytes(size)
	return encoded, err
}

func (service *Service) randomTokenBytes(size int) (string, []byte, error) {
	randomBytes := make([]byte, size)
	if _, err := io.ReadFull(service.random, randomBytes); err != nil {
		return "", nil, err
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), randomBytes, nil
}

func credentialHash(token string) ([32]byte, bool) {
	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != secretBytes {
		return [32]byte{}, false
	}
	return sha256.Sum256(secret), true
}
