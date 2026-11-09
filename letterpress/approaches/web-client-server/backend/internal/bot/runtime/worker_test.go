package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/bot/policy"
	"letterpress/backend/internal/game"
	"letterpress/backend/internal/httpapi"
	"letterpress/backend/internal/store/sqlite"
)

func TestWorkerRecoversPendingTurnAfterRestartWithoutDuplicate(t *testing.T) {
	environment := newRuntimeEnvironment(t, "restart-game", false)
	worker := environment.worker(t, fixedSelector{indexes: []int{1}})
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMoveCount(t, environment.repository, environment.gameID, 2)

	restarted := environment.worker(t, fixedSelector{indexes: []int{2}})
	if err := restarted.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMoveCount(t, environment.repository, environment.gameID, 2)
}

func TestConcurrentWorkersAcceptAtMostOneMove(t *testing.T) {
	environment := newRuntimeEnvironment(t, "concurrent-game", true)
	workers := []*Worker{
		environment.worker(t, fixedSelector{indexes: []int{0}}),
		environment.worker(t, fixedSelector{indexes: []int{0}}),
	}
	start := make(chan struct{})
	errorsByWorker := make(chan error, len(workers))
	var group sync.WaitGroup
	for _, worker := range workers {
		group.Add(1)
		go func(worker *Worker) {
			defer group.Done()
			<-start
			errorsByWorker <- worker.RunOnce(context.Background())
		}(worker)
	}
	close(start)
	group.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		if err != nil {
			t.Fatalf("concurrent worker failed: %v", err)
		}
	}
	assertMoveCount(t, environment.repository, environment.gameID, 1)
}

func TestHumanMoveRemainsCommittedWhenBotSelectionFails(t *testing.T) {
	environment := newRuntimeEnvironment(t, "failure-game", false)
	worker := environment.worker(t, fixedSelector{err: errors.New("calculation failed")})
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("worker unexpectedly succeeded")
	}
	assertMoveCount(t, environment.repository, environment.gameID, 1)
	stored, err := environment.repository.GetGame(context.Background(), environment.gameID)
	if err != nil || stored.Version != 1 || stored.CurrentPlayer != 2 {
		t.Fatalf("human state = %#v, err %v", stored, err)
	}
}

func TestWorkerCanCompleteGameThroughHTTP(t *testing.T) {
	environment := newRuntimeEnvironment(t, "completion-game", true)
	indexes := make([]int, 25)
	for index := range indexes {
		indexes[index] = index
	}
	if err := environment.worker(t, fixedSelector{indexes: indexes}).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := environment.repository.GetGame(context.Background(), environment.gameID)
	if err != nil || stored.Status != string(game.StatusCompleted) || stored.Version != 1 {
		t.Fatalf("completed state = %#v, err %v", stored, err)
	}
}

func TestWorkerRefetchesAndRecalculatesAfterVersionConflict(t *testing.T) {
	view := minimalView(0)
	client := &scriptedClient{
		views:        []app.GameView{view, minimalView(1)},
		submitErrors: []error{ErrVersionConflict, nil},
	}
	selector := &countingSelector{indexes: []int{0}}
	worker, err := NewWorker(staticFeed{turns: []sqlite.PendingBotTurn{{
		GameID: "stale", Seat: 1, ConfigurationVersion: 1, Style: policy.BalancedName, EnglishKnowledge: "full_dictionary",
	}}}, client, selector, "bot", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if selector.calls != 2 || !reflect.DeepEqual(client.submittedVersions, []int64{0, 1}) {
		t.Fatalf("selector calls = %d, submitted versions = %v", selector.calls, client.submittedVersions)
	}
}

func TestWorkerBoundsTransientRetries(t *testing.T) {
	client := &scriptedClient{getError: &APIError{Status: 503, Code: "unavailable"}}
	worker, err := NewWorker(staticFeed{turns: []sqlite.PendingBotTurn{{
		GameID: "retry", Seat: 1, ConfigurationVersion: 1, Style: policy.BalancedName, EnglishKnowledge: "full_dictionary",
	}}}, client, fixedSelector{indexes: []int{0}}, "bot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var sleeps int
	worker.sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("transient failure unexpectedly succeeded")
	}
	if client.getCalls != defaultMaxAttempts || sleeps != defaultMaxAttempts-1 {
		t.Fatalf("GET calls = %d, sleeps = %d", client.getCalls, sleeps)
	}
}

type runtimeEnvironment struct {
	repository *sqlite.Repository
	server     *httptest.Server
	client     *Client
	gameID     string
	botID      string
}

func newRuntimeEnvironment(t *testing.T, gameID string, botStarts bool) runtimeEnvironment {
	t.Helper()
	repository, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "letterpress.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	service := app.NewServiceWithDependencies(repository, fixedBoardGenerator{}, game.MVPWordValidator{})
	human, err := service.EnterPlayer(context.Background(), "Runtime Human")
	if err != nil {
		t.Fatal(err)
	}
	bot, err := repository.GetLatestEnabledBotProfile(context.Background(), "bot-balanced-v1")
	if err != nil {
		t.Fatal(err)
	}
	rawToken := make([]byte, 32)
	for index := range rawToken {
		rawToken[index] = byte(index + 1)
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	now := time.Date(2026, time.November, 9, 3, 0, 0, 0, time.UTC)
	if err := repository.PutBotRuntimeCredential(context.Background(), bot.Player.ID, sha256.Sum256(rawToken), now); err != nil {
		t.Fatal(err)
	}
	botSeat, humanSeat := int64(2), int64(1)
	seatOnePlayer, seatTwoPlayer := human.Player.ID, bot.Player.ID
	if botStarts {
		botSeat, humanSeat = 1, 2
		seatOnePlayer, seatTwoPlayer = bot.Player.ID, human.Player.ID
	}
	profile := &sqlite.BotProfileSnapshot{
		ConfigurationVersion: bot.Profile.ConfigurationVersion, Style: bot.Profile.Style,
		EnglishKnowledgeLevel: bot.Profile.EnglishKnowledgeLevel, SkillLevel: bot.Profile.SkillLevel,
	}
	seats := [2]sqlite.GameSeat{
		{GameID: gameID, Seat: 1, PlayerID: seatOnePlayer, CreatedAt: now},
		{GameID: gameID, Seat: 2, PlayerID: seatTwoPlayer, CreatedAt: now},
	}
	seats[botSeat-1].BotProfile = profile
	if err := repository.CreateGame(context.Background(), sqlite.Game{
		ID: gameID, BoardLetters: []byte("ABCDEFGHIJKLMNOPQRSTUVWXY"), TileOwners: make([]byte, 25),
		CurrentPlayer: 1, Status: string(game.StatusActive), CreatedAt: now, UpdatedAt: now,
	}, seats); err != nil {
		t.Fatal(err)
	}
	if !botStarts {
		if _, err := service.SubmitMove(context.Background(), gameID, human.Token, []int{0}, 0); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(httpapi.NewHandler(service, httpapi.Config{InsecureCookies: true}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_ = humanSeat
	return runtimeEnvironment{repository: repository, server: server, client: client, gameID: gameID, botID: bot.Player.ID}
}

func (environment runtimeEnvironment) worker(t *testing.T, selector Selector) *Worker {
	t.Helper()
	worker, err := NewWorker(environment.repository, environment.client, selector, environment.botID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

type fixedBoardGenerator struct{}

func (fixedBoardGenerator) Generate(width, height int) ([]byte, error) {
	return []byte("ABCDEFGHIJKLMNOPQRSTUVWXY"), nil
}

type fixedSelector struct {
	indexes []int
	err     error
}

func (selector fixedSelector) Select(context.Context, game.Snapshot, policy.Seed) ([]int, error) {
	return append([]int(nil), selector.indexes...), selector.err
}

type countingSelector struct {
	indexes []int
	calls   int
}

func (selector *countingSelector) Select(context.Context, game.Snapshot, policy.Seed) ([]int, error) {
	selector.calls++
	return append([]int(nil), selector.indexes...), nil
}

type staticFeed struct {
	turns []sqlite.PendingBotTurn
	err   error
}

func (feed staticFeed) ListPendingBotTurns(context.Context, string, int64) ([]sqlite.PendingBotTurn, error) {
	return feed.turns, feed.err
}

type scriptedClient struct {
	views             []app.GameView
	getError          error
	getCalls          int
	submitErrors      []error
	submitCalls       int
	submittedVersions []int64
}

func (client *scriptedClient) GetGame(context.Context, string) (app.GameView, error) {
	client.getCalls++
	if client.getError != nil {
		return app.GameView{}, client.getError
	}
	index := client.getCalls - 1
	if index >= len(client.views) {
		index = len(client.views) - 1
	}
	return client.views[index], nil
}

func (client *scriptedClient) SubmitMove(_ context.Context, _ string, _ []int, version int64) (app.GameView, error) {
	client.submittedVersions = append(client.submittedVersions, version)
	index := client.submitCalls
	client.submitCalls++
	if index < len(client.submitErrors) {
		return app.GameView{}, client.submitErrors[index]
	}
	return app.GameView{}, nil
}

func minimalView(version int64) app.GameView {
	return app.GameView{
		ID: "stale", Width: 1, Height: 1, Tiles: []app.GameTile{{Index: 0, Letter: "A"}},
		ActivePlayer: 1, PlayerSeat: 1, Version: version, Status: string(game.StatusActive),
	}
}

func assertMoveCount(t *testing.T, repository *sqlite.Repository, gameID string, want int) {
	t.Helper()
	moves, err := repository.ListMoves(context.Background(), gameID)
	if err != nil || len(moves) != want {
		t.Fatalf("moves = %d, err %v; want %d", len(moves), err, want)
	}
}
