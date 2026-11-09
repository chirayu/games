// Package runtime discovers durable bot turns and plays them only through HTTP.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/bot/policy"
	"letterpress/backend/internal/game"
	"letterpress/backend/internal/store/sqlite"
)

const (
	defaultBatchSize   = int64(50)
	defaultMaxAttempts = 3
)

type TurnFeed interface {
	ListPendingBotTurns(context.Context, string, int64) ([]sqlite.PendingBotTurn, error)
}

type GameClient interface {
	GetGame(context.Context, string) (app.GameView, error)
	SubmitMove(context.Context, string, []int, int64) (app.GameView, error)
}

type Selector interface {
	Select(context.Context, game.Snapshot, policy.Seed) ([]int, error)
}

// Worker recovers all work from durable game state on every polling pass.
type Worker struct {
	feed        TurnFeed
	client      GameClient
	selector    Selector
	playerID    string
	logger      *slog.Logger
	maxAttempts int
	sleep       func(context.Context, time.Duration) error
}

func NewWorker(feed TurnFeed, client GameClient, selector Selector, playerID string, logger *slog.Logger) (*Worker, error) {
	if feed == nil || client == nil || selector == nil || playerID == "" {
		return nil, errors.New("bot worker dependencies and player ID are required")
	}
	return &Worker{
		feed: feed, client: client, selector: selector, playerID: playerID, logger: logger,
		maxAttempts: defaultMaxAttempts, sleep: sleepContext,
	}, nil
}

// Run polls durable assignments. A restart simply begins another discovery pass.
func (worker *Worker) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("bot poll interval must be positive")
	}
	if err := worker.RunOnce(ctx); err != nil && worker.logger != nil {
		worker.logger.ErrorContext(ctx, "bot polling pass failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := worker.RunOnce(ctx); err != nil && worker.logger != nil {
				worker.logger.ErrorContext(ctx, "bot polling pass failed", "error", err)
			}
		}
	}
}

// RunOnce processes a bounded durable feed without holding a game transaction during calculation.
func (worker *Worker) RunOnce(ctx context.Context) error {
	turns, err := worker.feed.ListPendingBotTurns(ctx, worker.playerID, defaultBatchSize)
	if err != nil {
		return err
	}
	var failures []error
	for _, turn := range turns {
		if err := worker.play(ctx, turn); err != nil {
			failures = append(failures, fmt.Errorf("game %s: %w", turn.GameID, err))
			if worker.logger != nil {
				worker.logger.ErrorContext(ctx, "bot turn failed", "game_id", turn.GameID, "error", err)
			}
		}
	}
	return errors.Join(failures...)
}

func (worker *Worker) play(ctx context.Context, turn sqlite.PendingBotTurn) error {
	if turn.Style != policy.BalancedName || turn.EnglishKnowledge != "full_dictionary" || turn.ConfigurationVersion <= 0 {
		return errors.New("unsupported bot profile snapshot")
	}
	for attempt := 1; attempt <= worker.maxAttempts; attempt++ {
		view, err := worker.client.GetGame(ctx, turn.GameID)
		if err != nil {
			if worker.retryable(err) && attempt < worker.maxAttempts {
				if sleepErr := worker.sleep(ctx, retryDelay(attempt)); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return err
		}
		if view.Status != string(game.StatusActive) || view.PlayerSeat != view.ActivePlayer {
			return nil
		}
		snapshot, err := snapshotFromView(view)
		if err != nil {
			return err
		}
		indexes, err := worker.selector.Select(ctx, snapshot, policy.Seed{
			GameID: turn.GameID, ProfileVersion: turn.ConfigurationVersion, TurnNumber: len(view.Moves) + 1,
		})
		if err != nil {
			return err
		}
		_, err = worker.client.SubmitMove(ctx, turn.GameID, indexes, view.Version)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if worker.retryable(err) && attempt < worker.maxAttempts {
			if sleepErr := worker.sleep(ctx, retryDelay(attempt)); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		return err
	}
	return errors.New("bot turn retry budget exhausted")
}

func snapshotFromView(view app.GameView) (game.Snapshot, error) {
	letters := make([]byte, len(view.Tiles))
	owners := make([]game.Player, len(view.Tiles))
	for index, tile := range view.Tiles {
		if tile.Index != index || len(tile.Letter) != 1 || tile.Letter[0] < 'A' || tile.Letter[0] > 'Z' {
			return game.Snapshot{}, game.ErrInvalidBoard
		}
		letters[index] = tile.Letter[0]
		owners[index] = game.Player(tile.Owner)
	}
	moves := make([]game.Move, len(view.Moves))
	for index, move := range view.Moves {
		moves[index] = game.Move{
			Player: game.Player(move.Player), TurnNumber: int(move.TurnNumber), Word: move.Word,
			TileIndexes: append([]int(nil), move.TileIndexes...),
		}
	}
	return game.Snapshot{
		Width: view.Width, Height: view.Height, Letters: letters, Owners: owners,
		CurrentPlayer: game.Player(view.ActivePlayer), Status: game.Status(view.Status), Moves: moves,
	}, nil
}

func (worker *Worker) retryable(err error) bool {
	var apiError *APIError
	if errors.As(err, &apiError) {
		return apiError.Status == http.StatusTooManyRequests || apiError.Status >= 500
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func retryDelay(attempt int) time.Duration {
	return time.Duration(1<<(attempt-1)) * 100 * time.Millisecond
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
