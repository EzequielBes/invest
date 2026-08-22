// risk-engine/risk/kelly_evaluate_test.go
package risk

import (
	"context"
	"testing"
)

// TestEvaluate_KellyCapRejectsWhenHistoricalEdgeIsNegative proves the
// concentration check uses a Kelly-derived cap (not the fixed
// max_pct_per_asset limit) once an asset has enough closed-trade history:
// a losing track record must lower the cap below what the fixed limit
// alone would allow, rejecting a trade the fixed limit would have passed.
func TestEvaluate_KellyCapRejectsWhenHistoricalEdgeIsNegative(t *testing.T) {
	s := testEvaluateStore(t)
	seeder := testEvaluateSeeder(t)
	ctx := context.Background()
	asset := "E2ECOIN_KELLYNEG"

	seedFreshCandles(t, ctx, seeder, asset)

	// 20 closed trades (meets kellyMinimumSampleSize): 5 wins of +5%, 15
	// losses of -10% — a clearly negative edge (full Kelly < 0), so the
	// cap must drop to 0 regardless of the fixed max_pct_per_asset limit.
	for i := 0; i < 20; i++ {
		var pnlPct float64
		if i < 5 {
			pnlPct = 0.05
		} else {
			pnlPct = -0.10
		}
		costBasis := 100.0
		realizedPnL := pnlPct * costBasis
		if err := seeder.InsertPaperFill(ctx, asset, 1, 100+pnlPct*100, costBasis, realizedPnL); err != nil {
			t.Fatalf("InsertPaperFill %d: %v", i, err)
		}
	}
	t.Cleanup(func() { seeder.DeletePaperFills(context.Background(), asset) })

	// Portfolio has plenty of room under the fixed max_pct_per_asset limit
	// (seeded at 0.2 = 20%): proposing to buy up to 10% of total value.
	portfolio := PortfolioState{Cash: 9000, Positions: map[string]Position{}}
	proposed := ProposedOperation{Asset: asset, Side: SideBuy, Quantity: 1, Value: 500}

	decision, err := Evaluate(ctx, s, portfolio, proposed, EvalOptions{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected rejection: negative historical edge should drop the Kelly cap to 0, got approval with rules: %+v", decision.RulesChecked)
	}

	failedConcentration := false
	for _, r := range decision.RulesChecked {
		if r.Rule == "asset_concentration" && !r.Passed {
			failedConcentration = true
			if r.Limit != 0 {
				t.Errorf("asset_concentration Limit = %v, want 0 (Kelly cap for a negative edge)", r.Limit)
			}
		}
	}
	if !failedConcentration {
		t.Fatalf("expected asset_concentration to be among the failed rules, got RulesChecked: %+v", decision.RulesChecked)
	}
}
