package runner

import (
	"context"
	"fmt"
	"time"

	"strategist/internal/storage"
)

// EvaluateOutcomesWithDSN records due 1h, 4h, and 24h outcomes from stored
// Binance 1m candles. It makes no model or exchange calls.
func EvaluateOutcomesWithDSN(ctx context.Context, dsn string) (int, error) {
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()

	intents, err := store.DueIntentOutcomes(ctx, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("read due intent outcomes: %w", err)
	}
	count := 0
	for _, intent := range intents {
		entry, found, err := store.CloseAfter(ctx, intent.Asset, intent.CreatedAt)
		if err != nil {
			return count, fmt.Errorf("read %s entry close: %w", intent.Asset, err)
		}
		if !found || entry <= 0 {
			continue
		}
		exit, found, err := store.CloseAfter(ctx, intent.Asset, intent.CreatedAt.Add(intent.Horizon))
		if err != nil {
			return count, fmt.Errorf("read %s horizon close: %w", intent.Asset, err)
		}
		if !found {
			continue
		}
		returnPct, correct := outcome(intent.Side, entry, exit)
		created, err := store.SaveIntentOutcome(ctx, intent, returnPct, correct)
		if err != nil {
			return count, fmt.Errorf("save intent outcome: %w", err)
		}
		if created {
			count++
		}
	}
	return count, nil
}

func outcome(side string, entry, exit float64) (float64, bool) {
	returnPct := (exit - entry) / entry * 100
	if side == "sell" {
		returnPct = -returnPct
	}
	return returnPct, returnPct > 0
}
