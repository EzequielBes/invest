// simulation/internal/strategy/movingaverage_test.go
package strategy

import (
	"context"
	"testing"
	"time"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

type seriesView struct {
	now     time.Time
	candles []storage.Candle // fixed full series, most recent last
}

func (v *seriesView) Now() time.Time { return v.now }
func (v *seriesView) Candles(ctx context.Context, timeframe, asset string, n int) ([]storage.Candle, error) {
	if n > len(v.candles) {
		n = len(v.candles)
	}
	return v.candles[len(v.candles)-n:], nil
}

func closes(vals ...float64) []storage.Candle {
	cs := make([]storage.Candle, len(vals))
	for i, v := range vals {
		cs[i] = storage.Candle{Close: v}
	}
	return cs
}

func TestMovingAverageCrossStrategy_BuysOnGoldenCross(t *testing.T) {
	s := &MovingAverageCrossStrategy{Asset: "BTC", Timeframe: "1h", ShortPeriod: 2, LongPeriod: 4, TradeValue: 1000}

	// Long-period series still flat/declining: short == long, no cross yet.
	view := &seriesView{candles: closes(100, 100, 100, 100)}
	if _, err := s.Decide(context.Background(), view, portfolio.Snapshot{}); err != nil {
		t.Fatalf("Decide (warm-up): %v", err)
	}

	// Now short average (last 2) rises above long average (last 4).
	view.candles = closes(100, 100, 110, 130)
	ops, err := s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide (cross): %v", err)
	}
	if len(ops) != 1 || ops[0].Side != risk.SideBuy || ops[0].Asset != "BTC" {
		t.Fatalf("ops = %+v, want a single BTC buy", ops)
	}
}

func TestMovingAverageCrossStrategy_SellsOnDeathCrossIfHoldingPosition(t *testing.T) {
	s := &MovingAverageCrossStrategy{Asset: "BTC", Timeframe: "1h", ShortPeriod: 2, LongPeriod: 4, TradeValue: 1000}
	view := &seriesView{candles: closes(100, 100, 110, 130)} // short above long
	if _, err := s.Decide(context.Background(), view, portfolio.Snapshot{}); err != nil {
		t.Fatalf("Decide (warm-up): %v", err)
	}

	view.candles = closes(110, 130, 90, 80) // short now below long
	snap := portfolio.Snapshot{Positions: map[string]risk.Position{"BTC": {Asset: "BTC", Quantity: 10}}}
	ops, err := s.Decide(context.Background(), view, snap)
	if err != nil {
		t.Fatalf("Decide (death cross): %v", err)
	}
	if len(ops) != 1 || ops[0].Side != risk.SideSell || ops[0].Quantity != 10 {
		t.Fatalf("ops = %+v, want a single sell of the full 10 BTC position", ops)
	}
}

func TestMovingAverageCrossStrategy_NoSignal_ReturnsNoOps(t *testing.T) {
	s := &MovingAverageCrossStrategy{Asset: "BTC", Timeframe: "1h", ShortPeriod: 2, LongPeriod: 4, TradeValue: 1000}
	view := &seriesView{candles: closes(100, 100)} // fewer than LongPeriod candles
	ops, err := s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("ops = %+v, want none (insufficient history)", ops)
	}
}
