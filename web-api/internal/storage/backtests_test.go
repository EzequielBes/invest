package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBacktestDetail_UnknownIDReturnsErrNotFound(t *testing.T) {
	store := testStore(t)

	_, err := store.BacktestDetail(context.Background(), testID(t, "nonexistent-backtest"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestBacktestDetail_ReturnsRunTradesAndEquityCurve(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	runID := testID(t, "backtest-run")
	started := time.Now().UTC()

	_, err := store.pool.Exec(ctx, `
		INSERT INTO backtest_runs
			(id, strategy_name, period_start, period_end, timeframes, driving_timeframe,
			 initial_cash, fee_pct, status, started_at, ended_at)
		VALUES ($1, 'test-strategy', $2, $2, ARRAY['1h'], '1h', 10000, 0.001, 'completed', $2, $2)
	`, runID, started)
	if err != nil {
		t.Fatalf("seed backtest_runs: %v", err)
	}
	// Register parent cleanup first: t.Cleanup runs in LIFO order.
	t.Cleanup(func() { deleteBacktestRunForTest(t, store, runID) })

	_, err = store.pool.Exec(ctx, `
		INSERT INTO backtest_results
			(run_id, total_return_pct, max_drawdown_pct, sharpe_ratio, sortino_ratio,
			 annualized_volatility_pct, win_rate_pct, total_trades, avg_trade_pct)
		VALUES ($1, 12.5, -3.2, 1.4, 1.8, 20.0, 55.0, 10, 1.25)
	`, runID)
	if err != nil {
		t.Fatalf("seed backtest_results: %v", err)
	}
	t.Cleanup(func() { deleteBacktestResultsForTest(t, store, runID) })

	_, err = store.pool.Exec(ctx, `
		INSERT INTO backtest_trades (run_id, ts, asset, side, quantity, price, fee, allowed)
		VALUES ($1, $2, 'BTC', 'buy', 0.1, 50000, 5, true)
	`, runID, started)
	if err != nil {
		t.Fatalf("seed backtest_trades: %v", err)
	}
	t.Cleanup(func() { deleteBacktestTradesForTest(t, store, runID) })

	_, err = store.pool.Exec(ctx, `
		INSERT INTO backtest_equity_curve (run_id, ts, cash, positions_value, total_equity)
		VALUES ($1, $2, 5000, 5000, 10000)
	`, runID, started)
	if err != nil {
		t.Fatalf("seed backtest_equity_curve: %v", err)
	}
	t.Cleanup(func() { deleteBacktestEquityCurveForTest(t, store, runID) })

	detail, err := store.BacktestDetail(ctx, runID)
	if err != nil {
		t.Fatalf("BacktestDetail: %v", err)
	}
	if detail.Run.Results == nil || detail.Run.Results.SharpeRatio != 1.4 {
		t.Errorf("Run.Results = %+v, want SharpeRatio 1.4", detail.Run.Results)
	}
	if len(detail.Trades) != 1 || detail.Trades[0].Asset != "BTC" {
		t.Errorf("Trades = %+v, want one BTC trade", detail.Trades)
	}
	if len(detail.EquityCurve) != 1 || detail.EquityCurve[0].TotalEquity != 10000 {
		t.Errorf("EquityCurve = %+v, want one point with TotalEquity 10000", detail.EquityCurve)
	}
}

func deleteBacktestEquityCurveForTest(t *testing.T, store *Store, runID string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM backtest_equity_curve WHERE run_id = $1`, runID); err != nil {
		t.Errorf("cleanup backtest equity curve %s: %v", runID, err)
	}
}

func deleteBacktestTradesForTest(t *testing.T, store *Store, runID string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM backtest_trades WHERE run_id = $1`, runID); err != nil {
		t.Errorf("cleanup backtest trades %s: %v", runID, err)
	}
}

func deleteBacktestResultsForTest(t *testing.T, store *Store, runID string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM backtest_results WHERE run_id = $1`, runID); err != nil {
		t.Errorf("cleanup backtest results %s: %v", runID, err)
	}
}

func deleteBacktestRunForTest(t *testing.T, store *Store, id string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM backtest_runs WHERE id = $1`, id); err != nil {
		t.Errorf("cleanup backtest run %s: %v", id, err)
	}
}
