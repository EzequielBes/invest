// risk-engine/internal/storage/limits_test.go
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

	// restore original so the seeded fixture isn't permanently mutated for other tests/runs
	if err := s.SetLimits(ctx, original); err != nil {
		t.Fatalf("SetLimits (restore): %v", err)
	}
}
