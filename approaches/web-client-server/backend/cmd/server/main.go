package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"letterpress/backend/internal/app"
	"letterpress/backend/internal/game"
	"letterpress/backend/internal/httpapi"
	"letterpress/backend/internal/store/sqlite"
)

func main() {
	address := flag.String("addr", ":8080", "HTTP listen address")
	databasePath := flag.String("db", "letterpress.db", "SQLite database path")
	insecureCookies := flag.Bool("insecure-cookies", false, "allow session cookies over HTTP for local development")
	allowedOrigin := flag.String("allowed-origin", "", "allowed browser origin (defaults to the request host)")
	fixedBoard := flag.String("fixed-board", "", "fixed 25-letter A-Z board for deterministic development tests")
	flag.Parse()

	repository, err := sqlite.Open(context.Background(), *databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := repository.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

	service := app.NewService(repository)
	if *fixedBoard != "" {
		generator, err := game.NewFixedBoardGenerator(*fixedBoard)
		if err != nil {
			log.Fatal(err)
		}
		service = app.NewServiceWithBoardGenerator(repository, generator)
	}

	server := &http.Server{
		Addr: *address,
		Handler: httpapi.NewHandler(service, httpapi.Config{
			InsecureCookies: *insecureCookies,
			AllowedOrigin:   *allowedOrigin,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("letterpress backend listening on %s", *address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
