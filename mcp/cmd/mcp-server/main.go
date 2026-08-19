// mcp/cmd/mcp-server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	riskstorage "risk-engine/storage"

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
		Description: "Run the analysis pipeline (technical, derivatives, news, risk-context agents) for one or more assets, producing an analysis_run_id for run_strategist to consume.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.RunAnalysisArgs) (*mcp.CallToolResult, tools.RunAnalysisResult, error) {
		result, err := tools.RunAnalysis(ctx, dsn, riskStore, args)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_strategist",
		Description: "Decide buy/sell/hold for one or more assets from an existing analysis_run_id, validated against the real risk engine. Never executes a real order.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args tools.RunStrategistArgs) (*mcp.CallToolResult, tools.RunStrategistResult, error) {
		result, err := tools.RunStrategist(ctx, dsn, riskStore, args)
		return nil, result, err
	})

	return server.Run(ctx, &mcp.StdioTransport{})
}
