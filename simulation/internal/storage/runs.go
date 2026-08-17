// simulation/internal/storage/runs.go
package storage

import (
	"context"
	"time"

	"simulation/internal/metrics"
)

type Run struct {
	ID               string
	StrategyName     string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	Timeframes       []string
	DrivingTimeframe string
	InitialCash      float64
	FeePct           float64
}

func (s *Store) CreateRun(ctx context.Context, r Run) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_runs (id, strategy_name, period_start, period_end, timeframes, driving_timeframe, initial_cash, fee_pct, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'running')
	`, r.ID, r.StrategyName, r.PeriodStart, r.PeriodEnd, r.Timeframes, r.DrivingTimeframe, r.InitialCash, r.FeePct)
	return err
}

// FinishRun marks runID 'completed' (runErr nil) or 'failed' with runErr's
// message recorded, and stamps ended_at either way — a backtest never ends
// with a silent partial 'running' row.
func (s *Store) FinishRun(ctx context.Context, runID string, runErr error) error {
	status := "completed"
	var errMsg *string
	if runErr != nil {
		status = "failed"
		msg := runErr.Error()
		errMsg = &msg
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE backtest_runs SET status = $1, error = $2, ended_at = now() WHERE id = $3
	`, status, errMsg, runID)
	return err
}

type Trade struct {
	RunID        string
	Time         time.Time
	Asset        string
	Side         string
	Quantity     float64
	Price        float64
	Fee          float64
	Allowed      bool
	RejectReason *string
}

func (s *Store) RecordTrade(ctx context.Context, t Trade) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_trades (run_id, ts, asset, side, quantity, price, fee, allowed, reject_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, t.RunID, t.Time, t.Asset, t.Side, t.Quantity, t.Price, t.Fee, t.Allowed, t.RejectReason)
	return err
}

type EquityPoint struct {
	RunID          string
	Time           time.Time
	Cash           float64
	PositionsValue float64
	TotalEquity    float64
}

func (s *Store) RecordEquityPoint(ctx context.Context, e EquityPoint) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_equity_curve (run_id, ts, cash, positions_value, total_equity)
		VALUES ($1, $2, $3, $4, $5)
	`, e.RunID, e.Time, e.Cash, e.PositionsValue, e.TotalEquity)
	return err
}

func (s *Store) SaveResults(ctx context.Context, runID string, m metrics.Results) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_results (run_id, total_return_pct, max_drawdown_pct, sharpe_ratio, sortino_ratio, annualized_volatility_pct, win_rate_pct, total_trades, avg_trade_pct)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, runID, m.TotalReturnPct, m.MaxDrawdownPct, m.SharpeRatio, m.SortinoRatio, m.AnnualizedVolatilityPct, m.WinRatePct, m.TotalTrades, m.AvgTradePct)
	return err
}

// DeleteRunForTest removes a run and everything referencing it (trades,
// equity curve, results) — test-only cleanup, not used by production code.
func (s *Store) DeleteRunForTest(ctx context.Context, runID string) {
	s.pool.Exec(ctx, `DELETE FROM backtest_results WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM backtest_equity_curve WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM backtest_trades WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM backtest_runs WHERE id = $1`, runID)
}

// RunStatus reads back a run's current status — used by tests asserting a
// backtest reached 'completed' or 'failed'.
func (s *Store) RunStatus(ctx context.Context, runID string) (string, error) {
	var status string
	err := s.pool.QueryRow(ctx, `SELECT status FROM backtest_runs WHERE id = $1`, runID).Scan(&status)
	return status, err
}

// TradeCount counts backtest_trades rows for runID — used by tests
// asserting trades were actually recorded, not just that Run returned no
// error.
func (s *Store) TradeCount(ctx context.Context, runID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backtest_trades WHERE run_id = $1`, runID).Scan(&count)
	return count, err
}
