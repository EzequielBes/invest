// simulation/internal/engine/fill.go
package engine

import "risk-engine/risk"

// PendingFill is an approved operation queued at decision time (a
// candle's close), executed at the NEXT candle's open — never the candle
// that produced the signal, avoiding lookahead bias.
type PendingFill struct {
	Asset    string
	Side     risk.Side
	Quantity float64
}

// applyFee returns the fee amount for a trade of value at feePct (e.g.
// 0.001 for 0.1%).
func applyFee(value, feePct float64) float64 {
	return value * feePct
}
