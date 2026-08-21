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
		Name:        "run_analysis",
		Description: "Run the analysis pipeline (technical, derivatives, news, risk-context, macro, and committee agents) for one or more assets. The committee produces an auditable deterministic opportunity ranking for paper trading.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.RunAnalysisArgs) (*mcp.CallToolResult, tools.RunAnalysisResult, error) {
		result, err := tools.RunAnalysis(ctx, dsn, riskStore, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_strategist",
		Description: "Runs the strategist pipeline: decides buy/sell/hold for each asset from an existing analysis_run_id, validates against the real risk engine, and executes approved decisions as real limit orders on the Binance Futures testnet.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.RunStrategistArgs) (*mcp.CallToolResult, tools.RunStrategistResult, error) {
		result, err := tools.RunStrategist(ctx, dsn, riskStore, args)
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
		Name:        "get_simulation_status",
		Description: "Read whether paper/simulation mode is on and the current simulated portfolio (cash and positions).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ tools.GetSimulationStatusArgs) (*mcp.CallToolResult, tools.SimulationStatusResult, error) {
		result, err := tools.GetSimulationStatus(ctx, paperStore)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_simulation_enabled",
		Description: "Turn paper/simulation mode on or off. Must be on before run_paper_strategist will run a cycle.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.SetSimulationEnabledArgs) (*mcp.CallToolResult, tools.SimulationStatusResult, error) {
		result, err := tools.SetSimulationEnabled(ctx, paperStore, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_paper_strategist",
		Description: "Runs the simulated strategist pipeline from an existing analysis_run_id. When available, it adds the analysis committee's deterministic ranking to the LLM context; run_strategist real is not affected. Approved trades fill against the simulated portfolio and still pass the real risk-engine. Requires simulation to be enabled via set_simulation_enabled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.RunPaperStrategistArgs) (*mcp.CallToolResult, tools.RunStrategistResult, error) {
		result, err := tools.RunPaperStrategist(ctx, dsn, riskStore, paperStore, args)
		return nil, result, err
	})

	return server
}
