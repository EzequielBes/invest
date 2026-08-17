// simulation/internal/storage/runs_test.go
package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"simulation/internal/metrics"
)

func TestCreateRun_And_FinishRun_Completed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	r := Run{
		ID: runID, StrategyName: "fixed-replay",
		PeriodStart: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC),
		Timeframes:  []string{"1h"}, DrivingTimeframe: "1h",
		InitialCash: 10000, FeePct: 0.001,
	}
	if err := s.CreateRun(ctx, r); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		s.pool.Exec(ctx, `DELETE FROM backtest_results WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_equity_curve WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_trades WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_runs WHERE id = $1`, runID)
	})

	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM backtest_runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "running" {
		t.Errorf("status = %q, want %q", status, "running")
	}

	if err := s.FinishRun(ctx, runID, nil); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT status FROM backtest_runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "completed" {
		t.Errorf("status = %q, want %q", status, "completed")
	}
}

func TestFinishRun_Failed_RecordsError(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	if err := s.CreateRun(ctx, Run{ID: runID, StrategyName: "x", PeriodStart: time.Now(), PeriodEnd: time.Now(), Timeframes: []string{"1h"}, DrivingTimeframe: "1h", InitialCash: 1, FeePct: 0}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	t.Cleanup(func() { s.pool.Exec(context.Background(), `DELETE FROM backtest_runs WHERE id = $1`, runID) })

	if err := s.FinishRun(ctx, runID, errors.New("database connection lost")); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	var status, errMsg string
	if err := s.pool.QueryRow(ctx, `SELECT status, error FROM backtest_runs WHERE id = $1`, runID).Scan(&status, &errMsg); err != nil {
		t.Fatalf("query: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want %q", status, "failed")
	}
	if errMsg != "database connection lost" {
		t.Errorf("error = %q, want %q", errMsg, "database connection lost")
	}
}

func TestRecordTrade_And_RecordEquityPoint_And_SaveResults(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	if err := s.CreateRun(ctx, Run{ID: runID, StrategyName: "x", PeriodStart: time.Now(), PeriodEnd: time.Now(), Timeframes: []string{"1h"}, DrivingTimeframe: "1h", InitialCash: 10000, FeePct: 0.001}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		s.pool.Exec(ctx, `DELETE FROM backtest_results WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_equity_curve WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_trades WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_runs WHERE id = $1`, runID)
	})

	if err := s.RecordTrade(ctx, Trade{RunID: runID, Time: time.Now(), Asset: "BTC", Side: "buy", Quantity: 1, Price: 100, Fee: 0.1, Allowed: true}); err != nil {
		t.Fatalf("RecordTrade: %v", err)
	}
	reason := "daily_loss breached"
	if err := s.RecordTrade(ctx, Trade{RunID: runID, Time: time.Now(), Asset: "BTC", Side: "sell", Quantity: 1, Price: 0, Fee: 0, Allowed: false, RejectReason: &reason}); err != nil {
		t.Fatalf("RecordTrade (rejected): %v", err)
	}

	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backtest_trades WHERE run_id = $1`, runID).Scan(&count); err != nil {
		t.Fatalf("count trades: %v", err)
	}
	if count != 2 {
		t.Errorf("trade count = %d, want 2", count)
	}

	if err := s.RecordEquityPoint(ctx, EquityPoint{RunID: runID, Time: time.Now(), Cash: 9900, PositionsValue: 100, TotalEquity: 10000}); err != nil {
		t.Fatalf("RecordEquityPoint: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backtest_equity_curve WHERE run_id = $1`, runID).Scan(&count); err != nil {
		t.Fatalf("count equity points: %v", err)
	}
	if count != 1 {
		t.Errorf("equity point count = %d, want 1", count)
	}

	results := metrics.Results{
		TotalReturnPct: 5, MaxDrawdownPct: 2, SharpeRatio: 1.1, SortinoRatio: 1.5,
		AnnualizedVolatilityPct: 20, WinRatePct: 60, TotalTrades: 2, AvgTradePct: 1.2,
	}
	if err := s.SaveResults(ctx, runID, results); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}
	var gotSharpe float64
	if err := s.pool.QueryRow(ctx, `SELECT sharpe_ratio FROM backtest_results WHERE run_id = $1`, runID).Scan(&gotSharpe); err != nil {
		t.Fatalf("query results: %v", err)
	}
	if gotSharpe != 1.1 {
		t.Errorf("sharpe_ratio = %v, want 1.1", gotSharpe)
	}
}
