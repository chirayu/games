package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const maxLobbyPageSize = 50

var (
	// ErrInvalidPagination means a cursor or page size is invalid.
	ErrInvalidPagination = errors.New("invalid pagination")
	// ErrInvalidPlayerSearch means a discovery query is invalid.
	ErrInvalidPlayerSearch = errors.New("invalid player search")
)

// Page is one stable keyset-paginated collection.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// PublicPlayer is the non-secret identity exposed in discovery and game summaries.
type PublicPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ActiveGameSummary is the compact participant-scoped lobby representation.
type ActiveGameSummary struct {
	ID           string       `json:"id"`
	Opponent     PublicPlayer `json:"opponent"`
	PlayerSeat   int64        `json:"playerSeat"`
	ActivePlayer int64        `json:"activePlayer"`
	Status       string       `json:"status"`
	Scores       Scores       `json:"scores"`
	Version      int64        `json:"version"`
	LastActivity time.Time    `json:"lastActivity"`
}

type gamePageCursor struct {
	UpdatedAt time.Time `json:"updatedAt"`
	ID        string    `json:"id"`
}

type playerPageCursor struct {
	Query string `json:"query"`
	Name  string `json:"name"`
	ID    string `json:"id"`
}

// ListActiveGames returns only active games containing the authenticated player.
func (service *Service) ListActiveGames(ctx context.Context, token, cursor string, limit int) (Page[ActiveGameSummary], error) {
	if limit < 1 || limit > maxLobbyPageSize {
		return Page[ActiveGameSummary]{}, ErrInvalidPagination
	}
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return Page[ActiveGameSummary]{}, err
	}
	position := gamePageCursor{UpdatedAt: time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC), ID: "\uffff"}
	if cursor != "" && decodeCursor(cursor, &position) != nil {
		return Page[ActiveGameSummary]{}, ErrInvalidPagination
	}
	rows, err := service.store.ListActiveGamesForPlayer(ctx, player.ID, position.UpdatedAt, position.ID, limit+1)
	if err != nil {
		return Page[ActiveGameSummary]{}, fmt.Errorf("list active games: %w", err)
	}
	page := Page[ActiveGameSummary]{Items: make([]ActiveGameSummary, 0, min(len(rows), limit))}
	for _, row := range rows[:min(len(rows), limit)] {
		scores := Scores{}
		for _, owner := range row.TileOwners {
			switch owner {
			case 1:
				scores.PlayerOne++
			case 2:
				scores.PlayerTwo++
			}
		}
		page.Items = append(page.Items, ActiveGameSummary{
			ID: row.ID, Opponent: PublicPlayer{ID: row.OpponentID, Name: row.OpponentName, Type: "human"},
			PlayerSeat: row.PlayerSeat, ActivePlayer: row.CurrentPlayer, Status: row.Status,
			Scores: scores, Version: row.Version, LastActivity: row.UpdatedAt,
		})
	}
	if len(rows) > limit {
		last := rows[limit-1]
		page.NextCursor, err = encodeCursor(gamePageCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
		if err != nil {
			return Page[ActiveGameSummary]{}, err
		}
	}
	return page, nil
}

// SearchPlayers finds registered identities by a bounded case-insensitive name fragment.
func (service *Service) SearchPlayers(ctx context.Context, token, query, cursor string, limit int) (Page[PublicPlayer], error) {
	if limit < 1 || limit > maxLobbyPageSize {
		return Page[PublicPlayer]{}, ErrInvalidPagination
	}
	player, err := service.CurrentPlayer(ctx, token)
	if err != nil {
		return Page[PublicPlayer]{}, err
	}
	_, normalized, err := normalizePlayerName(query)
	if err != nil {
		return Page[PublicPlayer]{}, ErrInvalidPlayerSearch
	}
	position := playerPageCursor{}
	if cursor != "" && decodeCursor(cursor, &position) != nil {
		return Page[PublicPlayer]{}, ErrInvalidPagination
	}
	if cursor != "" && position.Query != normalized {
		return Page[PublicPlayer]{}, ErrInvalidPagination
	}
	rows, err := service.store.SearchRegisteredPlayers(
		ctx, player.ID, normalized, position.Name, position.ID, limit+1,
	)
	if err != nil {
		return Page[PublicPlayer]{}, fmt.Errorf("search players: %w", err)
	}
	page := Page[PublicPlayer]{Items: make([]PublicPlayer, 0, min(len(rows), limit))}
	for _, row := range rows[:min(len(rows), limit)] {
		page.Items = append(page.Items, PublicPlayer{ID: row.ID, Name: row.DisplayName, Type: "human"})
	}
	if len(rows) > limit {
		last := rows[limit-1]
		page.NextCursor, err = encodeCursor(playerPageCursor{Query: normalized, Name: last.NormalizedName, ID: last.ID})
		if err != nil {
			return Page[PublicPlayer]{}, err
		}
	}
	return page, nil
}

func encodeCursor(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode page cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeCursor(encoded string, destination any) error {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("cursor contains trailing data")
	}
	return nil
}
