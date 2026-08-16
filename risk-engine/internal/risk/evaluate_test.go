// risk-engine/internal/risk/evaluate_test.go
//
// NOTE: These tests mutate shared singleton rows (risk_state, risk_limits;
// both id=1) in the real database, the same rows internal/storage's tests
// mutate. Tests within this package run sequentially by default, so `go
// test ./internal/risk` alone is safe. But running the full module's test
// suite MUST use `go test -p 1 ./...` — otherwise Go may run this package's
// test binary concurrently with internal/storage's, racing writes to the
// same rows. See internal/storage/limits_test.go and state_test.go for the
// matching note.
package risk

import (
	"context"
	"os"
	"testing"
	"time"

	"risk-engine/internal/storage"
	"risk-engine/internal/storagetest"
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

// testEvaluateSeeder opens a storagetest.Seeder against the same test
// database as testEvaluateStore, for fixture seeding and audit-row
// assertions that don't belong on the production *storage.Store type.
func testEvaluateSeeder(t *testing.T) *storagetest.Seeder {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping evaluate integration tests")
	}
	seeder, err := storagetest.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storagetest.New: %v", err)
	}
	t.Cleanup(seeder.Close)
	return seeder
}

func TestEvaluate_RejectsWhenAlreadyPaused(t *testing.T) {
	s := testEvaluateStore(t)
	seeder := testEvaluateSeeder(t)
	ctx := context.Background()

	if err := s.SetState(ctx, storage.StatusPaused, "pre-existing pause for test"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	decision, err := Evaluate(ctx, s,
		PortfolioState{Cash: 10000},
		ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 100},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected rejection: system is paused")
	}

	count, err := seeder.CountDecisions(ctx, "BTC", false)
	if err != nil {
		t.Fatalf("CountDecisions: %v", err)
	}
	if count == 0 {
		t.Fatal("expected the rejection to be recorded in risk_decisions")
	}
}

func TestEvaluate_AutoPausesOnLossBreach(t *testing.T) {
	s := testEvaluateStore(t)
	seeder := testEvaluateSeeder(t)
	ctx := context.Background()

	portfolio := PortfolioState{Cash: 10000, DailyLoss: 0.99} // certain to breach the seeded 0.05 limit
	_, err := Evaluate(ctx, s, portfolio,
		ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 100},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	st, err := s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != storage.StatusPaused {
		t.Fatalf("Status = %q, want %q after a loss-limit breach", st.Status, storage.StatusPaused)
	}

	count, err := seeder.CountDecisions(ctx, "BTC", false)
	if err != nil {
		t.Fatalf("CountDecisions: %v", err)
	}
	if count == 0 {
		t.Fatal("expected the rejection to be recorded in risk_decisions")
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

// seedFreshCandles inserts n fresh, low-volatility, high-liquidity 1m
// candles for asset via the exchange market data checks read from
// (ReferenceExchange), so every quality rule (freshness/volatility/
// liquidity) passes for tests that need Evaluate to get past quality checks
// and exercise a different rule.
func seedFreshCandles(t *testing.T, ctx context.Context, seeder *storagetest.Seeder, asset string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 10; i++ {
		ts := now.Add(time.Duration(i-9) * time.Minute)
		price := 100 + float64(i)*0.01
		if err := seeder.InsertCandle(ctx, ReferenceExchange, asset, ts, price, price, price, price, 50000); err != nil {
			t.Fatalf("seed candle %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		seeder.DeleteCandles(context.Background(), ReferenceExchange, asset)
	})
}

func TestEvaluate_ApprovesHealthyOperationWithGoodMarketData(t *testing.T) {
	s := testEvaluateStore(t)
	seeder := testEvaluateSeeder(t)
	ctx := context.Background()
	asset := "E2ECOIN"

	// Seed 10 fresh, low-volatility, high-liquidity 1m candles so every
	// quality rule passes. This test seeds its own fixture data under a
	// dedicated symbol; it doesn't depend on or disturb real collected data.
	seedFreshCandles(t, ctx, seeder, asset)

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
	count, err := seeder.CountApprovedDecisions(ctx, asset)
	if err != nil {
		t.Fatalf("CountApprovedDecisions: %v", err)
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

// TestEvaluate_RejectsExcessiveTradeValue covers a concentration/trade-value
// rejection path through Evaluate end-to-end: market data is present and
// fresh (so quality rules pass), but the proposed operation's Value exceeds
// the seeded max_value_per_trade limit (1000). Concentration/trade-value
// violations must never pause the system — only loss violations do — so
// risk_state must remain normal, unlike the loss-breach path above.
func TestEvaluate_RejectsExcessiveTradeValue(t *testing.T) {
	s := testEvaluateStore(t)
	seeder := testEvaluateSeeder(t)
	ctx := context.Background()
	asset := "E2ECOIN_TRADEVALUE"

	seedFreshCandles(t, ctx, seeder, asset)

	portfolio := PortfolioState{Cash: 100000}
	// Seeded max_value_per_trade is 1000; propose well above it. Portfolio
	// cash is large enough that this trade doesn't also trip the
	// concentration limits, isolating the max_trade_value rule.
	proposed := ProposedOperation{Asset: asset, Side: SideBuy, Quantity: 15, Value: 1500}

	decision, err := Evaluate(ctx, s, portfolio, proposed)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected rejection: value 1500 exceeds max_value_per_trade 1000, got reasons: %v", decision.Reasons)
	}

	st, err := s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != storage.StatusNormal {
		t.Errorf("Status = %q, want %q — a trade-value rejection must never pause the system", st.Status, storage.StatusNormal)
	}

	count, err := seeder.CountDecisions(ctx, asset, false)
	if err != nil {
		t.Fatalf("CountDecisions: %v", err)
	}
	if count == 0 {
		t.Fatal("expected the rejected decision to be recorded in risk_decisions")
	}
}
