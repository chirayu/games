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
	gameIDBytes = 16
	secretBytes = 32
)

// ErrInvalidSecret means an invitation secret is malformed or does not belong
// to a player in the requested game.
var ErrInvalidSecret = errors.New("invalid invitation secret")

var (
	// ErrGameNotFound means the requested game does not exist.
	ErrGameNotFound = errors.New("game not found")
	// ErrUnauthorized means the game-scoped player session is absent or invalid.
	ErrUnauthorized = errors.New("unauthorized game session")
	// ErrVersionConflict means the client submitted against stale game state.
	ErrVersionConflict = errors.New("game version conflict")
)

// Store is the persistence needed by the application service.
type Store interface {
	CreateGame(context.Context, sqlite.Game, [2]sqlite.Player) error
	GetGame(context.Context, string) (sqlite.Game, error)
	ListMoves(context.Context, string) ([]sqlite.Move, error)
	GetPlayerByTokenHash(context.Context, string, [32]byte) (sqlite.Player, error)
	TransactMove(context.Context, string, [32]byte, int64, sqlite.MoveOperation) error
}

// CreatedGame contains the raw capabilities returned exactly once at creation.
// Persistence receives only their hashes.
type CreatedGame struct {
	ID             string
	PlayerOneToken string
	PlayerTwoToken string
}

// Session identifies the player seat established by an invitation secret.
type Session struct {
	GameID string
	Seat   int64
	Token  string
}

// GameView is the authenticated, derived state returned to a player.
type GameView struct {
	ID           string      `json:"id"`
	Width        int         `json:"width"`
	Height       int         `json:"height"`
	Tiles        []GameTile  `json:"tiles"`
	Scores       Scores      `json:"scores"`
	ActivePlayer int64       `json:"activePlayer"`
	PlayerSeat   int64       `json:"playerSeat"`
	Version      int64       `json:"version"`
	Status       string      `json:"status"`
	Moves        []MoveView  `json:"moves"`
	Result       *GameResult `json:"result,omitempty"`
}

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

// Service coordinates game creation and player authentication.
type Service struct {
	store  Store
	random io.Reader
	now    func() time.Time
}

// NewService returns the application service with production-safe randomness.
func NewService(store Store) *Service {
	return &Service{store: store, random: rand.Reader, now: time.Now}
}

// CreateGame creates a default game and two independent player capabilities.
func (service *Service) CreateGame(ctx context.Context) (CreatedGame, error) {
	domainGame, err := game.NewDefault()
	if err != nil {
		return CreatedGame{}, fmt.Errorf("generate game board: %w", err)
	}

	gameID, err := service.randomToken(gameIDBytes)
	if err != nil {
		return CreatedGame{}, fmt.Errorf("generate game ID: %w", err)
	}
	playerOneToken, playerOneHash, err := service.playerToken()
	if err != nil {
		return CreatedGame{}, fmt.Errorf("generate player one secret: %w", err)
	}
	playerTwoToken, playerTwoHash, err := service.playerToken()
	if err != nil {
		return CreatedGame{}, fmt.Errorf("generate player two secret: %w", err)
	}
	for playerTwoHash == playerOneHash {
		playerTwoToken, playerTwoHash, err = service.playerToken()
		if err != nil {
			return CreatedGame{}, fmt.Errorf("regenerate player two secret: %w", err)
		}
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
	players := [2]sqlite.Player{
		{GameID: gameID, Seat: int64(game.PlayerOne), TokenHash: playerOneHash, CreatedAt: createdAt},
		{GameID: gameID, Seat: int64(game.PlayerTwo), TokenHash: playerTwoHash, CreatedAt: createdAt},
	}
	if err := service.store.CreateGame(ctx, storedGame, players); err != nil {
		return CreatedGame{}, fmt.Errorf("persist game: %w", err)
	}

	return CreatedGame{ID: gameID, PlayerOneToken: playerOneToken, PlayerTwoToken: playerTwoToken}, nil
}

// ExchangeSession authenticates one invitation capability for a game.
func (service *Service) ExchangeSession(ctx context.Context, gameID, token string) (Session, error) {
	tokenHash, ok := invitationHash(token)
	if !ok {
		return Session{}, ErrInvalidSecret
	}
	player, err := service.store.GetPlayerByTokenHash(ctx, gameID, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrInvalidSecret
	}
	if err != nil {
		return Session{}, fmt.Errorf("look up player: %w", err)
	}
	return Session{GameID: gameID, Seat: player.Seat, Token: token}, nil
}

// GetGame returns the current authoritative state for an authenticated player.
func (service *Service) GetGame(ctx context.Context, gameID, token string) (GameView, error) {
	stored, err := service.store.GetGame(ctx, gameID)
	if errors.Is(err, sql.ErrNoRows) {
		return GameView{}, ErrGameNotFound
	}
	if err != nil {
		return GameView{}, fmt.Errorf("get game: %w", err)
	}

	tokenHash, ok := invitationHash(token)
	if !ok {
		return GameView{}, ErrUnauthorized
	}
	player, err := service.store.GetPlayerByTokenHash(ctx, gameID, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return GameView{}, ErrUnauthorized
	}
	if err != nil {
		return GameView{}, fmt.Errorf("authenticate player: %w", err)
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
	}, game.MVPWordValidator{})
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
		ActivePlayer: stored.CurrentPlayer, PlayerSeat: player.Seat, Version: stored.Version, Status: stored.Status,
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
	tokenHash, ok := invitationHash(token)
	if !ok {
		return GameView{}, ErrUnauthorized
	}
	err := service.store.TransactMove(ctx, gameID, tokenHash, expectedVersion, func(
		stored sqlite.Game,
		player sqlite.Player,
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
		}, game.MVPWordValidator{})
		if err != nil {
			return sqlite.Move{}, sqlite.GameState{}, fmt.Errorf("restore game for move: %w", err)
		}
		accepted, err := restored.Play(game.Player(player.Seat), tileIndexes)
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
	return service.GetGame(ctx, gameID, token)
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

func invitationHash(token string) ([32]byte, bool) {
	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != secretBytes {
		return [32]byte{}, false
	}
	return sha256.Sum256(secret), true
}
