// risk-engine/storage/closed_trades_test.go
package storage

import (
	"context"
	"testing"
	"time"
)

func seedPaperFill(t *testing.T, s *Store, asset, side string, quantity, price float64, costBasis, realizedPnL *float64) {
	t.Helper()
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO paper_fills (id, asset, side, quantity, price, cost_basis, realized_pnl, created_at)
		VALUES (gen_random_uuid()::text, $1, $2, $3, $4, $5, $6, now())
	`, asset, side, quantity, price, costBasis, realizedPnL)
	if err != nil {
		t.Fatalf("seedPaperFill insert: %v", err)
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM paper_fills WHERE asset = $1`, asset)
	})
}

func TestClosedTradeOutcomes_ReturnsPnLPctForSellsOnly(t *testing.T) {
	s := testStore(t)
	asset := "TESTASSET-" + time.Now().Format("150405.000000")

	costBasis := 100.0
	realizedPnL := 20.0 // sold 1 unit bought at 100 for 120: +20% return
	seedPaperFill(t, s, asset, "buy", 1, 100, nil, nil)
	seedPaperFill(t, s, asset, "sell", 1, 120, &costBasis, &realizedPnL)

	outcomes, err := s.ClosedTradeOutcomes(context.Background(), asset)
	if err != nil {
		t.Fatalf("ClosedTradeOutcomes: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("len(outcomes) = %d, want 1 (buy fill excluded)", len(outcomes))
	}
	want := 0.20
	if diff := outcomes[0].PnLPct - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("PnLPct = %v, want %v", outcomes[0].PnLPct, want)
	}
}
