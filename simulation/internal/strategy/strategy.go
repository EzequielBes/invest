// simulation/internal/strategy/strategy.go
package strategy

import (
	"context"
	"time"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

// MarketView gives a Strategy read access to candles closed at or before
// the current simulated instant, across whichever timeframes it asks for —
// never data from the future relative to Now().
type MarketView interface {
	Candles(ctx context.Context, timeframe, asset string, n int) ([]storage.Candle, error)
	Now() time.Time
}

// Strategy decides what to do at each driving-timeframe candle. Value on
// each returned risk.ProposedOperation is Quantity times the current
// driving-timeframe close (the price known at decision time) — the actual
// fill price (next candle's open) is resolved later by the engine.
type Strategy interface {
	Decide(ctx context.Context, view MarketView, snap portfolio.Snapshot) ([]risk.ProposedOperation, error)
}
