// analysis/internal/storage/macro_test.go
package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func testAnalysisStore(t *testing.T) *Store {
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

func TestLatestMacroObservations_ReadsMostRecentPerSeries(t *testing.T) {
	s := testAnalysisStore(t)
	ctx := context.Background()
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM macro_indicators WHERE series_id = 'TESTSERIES'`)
	})

	older := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO macro_indicators (series_id, observed_at, value, fetched_at)
		VALUES ('TESTSERIES', $1, 1.0, now()), ('TESTSERIES', $2, 2.0, now())
	`, older, newer)
	if err != nil {
		t.Fatalf("seed macro_indicators: %v", err)
	}

	got, err := s.LatestMacroObservations(ctx)
	if err != nil {
		t.Fatalf("LatestMacroObservations: %v", err)
	}
	obs, ok := got["TESTSERIES"]
	if !ok {
		t.Fatal("expected TESTSERIES in result")
	}
	if obs.Value != 2.0 {
		t.Errorf("Value = %v, want 2.0 (most recent observation)", obs.Value)
	}
	if !obs.ObservedAt.Equal(newer) {
		t.Errorf("ObservedAt = %v, want %v", obs.ObservedAt, newer)
	}
}
