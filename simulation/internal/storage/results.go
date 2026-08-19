// simulation/internal/storage/results.go
package storage

import (
	"context"

	"simulation/internal/metrics"
)

// GetResults reads the final metrics for a completed backtest run.
// engine.Run always calls SaveResults as its last successful step, so a
// runID that RunBacktest returned without error is guaranteed to have a
// row here.
func (s *Store) GetResults(ctx context.Context, runID string) (metrics.Results, error) {
	var m metrics.Results
	err := s.pool.QueryRow(ctx, `
		SELECT total_return_pct, max_drawdown_pct, sharpe_ratio, sortino_ratio,
		       annualized_volatility_pct, win_rate_pct, total_trades, avg_trade_pct
		FROM backtest_results WHERE run_id = $1
	`, runID).Scan(&m.TotalReturnPct, &m.MaxDrawdownPct, &m.SharpeRatio, &m.SortinoRatio,
		&m.AnnualizedVolatilityPct, &m.WinRatePct, &m.TotalTrades, &m.AvgTradePct)
	return m, err
}
