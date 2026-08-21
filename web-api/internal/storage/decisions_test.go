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

func TestRecentDecisions_ExcludesPaperDecisions(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	runID := testID(t, "decision-run")
	realID := testID(t, "decision-real")
	paperID := testID(t, "decision-paper")
	now := time.Now().UTC()

	seedDecision(t, store, realID, runID, now)
	t.Cleanup(func() { deleteDecisionForTest(t, store, realID) })
	seedDecision(t, store, paperID, runID, now)
	t.Cleanup(func() { deleteDecisionForTest(t, store, paperID) })
	if _, err := store.pool.Exec(ctx, `INSERT INTO paper_decision_ids (id, created_at) VALUES ($1, now())`, paperID); err != nil {
		t.Fatalf("seed paper_decision_ids: %v", err)
	}
	t.Cleanup(func() {
		store.pool.Exec(context.Background(), `DELETE FROM paper_decision_ids WHERE id = $1`, paperID)
	})

	real, err := store.RecentDecisions(ctx, 50)
	if err != nil {
		t.Fatalf("RecentDecisions: %v", err)
	}
	for _, d := range real {
		if d.ID == paperID {
			t.Error("RecentDecisions included a paper decision")
		}
	}

	paper, err := store.RecentPaperDecisions(ctx, 50)
	if err != nil {
		t.Fatalf("RecentPaperDecisions: %v", err)
	}
	found := false
	for _, d := range paper {
		if d.ID == realID {
			t.Error("RecentPaperDecisions included a real decision")
		}
		if d.ID == paperID {
			found = true
		}
	}
	if !found {
		t.Error("RecentPaperDecisions did not include the paper decision")
	}
}

func TestRecentDecisions_SeparatesIntentApplicationsByTarget(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	runID := testID(t, "application-run")
	testnetID := testID(t, "application-testnet")
	paperHoldID := testID(t, "application-paper-hold")
	paperRejectedID := testID(t, "application-paper-rejected")

	seedIntentApplication(t, store, testnetID, runID, "testnet", "buy", "filled")
	seedIntentApplication(t, store, paperHoldID, runID, "paper", "hold", "not_applicable")
	seedIntentApplication(t, store, paperRejectedID, runID, "paper", "buy", "rejected")
	t.Cleanup(func() {
		for _, id := range []string{testnetID, paperHoldID, paperRejectedID} {
			_, _ = store.pool.Exec(context.Background(), `DELETE FROM strategist_intent_applications WHERE analysis_run_id = $1 AND intent_id = $2`, runID, id)
		}
	})

	real, err := store.RecentDecisions(ctx, 50)
	if err != nil {
		t.Fatalf("RecentDecisions: %v", err)
	}
	if !containsDecision(real, testnetID) || containsDecision(real, paperHoldID) || containsDecision(real, paperRejectedID) {
		t.Fatalf("real decisions = %+v, want only the testnet application", real)
	}

	paper, err := store.RecentPaperDecisions(ctx, 50)
	if err != nil {
		t.Fatalf("RecentPaperDecisions: %v", err)
	}
	if containsDecision(paper, testnetID) || !containsDecision(paper, paperHoldID) || !containsDecision(paper, paperRejectedID) {
		t.Fatalf("paper decisions = %+v, want hold and rejected paper applications", paper)
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

func seedIntentApplication(t *testing.T, store *Store, intentID, runID, targetID, side, status string) {
	t.Helper()
	_, err := store.pool.Exec(context.Background(), `
		INSERT INTO strategist_intent_applications
			(intent_id, target_id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			 proposed_quantity, proposed_value, risk_reasons, execution_status, created_at)
		VALUES ($1, $2, $3, 'BTC', $4, 0.8, 0.1, 'test', 1, 100, '[]', $5, now())
	`, intentID, targetID, runID, side, status)
	if err != nil {
		t.Fatalf("seedIntentApplication: %v", err)
	}
}

func containsDecision(decisions []Decision, id string) bool {
	for _, decision := range decisions {
		if decision.ID == id {
			return true
		}
	}
	return false
}
