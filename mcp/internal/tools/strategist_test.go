// mcp/internal/tools/strategist_test.go
package tools

import (
	"context"
	"testing"
)

func TestRunStrategist_RequiresAnalysisRunID(t *testing.T) {
	if _, err := RunStrategist(context.Background(), "", nil, RunStrategistArgs{Assets: []string{"BTC"}, Cash: 1000}); err == nil {
		t.Fatal("expected an error for a missing analysis_run_id, got nil")
	}
}

func TestRunStrategist_RequiresAssets(t *testing.T) {
	if _, err := RunStrategist(context.Background(), "", nil, RunStrategistArgs{AnalysisRunID: "some-run-id", Cash: 1000}); err == nil {
		t.Fatal("expected an error for missing assets, got nil")
	}
}
