// risk-engine/internal/risk/evaluate_test.go
package risk

import (
	"context"
	"os"
	"testing"

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
}
