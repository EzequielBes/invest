package risk

// kellyMinimumSampleSize is the fewest closed trades required before Kelly
// sizing replaces the fixed fallback cap — below this, win_rate/payoff
// estimates are too noisy to size real money against.
const kellyMinimumSampleSize = 20

// kellyFraction is the fraction of full Kelly applied (half-Kelly): full
// Kelly maximizes long-run growth but with violent variance; half-Kelly
// trades some growth for a much smoother equity curve.
const kellyFraction = 0.5

// TradeOutcome is one closed trade's realized return, as a fraction of the
// position's cost basis (e.g. 0.10 = +10%, -0.05 = -5%).
type TradeOutcome struct {
	PnLPct float64
}

// KellyFractionCap returns the max position size (as a fraction of
// portfolio value) to allow for an asset, given its closed-trade history.
// Below kellyMinimumSampleSize trades, or when the computed edge is
// undefined (no losses recorded) or negative, it returns fallbackPct
// unchanged — Kelly only tightens the cap once there's a real edge to
// measure, it never loosens a caller-supplied cap.
func KellyFractionCap(trades []TradeOutcome, fallbackPct float64) float64 {
	if len(trades) < kellyMinimumSampleSize {
		return fallbackPct
	}

	var wins, losses int
	var winSum, lossSum float64
	for _, t := range trades {
		if t.PnLPct > 0 {
			wins++
			winSum += t.PnLPct
		} else if t.PnLPct < 0 {
			losses++
			lossSum += -t.PnLPct
		}
	}
	if losses == 0 || wins == 0 {
		return fallbackPct
	}

	winRate := float64(wins) / float64(len(trades))
	avgWin := winSum / float64(wins)
	avgLoss := lossSum / float64(losses)
	payoffRatio := avgWin / avgLoss

	fullKelly := winRate - (1-winRate)/payoffRatio
	if fullKelly <= 0 {
		return 0
	}
	return fullKelly * kellyFraction
}
