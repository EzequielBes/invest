// analysis/internal/derivatives/signals.go
package derivatives

const (
	fundingExtremeThreshold     = 0.001   // 0.1%
	liquidationCascadeThreshold = 1000000 // $1,000,000 notional in the last hour
)

// Liquidation is the minimal shape signals needs from one liquidation
// event — decoupled from any storage package.
type Liquidation struct {
	Price    float64
	Quantity float64
}

// Signals holds the structured derivatives indicators computed for one
// asset from its most recent funding rate, open interest, and liquidation
// data.
type Signals struct {
	FundingRate         float64 `json:"funding_rate"`
	FundingExtreme      bool    `json:"funding_extreme"`
	OIChangePct         float64 `json:"oi_change_pct"`
	LiquidationVolume1h float64 `json:"liquidation_volume_1h"`
	LiquidationCascade  bool    `json:"liquidation_cascade"`
}

// Compute derives derivatives signals from the latest funding rate, the
// current and 24h-ago open interest, and liquidations in the last hour.
// oi24hAgo of 0 means no comparison point was found — OIChangePct is 0 in
// that case rather than a divide-by-zero.
func Compute(fundingRate, currentOI, oi24hAgo float64, recentLiquidations []Liquidation) Signals {
	var oiChangePct float64
	if oi24hAgo != 0 {
		oiChangePct = (currentOI - oi24hAgo) / oi24hAgo * 100
	}

	var liqVolume float64
	for _, l := range recentLiquidations {
		liqVolume += l.Price * l.Quantity
	}

	return Signals{
		FundingRate:         fundingRate,
		FundingExtreme:      absFloat(fundingRate) > fundingExtremeThreshold,
		OIChangePct:         oiChangePct,
		LiquidationVolume1h: liqVolume,
		LiquidationCascade:  liqVolume > liquidationCascadeThreshold,
	}
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
