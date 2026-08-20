package storage

import (
	"context"
	"testing"
)

func TestLiveRiskState_ReadsLiveRowAndLimits(t *testing.T) {
	store := testStore(t)

	resp, err := store.LiveRiskState(context.Background())
	if err != nil {
		t.Fatalf("LiveRiskState: %v", err)
	}
	if resp.State.Status == "" {
		t.Error("State.Status is empty, want a real status")
	}
	if resp.Limits.MaxDailyLoss <= 0 {
		t.Errorf("Limits.MaxDailyLoss = %v, want a positive configured limit", resp.Limits.MaxDailyLoss)
	}
}
