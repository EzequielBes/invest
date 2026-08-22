package storage

import (
	"context"
	"testing"
	"time"
)

func TestInsertAndLatestMacroObservations(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM macro_indicators WHERE series_id IN ('FEDFUNDS', 'UNRATE')`)
	})

	older := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	if err := s.InsertMacroObservation(ctx, "FEDFUNDS", older, 5.25); err != nil {
		t.Fatalf("InsertMacroObservation (older): %v", err)
	}
	if err := s.InsertMacroObservation(ctx, "FEDFUNDS", newer, 5.50); err != nil {
		t.Fatalf("InsertMacroObservation (newer): %v", err)
	}
	if err := s.InsertMacroObservation(ctx, "UNRATE", newer, 4.1); err != nil {
		t.Fatalf("InsertMacroObservation (UNRATE): %v", err)
	}
	// Re-insert the same series_id+observed_at with a revised value —
	// confirms upsert semantics instead of a duplicate-key failure.
	if err := s.InsertMacroObservation(ctx, "FEDFUNDS", newer, 5.55); err != nil {
		t.Fatalf("InsertMacroObservation (revision): %v", err)
	}

	got, err := s.LatestMacroObservations(ctx)
	if err != nil {
		t.Fatalf("LatestMacroObservations: %v", err)
	}

	fedFunds, ok := got["FEDFUNDS"]
	if !ok {
		t.Fatal("expected FEDFUNDS in result")
	}
	if fedFunds.Value != 5.55 {
		t.Errorf("FEDFUNDS.Value = %v, want 5.55 (latest observed_at, revised value)", fedFunds.Value)
	}
	if !fedFunds.ObservedAt.Equal(newer) {
		t.Errorf("FEDFUNDS.ObservedAt = %v, want %v (most recent observation, not older)", fedFunds.ObservedAt, newer)
	}

	unrate, ok := got["UNRATE"]
	if !ok {
		t.Fatal("expected UNRATE in result")
	}
	if unrate.Value != 4.1 {
		t.Errorf("UNRATE.Value = %v, want 4.1", unrate.Value)
	}
}
