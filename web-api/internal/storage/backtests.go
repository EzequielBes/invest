package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type BacktestResults struct {
	TotalReturnPct          float64 `json:"total_return_pct"`
	MaxDrawdownPct          float64 `json:"max_drawdown_pct"`
	SharpeRatio             float64 `json:"sharpe_ratio"`
	SortinoRatio            float64 `json:"sortino_ratio"`
	AnnualizedVolatilityPct float64 `json:"annualized_volatility_pct"`
	WinRatePct              float64 `json:"win_rate_pct"`
	TotalTrades             int     `json:"total_trades"`
	AvgTradePct             float64 `json:"avg_trade_pct"`
}

type BacktestRun struct {
	ID               string           `json:"id"`
	StrategyName     string           `json:"strategy_name"`
	PeriodStart      time.Time        `json:"period_start"`
	PeriodEnd        time.Time        `json:"period_end"`
	Timeframes       []string         `json:"timeframes"`
	DrivingTimeframe string           `json:"driving_timeframe"`
	InitialCash      float64          `json:"initial_cash"`
	FeePct           float64          `json:"fee_pct"`
	Status           string           `json:"status"`
	Error            *string          `json:"error,omitempty"`
	StartedAt        time.Time        `json:"started_at"`
	EndedAt          *time.Time       `json:"ended_at,omitempty"`
	Results          *BacktestResults `json:"results,omitempty"`
}

type BacktestTrade struct {
	Timestamp    time.Time `json:"ts"`
	Asset        string    `json:"asset"`
	Side         string    `json:"side"`
	Quantity     float64   `json:"quantity"`
	Price        float64   `json:"price"`
	Fee          float64   `json:"fee"`
	Allowed      bool      `json:"allowed"`
	RejectReason *string   `json:"reject_reason,omitempty"`
}

type EquityPoint struct {
	Timestamp      time.Time `json:"ts"`
	Cash           float64   `json:"cash"`
	PositionsValue float64   `json:"positions_value"`
	TotalEquity    float64   `json:"total_equity"`
}

type BacktestDetail struct {
	Run         BacktestRun     `json:"run"`
	Trades      []BacktestTrade `json:"trades"`
	EquityCurve []EquityPoint   `json:"equity_curve"`
}

const backtestRunSelect = `
	SELECT r.id, r.strategy_name, r.period_start, r.period_end, r.timeframes,
	       r.driving_timeframe, r.initial_cash, r.fee_pct, r.status, r.error,
	       r.started_at, r.ended_at,
	       res.total_return_pct, res.max_drawdown_pct, res.sharpe_ratio, res.sortino_ratio,
	       res.annualized_volatility_pct, res.win_rate_pct, res.total_trades, res.avg_trade_pct
	FROM backtest_runs r
	LEFT JOIN backtest_results res ON res.run_id = r.id
`

func scanBacktestRun(row pgx.Row) (BacktestRun, error) {
	var r BacktestRun
	var totalReturnPct, maxDrawdownPct, sharpeRatio, sortinoRatio, annualizedVolatilityPct, winRatePct, avgTradePct *float64
	var totalTrades *int
	err := row.Scan(&r.ID, &r.StrategyName, &r.PeriodStart, &r.PeriodEnd, &r.Timeframes,
		&r.DrivingTimeframe, &r.InitialCash, &r.FeePct, &r.Status, &r.Error,
		&r.StartedAt, &r.EndedAt,
		&totalReturnPct, &maxDrawdownPct, &sharpeRatio, &sortinoRatio,
		&annualizedVolatilityPct, &winRatePct, &totalTrades, &avgTradePct)
	if err != nil {
		return BacktestRun{}, err
	}
	if totalReturnPct != nil {
		r.Results = &BacktestResults{
			TotalReturnPct: *totalReturnPct, MaxDrawdownPct: *maxDrawdownPct,
			SharpeRatio: *sharpeRatio, SortinoRatio: *sortinoRatio,
			AnnualizedVolatilityPct: *annualizedVolatilityPct, WinRatePct: *winRatePct,
			TotalTrades: *totalTrades, AvgTradePct: *avgTradePct,
		}
	}
	return r, nil
}

func (s *Store) RecentBacktests(ctx context.Context, limit int) ([]BacktestRun, error) {
	rows, err := s.pool.Query(ctx, backtestRunSelect+`
		ORDER BY r.started_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []BacktestRun{}
	for rows.Next() {
		r, err := scanBacktestRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) BacktestDetail(ctx context.Context, id string) (BacktestDetail, error) {
	run, err := scanBacktestRun(s.pool.QueryRow(ctx, backtestRunSelect+` WHERE r.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return BacktestDetail{}, ErrNotFound
	}
	if err != nil {
		return BacktestDetail{}, err
	}

	tradeRows, err := s.pool.Query(ctx, `
		SELECT ts, asset, side, quantity, price, fee, allowed, reject_reason
		FROM backtest_trades
		WHERE run_id = $1
		ORDER BY ts
	`, id)
	if err != nil {
		return BacktestDetail{}, err
	}
	defer tradeRows.Close()

	trades := []BacktestTrade{}
	for tradeRows.Next() {
		var t BacktestTrade
		if err := tradeRows.Scan(&t.Timestamp, &t.Asset, &t.Side, &t.Quantity, &t.Price, &t.Fee, &t.Allowed, &t.RejectReason); err != nil {
			return BacktestDetail{}, err
		}
		trades = append(trades, t)
	}
	if err := tradeRows.Err(); err != nil {
		return BacktestDetail{}, err
	}

	equityRows, err := s.pool.Query(ctx, `
		SELECT ts, cash, positions_value, total_equity
		FROM backtest_equity_curve
		WHERE run_id = $1
		ORDER BY ts
	`, id)
	if err != nil {
		return BacktestDetail{}, err
	}
	defer equityRows.Close()

	equity := []EquityPoint{}
	for equityRows.Next() {
		var e EquityPoint
		if err := equityRows.Scan(&e.Timestamp, &e.Cash, &e.PositionsValue, &e.TotalEquity); err != nil {
			return BacktestDetail{}, err
		}
		equity = append(equity, e)
	}
	if err := equityRows.Err(); err != nil {
		return BacktestDetail{}, err
	}

	return BacktestDetail{Run: run, Trades: trades, EquityCurve: equity}, nil
}
