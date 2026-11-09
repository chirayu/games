package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"letterpress/backend/internal/bot/policy"
	botruntime "letterpress/backend/internal/bot/runtime"
	"letterpress/backend/internal/store/sqlite"
)

const defaultBotPlayerID = "bot-balanced-v1"

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("bot runtime stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	databasePath := flag.String("db", valueOrDefault(os.Getenv("LETTERPRESS_DB_PATH"), "letterpress.db"), "SQLite database path")
	baseURL := flag.String("api-base-url", valueOrDefault(os.Getenv("LETTERPRESS_BOT_API_BASE_URL"), "http://127.0.0.1:8080"), "Letterpress API base URL")
	playerID := flag.String("player-id", valueOrDefault(os.Getenv("LETTERPRESS_BOT_PLAYER_ID"), defaultBotPlayerID), "managed bot player ID")
	pollInterval := flag.Duration("poll-interval", 2*time.Second, "durable turn polling interval")
	flag.Parse()

	token := strings.TrimSpace(os.Getenv("LETTERPRESS_BOT_TOKEN"))
	rawToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(rawToken) != 32 {
		return errors.New("LETTERPRESS_BOT_TOKEN must be a base64url-encoded 32-byte secret")
	}
	if *pollInterval <= 0 {
		return errors.New("poll interval must be positive")
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repository, err := sqlite.Open(ctx, *databasePath)
	if err != nil {
		return err
	}
	defer repository.Close()
	managed, err := repository.GetLatestEnabledBotProfile(ctx, *playerID)
	if err != nil {
		return fmt.Errorf("load managed bot profile: %w", err)
	}
	if managed.Profile.Style != policy.BalancedName || managed.Profile.EnglishKnowledgeLevel != "full_dictionary" {
		return errors.New("configured managed bot profile is not supported by this runtime")
	}
	tokenHash := sha256.Sum256(rawToken)
	if err := repository.PutBotRuntimeCredential(ctx, managed.Player.ID, tokenHash, time.Now().UTC()); err != nil {
		return err
	}
	authenticated, err := repository.AuthenticateBotRuntime(ctx, tokenHash)
	if err != nil || authenticated.ID != managed.Player.ID {
		return errors.New("provisioned bot runtime credential could not be authenticated")
	}
	client, err := botruntime.NewClient(*baseURL, token, &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		return err
	}
	selector, err := policy.NewBalancedEnglish(logger)
	if err != nil {
		return err
	}
	worker, err := botruntime.NewWorker(repository, client, selector, managed.Player.ID, logger)
	if err != nil {
		return err
	}
	logger.Info("bot runtime started", "player_id", managed.Player.ID, "poll_interval_ms", pollInterval.Milliseconds())
	return worker.Run(ctx, *pollInterval)
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
