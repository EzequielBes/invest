package ranking

import "testing"

func TestCompute_AppliesQualityPenaltiesAndDeterministicTieBreak(t *testing.T) {
	got, err := Compute([]Input{
		{Asset: "ETH", OpportunityScore: 0.8, DataAgeMinutes: 20, Liquidity: 50, Volatility: 0.04},
		{Asset: "BTC", OpportunityScore: 0.8, DataAgeMinutes: 10, Liquidity: 100, Volatility: 0.02},
		{Asset: "ADA", OpportunityScore: 0.8, DataAgeMinutes: 10, Liquidity: 100, Volatility: 0.02},
	}, Limits{MaxDataAgeMinutes: 10, MinLiquidity: 100, MaxVolatility: 0.02})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(got))
	}
	if got[0].Asset != "ADA" || got[0].Rank != 1 || got[1].Asset != "BTC" || got[1].Rank != 2 {
		t.Fatalf("tie order = %#v, want ADA then BTC", got)
	}
	if got[2].Asset != "ETH" || got[2].CompositeScore != 0.1 {
		t.Errorf("penalized result = %+v, want ETH score 0.1", got[2])
	}
}

func TestCompute_RejectsInvalidInputs(t *testing.T) {
	_, err := Compute([]Input{{Asset: "BTC", OpportunityScore: 1.1, DataAgeMinutes: 1, Liquidity: 1, Volatility: 0}}, Limits{MaxDataAgeMinutes: 1, MinLiquidity: 1, MaxVolatility: 1})
	if err == nil {
		t.Fatal("expected invalid opportunity score error")
	}
}
