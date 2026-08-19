// simulation/cmd/backtest/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	riskstorage "risk-engine/storage"

	simstorage "simulation/internal/storage"
	"simulation/runner"
)

func main() {
	var (
		periodStartStr = flag.String("period-start", "", "RFC3339 start of the backtest period (required)")
		periodEndStr   = flag.String("period-end", "", "RFC3339 end of the backtest period (required)")
		timeframesStr  = flag.String("timeframes", "", "comma-separated timeframes configured for this run, e.g. 1h,4h (required)")
		drivingTF      = flag.String("driving-timeframe", "", "the finest timeframe, drives the simulated clock (required, must be in -timeframes)")
		assetsStr      = flag.String("assets", "", "comma-separated asset symbols on the reference exchange (required)")
		initialCash    = flag.Float64("initial-cash", 10000, "starting cash")
		feePct         = flag.Float64("fee-pct", 0.001, "fee as a fraction of trade value, e.g. 0.001 for 0.1%")
		shortPeriod    = flag.Int("ma-short-period", 10, "moving-average strategy: short SMA period, in candles")
		longPeriod     = flag.Int("ma-long-period", 30, "moving-average strategy: long SMA period, in candles")
	)
	flag.Parse()

	if err := run(*periodStartStr, *periodEndStr, *timeframesStr, *drivingTF, *assetsStr, *initialCash, *feePct, *shortPeriod, *longPeriod); err != nil {
		log.Fatal(err)
	}
}

func run(periodStartStr, periodEndStr, timeframesStr, drivingTF, assetsStr string, initialCash, feePct float64, shortPeriod, longPeriod int) error {
	periodStart, err := time.Parse(time.RFC3339, periodStartStr)
	if err != nil {
		return fmt.Errorf("invalid -period-start: %w", err)
	}
	periodEnd, err := time.Parse(time.RFC3339, periodEndStr)
	if err != nil {
		return fmt.Errorf("invalid -period-end: %w", err)
	}
	timeframes := splitNonEmpty(timeframesStr)
	assets := splitNonEmpty(assetsStr)

	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()

	simStore, err := simstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect simulation storage: %w", err)
	}
	defer simStore.Close()

	runID, tradeCount, results, err := runner.RunBacktest(ctx, riskStore, simStore, runner.Config{
		PeriodStart: periodStart, PeriodEnd: periodEnd, Timeframes: timeframes,
		DrivingTimeframe: drivingTF, Assets: assets, InitialCash: initialCash, FeePct: feePct,
		MAShortPeriod: shortPeriod, MALongPeriod: longPeriod,
	})
	if err != nil {
		return err
	}
	fmt.Printf("backtest run %s completed (%d trade attempts recorded, return %.2f%%, max drawdown %.2f%%, sharpe %.2f)\n",
		runID, tradeCount, results.TotalReturnPct, results.MaxDrawdownPct, results.SharpeRatio)
	return nil
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
