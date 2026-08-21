// mcp/cmd/mcp-server/server_test.go
package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	riskstorage "risk-engine/storage"

	"execution/paperstore"

	"mcp/internal/storage"
)

func testStores(t *testing.T) (*storage.Store, *riskstorage.Store, *paperstore.Store, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration tests")
	}
	ctx := context.Background()
	store, err := storage.New(ctx, dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(store.Close)
	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	t.Cleanup(riskStore.Close)
	paperStore, err := paperstore.New(ctx, dsn)
	if err != nil {
		t.Fatalf("paperstore.New: %v", err)
	}
	t.Cleanup(paperStore.Close)
	return store, riskStore, paperStore, dsn
}

func TestServer_WorkflowSchemasGuideSubscriptionAgents(t *testing.T) {
	server := newServer(nil, nil, nil, "")
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

	page, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range page.Tools {
		var want string
		switch tool.Name {
		case "submit_analysis_narratives":
			want = "for risk_context, must be the empty string"
		case "submit_committee_assessments":
			want = "exact accepted enum: bull|bear|neutro"
		default:
			continue
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		if !strings.Contains(string(schema), want) {
			t.Errorf("%s schema = %s, want %q", tool.Name, schema, want)
		}
	}
}

// TestServer_ListsAllTools connects a real MCP client to the real
// server over an in-memory transport (no subprocess) and confirms every
// tool registered on the server is actually discoverable — the
// protocol-level check no direct Go-function-call test can give.
func TestServer_ListsAllTools(t *testing.T) {
	server := newServer(nil, nil, nil, "")

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer clientSession.Close()

	wantTools := map[string]bool{
		"get_latest_price": false, "get_risk_state": false, "set_risk_state": false,
		"prepare_analysis": false, "get_analysis_context": false, "submit_analysis_narratives": false, "submit_committee_assessments": false,
		"prepare_strategy": false, "apply_strategy_intents": false, "get_automation_controls": false,
		"set_automation_controls": false, "run_backtest": false,
	}
	page, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range page.Tools {
		if _, ok := wantTools[tool.Name]; !ok {
			t.Errorf("unexpected tool %q in ListTools result", tool.Name)
			continue
		}
		wantTools[tool.Name] = true
	}
	for name, found := range wantTools {
		if !found {
			t.Errorf("tool %q not found in ListTools result", name)
		}
	}
}

// TestServer_GetRiskStateRoundTrips calls one tool through the real
// protocol end to end (request -> schema validation -> dispatch ->
// response), proving get_risk_state's wiring — not just its handler
// function in isolation — actually works. get_risk_state is picked
// because it needs no fixture data, unlike the other five tools.
func TestServer_GetRiskStateRoundTrips(t *testing.T) {
	store, riskStore, paperStore, dsn := testStores(t)
	server := newServer(store, riskStore, paperStore, dsn)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_risk_state", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(get_risk_state): %v", err)
	}
	if result.IsError {
		t.Fatalf("get_risk_state returned an error result: %+v", result.Content)
	}
	if result.StructuredContent == nil {
		t.Fatal("StructuredContent is nil, want the RiskStateResult")
	}
}
