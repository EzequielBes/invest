package paperstore

import (
	"context"
	"os"
	"testing"
)

func TestApplyFill_BuyThenSellUpdatesCashAndPositions(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// Reset to a known baseline so this test is repeatable.
	if _, err := store.pool.Exec(ctx, `DELETE FROM paper_fills WHERE id LIKE 'test-paper-%'`); err != nil {
		t.Fatalf("cleanup fills: %v", err)
	}
	defer store.pool.Exec(ctx, `DELETE FROM paper_fills WHERE id LIKE 'test-paper-%'`)
	defer store.pool.Exec(ctx, `DELETE FROM paper_decision_ids WHERE id LIKE 'test-paper-%'`)

	cashBefore, _, err := store.Portfolio(ctx)
	if err != nil {
		t.Fatalf("Portfolio: %v", err)
	}

	if err := store.ApplyFill(ctx, "test-paper-buy", "BTC", "buy", 0.1, 50000); err != nil {
		t.Fatalf("ApplyFill buy: %v", err)
	}
	cash, positions, err := store.Portfolio(ctx)
	if err != nil {
		t.Fatalf("Portfolio after buy: %v", err)
	}
	if want := cashBefore - 5000; cash != want {
		t.Errorf("cash after buy = %v, want %v", cash, want)
	}
	if positions["BTC"] != 0.1 {
		t.Errorf("positions[BTC] after buy = %v, want 0.1", positions["BTC"])
	}

	if err := store.ApplyFill(ctx, "test-paper-sell", "BTC", "sell", 0.1, 51000); err != nil {
		t.Fatalf("ApplyFill sell: %v", err)
	}
	cash, positions, err = store.Portfolio(ctx)
	if err != nil {
		t.Fatalf("Portfolio after sell: %v", err)
	}
	if want := cashBefore - 5000 + 5100; cash != want {
		t.Errorf("cash after sell = %v, want %v", cash, want)
	}
	if _, held := positions["BTC"]; held {
		t.Errorf("positions[BTC] after selling the whole position = %v, want fully closed", positions["BTC"])
	}
}
