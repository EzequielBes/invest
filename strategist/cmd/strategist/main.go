// strategist/cmd/strategist/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	riskstorage "risk-engine/storage"

	"strategist/internal/llm"
	"strategist/internal/storage"
	"strategist/runner"
)

func main() {
	runID := flag.String("run-id", "", "analysis_run_id to decide from (required)")
	assetsStr := flag.String("assets", "", "comma-separated asset symbols to decide on (required)")
	timeframe := flag.String("timeframe", "1h", "timeframe used to look up the current price")
	cash := flag.Float64("cash", 0, "cash available, in USD (required)")
	positionsStr := flag.String("positions", "", "comma-separated SYMBOL:quantity current positions")
	dailyLoss := flag.Float64("daily-loss", 0, "portfolio daily loss so far, as a fraction (e.g. 0.02 = 2%)")
	weeklyLoss := flag.Float64("weekly-loss", 0, "portfolio weekly loss so far, as a fraction")
	drawdown := flag.Float64("drawdown", 0, "portfolio drawdown from peak, as a fraction")
	consecutiveLosses := flag.Int("consecutive-losses", 0, "number of consecutive losing trades")
	flag.Parse()

	if err := run(context.Background(), *runID, *assetsStr, *timeframe, *cash, *positionsStr, *dailyLoss, *weeklyLoss, *drawdown, *consecutiveLosses); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, runID, assetsStr, timeframe string, cash float64, positionsStr string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) error {
	if runID == "" {
		return fmt.Errorf("-run-id is required")
	}
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
	}
	positions, err := parsePositions(positionsStr)
	if err != nil {
		return err
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()
	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()

	client := llm.NewAnthropicClient()
	return runner.Run(ctx, store, riskStore, client, runID, assets, timeframe, cash, positions, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
}

func splitNonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parsePositions(value string) (map[string]float64, error) {
	positions := make(map[string]float64)
	for _, entry := range splitNonEmpty(value) {
		symbol, qtyStr, found := strings.Cut(entry, ":")
		symbol = strings.TrimSpace(symbol)
		qtyStr = strings.TrimSpace(qtyStr)
		if !found || symbol == "" || qtyStr == "" {
			return nil, fmt.Errorf("invalid -positions entry %q (want SYMBOL:quantity)", entry)
		}
		qty, err := strconv.ParseFloat(qtyStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid -positions entry %q: %w", entry, err)
		}
		positions[symbol] = qty
	}
	return positions, nil
}
