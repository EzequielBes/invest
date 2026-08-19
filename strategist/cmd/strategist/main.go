// strategist/cmd/strategist/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	riskstorage "risk-engine/storage"

	"execution/executor"

	"strategist/internal/llm"
	"strategist/internal/storage"
	"strategist/runner"
)

func main() {
	runID := flag.String("run-id", "", "analysis_run_id to decide from (required)")
	assetsStr := flag.String("assets", "", "comma-separated asset symbols to decide on (required)")
	dailyLoss := flag.Float64("daily-loss", 0, "portfolio daily loss so far, as a fraction (e.g. 0.02 = 2%)")
	weeklyLoss := flag.Float64("weekly-loss", 0, "portfolio weekly loss so far, as a fraction")
	drawdown := flag.Float64("drawdown", 0, "portfolio drawdown from peak, as a fraction")
	consecutiveLosses := flag.Int("consecutive-losses", 0, "number of consecutive losing trades")
	flag.Parse()

	if err := run(context.Background(), *runID, *assetsStr, *dailyLoss, *weeklyLoss, *drawdown, *consecutiveLosses); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, runID, assetsStr string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) error {
	if runID == "" {
		return fmt.Errorf("-run-id is required")
	}
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
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

	client, err := llm.NewClient()
	if err != nil {
		return err
	}
	execClient, err := executor.NewClient(ctx, dsn)
	if err != nil {
		return err
	}
	defer execClient.Close()

	return runner.Run(ctx, store, riskStore, client, execClient, runID, assets, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
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
