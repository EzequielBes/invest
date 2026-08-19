// mcp/internal/tools/strategist.go
package tools

import (
	"context"
	"fmt"

	riskstorage "risk-engine/storage"

	"strategist/runner"
)

// RunStrategistArgs is the run_strategist tool's input. Since
// sub-project 8, portfolio is always fetched from the real exchange
// account — cash/positions are no longer caller-supplied.
type RunStrategistArgs struct {
	AnalysisRunID     string   `json:"analysis_run_id" jsonschema:"an analysis_run_id already produced by run_analysis"`
	Assets            []string `json:"assets" jsonschema:"asset symbols to decide on, a subset of what was analyzed"`
	DailyLoss         float64  `json:"daily_loss,omitempty" jsonschema:"portfolio daily loss so far, as a fraction, e.g. 0.02 for 2%"`
	WeeklyLoss        float64  `json:"weekly_loss,omitempty" jsonschema:"portfolio weekly loss so far, as a fraction"`
	Drawdown          float64  `json:"drawdown,omitempty" jsonschema:"portfolio drawdown from peak, as a fraction"`
	ConsecutiveLosses int      `json:"consecutive_losses,omitempty" jsonschema:"number of consecutive losing trades"`
}

// DecisionResult is one asset's decision, as returned by run_strategist.
// Since sub-project 8, an approved decision is also executed for real
// against the Binance testnet — the Execution* fields report that
// outcome, nil when execution wasn't attempted or failed.
type DecisionResult struct {
	Asset                   string   `json:"asset"`
	Side                    string   `json:"side"`
	Confidence              float64  `json:"confidence"`
	SizingPct               float64  `json:"sizing_pct"`
	Rationale               string   `json:"rationale"`
	ProposedQuantity        float64  `json:"proposed_quantity"`
	ProposedValue           float64  `json:"proposed_value"`
	RiskAllowed             *bool    `json:"risk_allowed,omitempty"`
	RiskReasons             []string `json:"risk_reasons,omitempty"`
	ExecutionStatus         *string  `json:"execution_status,omitempty"`
	ExecutionOrderID        *string  `json:"execution_order_id,omitempty"`
	ExecutionFilledQuantity *float64 `json:"execution_filled_quantity,omitempty"`
	ExecutionFilledPrice    *float64 `json:"execution_filled_price,omitempty"`
}

// RunStrategistResult is the run_strategist tool's output.
type RunStrategistResult struct {
	Decisions []DecisionResult `json:"decisions"`
}

// RunStrategist runs the strategist pipeline via
// strategist/runner.RunWithDSN, which already reads back the persisted
// decisions internally (see that function's doc comment for why).
func RunStrategist(ctx context.Context, dsn string, riskStore *riskstorage.Store, args RunStrategistArgs) (RunStrategistResult, error) {
	if args.AnalysisRunID == "" {
		return RunStrategistResult{}, fmt.Errorf("analysis_run_id is required")
	}
	if len(args.Assets) == 0 {
		return RunStrategistResult{}, fmt.Errorf("assets is required")
	}

	decisions, err := runner.RunWithDSN(ctx, dsn, riskStore, args.AnalysisRunID, args.Assets, args.DailyLoss, args.WeeklyLoss, args.Drawdown, args.ConsecutiveLosses)
	if err != nil {
		return RunStrategistResult{}, err
	}
	result := RunStrategistResult{Decisions: make([]DecisionResult, len(decisions))}
	for i, d := range decisions {
		result.Decisions[i] = DecisionResult{
			Asset: d.Asset, Side: d.Side, Confidence: d.Confidence, SizingPct: d.SizingPct,
			Rationale: d.Rationale, ProposedQuantity: d.ProposedQuantity, ProposedValue: d.ProposedValue,
			RiskAllowed: d.RiskAllowed, RiskReasons: d.RiskReasons,
			ExecutionStatus: d.ExecutionStatus, ExecutionOrderID: d.ExecutionOrderID,
			ExecutionFilledQuantity: d.ExecutionFilledQuantity, ExecutionFilledPrice: d.ExecutionFilledPrice,
		}
	}
	return result, nil
}
