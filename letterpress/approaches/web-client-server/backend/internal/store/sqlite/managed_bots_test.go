package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBaselineManagedBotProfileAndHistoricalSeatSnapshot(t *testing.T) {
	repository := openTestRepository(t, filepath.Join(t.TempDir(), "letterpress.db"))
	ctx := context.Background()
	baseline, err := repository.GetLatestEnabledBotProfile(ctx, "bot-balanced-v1")
	if err != nil {
		t.Fatalf("get baseline bot: %v", err)
	}
	if baseline.Player.PlayerType != "bot" || baseline.Player.DisplayName != "Balanced Bot" ||
		baseline.Profile.ConfigurationVersion != 1 || baseline.Profile.Style != "balanced" ||
		baseline.Profile.EnglishKnowledgeLevel != "full_dictionary" || baseline.Profile.SkillLevel != "baseline" {
		t.Fatalf("baseline bot = %#v", baseline)
	}
	search, err := repository.SearchRegisteredPlayers(ctx, "human-unknown", "balanced", "", "", 10)
	if err != nil || len(search) != 0 {
		t.Fatalf("public player search exposed managed bot: %#v, err %v", search, err)
	}

	now := time.Date(2026, time.November, 9, 1, 0, 0, 0, time.UTC)
	human, _, err := repository.CreateRegisteredPlayerSession(ctx, RegisteredPlayer{
		ID: "human-1", DisplayName: "Alice", NormalizedName: "alice", CreatedAt: now,
	}, PlayerSession{TokenHash: sha256.Sum256([]byte("human credential")), PlayerID: "human-1", ExpiresAt: now.Add(time.Hour), CreatedAt: now})
	if err != nil {
		t.Fatalf("create human: %v", err)
	}
	snapshot := &BotProfileSnapshot{
		ConfigurationVersion: baseline.Profile.ConfigurationVersion,
		Style:                baseline.Profile.Style, EnglishKnowledgeLevel: baseline.Profile.EnglishKnowledgeLevel,
		SkillLevel: baseline.Profile.SkillLevel,
	}
	game := Game{ID: "bot-game", BoardLetters: []byte("ABCDEFGHIJKLMNOPQRSTUVWXY"), TileOwners: make([]byte, 25), CurrentPlayer: 1, Status: "active", CreatedAt: now, UpdatedAt: now}
	seats := [2]GameSeat{
		{GameID: game.ID, Seat: 1, PlayerID: human.ID, CreatedAt: now},
		{GameID: game.ID, Seat: 2, PlayerID: baseline.Player.ID, CreatedAt: now, BotProfile: snapshot},
	}
	if err := repository.CreateGame(ctx, game, seats); err != nil {
		t.Fatalf("create bot game: %v", err)
	}

	if err := repository.CreateBotProfileVersion(ctx, BotProfile{
		PlayerID: baseline.Player.ID, ConfigurationVersion: 2, Style: "aggressive",
		EnglishKnowledgeLevel: "full_dictionary", SkillLevel: "baseline", Enabled: true, CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create new profile version: %v", err)
	}
	latest, err := repository.GetLatestEnabledBotProfile(ctx, baseline.Player.ID)
	if err != nil || latest.Profile.ConfigurationVersion != 2 {
		t.Fatalf("latest profile = %#v, err %v", latest, err)
	}
	storedSeats, err := repository.ListGameSeats(ctx, game.ID)
	if err != nil || len(storedSeats) != 2 || storedSeats[1].BotProfile == nil || storedSeats[1].BotProfile.ConfigurationVersion != 1 || storedSeats[1].BotProfile.Style != "balanced" {
		t.Fatalf("historical seats = %#v, err %v", storedSeats, err)
	}
	games, err := repository.ListActiveGamesForPlayer(ctx, human.ID, time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), "\uffff", 10)
	if err != nil || len(games) != 1 || games[0].OpponentType != "bot" || games[0].OpponentBotProfile == nil || games[0].OpponentBotProfile.ConfigurationVersion != 1 {
		t.Fatalf("bot game summary = %#v, err %v", games, err)
	}
}

func TestManagedBotAdministrationStoresOnlyCredentialHash(t *testing.T) {
	repository := openTestRepository(t, filepath.Join(t.TempDir(), "letterpress.db"))
	ctx := context.Background()
	now := time.Date(2026, time.November, 9, 2, 0, 0, 0, time.UTC)
	rawCredential := "runtime-secret-that-is-never-persisted"
	credentialHash := sha256.Sum256([]byte(rawCredential))
	managed, err := repository.CreateManagedBot(ctx, RegisteredPlayer{
		ID: "bot-test-v1", DisplayName: "Test Bot", NormalizedName: "test bot", PlayerType: "bot", CreatedAt: now,
	}, BotProfile{
		PlayerID: "bot-test-v1", ConfigurationVersion: 1, Style: "balanced",
		EnglishKnowledgeLevel: "full_dictionary", SkillLevel: "baseline", Enabled: true, CreatedAt: now,
	}, credentialHash)
	if err != nil || managed.Player.PlayerType != "bot" {
		t.Fatalf("create managed bot = %#v, err %v", managed, err)
	}
	authenticated, err := repository.AuthenticateBotRuntime(ctx, credentialHash)
	if err != nil || authenticated.ID != managed.Player.ID || authenticated.PlayerType != "bot" {
		t.Fatalf("authenticate bot = %#v, err %v", authenticated, err)
	}
	wrongHash := sha256.Sum256([]byte("wrong"))
	if _, err := repository.AuthenticateBotRuntime(ctx, wrongHash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("wrong bot credential error = %v", err)
	}
	var rawMatches int
	if err := repository.database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM bot_runtime_credentials WHERE CAST(token_hash AS TEXT) = ?", rawCredential,
	).Scan(&rawMatches); err != nil || rawMatches != 0 {
		t.Fatalf("raw credential matches = %d, err %v", rawMatches, err)
	}
}
