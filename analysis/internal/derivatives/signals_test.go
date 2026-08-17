// analysis/internal/derivatives/signals_test.go
package derivatives

import "testing"

func TestCompute_NormalFundingNoCascade(t *testing.T) {
	got := Compute(0.0002, 1000, 900, nil)
	if got.FundingExtreme {
		t.Error("FundingExtreme = true, want false for 0.02% funding")
	}
	if got.LiquidationCascade {
		t.Error("LiquidationCascade = true, want false with no liquidations")
	}
	wantOIChange := (1000.0 - 900.0) / 900.0 * 100
	if diff := got.OIChangePct - wantOIChange; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("OIChangePct = %.6f, want %.6f", got.OIChangePct, wantOIChange)
	}
}

func TestCompute_ExtremeFundingAndCascade(t *testing.T) {
	liqs := []Liquidation{
		{Price: 50000, Quantity: 15},
		{Price: 50000, Quantity: 10},
	} // 25 * 50000 = 1,250,000 > threshold
	got := Compute(-0.005, 1000, 1000, liqs)
	if !got.FundingExtreme {
		t.Error("FundingExtreme = false, want true for -0.5% funding")
	}
	if !got.LiquidationCascade {
		t.Error("LiquidationCascade = false, want true for $1.25M in liquidations")
	}
	if got.LiquidationVolume1h != 1_250_000 {
		t.Errorf("LiquidationVolume1h = %.2f, want 1250000", got.LiquidationVolume1h)
	}
}

func TestCompute_ZeroOI24hAgoNoDivideByZero(t *testing.T) {
	got := Compute(0, 1000, 0, nil)
	if got.OIChangePct != 0 {
		t.Errorf("OIChangePct = %.2f, want 0 when oi24hAgo is 0 (avoid divide-by-zero)", got.OIChangePct)
	}
}
