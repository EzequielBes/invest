// simulation/internal/metrics/metrics_test.go
package metrics

import "testing"

func approxEqual(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestCompute_TotalReturnAndDrawdown(t *testing.T) {
	// Equity rises to a peak of 11000, drops to 9900 (drawdown from peak),
	// ends at 10500.
	equity := []float64{10000, 11000, 9900, 10500}
	r := Compute(equity, nil, 365)

	wantReturn := (10500.0 - 10000.0) / 10000.0 * 100
	if !approxEqual(r.TotalReturnPct, wantReturn, 1e-9) {
		t.Errorf("TotalReturnPct = %v, want %v", r.TotalReturnPct, wantReturn)
	}

	wantDD := (11000.0 - 9900.0) / 11000.0 * 100
	if !approxEqual(r.MaxDrawdownPct, wantDD, 1e-9) {
		t.Errorf("MaxDrawdownPct = %v, want %v", r.MaxDrawdownPct, wantDD)
	}
}

func TestCompute_WinRateAndAvgTrade(t *testing.T) {
	trades := []float64{5, -2, 3, -1} // 2 of 4 positive
	r := Compute([]float64{10000, 10000}, trades, 365)

	if r.TotalTrades != 4 {
		t.Errorf("TotalTrades = %d, want 4", r.TotalTrades)
	}
	if !approxEqual(r.WinRatePct, 50, 1e-9) {
		t.Errorf("WinRatePct = %v, want 50", r.WinRatePct)
	}
	wantAvg := (5.0 - 2 + 3 - 1) / 4
	if !approxEqual(r.AvgTradePct, wantAvg, 1e-9) {
		t.Errorf("AvgTradePct = %v, want %v", r.AvgTradePct, wantAvg)
	}
}

func TestCompute_SharpeAndSortino_KnownSeries(t *testing.T) {
	// Equity returns: +1%, -1%, +1%, -1% — mean return 0, so Sharpe and
	// Sortino are both exactly 0 regardless of volatility.
	equity := []float64{100, 101, 99.99, 100.99, 99.98}
	r := Compute(equity, nil, 365)
	if !approxEqual(r.SharpeRatio, 0, 1e-5) {
		t.Errorf("SharpeRatio = %v, want ~0 (mean return is ~0)", r.SharpeRatio)
	}
	if !approxEqual(r.SortinoRatio, 0, 1e-5) {
		t.Errorf("SortinoRatio = %v, want ~0 (mean return is ~0)", r.SortinoRatio)
	}
}

func TestCompute_Sortino_OnlyPenalizesDownside(t *testing.T) {
	// Series A: alternating +2%/-2% (symmetric). Series B: same magnitude
	// of downside moves but the upside moves are much larger — Sortino
	// should be higher for B than A even though both have the same
	// downside deviation, because Sortino's numerator (mean return) is
	// larger while the denominator (downside deviation) is identical.
	equityA := []float64{100, 102, 99.96, 101.96, 99.92}
	equityB := []float64{100, 110, 107.8, 118.58, 116.21}
	a := Compute(equityA, nil, 365)
	b := Compute(equityB, nil, 365)
	if !(b.SortinoRatio > a.SortinoRatio) {
		t.Errorf("expected series B's Sortino (%v) > series A's (%v) — same downside shape, larger mean return", b.SortinoRatio, a.SortinoRatio)
	}
}

func TestCompute_EmptyInputs_NoDivideByZero(t *testing.T) {
	r := Compute(nil, nil, 365)
	if r.TotalReturnPct != 0 || r.SharpeRatio != 0 || r.SortinoRatio != 0 || r.WinRatePct != 0 {
		t.Errorf("expected all-zero Results for empty input, got %+v", r)
	}
}
