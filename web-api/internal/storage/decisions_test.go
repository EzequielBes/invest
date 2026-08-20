package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"
)

func TestRecentDecisions_ReturnsNewestFirstUpToLimit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	runID := testID(t, "decision-run")
	olderID := testID(t, "decision-older")
	newerID := testID(t, "decision-newer")
	older := time.Now().UTC().Add(time.Hour)
	newer := older.Add(time.Hour)

	seedDecision(t, store, olderID, runID, older)
	t.Cleanup(func() { deleteDecisionForTest(t, store, olderID) })
	seedDecision(t, store, newerID, runID, newer)
	t.Cleanup(func() { deleteDecisionForTest(t, store, newerID) })

	decisions, err := store.RecentDecisions(ctx, 1)
	if err != nil {
		t.Fatalf("RecentDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1 (limit enforced)", len(decisions))
	}
	if decisions[0].ID != newerID {
		t.Errorf("decisions[0].ID = %q, want the newer fixture", decisions[0].ID)
	}
}

func TestRecentDecisions_NoRowsReturnsEmptySliceNotNil(t *testing.T) {
	store := testStore(t)

	decisions, err := store.RecentDecisions(context.Background(), 1)
	if err != nil {
		t.Fatalf("RecentDecisions: %v", err)
	}
	if decisions == nil {
		t.Error("decisions is nil, want a non-nil slice so it JSON-encodes as [] not null")
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func testID(t *testing.T, prefix string) string {
	t.Helper()
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatalf("random test ID: %v", err)
	}
	return "test-web-api-" + prefix + "-" + hex.EncodeToString(random[:])
}

func seedDecision(t *testing.T, store *Store, id, runID string, createdAt time.Time) {
	t.Helper()
	_, err := store.pool.Exec(context.Background(), `
		INSERT INTO strategist_decisions
			(id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			 proposed_quantity, proposed_value, risk_reasons, created_at)
		VALUES ($1, $2, 'BTC', 'buy', 0.8, 0.1, 'test', 1, 100, '[]', $3)
	`, id, runID, createdAt)
	if err != nil {
		t.Fatalf("seedDecision: %v", err)
	}
}

func deleteDecisionForTest(t *testing.T, store *Store, id string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM strategist_decisions WHERE id = $1`, id); err != nil {
		t.Errorf("cleanup decision %s: %v", id, err)
	}
}
