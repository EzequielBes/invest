// simulation/internal/engine/run_test.go
package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	simstorage "simulation/internal/storage"
	"simulation/internal/strategy"
)

func testStores(t *testing.T) (*riskstorage.Store, *simstorage.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping engine integration tests")
	}
	riskStore, err := riskstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	t.Cleanup(func() { riskStore.Close() })
	simStore, err := simstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("simstorage.New: %v", err)
	}
	t.Cleanup(func() { simStore.Close() })
	return riskStore, simStore
}

func seedHourlyCandles(t *testing.T, simStore *simstorage.Store, asset string, base time.Time, closes []float64) {
	t.Helper()
	ctx := context.Background()
	for i, c := range closes {
		ts := base.Add(time.Duration(i) * time.Hour)
		if err := simStore.InsertCandleForTest(ctx, risk.ReferenceExchange, asset, "1h", ts, c, c, c, c, 100000); err != nil {
			t.Fatalf("seed candle %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		simStore.DeleteCandlesForTest(context.Background(), risk.ReferenceExchange, asset, "1h")
	})
}

// seedMinuteCandles inserts two 1m candles near the end of each hour in
// hourlyCloses, matching that hour's price. The 1h candles seedHourlyCandles
// writes drive the simulation's own strategy/mark-to-market path, but
// risk-engine's quality checks (freshness/volatility/liquidity) read a
// hardcoded 1m timeframe from the same candles table — without this, any
// real proposed operation fails those checks regardless of price, and the
// tests below can't tell a genuine approval/breach from a data-starved
// rejection.
func seedMinuteCandles(t *testing.T, simStore *simstorage.Store, asset string, base time.Time, hourlyCloses []float64) {
	t.Helper()
	ctx := context.Background()
	for h, c := range hourlyCloses {
		for _, min := range []int{58, 59} {
			ts := base.Add(time.Duration(h)*time.Hour + time.Duration(min)*time.Minute)
			if err := simStore.InsertCandleForTest(ctx, risk.ReferenceExchange, asset, "1m", ts, c, c, c, c, 100000); err != nil {
				t.Fatalf("seed 1m candle at %s: %v", ts, err)
			}
		}
	}
	t.Cleanup(func() {
		simStore.DeleteCandlesForTest(context.Background(), risk.ReferenceExchange, asset, "1m")
	})
}

// cleanupRun registers deletion of every row a completed/failed Run call
// may have written, across both modules' stores.
func cleanupRun(t *testing.T, simStore *simstorage.Store, riskStore *riskstorage.Store, runID string) {
	t.Helper()
	if runID == "" {
		return
	}
	t.Cleanup(func() {
		ctx := context.Background()
		simStore.DeleteRunForTest(ctx, runID)
		riskStore.DeleteRunStateForTest(ctx, runID)
	})
}

func TestRun_FullBacktest_ProducesConsistentResults(t *testing.T) {
	riskStore, simStore := testStores(t)
	ctx := context.Background()
	asset := "ENGCOIN1_" + t.Name()
	base := time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC)

	// 6 flat, liquid, low-volatility candles so every risk-engine quality
	// rule passes throughout the run.
	closes := []float64{100, 100, 100, 100, 100, 100}
	seedHourlyCandles(t, simStore, asset, base, closes)
	seedMinuteCandles(t, simStore, asset, base, closes)

	strat, err := strategy.NewFixedOperationsStrategy([]strategy.FixedOp{
		{Time: base.Add(90 * time.Minute), Asset: asset, Side: risk.SideBuy, Quantity: 1},
	}, "1h")
	if err != nil {
		t.Fatalf("NewFixedOperationsStrategy: %v", err)
	}

	runID, err := Run(ctx, riskStore, simStore, Config{
		StrategyName: "fixed-replay", Strategy: strat,
		PeriodStart: base, PeriodEnd: base.Add(5 * time.Hour),
		Timeframes: []string{"1h"}, DrivingTimeframe: "1h", Assets: []string{asset},
		InitialCash: 10000, FeePct: 0.001,
	})
	cleanupRun(t, simStore, riskStore, runID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runID == "" {
		t.Fatal("expected a non-empty runID")
	}

	status, err := simStore.RunStatus(ctx, runID)
	if err != nil {
		t.Fatalf("RunStatus: %v", err)
	}
	if status != "completed" {
		t.Fatalf("run status = %q, want %q", status, "completed")
	}

	tradeCount, err := simStore.TradeCount(ctx, runID)
	if err != nil {
		t.Fatalf("TradeCount: %v", err)
	}
	if tradeCount == 0 {
		t.Fatal("expected at least one recorded trade (the scripted buy)")
	}
}

func TestRun_NonLookahead_NeverSeesCandlesPastPeriodEnd(t *testing.T) {
	riskStore, simStore := testStores(t)
	ctx := context.Background()
	asset := "ENGCOIN2_" + t.Name()
	base := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)

	// A moving-average strategy that would clearly signal a buy IF it
	// could see the sharp rise at the end — but that candle is beyond
	// period_end and must never be visible.
	seedHourlyCandles(t, simStore, asset, base, []float64{100, 100, 100, 100, 100, 100, 100, 900})

	strat := &strategy.MovingAverageCrossStrategy{Asset: asset, Timeframe: "1h", ShortPeriod: 2, LongPeriod: 4, TradeValue: 1000}

	runID, err := Run(ctx, riskStore, simStore, Config{
		StrategyName: "moving-average", Strategy: strat,
		PeriodStart: base, PeriodEnd: base.Add(6 * time.Hour), // deliberately excludes the 900-close candle
		Timeframes: []string{"1h"}, DrivingTimeframe: "1h", Assets: []string{asset},
		InitialCash: 10000, FeePct: 0.001,
	})
	cleanupRun(t, simStore, riskStore, runID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	tradeCount, err := simStore.TradeCount(ctx, runID)
	if err != nil {
		t.Fatalf("TradeCount: %v", err)
	}
	if tradeCount != 0 {
		t.Fatalf("trade count = %d, want 0 — a flat series with no visible cross must produce no trades; a non-zero count means the 900-close future candle leaked in", tradeCount)
	}
}

func TestRun_RiskBreach_PausesOnlyThisRun(t *testing.T) {
	riskStore, simStore := testStores(t)
	ctx := context.Background()
	asset := "ENGCOIN3_" + t.Name()
	base := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)

	// Sharp drop drives DailyLoss past the seeded 0.05 limit after the
	// first buy is filled and marked to market.
	closes := []float64{100, 100, 100, 40, 40, 40}
	seedHourlyCandles(t, simStore, asset, base, closes)
	seedMinuteCandles(t, simStore, asset, base, closes)

	// Quantity 9 @ ~100 keeps trade value (900) under the seeded
	// max_value_per_trade limit (1000) so the buy is actually approved.
	// The second op lands in [base+3h, base+4h) — the window where the
	// crash to 40 has already been marked to market — so this genuinely
	// Strategy-proposed operation is the one that flows through
	// risk.Evaluate, gets rejected on daily_loss, and triggers the pause
	// (mirroring exactly how live operation only checks limits when a
	// trade is actually proposed).
	strat, err := strategy.NewFixedOperationsStrategy([]strategy.FixedOp{
		{Time: base.Add(30 * time.Minute), Asset: asset, Side: risk.SideBuy, Quantity: 9},
		{Time: base.Add(3*time.Hour + 30*time.Minute), Asset: asset, Side: risk.SideBuy, Quantity: 1},
	}, "1h")
	if err != nil {
		t.Fatalf("NewFixedOperationsStrategy: %v", err)
	}

	runID, err := Run(ctx, riskStore, simStore, Config{
		StrategyName: "fixed-replay", Strategy: strat,
		PeriodStart: base, PeriodEnd: base.Add(5 * time.Hour),
		Timeframes: []string{"1h"}, DrivingTimeframe: "1h", Assets: []string{asset},
		InitialCash: 10000, FeePct: 0.001,
	})
	cleanupRun(t, simStore, riskStore, runID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	runIDCopy := runID
	state, err := riskStore.GetState(ctx, &runIDCopy)
	if err != nil {
		t.Fatalf("GetState(runID): %v", err)
	}
	if state.Status != riskstorage.StatusPaused {
		t.Fatalf("run's risk_state.status = %q, want %q — the price drop should have breached daily_loss", state.Status, riskstorage.StatusPaused)
	}

	live, err := riskStore.GetState(ctx, nil)
	if err != nil {
		t.Fatalf("GetState(live): %v", err)
	}
	if live.Status != riskstorage.StatusNormal {
		t.Errorf("live Status = %q, want %q — a backtest breach must never pause live operation", live.Status, riskstorage.StatusNormal)
	}
}
