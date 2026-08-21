package audit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"validation/internal/metrics"
	"validation/internal/storage"
	validation "validation/internal/validation"
)

type BacktestInput struct {
	HypothesisID  string
	BacktestRunID string
	Config        json.RawMessage
	GitCommit     string
	Splits        []validation.Split
}

// Backtest records an observational audit of an existing simulation run. It
// never writes to simulation tables or recomputes simulation's saved results.
func Backtest(ctx context.Context, store *storage.Store, input BacktestInput) (storage.Run, error) {
	run, err := createRunWithSplits(ctx, store, storage.Run{
		HypothesisID:  input.HypothesisID,
		BacktestRunID: input.BacktestRunID,
		Config:        input.Config,
		GitCommit:     input.GitCommit,
	}, input.Splits)
	if err != nil {
		return storage.Run{}, err
	}

	backtest, err := store.BacktestRun(ctx, input.BacktestRunID)
	if err != nil {
		if err == pgx.ErrNoRows {
			err := finishWithFinding(ctx, store, run.ID, "failed", validation.Finding{
				Severity: "invalid", Rule: "backtest_not_found", Message: "backtest run does not exist",
				Evidence: map[string]any{"backtest_run_id": input.BacktestRunID},
			})
			run.Status = "failed"
			return run, err
		}
		return run, err
	}

	if backtest.Status != "completed" {
		err := finishWithFinding(ctx, store, run.ID, "inconclusive", validation.Finding{
			Severity: "warning", Rule: "backtest_not_completed", Message: "backtest run is not completed",
			Evidence: map[string]any{"backtest_run_id": backtest.ID, "status": backtest.Status},
		})
		run.Status = "inconclusive"
		return run, err
	}
	if math.IsNaN(backtest.FeePct) || math.IsInf(backtest.FeePct, 0) || backtest.FeePct < 0 {
		err := finishWithFinding(ctx, store, run.ID, "inconclusive", validation.Finding{
			Severity: "warning", Rule: "invalid_fee_pct", Message: "backtest fee_pct must be declared as a non-negative finite value",
			Evidence: map[string]any{"backtest_run_id": backtest.ID, "fee_pct": backtest.FeePct},
		})
		run.Status = "inconclusive"
		return run, err
	}

	points, err := store.BacktestEquityCurve(ctx, backtest.ID)
	if err != nil {
		return run, err
	}
	if len(points) == 0 {
		err := finishWithFinding(ctx, store, run.ID, "inconclusive", validation.Finding{
			Severity: "warning", Rule: "missing_equity_curve", Message: "completed backtest has no equity curve",
			Evidence: map[string]any{"backtest_run_id": backtest.ID},
		})
		run.Status = "inconclusive"
		return run, err
	}

	equity := make([]metrics.EquityPoint, len(points))
	var totalEquity float64
	for i, point := range points {
		equity[i] = metrics.EquityPoint{Time: point.Time, Equity: point.TotalEquity}
		totalEquity += point.TotalEquity
	}
	summary, err := metrics.EquityMetrics(equity)
	if err != nil {
		finishErr := finishWithFinding(ctx, store, run.ID, "inconclusive", validation.Finding{
			Severity: "warning", Rule: "invalid_equity_curve", Message: "equity curve cannot be used for validation metrics",
			Evidence: map[string]any{"backtest_run_id": backtest.ID, "error": err.Error()},
		})
		run.Status = "inconclusive"
		return run, finishErr
	}

	trades, err := store.BacktestTrades(ctx, backtest.ID)
	if err != nil {
		return run, err
	}
	datasetEvidence, err := backtestDatasetEvidence(backtest, points, trades)
	if err != nil {
		return run, err
	}
	allowedTrades := make([]metrics.Trade, 0, len(trades))
	for _, trade := range trades {
		if trade.Allowed {
			allowedTrades = append(allowedTrades, metrics.Trade{Quantity: trade.Quantity, Price: trade.Price})
		}
	}
	turnover, err := metrics.TurnoverPct(allowedTrades, totalEquity/float64(len(equity)))
	if err != nil {
		finishErr := finishWithFinding(ctx, store, run.ID, "inconclusive", validation.Finding{
			Severity: "warning", Rule: "invalid_turnover_inputs", Message: "trades or equity cannot be used for turnover",
			Evidence: map[string]any{"backtest_run_id": backtest.ID, "error": err.Error()},
		})
		run.Status = "inconclusive"
		return run, finishErr
	}

	returnPct := (equity[len(equity)-1].Equity/equity[0].Equity - 1) * 100
	metricsToSave := []storage.Metric{
		{Name: "total_return_pct", Value: returnPct, Segment: "backtest", Unit: "percent", Evidence: datasetEvidence},
		{Name: "max_drawdown_pct", Value: summary.MaxDrawdownPct, Segment: "backtest", Unit: "percent"},
		{Name: "current_drawdown_pct", Value: summary.CurrentDrawdownPct, Segment: "backtest", Unit: "percent"},
		{Name: "max_recovery_duration_seconds", Value: summary.MaxRecoveryDuration.Seconds(), Segment: "backtest", Unit: "seconds"},
		{Name: "current_time_under_water_seconds", Value: summary.CurrentTimeUnderWater.Seconds(), Segment: "backtest", Unit: "seconds"},
		{Name: "time_under_water_seconds", Value: summary.TimeUnderWater.Seconds(), Segment: "backtest", Unit: "seconds"},
		{Name: "turnover_pct", Value: turnover, Segment: "backtest", Unit: "percent", Evidence: map[string]any{"allowed_trade_count": len(allowedTrades)}},
		{Name: "trade_count", Value: float64(len(allowedTrades)), Segment: "backtest", Unit: "count"},
		{Name: "dataset_row_count", Value: float64(len(points) + len(trades)), Segment: "backtest", Unit: "count", Evidence: datasetEvidence},
	}
	if err := store.SaveMetrics(ctx, run.ID, metricsToSave); err != nil {
		return run, err
	}
	if err := store.FinishRun(ctx, run.ID, "completed", ""); err != nil {
		return run, err
	}
	run.Status = "completed"
	return run, nil
}

func createRunWithSplits(ctx context.Context, store *storage.Store, run storage.Run, splits []validation.Split) (storage.Run, error) {
	if err := validation.ValidateSplits(splits); err != nil {
		return storage.Run{}, err
	}
	run, err := storage.CreateRun(ctx, store, run)
	if err != nil {
		return storage.Run{}, err
	}
	if err := store.SaveSplits(ctx, run.ID, splits); err != nil {
		return storage.Run{}, err
	}
	return run, nil
}

func backtestDatasetEvidence(backtest storage.BacktestRun, points []storage.BacktestEquityPoint, trades []storage.BacktestTrade) (map[string]any, error) {
	// Struct fields keep the serialized audit input ordered and therefore hashable.
	dataset := struct {
		Backtest storage.BacktestRun
		Points   []storage.BacktestEquityPoint
		Trades   []storage.BacktestTrade
	}{backtest, points, trades}
	encoded, err := json.Marshal(dataset)
	if err != nil {
		return nil, fmt.Errorf("marshal backtest dataset: %w", err)
	}
	fingerprint := sha256.Sum256(encoded)
	return map[string]any{
		"dataset_fingerprint": fmt.Sprintf("%x", fingerprint),
		"equity_point_count":  len(points),
		"trade_count":         len(trades),
	}, nil
}

func finishWithFinding(ctx context.Context, store *storage.Store, runID, status string, finding validation.Finding) error {
	if err := store.SaveFindings(ctx, runID, []validation.Finding{finding}); err != nil {
		return err
	}
	if err := store.FinishRun(ctx, runID, status, finding.Message); err != nil {
		return err
	}
	return nil
}
