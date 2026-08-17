// simulation/internal/strategy/movingaverage.go
package strategy

import (
	"context"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

// MovingAverageCrossStrategy buys Asset when the short-period SMA crosses
// above the long-period SMA while flat, and sells the full position when
// it crosses back below — a minimal example proving the end-to-end loop
// works with a real decision, not just replay.
type MovingAverageCrossStrategy struct {
	Asset       string
	Timeframe   string
	ShortPeriod int
	LongPeriod  int
	TradeValue  float64 // cash value of each buy, in quote currency

	wasAbove bool
	hasPrev  bool
}

func (s *MovingAverageCrossStrategy) Decide(ctx context.Context, view MarketView, snap portfolio.Snapshot) ([]risk.ProposedOperation, error) {
	candles, err := view.Candles(ctx, s.Timeframe, s.Asset, s.LongPeriod)
	if err != nil {
		return nil, err
	}
	if len(candles) < s.LongPeriod {
		return nil, nil
	}

	short := sma(candles[len(candles)-s.ShortPeriod:])
	long := sma(candles)
	above := short > long
	wasAbove, hasPrev := s.wasAbove, s.hasPrev
	s.wasAbove, s.hasPrev = above, true

	if !hasPrev {
		return nil, nil
	}

	price := candles[len(candles)-1].Close
	pos := snap.Positions[s.Asset]

	if above && !wasAbove && pos.Quantity == 0 {
		qty := s.TradeValue / price
		return []risk.ProposedOperation{{Asset: s.Asset, Side: risk.SideBuy, Quantity: qty, Value: s.TradeValue}}, nil
	}
	if !above && wasAbove && pos.Quantity > 0 {
		return []risk.ProposedOperation{{Asset: s.Asset, Side: risk.SideSell, Quantity: pos.Quantity, Value: pos.Quantity * price}}, nil
	}
	return nil, nil
}

func sma(candles []storage.Candle) float64 {
	var sum float64
	for _, c := range candles {
		sum += c.Close
	}
	return sum / float64(len(candles))
}
