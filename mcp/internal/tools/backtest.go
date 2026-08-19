// mcp/internal/tools/backtest.go
package tools

import (
	"context"
	"time"

	riskstorage "risk-engine/storage"

	simrunner "simulation/runner"
)

// RunBacktestArgs is the run_backtest tool's input. PeriodStart/PeriodEnd
// decode straight from RFC3339 JSON strings via time.Time's native
// json.Unmarshaler — no manual parsing needed here, unlike cmd/backtest's
// CLI flags (see Global Constraints).
type RunBacktestArgs struct {
	PeriodStart      time.Time `json:"period_start" jsonschema:"RFC3339 start of the backtest period"`
	PeriodEnd        time.Time `json:"period_end" jsonschema:"RFC3339 end of the backtest period"`
	Timeframes       []string  `json:"timeframes" jsonschema:"timeframes configured for this run, e.g. [\"1h\", \"4h\"]"`
	DrivingTimeframe string    `json:"driving_timeframe" jsonschema:"the finest timeframe, drives the simulated clock — must be one of timeframes"`
	Assets           []string  `json:"assets" jsonschema:"asset symbols on the reference exchange"`
	InitialCash      float64   `json:"initial_cash,omitempty" jsonschema:"starting cash, defaults to 10000"`
	FeePct           float64   `json:"fee_pct,omitempty" jsonschema:"fee as a fraction of trade value, defaults to 0.001 (0.1%)"`
	MAShortPeriod    int       `json:"ma_short_period,omitempty" jsonschema:"moving-average strategy short SMA period in candles, defaults to 10"`
	MALongPeriod     int       `json:"ma_long_period,omitempty" jsonschema:"moving-average strategy long SMA period in candles, defaults to 30"`
}

// RunBacktestResult is the run_backtest tool's output. TradeAttempts and
// TotalTrades are deliberately distinct: TradeAttempts is every proposed
// operation the strategy generated, whether risk-engine allowed it or
// not (cmd/backtest's original CLI called this "trade attempts
// recorded"); TotalTrades is the count of allowed, closed trades
// win-rate/avg-trade percentages are actually computed from.
type RunBacktestResult struct {
	BacktestRunID          string  `json:"backtest_run_id"`
	TradeAttempts           int     `json:"trade_attempts"`
	TotalReturnPct           float64 `json:"total_return_pct"`
	MaxDrawdownPct           float64 `json:"max_drawdown_pct"`
	SharpeRatio              float64 `json:"sharpe_ratio"`
	SortinoRatio             float64 `json:"sortino_ratio"`
	AnnualizedVolatilityPct float64 `json:"annualized_volatility_pct"`
	WinRatePct               float64 `json:"win_rate_pct"`
	TotalTrades              int     `json:"total_trades"`
	AvgTradePct              float64 `json:"avg_trade_pct"`
}

// RunBacktest runs a moving-average-cross backtest via
// simulation/runner.RunWithDSN, applying the same defaults cmd/backtest's
// CLI flags do for anything left unset (InitialCash, FeePct,
// MAShortPeriod, MALongPeriod) — RunWithDSN/RunBacktest themselves have
// no opinion on defaults, they only validate whatever values they're
// given.
func RunBacktest(ctx context.Context, dsn string, riskStore *riskstorage.Store, args RunBacktestArgs) (RunBacktestResult, error) {
	cfg := simrunner.Config{
		PeriodStart: args.PeriodStart, PeriodEnd: args.PeriodEnd,
		Timeframes: args.Timeframes, DrivingTimeframe: args.DrivingTimeframe, Assets: args.Assets,
		InitialCash: args.InitialCash, FeePct: args.FeePct,
		MAShortPeriod: args.MAShortPeriod, MALongPeriod: args.MALongPeriod,
	}
	if cfg.InitialCash == 0 {
		cfg.InitialCash = 10000
	}
	if args.FeePct == 0 {
		cfg.FeePct = 0.001
	}
	if cfg.MAShortPeriod == 0 {
		cfg.MAShortPeriod = 10
	}
	if cfg.MALongPeriod == 0 {
		cfg.MALongPeriod = 30
	}

	runID, tradeAttempts, results, err := simrunner.RunWithDSN(ctx, dsn, riskStore, cfg)
	if err != nil {
		return RunBacktestResult{}, err
	}
	return RunBacktestResult{
		BacktestRunID: runID, TradeAttempts: tradeAttempts,
		TotalReturnPct: results.TotalReturnPct, MaxDrawdownPct: results.MaxDrawdownPct,
		SharpeRatio: results.SharpeRatio, SortinoRatio: results.SortinoRatio,
		AnnualizedVolatilityPct: results.AnnualizedVolatilityPct, WinRatePct: results.WinRatePct,
		TotalTrades: results.TotalTrades, AvgTradePct: results.AvgTradePct,
	}, nil
}
