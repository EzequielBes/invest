// risk-engine/storage/limits_test.go
//
// NOTE: These tests mutate the shared singleton row risk_limits (id=1) in
// the real database, the same row risk's evaluate_test.go reads limits
// from. Tests within this package run sequentially by default, so `go
// test ./storage` alone is safe. But running the full module's test suite
// MUST use `go test -p 1 ./...` — otherwise Go may run this package's test
// binary concurrently with risk's, racing these rows across packages.
package storage

import (
	"context"
	"os"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping storage tests")
	}
	s, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestGetAndSetLimits(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	original, err := s.GetLimits(ctx)
	if err != nil {
		t.Fatalf("GetLimits: %v", err)
	}
	// Register cleanup immediately after capturing original, so restore runs
	// regardless of how the rest of the test exits (even if t.Fatalf is called)
	t.Cleanup(func() {
		if err := s.SetLimits(context.Background(), original); err != nil {
			t.Logf("cleanup: failed to restore original limits: %v", err)
		}
	})

	if original.MaxPctPerAsset <= 0 {
		t.Fatalf("expected a seeded MaxPctPerAsset > 0, got %v", original.MaxPctPerAsset)
	}

	updated := original
	updated.MaxValuePerTrade = 12345
	if err := s.SetLimits(ctx, updated); err != nil {
		t.Fatalf("SetLimits: %v", err)
	}

	got, err := s.GetLimits(ctx)
	if err != nil {
		t.Fatalf("GetLimits after update: %v", err)
	}
	if got.MaxValuePerTrade != 12345 {
		t.Errorf("MaxValuePerTrade = %v, want 12345", got.MaxValuePerTrade)
	}
}
