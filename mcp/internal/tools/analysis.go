// mcp/internal/tools/analysis.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	riskstorage "risk-engine/storage"

	"analysis/runner"
)

var validAnalysisAgents = map[string]bool{"technical": true, "derivatives": true, "news": true, "risk_context": true, "macro": true, "committee": true}

// RunAnalysisArgs is the run_analysis tool's input.
type RunAnalysisArgs struct {
	Assets     []string          `json:"assets" jsonschema:"asset symbols to analyze, on the reference exchange"`
	Timeframe  string            `json:"timeframe,omitempty" jsonschema:"timeframe used by the technical agent, defaults to 1h"`
	Agents     []string          `json:"agents,omitempty" jsonschema:"which agents to run: technical, derivatives, news, risk_context, macro, committee — defaults to all six"`
	AssetNames map[string]string `json:"asset_names,omitempty" jsonschema:"optional symbol to full name mapping used by the news agent, e.g. {\"BTC\": \"Bitcoin\"}"`
}

// AnalysisResultItem is one agent's output for one asset (or, for
// risk_context, for the portfolio as a whole — Asset is "" in that case),
// as returned by run_analysis.
type AnalysisResultItem struct {
	AgentType  string          `json:"agent_type"`
	Asset      string          `json:"asset"`
	Narrative  string          `json:"narrative"`
	Indicators json.RawMessage `json:"indicators,omitempty"`
}

// RunAnalysisResult is the run_analysis tool's output.
type RunAnalysisResult struct {
	AnalysisRunID string               `json:"analysis_run_id"`
	SuccessCount  int                  `json:"success_count"`
	Results       []AnalysisResultItem `json:"results"`
}

// RunAnalysis runs the analysis pipeline via analysis/runner.RunWithDSN.
// dsn is passed through rather than an already-open store, since this
// tool can never import analysis/internal/storage to name that type
// itself (Go's internal-package visibility rule) — RunWithDSN is the
// module's public entry point built for exactly this kind of external
// caller.
func RunAnalysis(ctx context.Context, dsn string, riskStore *riskstorage.Store, args RunAnalysisArgs) (RunAnalysisResult, error) {
	if len(args.Assets) == 0 {
		return RunAnalysisResult{}, fmt.Errorf("assets is required")
	}
	agents := args.Agents
	if len(agents) == 0 {
		agents = []string{"technical", "derivatives", "news", "risk_context", "macro", "committee"}
	}
	for _, agentType := range agents {
		if !validAnalysisAgents[agentType] {
			return RunAnalysisResult{}, fmt.Errorf("unknown agent %q (valid: technical, derivatives, news, risk_context, macro, committee)", agentType)
		}
	}
	timeframe := args.Timeframe
	if timeframe == "" {
		timeframe = "1h"
	}

	runID, successCount, results, err := runner.RunWithDSN(ctx, dsn, riskStore, args.Assets, args.AssetNames, timeframe, agents)
	items := make([]AnalysisResultItem, len(results))
	for i, r := range results {
		items[i] = AnalysisResultItem{AgentType: r.AgentType, Asset: r.Asset, Narrative: r.Narrative, Indicators: r.Indicators}
	}
	if err != nil {
		return RunAnalysisResult{AnalysisRunID: runID, SuccessCount: successCount, Results: items}, err
	}
	return RunAnalysisResult{AnalysisRunID: runID, SuccessCount: successCount, Results: items}, nil
}
