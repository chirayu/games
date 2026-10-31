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

// Store is the persistence needed by the game creation and session flows.
type Store interface {
	CreateGame(context.Context, sqlite.Game, [2]sqlite.Player) error
	GetPlayerByTokenHash(context.Context, string, [32]byte) (sqlite.Player, error)
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
	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != secretBytes {
		return Session{}, ErrInvalidSecret
	}
	tokenHash := sha256.Sum256(secret)
	player, err := service.store.GetPlayerByTokenHash(ctx, gameID, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrInvalidSecret
	}
	if err != nil {
		return Session{}, fmt.Errorf("look up player: %w", err)
	}
	return Session{GameID: gameID, Seat: player.Seat, Token: token}, nil
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
