package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	riskstorage "risk-engine/storage"
	simrunner "simulation/runner"
)

type triggerBacktestRequest struct {
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	Timeframes       []string  `json:"timeframes"`
	DrivingTimeframe string    `json:"driving_timeframe"`
	Assets           []string  `json:"assets"`
	InitialCash      float64   `json:"initial_cash,omitempty"`
	FeePct           float64   `json:"fee_pct,omitempty"`
	MAShortPeriod    int       `json:"ma_short_period,omitempty"`
	MALongPeriod     int       `json:"ma_long_period,omitempty"`
}

type triggerBacktestResponse struct {
	BacktestRunID           string  `json:"backtest_run_id"`
	TradeAttempts           int     `json:"trade_attempts"`
	TotalReturnPct          float64 `json:"total_return_pct"`
	MaxDrawdownPct          float64 `json:"max_drawdown_pct"`
	SharpeRatio             float64 `json:"sharpe_ratio"`
	SortinoRatio            float64 `json:"sortino_ratio"`
	AnnualizedVolatilityPct float64 `json:"annualized_volatility_pct"`
	WinRatePct              float64 `json:"win_rate_pct"`
	TotalTrades             int     `json:"total_trades"`
	AvgTradePct             float64 `json:"avg_trade_pct"`
}

// handleTriggerBacktest runs a moving-average-cross backtest via
// simulation/runner.RunWithDSN — the one write endpoint in this
// otherwise read-only API, mirroring mcp's run_backtest tool exactly
// (same defaults, same validation delegated to RunWithDSN itself).
//
// Unlike every read handler in this package, a failure here returns
// RunWithDSN's actual error text (400, not a generic 500): those errors
// are the request's own validation feedback (bad period, unknown
// timeframe, missing assets) that the person filling out the form needs
// to see and act on, not an internal implementation detail to hide.
func handleTriggerBacktest(dsn string, riskStore *riskstorage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req triggerBacktestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		cfg := simrunner.Config{
			PeriodStart: req.PeriodStart, PeriodEnd: req.PeriodEnd,
			Timeframes: req.Timeframes, DrivingTimeframe: req.DrivingTimeframe, Assets: req.Assets,
			InitialCash: req.InitialCash, FeePct: req.FeePct,
			MAShortPeriod: req.MAShortPeriod, MALongPeriod: req.MALongPeriod,
		}
		if cfg.InitialCash == 0 {
			cfg.InitialCash = 10000
		}
		if cfg.FeePct == 0 {
			cfg.FeePct = 0.001
		}
		if cfg.MAShortPeriod == 0 {
			cfg.MAShortPeriod = 10
		}
		if cfg.MALongPeriod == 0 {
			cfg.MALongPeriod = 30
		}

		runID, tradeAttempts, results, err := simrunner.RunWithDSN(r.Context(), dsn, riskStore, cfg)
		if err != nil {
			log.Printf("web-api: RunWithDSN: %v", err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, triggerBacktestResponse{
			BacktestRunID: runID, TradeAttempts: tradeAttempts,
			TotalReturnPct: results.TotalReturnPct, MaxDrawdownPct: results.MaxDrawdownPct,
			SharpeRatio: results.SharpeRatio, SortinoRatio: results.SortinoRatio,
			AnnualizedVolatilityPct: results.AnnualizedVolatilityPct, WinRatePct: results.WinRatePct,
			TotalTrades: results.TotalTrades, AvgTradePct: results.AvgTradePct,
		})
	}
}
