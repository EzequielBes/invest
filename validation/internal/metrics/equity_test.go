package metrics

import (
	"testing"
	"time"
)

func TestEquityMetrics_FlatSeries(t *testing.T) {
	start := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	summary, err := EquityMetrics([]EquityPoint{
		{Time: start, Equity: 100},
		{Time: start.Add(time.Hour), Equity: 100},
		{Time: start.Add(2 * time.Hour), Equity: 100},
	})
	if err != nil {
		t.Fatalf("EquityMetrics: %v", err)
	}
	if summary != (EquitySummary{}) {
		t.Errorf("EquityMetrics() = %+v, want zero summary", summary)
	}
}

func TestEquityMetrics_RecoveredDrawdown(t *testing.T) {
	start := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	summary, err := EquityMetrics([]EquityPoint{
		{Time: start, Equity: 100},
		{Time: start.Add(time.Hour), Equity: 80},
		{Time: start.Add(3 * time.Hour), Equity: 100},
	})
	if err != nil {
		t.Fatalf("EquityMetrics: %v", err)
	}
	if summary.MaxDrawdownPct != 20 {
		t.Errorf("MaxDrawdownPct = %v, want 20", summary.MaxDrawdownPct)
	}
	if summary.CurrentDrawdownPct != 0 {
		t.Errorf("CurrentDrawdownPct = %v, want 0", summary.CurrentDrawdownPct)
	}
	if summary.MaxRecoveryDuration != 3*time.Hour {
		t.Errorf("MaxRecoveryDuration = %v, want 3h", summary.MaxRecoveryDuration)
	}
	if summary.TimeUnderWater != 3*time.Hour {
		t.Errorf("TimeUnderWater = %v, want 3h", summary.TimeUnderWater)
	}
}

func TestEquityMetrics_OpenDrawdown(t *testing.T) {
	start := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	summary, err := EquityMetrics([]EquityPoint{
		{Time: start, Equity: 100},
		{Time: start.Add(time.Hour), Equity: 120},
		{Time: start.Add(2 * time.Hour), Equity: 90},
		{Time: start.Add(5 * time.Hour), Equity: 96},
	})
	if err != nil {
		t.Fatalf("EquityMetrics: %v", err)
	}
	if summary.MaxDrawdownPct != 25 {
		t.Errorf("MaxDrawdownPct = %v, want 25", summary.MaxDrawdownPct)
	}
	if summary.CurrentDrawdownPct != 20 {
		t.Errorf("CurrentDrawdownPct = %v, want 20", summary.CurrentDrawdownPct)
	}
	if summary.CurrentTimeUnderWater != 4*time.Hour {
		t.Errorf("CurrentTimeUnderWater = %v, want 4h", summary.CurrentTimeUnderWater)
	}
	if summary.TimeUnderWater != 0 || summary.MaxRecoveryDuration != 0 {
		t.Errorf("open episode must not be counted as recovered: %+v", summary)
	}
}

func TestEquityMetrics_InvalidPoints(t *testing.T) {
	start := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	for _, points := range [][]EquityPoint{
		nil,
		{{Time: start, Equity: 0}},
		{{Time: start.Add(time.Hour), Equity: 100}, {Time: start, Equity: 100}},
	} {
		if _, err := EquityMetrics(points); err == nil {
			t.Errorf("EquityMetrics(%+v) returned nil error", points)
		}
	}
}
