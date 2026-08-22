package risk

import "testing"

func TestKellyFractionCap_BelowMinimumSampleUsesFallback(t *testing.T) {
	trades := make([]TradeOutcome, kellyMinimumSampleSize-1)
	for i := range trades {
		trades[i] = TradeOutcome{PnLPct: 0.1}
	}
	got := KellyFractionCap(trades, 0.2)
	if got != 0.2 {
		t.Errorf("KellyFractionCap() = %v, want fallback 0.2", got)
	}
}

func TestKellyFractionCap_ComputesHalfKellyAtMinimumSample(t *testing.T) {
	trades := make([]TradeOutcome, kellyMinimumSampleSize)
	// 15 wins of +10%, 5 losses of -5%: win_rate=0.75, payoff=10/5=2
	for i := range trades {
		if i < 15 {
			trades[i] = TradeOutcome{PnLPct: 0.10}
		} else {
			trades[i] = TradeOutcome{PnLPct: -0.05}
		}
	}
	// full Kelly = winRate - (1-winRate)/payoff = 0.75 - 0.25/2 = 0.625
	// half Kelly = 0.3125
	got := KellyFractionCap(trades, 0.2)
	want := 0.3125
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("KellyFractionCap() = %v, want %v", got, want)
	}
}

func TestKellyFractionCap_NegativeEdgeReturnsZero(t *testing.T) {
	trades := make([]TradeOutcome, kellyMinimumSampleSize)
	// 5 wins of +5%, 15 losses of -10%: win_rate=0.25, payoff=5/10=0.5
	// full Kelly = 0.25 - 0.75/0.5 = 0.25 - 1.5 = -1.25 (negative edge)
	for i := range trades {
		if i < 5 {
			trades[i] = TradeOutcome{PnLPct: 0.05}
		} else {
			trades[i] = TradeOutcome{PnLPct: -0.10}
		}
	}
	got := KellyFractionCap(trades, 0.2)
	if got != 0 {
		t.Errorf("KellyFractionCap() = %v, want 0 for negative edge", got)
	}
}

func TestKellyFractionCap_AllWinsReturnsFallback(t *testing.T) {
	trades := make([]TradeOutcome, kellyMinimumSampleSize)
	for i := range trades {
		trades[i] = TradeOutcome{PnLPct: 0.1}
	}
	// no losses means payoff ratio is undefined (division by zero avg loss)
	got := KellyFractionCap(trades, 0.2)
	if got != 0.2 {
		t.Errorf("KellyFractionCap() = %v, want fallback 0.2 when no losses recorded", got)
	}
}
