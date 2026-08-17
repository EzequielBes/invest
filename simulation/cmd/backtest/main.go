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

	"simulation/internal/engine"
	simstorage "simulation/internal/storage"
	"simulation/internal/strategy"
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
	if !periodStart.Before(periodEnd) {
		return fmt.Errorf("-period-start must be before -period-end")
	}
	if feePct < 0 {
		return fmt.Errorf("-fee-pct must be >= 0")
	}
	timeframes := splitNonEmpty(timeframesStr)
	if len(timeframes) == 0 {
		return fmt.Errorf("-timeframes is required")
	}
	if drivingTF == "" {
		return fmt.Errorf("-driving-timeframe is required")
	}
	found := false
	for _, tf := range timeframes {
		if tf == drivingTF {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("-driving-timeframe %q must be one of -timeframes %v", drivingTF, timeframes)
	}
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
	}
	if shortPeriod <= 0 || longPeriod <= 0 {
		return fmt.Errorf("-ma-short-period and -ma-long-period must be > 0")
	}
	if shortPeriod >= longPeriod {
		return fmt.Errorf("-ma-short-period must be < -ma-long-period")
	}

	strat := &strategy.MovingAverageCrossStrategy{
		Asset: assets[0], Timeframe: drivingTF,
		ShortPeriod: shortPeriod, LongPeriod: longPeriod, TradeValue: initialCash * 0.1,
	}

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

	runID, err := engine.Run(ctx, riskStore, simStore, engine.Config{
		StrategyName: "moving-average", Strategy: strat,
		PeriodStart: periodStart, PeriodEnd: periodEnd,
		Timeframes: timeframes, DrivingTimeframe: drivingTF, Assets: assets,
		InitialCash: initialCash, FeePct: feePct,
	})
	if err != nil {
		return fmt.Errorf("backtest run %s: %w", runID, err)
	}

	tradeCount, err := simStore.TradeCount(ctx, runID)
	if err != nil {
		fmt.Printf("backtest run %s completed\n", runID)
		fmt.Fprintf(os.Stderr, "warning: failed to fetch trade count for run %s: %v\n", runID, err)
		return nil
	}
	fmt.Printf("backtest run %s completed (%d trade attempts recorded)\n", runID, tradeCount)
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
