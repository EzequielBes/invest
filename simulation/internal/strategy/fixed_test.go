// simulation/internal/strategy/fixed_test.go
package strategy

import (
	"context"
	"testing"
	"time"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

type fakeView struct {
	now    time.Time
	closes map[string]float64
}

func (v *fakeView) Now() time.Time { return v.now }
func (v *fakeView) Candles(ctx context.Context, timeframe, asset string, n int) ([]storage.Candle, error) {
	price, ok := v.closes[asset]
	if !ok {
		return nil, nil
	}
	return []storage.Candle{{Time: v.now, Close: price}}, nil
}

func TestFixedOperationsStrategy_EmitsEachOpExactlyOnceInItsWindow(t *testing.T) {
	base := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := NewFixedOperationsStrategy([]FixedOp{
		{Time: base.Add(30 * time.Minute), Asset: "BTC", Side: risk.SideBuy, Quantity: 1},
		{Time: base.Add(90 * time.Minute), Asset: "ETH", Side: risk.SideBuy, Quantity: 2},
	}, "1h")
	if err != nil {
		t.Fatalf("NewFixedOperationsStrategy: %v", err)
	}

	view := &fakeView{now: base.Add(time.Hour), closes: map[string]float64{"BTC": 100, "ETH": 200}}
	ops, err := s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide (step 1): %v", err)
	}
	if len(ops) != 1 || ops[0].Asset != "BTC" {
		t.Fatalf("step 1 ops = %+v, want exactly the BTC op (its window is [0h,1h))", ops)
	}
	if ops[0].Value != 100 {
		t.Errorf("Value = %v, want 100 (1 * close 100)", ops[0].Value)
	}

	// Re-calling Decide for the same window must not re-emit the BTC op.
	ops, err = s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide (re-call): %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("re-call ops = %+v, want none (already emitted)", ops)
	}

	view.now = base.Add(2 * time.Hour)
	ops, err = s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide (step 2): %v", err)
	}
	if len(ops) != 1 || ops[0].Asset != "ETH" {
		t.Fatalf("step 2 ops = %+v, want exactly the ETH op (its window is [1h,2h))", ops)
	}
}

func TestNewFixedOperationsStrategy_RejectsUnknownTimeframe(t *testing.T) {
	if _, err := NewFixedOperationsStrategy(nil, "3m"); err == nil {
		t.Fatal("expected an error for an uncollected timeframe")
	}
}
