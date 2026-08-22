// simulation/internal/engine/run.go
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"simulation/internal/marketview"
	"simulation/internal/metrics"
	"simulation/internal/portfolio"
	simstorage "simulation/internal/storage"
	"simulation/internal/strategy"
)

// Config is everything one backtest run needs, taken directly from CLI
// flags after validation.
type Config struct {
	StrategyName     string
	Strategy         strategy.Strategy
	PeriodStart      time.Time
	PeriodEnd        time.Time
	Timeframes       []string
	DrivingTimeframe string
	Assets           []string
	InitialCash      float64
	FeePct           float64
}

// Run executes one full backtest: creates the run record and an isolated
// risk_state row, advances the simulated clock candle by candle (driving
// timeframe), marks the portfolio to market, applies any pending fill,
// asks the Strategy for new operations, evaluates each through the real
// risk-engine, records every trade attempt, and finally computes and
// persists metrics. A failure mid-run marks the run 'failed' with the
// error message rather than leaving a silent partial result.
func Run(ctx context.Context, riskStore *riskstorage.Store, simStore *simstorage.Store, cfg Config) (runID string, err error) {
	drivingDuration, err := simstorage.TimeframeDuration(cfg.DrivingTimeframe)
	if err != nil {
		return "", fmt.Errorf("engine: driving timeframe: %w", err)
	}

	runID = uuid.NewString()
	if err := simStore.CreateRun(ctx, simstorage.Run{
		ID: runID, StrategyName: cfg.StrategyName,
		PeriodStart: cfg.PeriodStart, PeriodEnd: cfg.PeriodEnd,
		Timeframes: cfg.Timeframes, DrivingTimeframe: cfg.DrivingTimeframe,
		InitialCash: cfg.InitialCash, FeePct: cfg.FeePct,
	}); err != nil {
		return "", fmt.Errorf("engine: create run: %w", err)
	}
	if err := riskStore.InitRunState(ctx, runID); err != nil {
		return runID, finish(ctx, simStore, runID, fmt.Errorf("engine: init run state: %w", err))
	}

	runErr := runLoop(ctx, riskStore, simStore, runID, drivingDuration, cfg)
	return runID, finish(ctx, simStore, runID, runErr)
}

func finish(ctx context.Context, simStore *simstorage.Store, runID string, runErr error) error {
	if err := simStore.FinishRun(ctx, runID, runErr); err != nil {
		if runErr != nil {
			return fmt.Errorf("%w (also failed to record failure: %v)", runErr, err)
		}
		return fmt.Errorf("engine: finish run: %w", err)
	}
	return runErr
}

func runLoop(ctx context.Context, riskStore *riskstorage.Store, simStore *simstorage.Store, runID string, drivingDuration time.Duration, cfg Config) error {
	clock := NewClock(cfg.PeriodStart, cfg.PeriodEnd, drivingDuration)
	view := marketview.New(simStore)
	tracker := portfolio.NewTracker(cfg.InitialCash)

	var pending []PendingFill
	var tradeReturnsPct []float64

	for {
		openTime, closeTime, ok := clock.Next()
		if !ok {
			break
		}
		view.Advance(closeTime)

		candles, err := latestDrivingCandles(ctx, simStore, cfg.Assets, cfg.DrivingTimeframe, closeTime)
		if err != nil {
			return fmt.Errorf("engine: fetch candles at %s: %w", closeTime, err)
		}

		closes := make(map[string]float64, len(candles))
		for asset, c := range candles {
			closes[asset] = c.Close
		}
		cash, positionsValue, totalEquity := tracker.MarkToMarket(closeTime, closes)
		if err := simStore.RecordEquityPoint(ctx, simstorage.EquityPoint{
			RunID: runID, Time: closeTime, Cash: cash, PositionsValue: positionsValue, TotalEquity: totalEquity,
		}); err != nil {
			return fmt.Errorf("engine: record equity point: %w", err)
		}

		for _, fill := range pending {
			c, ok := candles[fill.Asset]
			if !ok {
				continue // no candle for this asset at this instant; the fill is dropped
			}
			openPrice := c.Open
			fee := applyFee(fill.Quantity*openPrice, cfg.FeePct)
			realized := tracker.ApplyFill(portfolio.Fill{
				Time: openTime, Asset: fill.Asset, Side: fill.Side, Quantity: fill.Quantity, Price: openPrice, Fee: fee,
			})
			if fill.Side == risk.SideSell {
				tradeReturnsPct = append(tradeReturnsPct, realized/(fill.Quantity*openPrice)*100)
			}
			if err := simStore.RecordTrade(ctx, simstorage.Trade{
				RunID: runID, Time: openTime, Asset: fill.Asset, Side: string(fill.Side),
				Quantity: fill.Quantity, Price: openPrice, Fee: fee, Allowed: true,
			}); err != nil {
				return fmt.Errorf("engine: record trade: %w", err)
			}
		}
		pending = pending[:0]

		snap := tracker.Snapshot(closeTime)
		proposed, err := cfg.Strategy.Decide(ctx, view, snap)
		if err != nil {
			return fmt.Errorf("engine: strategy decide at %s: %w", closeTime, err)
		}

		asOf := closeTime
		for _, op := range proposed {
			decision, err := risk.Evaluate(ctx, riskStore, risk.PortfolioState(snap), op, risk.EvalOptions{AsOf: &asOf, RunID: &runID})
			if err != nil {
				return fmt.Errorf("engine: risk evaluate at %s: %w", closeTime, err)
			}
			if !decision.Allowed {
				var reason string
				if len(decision.Reasons) > 0 {
					reason = decision.Reasons[0]
				}
				if err := simStore.RecordTrade(ctx, simstorage.Trade{
					RunID: runID, Time: closeTime, Asset: op.Asset, Side: string(op.Side),
					Quantity: op.Quantity, Price: 0, Fee: 0, Allowed: false, RejectReason: &reason,
				}); err != nil {
					return fmt.Errorf("engine: record rejected trade: %w", err)
				}
				continue
			}
			pending = append(pending, PendingFill{Asset: op.Asset, Side: op.Side, Quantity: op.Quantity})
		}
	}

	periodsPerYear := (365 * 24 * time.Hour).Seconds() / drivingDuration.Seconds()
	results := metrics.Compute(tracker.EquityCurve(), tradeReturnsPct, periodsPerYear)
	if err := simStore.SaveResults(ctx, runID, results); err != nil {
		return fmt.Errorf("engine: save results: %w", err)
	}
	return nil
}

// latestDrivingCandles fetches each configured asset's most recent
// driving-timeframe candle closed at or before asOf — used both for
// mark-to-market (via its Close) and for resolving a pending fill's
// execution price (via its Open, this same candle one step later).
func latestDrivingCandles(ctx context.Context, simStore *simstorage.Store, assets []string, drivingTimeframe string, asOf time.Time) (map[string]simstorage.Candle, error) {
	out := map[string]simstorage.Candle{}
	for _, asset := range assets {
		candles, err := simStore.RecentCandles(ctx, risk.ExchangeFor(asset), asset, drivingTimeframe, 1, asOf)
		if err != nil {
			return nil, err
		}
		if len(candles) == 0 {
			continue
		}
		out[asset] = candles[0]
	}
	return out, nil
}
