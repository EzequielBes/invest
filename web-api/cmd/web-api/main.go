package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	riskstorage "risk-engine/storage"

	"execution/paperstore"

	"web-api/internal/api"
	"web-api/internal/storage"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := storage.New(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("connect storage: %w", err)
	}
	defer store.Close()

	riskStore, err := riskstorage.New(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()

	paperStore, err := paperstore.New(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("connect paper storage: %w", err)
	}
	defer paperStore.Close()

	frontendDir := os.Getenv("FRONTEND_DIST_DIR")
	handler := api.NewServer(store, dsn, riskStore, paperStore, frontendDir)
	addr := os.Getenv("WEB_API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("web-api listening on %s (frontend dir: %q)", addr, frontendDir)
	return http.ListenAndServe(addr, handler)
}
