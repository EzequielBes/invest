// mcp/cmd/mcp-server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	riskstorage "risk-engine/storage"

	"execution/paperstore"

	"mcp/internal/storage"
	"mcp/internal/tools"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect storage: %w", err)
	}
	defer store.Close()

	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()

	paperStore, err := paperstore.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect paper storage: %w", err)
	}
	defer paperStore.Close()

	server := newServer(store, riskStore, paperStore, dsn)
	return server.Run(ctx, &mcp.StdioTransport{})
}

func newServer(store *storage.Store, riskStore *riskstorage.Store, paperStore *paperstore.Store, dsn string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "investment-platform", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_latest_price",
		Description: "Get the most recently collected close price for an asset.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.GetLatestPriceArgs) (*mcp.CallToolResult, tools.GetLatestPriceResult, error) {
		result, err := tools.GetLatestPrice(ctx, store, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_risk_state",
		Description: "Read the risk-engine's live operational status and configured limits.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ tools.GetRiskStateArgs) (*mcp.CallToolResult, tools.RiskStateResult, error) {
		result, err := tools.GetRiskState(ctx, riskStore)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_risk_state",
		Description: "Manually set the risk-engine's live operational status (normal, paused, or kill_switch).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.SetRiskStateArgs) (*mcp.CallToolResult, tools.RiskStateResult, error) {
		result, err := tools.SetRiskState(ctx, riskStore, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "prepare_analysis",
		Description: "Collect and persist deterministic analysis context in a pending run. Submit provider-generated narratives in subsequent calls.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.PrepareAnalysisArgs) (*mcp.CallToolResult, tools.PrepareAnalysisResult, error) {
		result, err := tools.PrepareAnalysis(ctx, dsn, riskStore, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_analysis_context",
		Description: "Retrieve the persisted deterministic context for a pending analysis run before submitting provider-generated narratives.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.GetAnalysisContextArgs) (*mcp.CallToolResult, tools.PrepareAnalysisResult, error) {
		result, err := tools.GetAnalysisContext(ctx, dsn, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_analysis_narratives",
		Description: "Persist one provider-generated narrative stage for a pending analysis run. Submit technical, derivatives, news, risk_context, then macro in order.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.SubmitAnalysisNarrativesArgs) (*mcp.CallToolResult, tools.SubmissionResult, error) {
		result, err := tools.SubmitAnalysisNarratives(ctx, dsn, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_committee_assessments",
		Description: "Validate and persist structured provider-generated committee assessments, calculate deterministic rankings, and complete the analysis run.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.SubmitCommitteeAssessmentsArgs) (*mcp.CallToolResult, tools.SubmissionResult, error) {
		result, err := tools.SubmitCommitteeAssessments(ctx, dsn, riskStore, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_backtest",
		Description: "Run a moving-average-cross backtest over a historical period, validated against the real risk engine, and return final metrics.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.RunBacktestArgs) (*mcp.CallToolResult, tools.RunBacktestResult, error) {
		result, err := tools.RunBacktest(ctx, dsn, riskStore, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "prepare_strategy",
		Description: "Read a completed analysis run's deterministic ranking so an external provider can form stable structured strategy intents.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.PrepareStrategyArgs) (*mcp.CallToolResult, tools.PrepareStrategyResult, error) {
		result, err := tools.PrepareStrategy(ctx, dsn, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "apply_strategy_intents",
		Description: "Risk-check and apply structured intents only to explicitly requested paper and/or testnet targets. Each requested target must be enabled; real trading is never an implicit target.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.ApplyStrategyIntentsArgs) (*mcp.CallToolResult, tools.ApplyStrategyIntentsResult, error) {
		result, err := tools.ApplyStrategyIntents(ctx, dsn, riskStore, paperStore, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_automation_controls",
		Description: "Read paper and testnet automation gates and the external agent selected to run automation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ tools.GetAutomationControlsArgs) (*mcp.CallToolResult, tools.AutomationControlsResult, error) {
		result, err := tools.GetAutomationControls(ctx, paperStore)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_automation_controls",
		Description: "Update one or more automation gates. Testnet execution stays disabled until testnet_enabled is explicitly set true.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.SetAutomationControlsArgs) (*mcp.CallToolResult, tools.AutomationControlsResult, error) {
		result, err := tools.SetAutomationControls(ctx, paperStore, args)
		return nil, result, err
	})

	return server
}
