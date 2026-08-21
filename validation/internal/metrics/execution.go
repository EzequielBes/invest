package metrics

import (
	"fmt"
	"math"
	"strings"
)

// Trade carries the fields needed to calculate absolute traded notional.
type Trade struct {
	Quantity float64
	Price    float64
}

// SlippageBps reports adverse slippage in basis points. Positive values are
// worse fills and negative values are price improvement.
func SlippageBps(side string, requestedPrice, filledPrice float64) (float64, error) {
	if err := validatePositiveFinite("requested price", requestedPrice); err != nil {
		return 0, err
	}
	if err := validatePositiveFinite("filled price", filledPrice); err != nil {
		return 0, err
	}

	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy":
		return (filledPrice - requestedPrice) / requestedPrice * 10_000, nil
	case "sell":
		return (requestedPrice - filledPrice) / requestedPrice * 10_000, nil
	default:
		return 0, fmt.Errorf("unknown side %q", side)
	}
}

// TurnoverPct returns absolute traded notional as a percentage of average
// equity.
func TurnoverPct(trades []Trade, averageEquity float64) (float64, error) {
	if err := validatePositiveFinite("average equity", averageEquity); err != nil {
		return 0, err
	}

	var notional float64
	for _, trade := range trades {
		if err := validatePositiveFinite("trade price", trade.Price); err != nil {
			return 0, err
		}
		if math.IsNaN(trade.Quantity) || math.IsInf(trade.Quantity, 0) {
			return 0, fmt.Errorf("trade quantity must be finite")
		}
		notional += math.Abs(trade.Quantity * trade.Price)
	}
	return notional / averageEquity * 100, nil
}

func validatePositiveFinite(name string, value float64) error {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s must be positive and finite", name)
	}
	return nil
}
