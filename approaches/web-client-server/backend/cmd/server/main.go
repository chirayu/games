package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"letterpress/backend/internal/httpapi"
)

func main() {
	address := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	server := &http.Server{
		Addr:              *address,
		Handler:           httpapi.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("letterpress backend listening on %s", *address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
