// simulation/runner/runner.go
package runner

import (
	"context"
	"fmt"
	"time"

	riskstorage "risk-engine/storage"

	"simulation/internal/engine"
	"simulation/internal/metrics"
	simstorage "simulation/internal/storage"
	"simulation/internal/strategy"
)

// Config is one backtest run's parameters — the same values cmd/backtest's
// CLI flags validate and construct engine.Config from, minus anything
// that's a string-parsing concern (RFC3339 timestamps, comma-separated
// lists) rather than a semantic one. Callers (the CLI, the MCP server)
// each handle their own string parsing and pass typed values here.
type Config struct {
	PeriodStart      time.Time
	PeriodEnd        time.Time
	Timeframes       []string
	DrivingTimeframe string
	Assets           []string
	InitialCash      float64
	FeePct           float64
	MAShortPeriod    int
	MALongPeriod     int
}

// RunBacktest validates cfg, runs a moving-average-cross backtest (the
// only strategy this module offers), and returns the run ID, the number
// of trade attempts recorded, and the final metrics. Exported so both
// cmd/backtest and other modules (the MCP server) can call it directly.
func RunBacktest(ctx context.Context, riskStore *riskstorage.Store, simStore *simstorage.Store, cfg Config) (runID string, tradeCount int, results metrics.Results, err error) {
	if !cfg.PeriodStart.Before(cfg.PeriodEnd) {
		return "", 0, metrics.Results{}, fmt.Errorf("period start must be before period end")
	}
	if cfg.FeePct < 0 {
		return "", 0, metrics.Results{}, fmt.Errorf("fee percentage must be >= 0")
	}
	if len(cfg.Timeframes) == 0 {
		return "", 0, metrics.Results{}, fmt.Errorf("timeframes is required")
	}
	if cfg.DrivingTimeframe == "" {
		return "", 0, metrics.Results{}, fmt.Errorf("driving timeframe is required")
	}
	found := false
	for _, tf := range cfg.Timeframes {
		if tf == cfg.DrivingTimeframe {
			found = true
			break
		}
	}
	if !found {
		return "", 0, metrics.Results{}, fmt.Errorf("driving timeframe %q must be one of timeframes %v", cfg.DrivingTimeframe, cfg.Timeframes)
	}
	if len(cfg.Assets) == 0 {
		return "", 0, metrics.Results{}, fmt.Errorf("assets is required")
	}
	if cfg.MAShortPeriod <= 0 || cfg.MALongPeriod <= 0 {
		return "", 0, metrics.Results{}, fmt.Errorf("ma-short-period and ma-long-period must be > 0")
	}
	if cfg.MAShortPeriod >= cfg.MALongPeriod {
		return "", 0, metrics.Results{}, fmt.Errorf("ma-short-period must be < ma-long-period")
	}

	strat := &strategy.MovingAverageCrossStrategy{
		Asset: cfg.Assets[0], Timeframe: cfg.DrivingTimeframe,
		ShortPeriod: cfg.MAShortPeriod, LongPeriod: cfg.MALongPeriod, TradeValue: cfg.InitialCash * 0.1,
	}

	runID, err = engine.Run(ctx, riskStore, simStore, engine.Config{
		StrategyName: "moving-average", Strategy: strat,
		PeriodStart: cfg.PeriodStart, PeriodEnd: cfg.PeriodEnd,
		Timeframes: cfg.Timeframes, DrivingTimeframe: cfg.DrivingTimeframe, Assets: cfg.Assets,
		InitialCash: cfg.InitialCash, FeePct: cfg.FeePct,
	})
	if err != nil {
		return runID, 0, metrics.Results{}, fmt.Errorf("backtest run %s: %w", runID, err)
	}

	tradeCount, err = simStore.TradeCount(ctx, runID)
	if err != nil {
		return runID, 0, metrics.Results{}, fmt.Errorf("fetch trade count for run %s: %w", runID, err)
	}
	results, err = simStore.GetResults(ctx, runID)
	if err != nil {
		return runID, tradeCount, metrics.Results{}, fmt.Errorf("fetch results for run %s: %w", runID, err)
	}
	return runID, tradeCount, results, nil
}

// RunWithDSN connects its own storage using dsn and calls RunBacktest —
// same cross-module-visibility reason as analysis/strategist's
// RunWithDSN (sub-project 6, Tasks 6-7): callers outside this module
// can't import simulation/internal/storage directly.
func RunWithDSN(ctx context.Context, dsn string, riskStore *riskstorage.Store, cfg Config) (runID string, tradeCount int, results metrics.Results, err error) {
	simStore, err := simstorage.New(ctx, dsn)
	if err != nil {
		return "", 0, metrics.Results{}, fmt.Errorf("connect simulation storage: %w", err)
	}
	defer simStore.Close()
	return RunBacktest(ctx, riskStore, simStore, cfg)
}
