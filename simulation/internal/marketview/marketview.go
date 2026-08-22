// simulation/internal/marketview/marketview.go
package marketview

import (
	"context"
	"time"

	"risk-engine/risk"

	"simulation/internal/storage"
)

// View implements strategy.MarketView: candles closed at or before Now(),
// read from risk.ExchangeFor(asset) — the same per-asset exchange
// risk-engine's own quality checks use.
type View struct {
	store *storage.Store
	now   time.Time
}

func New(store *storage.Store) *View {
	return &View{store: store}
}

// Advance moves the view's simulated "now" forward — called once per
// engine loop iteration, before Strategy.Decide.
func (v *View) Advance(now time.Time) {
	v.now = now
}

func (v *View) Now() time.Time {
	return v.now
}

func (v *View) Candles(ctx context.Context, timeframe, asset string, n int) ([]storage.Candle, error) {
	return v.store.RecentCandles(ctx, risk.ExchangeFor(asset), asset, timeframe, n, v.now)
}
