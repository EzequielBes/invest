package tools

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPrepareAnalysisRequiresAssets(t *testing.T) {
	if _, err := PrepareAnalysis(context.Background(), "", nil, PrepareAnalysisArgs{}); err == nil {
		t.Fatal("expected missing assets error")
	}
}

func TestGetAnalysisContextRequiresRunID(t *testing.T) {
	if _, err := GetAnalysisContext(context.Background(), "", GetAnalysisContextArgs{}); err == nil {
		t.Fatal("expected missing run ID error")
	}
}

func TestAnalysisContextObjectIndicatorsPassMCPOutputValidation(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "context"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, PrepareAnalysisResult, error) {
		return nil, PrepareAnalysisResult{Context: []AnalysisContextItem{{
			AgentType:  "technical",
			Asset:      "BTCUSDT",
			Indicators: map[string]any{"rsi": 55.2},
		}}}, nil
	})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "context", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool output failed validation: %+v", result.Content)
	}
}

func TestSubmitAnalysisNarrativesRejectsInvalidStage(t *testing.T) {
	if _, err := SubmitAnalysisNarratives(context.Background(), "", SubmitAnalysisNarrativesArgs{AnalysisRunID: "run", Stage: "committee"}); err == nil {
		t.Fatal("expected invalid stage error")
	}
}

func TestApplyStrategyIntentsRequiresExplicitSupportedTarget(t *testing.T) {
	for _, targets := range [][]string{nil, {"real"}, {"paper", "paper"}} {
		if _, err := requestedTargets(targets); err == nil {
			t.Fatalf("requestedTargets(%v) accepted", targets)
		}
	}
	requested, err := requestedTargets([]string{"paper", "testnet", "alpaca_paper"})
	if err != nil || !requested["paper"] || !requested["testnet"] || !requested["alpaca_paper"] {
		t.Fatalf("requestedTargets returned %v, %v", requested, err)
	}
}

func TestSetAutomationControlsRequiresPatch(t *testing.T) {
	if _, err := SetAutomationControls(context.Background(), nil, SetAutomationControlsArgs{}); err == nil {
		t.Fatal("expected missing controls error")
	}
}
