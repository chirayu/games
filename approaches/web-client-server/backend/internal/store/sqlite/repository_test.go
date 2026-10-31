package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestOpenCreatesDatabaseFromMigrationsAndConfiguresPragmas(t *testing.T) {
	repository := openTestRepository(t, filepath.Join(t.TempDir(), "letterpress.db"))

	var tableName string
	if err := repository.database.QueryRowContext(
		context.Background(),
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'games'",
	).Scan(&tableName); err != nil {
		t.Fatalf("find migrated games table: %v", err)
	}
	if tableName != "games" {
		t.Fatalf("table name = %q, want games", tableName)
	}

	var foreignKeys int
	if err := repository.database.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	var timeoutMilliseconds int
	if err := repository.database.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&timeoutMilliseconds); err != nil {
		t.Fatalf("read busy_timeout pragma: %v", err)
	}
	if timeoutMilliseconds != int(busyTimeout.Milliseconds()) {
		t.Fatalf("busy_timeout = %d, want %d", timeoutMilliseconds, busyTimeout.Milliseconds())
	}
}

func TestCreateAndRetrieveGameAndPlayers(t *testing.T) {
	repository := openTestRepository(t, filepath.Join(t.TempDir(), "letterpress.db"))
	game, players := testGame()

	if err := repository.CreateGame(context.Background(), game, players); err != nil {
		t.Fatalf("create game: %v", err)
	}

	storedGame, err := repository.GetGame(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if !reflect.DeepEqual(storedGame, game) {
		t.Fatalf("stored game = %#v, want %#v", storedGame, game)
	}

	storedPlayers, err := repository.ListPlayers(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("list players: %v", err)
	}
	if !reflect.DeepEqual(storedPlayers, players[:]) {
		t.Fatalf("stored players = %#v, want %#v", storedPlayers, players)
	}

	player, err := repository.GetPlayerByTokenHash(context.Background(), game.ID, players[1].TokenHash)
	if err != nil {
		t.Fatalf("get player by token hash: %v", err)
	}
	if !reflect.DeepEqual(player, players[1]) {
		t.Fatalf("player = %#v, want %#v", player, players[1])
	}
}

func TestMoveAndStatePersistAcrossRepositoryRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "letterpress.db")
	repository := openTestRepository(t, path)
	game, players := testGame()
	if err := repository.CreateGame(context.Background(), game, players); err != nil {
		t.Fatalf("create game: %v", err)
	}

	moveTime := time.Date(2026, time.October, 31, 6, 30, 0, 0, time.UTC)
	owners := append([]byte(nil), game.TileOwners...)
	owners[0], owners[7], owners[12] = 1, 1, 1
	move := Move{
		GameID: game.ID, Player: 1, TurnNumber: 1,
		TileIndexes: []int64{12, 7, 0}, Word: "MAF", CreatedAt: moveTime,
	}
	state := GameState{TileOwners: owners, CurrentPlayer: 2, Status: "active", UpdatedAt: moveTime}
	if err := repository.ApplyMove(context.Background(), game.ID, 0, move, state); err != nil {
		t.Fatalf("apply move: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close repository: %v", err)
	}

	repository, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	storedGame, err := repository.GetGame(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("get game after restart: %v", err)
	}
	if storedGame.Version != 1 || storedGame.CurrentPlayer != 2 || !reflect.DeepEqual(storedGame.TileOwners, owners) {
		t.Fatalf("stored state after restart = %#v", storedGame)
	}
	storedMoves, err := repository.ListMoves(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("list moves after restart: %v", err)
	}
	if len(storedMoves) != 1 || !reflect.DeepEqual(storedMoves[0].TileIndexes, move.TileIndexes) || storedMoves[0].Word != move.Word {
		t.Fatalf("stored moves = %#v, want move %#v", storedMoves, move)
	}
}

func TestApplyMoveIsAtomicAndRejectsStaleConcurrentWrites(t *testing.T) {
	repository := openTestRepository(t, filepath.Join(t.TempDir(), "letterpress.db"))
	game, players := testGame()
	if err := repository.CreateGame(context.Background(), game, players); err != nil {
		t.Fatalf("create game: %v", err)
	}

	firstTime := game.CreatedAt.Add(time.Minute)
	firstOwners := append([]byte(nil), game.TileOwners...)
	firstOwners[0] = 1
	firstMove := Move{GameID: game.ID, Player: 1, TurnNumber: 1, TileIndexes: []int64{0}, Word: "A", CreatedAt: firstTime}
	if err := repository.ApplyMove(context.Background(), game.ID, 0, firstMove, GameState{
		TileOwners: firstOwners, CurrentPlayer: 2, Status: "active", UpdatedAt: firstTime,
	}); err != nil {
		t.Fatalf("apply first move: %v", err)
	}

	// The state update executes before the move insert. A duplicate turn makes
	// the insert fail and proves that the preceding state update is rolled back.
	failedOwners := append([]byte(nil), firstOwners...)
	failedOwners[1] = 2
	err := repository.ApplyMove(context.Background(), game.ID, 1, firstMove, GameState{
		TileOwners: failedOwners, CurrentPlayer: 1, Status: "active", UpdatedAt: firstTime.Add(time.Minute),
	})
	if err == nil {
		t.Fatal("duplicate move unexpectedly succeeded")
	}
	assertGameVersionAndOwners(t, repository, game.ID, 1, firstOwners)
	assertMoveCount(t, repository, game.ID, 1)

	start := make(chan struct{})
	errorsByWriter := make(chan error, 2)
	var writers sync.WaitGroup
	for writer := int64(0); writer < 2; writer++ {
		writers.Add(1)
		go func(writer int64) {
			defer writers.Done()
			<-start
			owners := append([]byte(nil), firstOwners...)
			owners[writer+1] = 2
			errorsByWriter <- repository.ApplyMove(context.Background(), game.ID, 1, Move{
				GameID: game.ID, Player: 2, TurnNumber: 2,
				TileIndexes: []int64{writer + 1}, Word: string(rune('B' + writer)), CreatedAt: firstTime.Add(2 * time.Minute),
			}, GameState{TileOwners: owners, CurrentPlayer: 1, Status: "active", UpdatedAt: firstTime.Add(2 * time.Minute)})
		}(writer)
	}
	close(start)
	writers.Wait()
	close(errorsByWriter)

	var successes, conflicts int
	for err := range errorsByWriter {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrVersionConflict):
			conflicts++
		default:
			t.Fatalf("concurrent writer error = %v, want nil or version conflict", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d, want 1 and 1", successes, conflicts)
	}
	assertMoveCount(t, repository, game.ID, 2)
	storedGame, err := repository.GetGame(context.Background(), game.ID)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if storedGame.Version != 2 {
		t.Fatalf("version = %d, want 2", storedGame.Version)
	}
}

func openTestRepository(t *testing.T, path string) *Repository {
	t.Helper()
	repository, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository
}

func testGame() (Game, [2]Player) {
	createdAt := time.Date(2026, time.October, 31, 6, 0, 0, 0, time.UTC)
	game := Game{
		ID: "game-1", BoardLetters: []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ"[:25]), TileOwners: make([]byte, 25),
		CurrentPlayer: 1, Status: "active", Version: 0, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	return game, [2]Player{
		{GameID: game.ID, Seat: 1, TokenHash: sha256.Sum256([]byte("player-one-secret")), CreatedAt: createdAt},
		{GameID: game.ID, Seat: 2, TokenHash: sha256.Sum256([]byte("player-two-secret")), CreatedAt: createdAt},
	}
}

func assertGameVersionAndOwners(t *testing.T, repository *Repository, gameID string, version int64, owners []byte) {
	t.Helper()
	game, err := repository.GetGame(context.Background(), gameID)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if game.Version != version || !reflect.DeepEqual(game.TileOwners, owners) {
		t.Fatalf("game version/owners = %d/%v, want %d/%v", game.Version, game.TileOwners, version, owners)
	}
}

func assertMoveCount(t *testing.T, repository *Repository, gameID string, count int) {
	t.Helper()
	moves, err := repository.ListMoves(context.Background(), gameID)
	if err != nil {
		t.Fatalf("list moves: %v", err)
	}
	if len(moves) != count {
		t.Fatalf("move count = %d, want %d", len(moves), count)
	}
}
