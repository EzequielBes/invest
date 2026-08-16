// risk-engine/internal/risk/evaluate_test.go
package risk

import (
	"context"
	"os"
	"testing"
	"time"

	"risk-engine/internal/storage"
)

func testEvaluateStore(t *testing.T) *storage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping evaluate integration tests")
	}
	s, err := storage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.SetState(context.Background(), storage.StatusNormal, "test setup"); err != nil {
		t.Fatalf("reset state: %v", err)
	}
	t.Cleanup(func() {
		if err := s.SetState(context.Background(), storage.StatusNormal, "test cleanup"); err != nil {
			t.Logf("cleanup: failed to reset state: %v", err)
		}
	})
	return s
}

func TestEvaluate_RejectsWhenAlreadyPaused(t *testing.T) {
	s := testEvaluateStore(t)
	if err := s.SetState(context.Background(), storage.StatusPaused, "pre-existing pause for test"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	decision, err := Evaluate(context.Background(), s,
		PortfolioState{Cash: 10000},
		ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 100},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected rejection: system is paused")
	}
}

func TestEvaluate_AutoPausesOnLossBreach(t *testing.T) {
	s := testEvaluateStore(t)

	portfolio := PortfolioState{Cash: 10000, DailyLoss: 0.99} // certain to breach the seeded 0.05 limit
	_, err := Evaluate(context.Background(), s, portfolio,
		ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 100},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	st, err := s.GetState(context.Background())
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != storage.StatusPaused {
		t.Fatalf("Status = %q, want %q after a loss-limit breach", st.Status, storage.StatusPaused)
	}
}

func TestEvaluate_RejectsOnMissingMarketData(t *testing.T) {
	s := testEvaluateStore(t)

	decision, err := Evaluate(context.Background(), s,
		PortfolioState{Cash: 10000},
		ProposedOperation{Asset: "NOSUCHASSET_" + t.Name(), Side: SideBuy, Value: 50},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected rejection: no market data exists for this asset (fail-safe)")
	}

	st, err := s.GetState(context.Background())
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != storage.StatusNormal {
		t.Fatalf("Status = %q, want %q — a quality-rule rejection must not touch operational state", st.Status, storage.StatusNormal)
	}
}

func TestEvaluate_ApprovesHealthyOperationWithGoodMarketData(t *testing.T) {
	s := testEvaluateStore(t)
	ctx := context.Background()
	asset := "E2ECOIN"

	// Seed 10 fresh, low-volatility, high-liquidity 1m candles so every
	// quality rule passes. This test seeds its own fixture data under a
	// dedicated symbol; it doesn't depend on or disturb real collected data.
	now := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 10; i++ {
		ts := now.Add(time.Duration(i-9) * time.Minute)
		price := 100 + float64(i)*0.01
		if err := storage.TestOnlyInsertCandle(ctx, s, ReferenceExchange, asset, ts, price, price, price, price, 50000); err != nil {
			t.Fatalf("seed candle %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		storage.TestOnlyDeleteCandles(context.Background(), s, ReferenceExchange, asset)
	})

	portfolio := PortfolioState{
		Cash: 8000,
		Positions: map[string]Position{
			asset: {Asset: asset, Quantity: 1, Value: 1000},
		},
		DailyLoss:         0.01,
		WeeklyLoss:        0.02,
		Drawdown:          0.03,
		ConsecutiveLosses: 1,
	}
	proposed := ProposedOperation{Asset: asset, Side: SideBuy, Quantity: 0.5, Value: 500}

	decision, err := Evaluate(ctx, s, portfolio, proposed)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected approval, got rejection with reasons: %v", decision.Reasons)
	}
	if len(decision.RulesChecked) != 10 {
		t.Errorf("RulesChecked len = %d, want 10 (3 concentration + 4 loss + 3 quality)", len(decision.RulesChecked))
	}

	// Confirm the approval was actually logged for audit.
	var count int
	err = s.QueryRowTestHelper(ctx, `SELECT count(*) FROM risk_decisions WHERE asset = $1 AND allowed = true`, asset).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count == 0 {
		t.Fatal("expected the approved decision to be recorded in risk_decisions")
	}

	// State should remain normal after a clean approval.
	st, err := s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != storage.StatusNormal {
		t.Errorf("Status = %q, want %q after a clean approval", st.Status, storage.StatusNormal)
	}
}
