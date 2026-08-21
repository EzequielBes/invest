// mcp/internal/tools/paperstrategist.go
package tools

import (
	"context"
	"fmt"

	riskstorage "risk-engine/storage"

	"execution/paperexec"
	"execution/paperstore"

	"strategist/runner"
)

// RunPaperStrategistArgs mirrors RunStrategistArgs exactly — same
// pipeline, same inputs, only the execution side differs.
type RunPaperStrategistArgs struct {
	AnalysisRunID     string   `json:"analysis_run_id" jsonschema:"an analysis_run_id already produced by run_analysis"`
	Assets            []string `json:"assets" jsonschema:"asset symbols to decide on, a subset of what was analyzed"`
	DailyLoss         float64  `json:"daily_loss,omitempty" jsonschema:"simulated portfolio daily loss so far, as a fraction, e.g. 0.02 for 2%"`
	WeeklyLoss        float64  `json:"weekly_loss,omitempty" jsonschema:"simulated portfolio weekly loss so far, as a fraction"`
	Drawdown          float64  `json:"drawdown,omitempty" jsonschema:"simulated portfolio drawdown from peak, as a fraction"`
	ConsecutiveLosses int      `json:"consecutive_losses,omitempty" jsonschema:"number of consecutive simulated losing trades"`
}

// RunPaperStrategist runs the simulated decision pipeline. It adds the
// deterministic committee ranking to prompts when an analysis run has one,
// while deliberately leaving run_strategist's real path untouched.
// Refuses to run when simulation is turned off (see set_simulation_enabled)
// so no LLM call is spent on a cycle nobody asked for.
func RunPaperStrategist(ctx context.Context, dsn string, riskStore *riskstorage.Store, paperStore *paperstore.Store, args RunPaperStrategistArgs) (RunStrategistResult, error) {
	if args.AnalysisRunID == "" {
		return RunStrategistResult{}, fmt.Errorf("analysis_run_id is required")
	}
	if len(args.Assets) == 0 {
		return RunStrategistResult{}, fmt.Errorf("assets is required")
	}
	enabled, err := paperStore.Enabled(ctx)
	if err != nil {
		return RunStrategistResult{}, fmt.Errorf("check simulation status: %w", err)
	}
	if !enabled {
		return RunStrategistResult{}, fmt.Errorf("simulation is turned off — call set_simulation_enabled first")
	}

	execClient, err := paperexec.New(ctx, dsn)
	if err != nil {
		return RunStrategistResult{}, err
	}
	defer execClient.Close()

	decisions, err := runner.RunWithExecutorAndRanking(ctx, dsn, riskStore, execClient, args.AnalysisRunID, args.Assets, args.DailyLoss, args.WeeklyLoss, args.Drawdown, args.ConsecutiveLosses)
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
