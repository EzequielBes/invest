// mcp/internal/tools/risk_test.go
package tools

import (
	"context"
	"os"
	"testing"

	riskstorage "risk-engine/storage"
)

func testRiskStore(t *testing.T) *riskstorage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration tests")
	}
	s, err := riskstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestGetRiskState_ReadsLiveState(t *testing.T) {
	store := testRiskStore(t)
	result, err := GetRiskState(context.Background(), store)
	if err != nil {
		t.Fatalf("GetRiskState: %v", err)
	}
	if result.Status == "" {
		t.Fatal("Status is empty, want the live risk_state row's status")
	}
}

func TestSetRiskState_RejectsInvalidStatus(t *testing.T) {
	store := testRiskStore(t)
	if _, err := SetRiskState(context.Background(), store, SetRiskStateArgs{Status: "not-a-status", Reason: "test"}); err == nil {
		t.Fatal("expected an error for an invalid status, got nil")
	}
}

func TestSetRiskState_RejectsMissingReason(t *testing.T) {
	store := testRiskStore(t)
	if _, err := SetRiskState(context.Background(), store, SetRiskStateArgs{Status: riskstorage.StatusNormal}); err == nil {
		t.Fatal("expected an error for a missing reason, got nil")
	}
}

func TestSetRiskState_PauseAndResumeRoundTrips(t *testing.T) {
	store := testRiskStore(t)
	ctx := context.Background()
	t.Cleanup(func() { store.Reset(ctx, nil, "test cleanup: restore normal") })

	paused, err := SetRiskState(ctx, store, SetRiskStateArgs{Status: riskstorage.StatusPaused, Reason: "test pause"})
	if err != nil {
		t.Fatalf("SetRiskState(paused): %v", err)
	}
	if paused.Status != riskstorage.StatusPaused || paused.Reason != "test pause" {
		t.Fatalf("result = %+v, want status=paused reason=%q", paused, "test pause")
	}

	resumed, err := SetRiskState(ctx, store, SetRiskStateArgs{Status: riskstorage.StatusNormal, Reason: "test resume"})
	if err != nil {
		t.Fatalf("SetRiskState(normal): %v", err)
	}
	if resumed.Status != riskstorage.StatusNormal {
		t.Fatalf("result = %+v, want status=normal", resumed)
	}
}
