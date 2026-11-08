package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"letterpress/backend/internal/store/sqlite"
)

const (
	playerNameMaxLength   = 24
	playerSessionLifetime = 180 * 24 * time.Hour
)

var (
	// ErrInvalidPlayerName means a name does not meet the public name rules.
	ErrInvalidPlayerName = errors.New("invalid player name")
	// ErrInvalidPlayerSession means a registered-player session is absent,
	// malformed, expired, or otherwise invalid.
	ErrInvalidPlayerSession = errors.New("invalid player session")
)

// RegisteredPlayer is the public identity returned after name entry.
type RegisteredPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PlayerLogin is a newly established independent browser session.
type PlayerLogin struct {
	Player    RegisteredPlayer
	Token     string
	ExpiresAt time.Time
	Created   bool
}

// EnterPlayer creates a new name or logs in to the existing normalized name.
func (service *Service) EnterPlayer(ctx context.Context, rawName string) (PlayerLogin, error) {
	displayName, normalizedName, err := normalizePlayerName(rawName)
	if err != nil {
		return PlayerLogin{}, err
	}
	playerID, err := service.randomToken(playerIDBytes)
	if err != nil {
		return PlayerLogin{}, fmt.Errorf("generate player ID: %w", err)
	}
	token, tokenHash, err := service.playerToken()
	if err != nil {
		return PlayerLogin{}, fmt.Errorf("generate player session: %w", err)
	}
	now := service.now().UTC()
	expiresAt := now.Add(playerSessionLifetime)
	stored, created, err := service.store.CreateRegisteredPlayerSession(ctx, sqlite.RegisteredPlayer{
		ID: playerID, DisplayName: displayName, NormalizedName: normalizedName, CreatedAt: now,
	}, sqlite.PlayerSession{
		TokenHash: tokenHash, PlayerID: playerID, ExpiresAt: expiresAt, CreatedAt: now,
	})
	if err != nil {
		return PlayerLogin{}, fmt.Errorf("establish player session: %w", err)
	}
	return PlayerLogin{
		Player: RegisteredPlayer{ID: stored.ID, Name: stored.DisplayName},
		Token:  token, ExpiresAt: expiresAt, Created: created,
	}, nil
}

// CurrentPlayer authenticates one unexpired registered-player session.
func (service *Service) CurrentPlayer(ctx context.Context, token string) (RegisteredPlayer, error) {
	tokenHash, ok := credentialHash(token)
	if !ok {
		return RegisteredPlayer{}, ErrInvalidPlayerSession
	}
	stored, err := service.store.GetRegisteredPlayerBySessionHash(ctx, tokenHash, service.now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		return RegisteredPlayer{}, ErrInvalidPlayerSession
	}
	if err != nil {
		return RegisteredPlayer{}, fmt.Errorf("authenticate player session: %w", err)
	}
	return RegisteredPlayer{ID: stored.ID, Name: stored.DisplayName}, nil
}

// LogoutPlayer revokes only the supplied registered-player session.
func (service *Service) LogoutPlayer(ctx context.Context, token string) error {
	tokenHash, ok := credentialHash(token)
	if !ok {
		return ErrInvalidPlayerSession
	}
	if err := service.store.DeletePlayerSession(ctx, tokenHash); err != nil {
		return fmt.Errorf("delete player session: %w", err)
	}
	return nil
}

func normalizePlayerName(rawName string) (string, string, error) {
	trimmed := strings.Trim(rawName, " ")
	if trimmed == "" {
		return "", "", ErrInvalidPlayerName
	}
	var display strings.Builder
	display.Grow(len(trimmed))
	previousSpace := false
	for _, character := range trimmed {
		switch {
		case character >= 'A' && character <= 'Z',
			character >= 'a' && character <= 'z',
			character >= '0' && character <= '9',
			character == '-', character == '_':
			display.WriteRune(character)
			previousSpace = false
		case character == ' ':
			if !previousSpace {
				display.WriteByte(' ')
				previousSpace = true
			}
		default:
			return "", "", ErrInvalidPlayerName
		}
	}
	displayName := display.String()
	if len(displayName) == 0 || len(displayName) > playerNameMaxLength {
		return "", "", ErrInvalidPlayerName
	}
	return displayName, strings.ToLower(displayName), nil
}
