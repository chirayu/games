package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/game"
	"letterpress/backend/internal/httpapi"
	"letterpress/backend/internal/serverconfig"
	"letterpress/backend/internal/store/sqlite"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := serverconfig.Load(os.Getenv)
	if err != nil {
		return err
	}
	address := flag.String("addr", config.Address, "HTTP listen address")
	databasePath := flag.String("db", config.DatabasePath, "SQLite database path")
	publicBaseURL := flag.String("public-base-url", config.PublicBaseURL, "public HTTPS origin")
	staticDirectory := flag.String("static-dir", config.StaticDirectory, "compiled frontend directory")
	insecureCookies := flag.Bool("insecure-cookies", config.InsecureCookies, "allow session cookies over HTTP for local development")
	logLevel := flag.String("log-level", config.LogLevel.String(), "structured log level (debug, info, warn, error)")
	allowedOrigin := flag.String("allowed-origin", "", "development-only allowed browser origin override")
	fixedBoard := flag.String("fixed-board", "", "fixed 25-letter A-Z board for deterministic development tests")
	flag.Parse()

	config.Address = *address
	config.DatabasePath = *databasePath
	config.PublicBaseURL = *publicBaseURL
	config.StaticDirectory = *staticDirectory
	config.InsecureCookies = *insecureCookies
	if err := config.LogLevel.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("log level: %w", err)
	}
	if err := serverconfig.Validate(config); err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: config.LogLevel}))

	repository, err := sqlite.Open(context.Background(), config.DatabasePath)
	if err != nil {
		return err
	}
	defer repository.Close()

	service := app.NewService(repository)
	if *fixedBoard != "" {
		generator, err := game.NewFixedBoardGenerator(*fixedBoard)
		if err != nil {
			return err
		}
		service = app.NewServiceWithBoardGenerator(repository, generator)
	}

	var frontend fs.FS
	if config.StaticDirectory != "" {
		frontend = os.DirFS(config.StaticDirectory)
		if _, err := fs.Stat(frontend, "index.html"); err != nil {
			return fmt.Errorf("static frontend index: %w", err)
		}
	}
	origin := config.AllowedOrigin()
	if *allowedOrigin != "" {
		origin = *allowedOrigin
	}
	api := httpapi.NewHandler(service, httpapi.Config{
		InsecureCookies: config.InsecureCookies,
		AllowedOrigin:   origin,
		Frontend:        frontend,
	})
	server := &http.Server{
		Addr:              config.Address,
		Handler:           httpapi.LogRequests(api, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	logger.Info("letterpress server started", "address", config.Address, "static", config.StaticDirectory != "")

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("graceful shutdown started")
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		logger.Info("graceful shutdown complete")
		return nil
	}
}
