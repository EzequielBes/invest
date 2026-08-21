package storage

import (
	"context"
	"time"
)

// BacktestRun is the read-only subset of a simulation backtest needed for an
// observational validation audit.
type BacktestRun struct {
	ID        string
	Status    string
	FeePct    float64
	StartedAt time.Time
	EndedAt   *time.Time
}

type BacktestTrade struct {
	Time     time.Time
	Quantity float64
	Price    float64
	Allowed  bool
}

type BacktestEquityPoint struct {
	Time        time.Time
	TotalEquity float64
}

func (s *Store) BacktestRun(ctx context.Context, id string) (BacktestRun, error) {
	var run BacktestRun
	err := s.pool.QueryRow(ctx, `
		SELECT id, status, fee_pct, started_at, ended_at
		FROM backtest_runs
		WHERE id = $1`, id).Scan(&run.ID, &run.Status, &run.FeePct, &run.StartedAt, &run.EndedAt)
	return run, err
}

func (s *Store) BacktestTrades(ctx context.Context, runID string) ([]BacktestTrade, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, quantity, price, allowed
		FROM backtest_trades
		WHERE run_id = $1
		ORDER BY ts, id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []BacktestTrade
	for rows.Next() {
		var trade BacktestTrade
		if err := rows.Scan(&trade.Time, &trade.Quantity, &trade.Price, &trade.Allowed); err != nil {
			return nil, err
		}
		trades = append(trades, trade)
	}
	return trades, rows.Err()
}

func (s *Store) BacktestEquityCurve(ctx context.Context, runID string) ([]BacktestEquityPoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, total_equity
		FROM backtest_equity_curve
		WHERE run_id = $1
		ORDER BY ts, id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []BacktestEquityPoint
	for rows.Next() {
		var point BacktestEquityPoint
		if err := rows.Scan(&point.Time, &point.TotalEquity); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, rows.Err()
}
