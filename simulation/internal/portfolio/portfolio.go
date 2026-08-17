// simulation/internal/portfolio/portfolio.go
package portfolio

import (
	"time"

	"risk-engine/risk"
)

// Snapshot is the simulated portfolio's state at one instant, in the exact
// shape risk.PortfolioState expects — engine converts between the two with
// a direct type conversion (identical underlying type), no wrapper needed.
type Snapshot struct {
	Positions         map[string]risk.Position
	Cash              float64
	DailyLoss         float64
	WeeklyLoss        float64
	Drawdown          float64
	ConsecutiveLosses int
}

// Fill is one executed trade applied to the tracker. Price is the actual
// fill price (next driving-timeframe candle's open); Fee is charged in
// cash separately from Price*Quantity.
type Fill struct {
	Time     time.Time
	Asset    string
	Side     risk.Side
	Quantity float64
	Price    float64
	Fee      float64
}

type equityPoint struct {
	time  time.Time
	total float64
}

// Tracker holds simulated portfolio state across one backtest run: cash,
// per-asset quantity and weighted-average cost basis, and the equity
// history needed to compute DailyLoss/WeeklyLoss/Drawdown at each step.
type Tracker struct {
	cash              float64
	quantity          map[string]float64
	avgEntry          map[string]float64
	lastClose         map[string]float64
	peakEquity        float64
	consecutiveLosses int
	equityHistory     []equityPoint
}

func NewTracker(initialCash float64) *Tracker {
	return &Tracker{
		cash:       initialCash,
		quantity:   map[string]float64{},
		avgEntry:   map[string]float64{},
		lastClose:  map[string]float64{},
		peakEquity: initialCash,
	}
}

// ApplyFill executes a fill: debits/credits cash, updates quantity and
// weighted-average cost basis on a buy, and on a sell computes realized
// P&L as (price-avgEntry)*qty-fee, feeding ConsecutiveLosses. Returns the
// realized P&L in currency units (always 0 for a buy).
func (t *Tracker) ApplyFill(f Fill) float64 {
	switch f.Side {
	case risk.SideBuy:
		cost := f.Quantity * f.Price
		newQty := t.quantity[f.Asset] + f.Quantity
		if newQty > 0 {
			t.avgEntry[f.Asset] = (t.avgEntry[f.Asset]*t.quantity[f.Asset] + cost) / newQty
		}
		t.quantity[f.Asset] = newQty
		t.cash -= cost + f.Fee
		return 0
	case risk.SideSell:
		realized := (f.Price-t.avgEntry[f.Asset])*f.Quantity - f.Fee
		t.quantity[f.Asset] -= f.Quantity
		t.cash += f.Quantity*f.Price - f.Fee
		if realized < 0 {
			t.consecutiveLosses++
		} else {
			t.consecutiveLosses = 0
		}
		return realized
	}
	return 0
}

// MarkToMarket values the whole portfolio at now using closes (per-asset
// last-known close price; an asset absent from closes keeps its previous
// price), updates the peak-equity high-water mark, and records the point
// for later DailyLoss/WeeklyLoss/EquityCurve queries.
func (t *Tracker) MarkToMarket(now time.Time, closes map[string]float64) (cash, positionsValue, totalEquity float64) {
	for asset, price := range closes {
		t.lastClose[asset] = price
	}
	for asset, qty := range t.quantity {
		positionsValue += qty * t.lastClose[asset]
	}
	cash = t.cash
	totalEquity = cash + positionsValue
	if totalEquity > t.peakEquity {
		t.peakEquity = totalEquity
	}
	t.equityHistory = append(t.equityHistory, equityPoint{time: now, total: totalEquity})
	return cash, positionsValue, totalEquity
}

// Snapshot reports the portfolio's current state for the risk-engine and
// the Strategy. Call MarkToMarket for now before Snapshot in the same
// loop step, so the equity figures Snapshot derives are current.
func (t *Tracker) Snapshot(now time.Time) Snapshot {
	positions := make(map[string]risk.Position, len(t.quantity))
	for asset, qty := range t.quantity {
		if qty == 0 {
			continue
		}
		positions[asset] = risk.Position{Asset: asset, Quantity: qty, Value: qty * t.lastClose[asset]}
	}
	var current float64
	if n := len(t.equityHistory); n > 0 {
		current = t.equityHistory[n-1].total
	}
	return Snapshot{
		Positions:         positions,
		Cash:              t.cash,
		DailyLoss:         t.periodLoss(startOfUTCDay(now)),
		WeeklyLoss:        t.periodLoss(startOfUTCWeek(now)),
		Drawdown:          t.drawdown(current),
		ConsecutiveLosses: t.consecutiveLosses,
	}
}

// EquityCurve returns the recorded total-equity values in chronological
// order, for metrics.Compute at the end of a run.
func (t *Tracker) EquityCurve() []float64 {
	vals := make([]float64, len(t.equityHistory))
	for i, p := range t.equityHistory {
		vals[i] = p.total
	}
	return vals
}

// periodLoss is the fractional drop in total equity from the first
// recorded point at or after periodStart to the most recent point — never
// negative (a gain reports 0 loss).
func (t *Tracker) periodLoss(periodStart time.Time) float64 {
	if len(t.equityHistory) == 0 {
		return 0
	}
	current := t.equityHistory[len(t.equityHistory)-1].total
	baseline := current
	for _, p := range t.equityHistory {
		if !p.time.Before(periodStart) {
			baseline = p.total
			break
		}
	}
	if baseline <= 0 {
		return 0
	}
	if loss := (baseline - current) / baseline; loss > 0 {
		return loss
	}
	return 0
}

func (t *Tracker) drawdown(current float64) float64 {
	if t.peakEquity <= 0 {
		return 0
	}
	if dd := (t.peakEquity - current) / t.peakEquity; dd > 0 {
		return dd
	}
	return 0
}

func startOfUTCDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func startOfUTCWeek(t time.Time) time.Time {
	day := startOfUTCDay(t)
	offset := (int(day.Weekday()) + 6) % 7 // Monday = 0
	return day.AddDate(0, 0, -offset)
}
