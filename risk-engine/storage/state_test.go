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
	if err := s.SetState(ctx, nil, StatusNormal, "test setup"); err != nil {
		t.Fatalf("SetState (setup): %v", err)
	}
	// Register cleanup immediately after the state is set, so restore runs
	// regardless of how the rest of the test exits (even if t.Fatalf is called)
	t.Cleanup(func() {
		if err := s.SetState(context.Background(), nil, StatusNormal, "test cleanup"); err != nil {
			t.Logf("cleanup: failed to reset state: %v", err)
		}
	})

	st, err := s.GetState(ctx, nil)
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

	if err := s.SetState(ctx, nil, StatusPaused, "test: daily_loss breached"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	// Register cleanup immediately after the state is set to paused, so reset runs
	// regardless of how the rest of the test exits (even if t.Fatalf is called)
	t.Cleanup(func() {
		if err := s.Reset(context.Background(), nil, "test cleanup"); err != nil {
			t.Logf("cleanup: failed to reset state: %v", err)
		}
	})

	st, err := s.GetState(ctx, nil)
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

func TestInitRunState_CreatesNormalRowIsolatedFromLive(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	if err := s.InitRunState(ctx, runID); err != nil {
		t.Fatalf("InitRunState: %v", err)
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM risk_state WHERE run_id = $1`, runID)
	})

	st, err := s.GetState(ctx, &runID)
	if err != nil {
		t.Fatalf("GetState(runID): %v", err)
	}
	if st.Status != StatusNormal {
		t.Errorf("Status = %q, want %q", st.Status, StatusNormal)
	}

	live, err := s.GetState(ctx, nil)
	if err != nil {
		t.Fatalf("GetState(nil): %v", err)
	}
	if live.Status == "" {
		t.Fatal("expected the live row (run_id IS NULL) to still be readable")
	}
}

func TestSetState_RunScoped_DoesNotAffectLiveOrOtherRuns(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runA := "test-run-a-" + t.Name()
	runB := "test-run-b-" + t.Name()

	if err := s.InitRunState(ctx, runA); err != nil {
		t.Fatalf("InitRunState(A): %v", err)
	}
	if err := s.InitRunState(ctx, runB); err != nil {
		t.Fatalf("InitRunState(B): %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		s.pool.Exec(ctx, `DELETE FROM risk_state WHERE run_id IN ($1, $2)`, runA, runB)
	})

	if err := s.SetState(ctx, &runA, StatusPaused, "test: run A paused"); err != nil {
		t.Fatalf("SetState(A): %v", err)
	}

	stA, err := s.GetState(ctx, &runA)
	if err != nil {
		t.Fatalf("GetState(A): %v", err)
	}
	if stA.Status != StatusPaused {
		t.Errorf("run A Status = %q, want %q", stA.Status, StatusPaused)
	}

	stB, err := s.GetState(ctx, &runB)
	if err != nil {
		t.Fatalf("GetState(B): %v", err)
	}
	if stB.Status != StatusNormal {
		t.Errorf("run B Status = %q, want %q (must not be affected by run A)", stB.Status, StatusNormal)
	}

	live, err := s.GetState(ctx, nil)
	if err != nil {
		t.Fatalf("GetState(nil): %v", err)
	}
	if live.Status != StatusNormal {
		t.Errorf("live Status = %q, want %q (must not be affected by either run)", live.Status, StatusNormal)
	}
}
