package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"

	"letterpress/backend/internal/store/sqlite/migrations"
)

func TestRegisteredPlayerMigrationUpgradesPopulatedInitialDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "letterpress.db")
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("open initial database: %v", err)
	}
	initialMigration, err := migrations.Files.ReadFile("00001_initial.sql")
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, database, fstest.MapFS{
		"00001_initial.sql": {Data: initialMigration},
	})
	if err != nil {
		t.Fatalf("create initial migration provider: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}
	createdAt := time.Date(2026, time.November, 8, 5, 0, 0, 0, time.UTC)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO games (id, board_letters, tile_owners, current_player, status, version, created_at, updated_at)
		VALUES ('legacy-game', ?, ?, 1, 'active', 0, ?, ?)
	`, []byte("ABCDEFGHIJKLMNOPQRSTUVWXY"), make([]byte, 25), createdAt, createdAt); err != nil {
		t.Fatalf("seed legacy game: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close initial database: %v", err)
	}

	repository := openTestRepository(t, path)
	if _, err := repository.GetGame(ctx, "legacy-game"); err != nil {
		t.Fatalf("legacy game after additive migration: %v", err)
	}
	for _, table := range []string{"registered_players", "player_sessions"} {
		var found string
		if err := repository.database.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&found); err != nil {
			t.Fatalf("find migrated table %s: %v", table, err)
		}
	}
}

func TestRegisteredPlayerSessionsFindCreateAndRevokeIndependently(t *testing.T) {
	repository := openTestRepository(t, filepath.Join(t.TempDir(), "letterpress.db"))
	now := time.Date(2026, time.November, 8, 6, 0, 0, 0, time.UTC)
	firstHash := sha256.Sum256([]byte("first-session"))
	secondHash := sha256.Sum256([]byte("second-session"))

	first, created, err := repository.CreateRegisteredPlayerSession(context.Background(), RegisteredPlayer{
		ID: "player-first", DisplayName: "Blue Fox", NormalizedName: "blue fox", CreatedAt: now,
	}, PlayerSession{TokenHash: firstHash, PlayerID: "player-first", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil || !created {
		t.Fatalf("create first session = %#v/%t/%v", first, created, err)
	}
	second, created, err := repository.CreateRegisteredPlayerSession(context.Background(), RegisteredPlayer{
		ID: "player-second", DisplayName: "blue fox", NormalizedName: "blue fox", CreatedAt: now.Add(time.Minute),
	}, PlayerSession{TokenHash: secondHash, PlayerID: "player-second", CreatedAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil || created {
		t.Fatalf("create second session = %#v/%t/%v", second, created, err)
	}
	if first != second || second.ID != "player-first" || second.DisplayName != "Blue Fox" {
		t.Fatalf("players = %#v and %#v, want the first identity", first, second)
	}

	for _, hash := range [][32]byte{firstHash, secondHash} {
		player, err := repository.GetRegisteredPlayerBySessionHash(context.Background(), hash, now.Add(30*time.Minute))
		if err != nil || player.ID != first.ID {
			t.Fatalf("authenticate session = %#v/%v", player, err)
		}
	}
	if err := repository.DeletePlayerSession(context.Background(), firstHash); err != nil {
		t.Fatalf("delete first session: %v", err)
	}
	if _, err := repository.GetRegisteredPlayerBySessionHash(context.Background(), firstHash, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted session error = %v, want sql.ErrNoRows", err)
	}
	if player, err := repository.GetRegisteredPlayerBySessionHash(context.Background(), secondHash, now); err != nil || player.ID != first.ID {
		t.Fatalf("remaining session = %#v/%v", player, err)
	}
	if _, err := repository.GetRegisteredPlayerBySessionHash(context.Background(), secondHash, now.Add(2*time.Hour)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired session error = %v, want sql.ErrNoRows", err)
	}
}

func TestConcurrentFirstNameEntryCreatesOneRegisteredPlayer(t *testing.T) {
	repository := openTestRepository(t, filepath.Join(t.TempDir(), "letterpress.db"))
	now := time.Date(2026, time.November, 8, 6, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	type result struct {
		player  RegisteredPlayer
		created bool
		err     error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			hash := sha256.Sum256([]byte{byte(index + 1)})
			player, created, err := repository.CreateRegisteredPlayerSession(context.Background(), RegisteredPlayer{
				ID: "player-" + string(rune('a'+index)), DisplayName: "Alice", NormalizedName: "alice", CreatedAt: now,
			}, PlayerSession{TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
			results <- result{player: player, created: created, err: err}
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)

	createdCount := 0
	playerID := ""
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent entry: %v", result.err)
		}
		if result.created {
			createdCount++
		}
		if playerID == "" {
			playerID = result.player.ID
		} else if result.player.ID != playerID {
			t.Fatalf("player IDs differ: %q and %q", playerID, result.player.ID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want 1", createdCount)
	}
}
