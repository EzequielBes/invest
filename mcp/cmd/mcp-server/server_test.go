// mcp/cmd/mcp-server/server_test.go
package main

import (
	"context"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	riskstorage "risk-engine/storage"

	"mcp/internal/storage"
)

func testStores(t *testing.T) (*storage.Store, *riskstorage.Store, string) {
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
	return store, riskStore, dsn
}

// TestServer_ListsAllSixTools connects a real MCP client to the real
// server over an in-memory transport (no subprocess) and confirms every
// tool this plan adds is actually registered and discoverable — the
// protocol-level check no direct Go-function-call test can give.
func TestServer_ListsAllSixTools(t *testing.T) {
	store, riskStore, dsn := testStores(t)
	server := newServer(store, riskStore, dsn)

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
		"run_analysis": false, "run_strategist": false, "run_backtest": false,
	}
	page, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range page.Tools {
		if _, ok := wantTools[tool.Name]; ok {
			wantTools[tool.Name] = true
		}
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
	store, riskStore, dsn := testStores(t)
	server := newServer(store, riskStore, dsn)

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
