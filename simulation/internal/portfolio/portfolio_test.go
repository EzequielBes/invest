// simulation/internal/portfolio/portfolio_test.go
package portfolio

import (
	"testing"
	"time"

	"risk-engine/risk"
)

func TestApplyFill_Buy_UpdatesCashAndWeightedAvgCost(t *testing.T) {
	tr := NewTracker(10000)
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 1, Price: 100, Fee: 1})
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 1, Price: 200, Fee: 1})

	tr.MarkToMarket(time.Now(), map[string]float64{"BTC": 200})
	snap := tr.Snapshot(time.Now())

	wantCash := 10000.0 - 100 - 1 - 200 - 1
	if snap.Cash != wantCash {
		t.Errorf("Cash = %v, want %v", snap.Cash, wantCash)
	}
	pos, ok := snap.Positions["BTC"]
	if !ok {
		t.Fatal("expected a BTC position")
	}
	if pos.Quantity != 2 {
		t.Errorf("Quantity = %v, want 2", pos.Quantity)
	}
	wantValue := 2 * 200.0 // marked at last close
	if pos.Value != wantValue {
		t.Errorf("Value = %v, want %v", pos.Value, wantValue)
	}
}

func TestApplyFill_Sell_RealizesPnLAndTracksConsecutiveLosses(t *testing.T) {
	tr := NewTracker(10000)
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 1, Price: 100, Fee: 0})

	// Sell at a loss: realized = (90-100)*1 - 1 = -11
	realized := tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideSell, Quantity: 1, Price: 90, Fee: 1})
	if realized != -11 {
		t.Errorf("realized = %v, want -11", realized)
	}
	tr.MarkToMarket(time.Now(), map[string]float64{"BTC": 90})
	snap := tr.Snapshot(time.Now())
	if snap.ConsecutiveLosses != 1 {
		t.Errorf("ConsecutiveLosses = %d, want 1", snap.ConsecutiveLosses)
	}

	// Buy and sell again, this time at a profit — resets the streak.
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 1, Price: 100, Fee: 0})
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideSell, Quantity: 1, Price: 110, Fee: 0})
	tr.MarkToMarket(time.Now(), map[string]float64{"BTC": 110})
	snap = tr.Snapshot(time.Now())
	if snap.ConsecutiveLosses != 0 {
		t.Errorf("ConsecutiveLosses = %d, want 0 after a winning trade", snap.ConsecutiveLosses)
	}
}

func TestSnapshot_Drawdown_MeasuredFromPeak(t *testing.T) {
	tr := NewTracker(10000)
	now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	tr.MarkToMarket(now, nil) // 10000, new peak
	now = now.Add(time.Hour)
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 10, Price: 100, Fee: 0}) // cash 9000, 10 BTC
	tr.MarkToMarket(now, map[string]float64{"BTC": 150})                                   // equity = 9000 + 1500 = 10500, new peak
	now = now.Add(time.Hour)
	tr.MarkToMarket(now, map[string]float64{"BTC": 100}) // equity = 9000 + 1000 = 10000

	snap := tr.Snapshot(now)
	wantDD := (10500.0 - 10000.0) / 10500.0
	if snap.Drawdown != wantDD {
		t.Errorf("Drawdown = %v, want %v", snap.Drawdown, wantDD)
	}
}

func TestSnapshot_DailyLoss_MeasuredSinceStartOfUTCDay(t *testing.T) {
	tr := NewTracker(10000)
	dayStart := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)

	tr.MarkToMarket(dayStart.Add(-time.Hour), nil)  // yesterday, equity 10000 — must not count
	tr.MarkToMarket(dayStart.Add(1*time.Hour), nil) // today's first point, still 10000
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 10, Price: 100, Fee: 0})
	now := dayStart.Add(2 * time.Hour)
	tr.MarkToMarket(now, map[string]float64{"BTC": 90}) // equity = 9000 + 900 = 9900

	snap := tr.Snapshot(now)
	wantDailyLoss := (10000.0 - 9900.0) / 10000.0
	if snap.DailyLoss != wantDailyLoss {
		t.Errorf("DailyLoss = %v, want %v", snap.DailyLoss, wantDailyLoss)
	}
}
