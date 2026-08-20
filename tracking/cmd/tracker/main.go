package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"execution/executor"
	"risk-engine/risk"
	"tracking/internal/storage"
)

const (
	defaultIntervalMinutes = 15
	priceTimeframe         = "1m"
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

	execClient, err := executor.NewClient(context.Background(), dsn)
	if err != nil {
		return err
	}
	defer execClient.Close()

	interval := defaultIntervalMinutes
	if raw := os.Getenv("SNAPSHOT_INTERVAL_MINUTES"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			interval = n
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	log.Printf("tracker: snapshotting every %d minutes", interval)
	runLoop(ctx, store, execClient, time.Duration(interval)*time.Minute)
	return nil
}

type executorClient interface {
	FetchPortfolio(ctx context.Context) (cash float64, positions map[string]float64, err error)
}

type priceStore interface {
	LatestPrice(ctx context.Context, exchange, symbol, timeframe string) (price float64, found bool, err error)
	SaveSnapshot(ctx context.Context, s storage.Snapshot) error
}

// runLoop snapshots once immediately, then again on every tick until ctx is done.
func runLoop(ctx context.Context, store priceStore, execClient executorClient, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	snapshotOnce(ctx, store, execClient)
	for {
		select {
		case <-ticker.C:
			snapshotOnce(ctx, store, execClient)
		case <-ctx.Done():
			return
		}
	}
}

// snapshotOnce saves only fully priced portfolios; any failure skips the cycle.
func snapshotOnce(ctx context.Context, store priceStore, execClient executorClient) {
	cash, positions, err := execClient.FetchPortfolio(ctx)
	if err != nil {
		log.Printf("tracker: fetch portfolio: %v", err)
		return
	}

	positionsValue := 0.0
	for asset, qty := range positions {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, asset, priceTimeframe)
		if err != nil {
			log.Printf("tracker: price for %s: %v", asset, err)
			return
		}
		if !found {
			log.Printf("tracker: no price data for %s, skipping this cycle", asset)
			return
		}
		positionsValue += qty * price
	}

	snapshot := storage.Snapshot{
		Timestamp:      time.Now().UTC(),
		Cash:           cash,
		PositionsValue: positionsValue,
		TotalEquity:    cash + positionsValue,
	}
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		log.Printf("tracker: save snapshot: %v", err)
		return
	}
	log.Printf("tracker: snapshot saved: cash=%.2f positions_value=%.2f total_equity=%.2f", snapshot.Cash, snapshot.PositionsValue, snapshot.TotalEquity)
}
