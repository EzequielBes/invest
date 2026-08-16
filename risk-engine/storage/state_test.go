// risk-engine/storage/state_test.go
//
// NOTE: These tests mutate the shared singleton row risk_state (id=1) in
// the real database, the same row risk's evaluate_test.go reads and
// writes state through. Tests within this package run sequentially by
// default, so `go test ./storage` alone is safe. But running the full
// module's test suite MUST use `go test -p 1 ./...` — otherwise Go may run
// this package's test binary concurrently with risk's, racing this row
// across packages.
package storage

import (
	"context"
	"testing"
)

func TestGetState_SeededAsNormal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Ensure a clean starting point regardless of what earlier test runs left behind.
	if err := s.SetState(ctx, StatusNormal, "test setup"); err != nil {
		t.Fatalf("SetState (setup): %v", err)
	}
	// Register cleanup immediately after the state is set, so restore runs
	// regardless of how the rest of the test exits (even if t.Fatalf is called)
	t.Cleanup(func() {
		if err := s.SetState(context.Background(), StatusNormal, "test cleanup"); err != nil {
			t.Logf("cleanup: failed to reset state: %v", err)
		}
	})

	st, err := s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != StatusNormal {
		t.Errorf("Status = %q, want %q", st.Status, StatusNormal)
	}
}

func TestSetState_TransitionsAndPersists(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.SetState(ctx, StatusPaused, "test: daily_loss breached"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	// Register cleanup immediately after the state is set to paused, so reset runs
	// regardless of how the rest of the test exits (even if t.Fatalf is called)
	t.Cleanup(func() {
		if err := s.Reset(context.Background(), "test cleanup"); err != nil {
			t.Logf("cleanup: failed to reset state: %v", err)
		}
	})

	st, err := s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != StatusPaused {
		t.Errorf("Status = %q, want %q", st.Status, StatusPaused)
	}
	if st.Reason != "test: daily_loss breached" {
		t.Errorf("Reason = %q", st.Reason)
	}
}
