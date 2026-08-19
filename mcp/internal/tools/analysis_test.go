// mcp/internal/tools/analysis_test.go
package tools

import (
	"context"
	"testing"
)

func TestRunAnalysis_RequiresAssets(t *testing.T) {
	if _, err := RunAnalysis(context.Background(), "", nil, RunAnalysisArgs{}); err == nil {
		t.Fatal("expected an error for missing assets, got nil")
	}
}

func TestRunAnalysis_RejectsUnknownAgent(t *testing.T) {
	if _, err := RunAnalysis(context.Background(), "", nil, RunAnalysisArgs{Assets: []string{"BTC"}, Agents: []string{"not-a-real-agent"}}); err == nil {
		t.Fatal("expected an error for an unknown agent, got nil")
	}
}
