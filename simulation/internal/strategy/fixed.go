// simulation/internal/strategy/fixed.go
package strategy

import (
	"context"
	"time"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

// FixedOp is one pre-scripted operation FixedOperationsStrategy replays.
type FixedOp struct {
	Time     time.Time
	Asset    string
	Side     risk.Side
	Quantity float64
}

// FixedOperationsStrategy replays a pre-defined list of operations,
// emitting each one on the first Decide call whose driving-timeframe
// candle window [Now()-duration, Now()) contains its Time. Ops need not be
// sorted; already-emitted ops are never re-emitted. Covers the replay case
// and serves as the fixture for this module's own integration tests.
type FixedOperationsStrategy struct {
	Ops             []FixedOp
	drivingDuration time.Duration
	drivingTF       string
	emitted         map[int]bool
}

func NewFixedOperationsStrategy(ops []FixedOp, drivingTimeframe string) (*FixedOperationsStrategy, error) {
	dur, err := storage.TimeframeDuration(drivingTimeframe)
	if err != nil {
		return nil, err
	}
	return &FixedOperationsStrategy{Ops: ops, drivingDuration: dur, drivingTF: drivingTimeframe, emitted: map[int]bool{}}, nil
}

func (s *FixedOperationsStrategy) Decide(ctx context.Context, view MarketView, snap portfolio.Snapshot) ([]risk.ProposedOperation, error) {
	now := view.Now()
	windowStart := now.Add(-s.drivingDuration)
	var out []risk.ProposedOperation
	for i, op := range s.Ops {
		if s.emitted[i] || op.Time.Before(windowStart) || !op.Time.Before(now) {
			continue
		}
		s.emitted[i] = true
		candles, err := view.Candles(ctx, s.drivingTF, op.Asset, 1)
		if err != nil {
			return nil, err
		}
		var price float64
		if len(candles) > 0 {
			price = candles[len(candles)-1].Close
		}
		out = append(out, risk.ProposedOperation{
			Asset: op.Asset, Side: op.Side, Quantity: op.Quantity, Value: op.Quantity * price,
		})
	}
	return out, nil
}
