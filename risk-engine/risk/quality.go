// risk-engine/risk/quality.go
package risk

import (
	"context"
	"fmt"
	"math"
	"time"

	"risk-engine/storage"
)

// ReferenceExchange is which exchange's market data quality rules are
// checked against. All three exchanges are collected by the
// market-data-foundation sub-project, but for a single personal risk
// check, one consistent reference source is simpler and sufficient for
// this phase.
const ReferenceExchange = "binance"

// marketDataReader is the slice of *storage.Store this file depends on, so
// quality rules are unit-testable with a fake instead of a real database.
type marketDataReader interface {
	LatestCandle(ctx context.Context, exchange, symbol string) (storage.Candle, bool, error)
	RecentCandles(ctx context.Context, exchange, symbol string, n int) ([]storage.Candle, error)
}

func checkDataFreshness(ctx context.Context, md marketDataReader, asset string, maxAgeMinutes int) RuleResult {
	candle, found, err := md.LatestCandle(ctx, ReferenceExchange, asset)
	if err != nil {
		return RuleResult{Rule: "data_freshness", Passed: false, Limit: float64(maxAgeMinutes), Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if !found {
		return RuleResult{Rule: "data_freshness", Passed: false, Limit: float64(maxAgeMinutes), Detail: "no market data available"}
	}
	age := time.Since(candle.Time).Minutes()
	return RuleResult{
		Rule: "data_freshness", Passed: age <= float64(maxAgeMinutes),
		Measured: age, Limit: float64(maxAgeMinutes),
		Detail: fmt.Sprintf("latest candle is %.1f minutes old", age),
	}
}

func checkVolatility(ctx context.Context, md marketDataReader, asset string, maxVolatility float64) RuleResult {
	candles, err := md.RecentCandles(ctx, ReferenceExchange, asset, 60)
	if err != nil {
		return RuleResult{Rule: "volatility", Passed: false, Limit: maxVolatility, Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if len(candles) < 2 {
		return RuleResult{Rule: "volatility", Passed: false, Limit: maxVolatility, Detail: "insufficient market data"}
	}
	vol := stddevReturns(candles)
	return RuleResult{
		Rule: "volatility", Passed: vol <= maxVolatility,
		Measured: vol, Limit: maxVolatility,
		Detail: fmt.Sprintf("volatility over last %d candles: %.4f", len(candles), vol),
	}
}

func stddevReturns(candles []storage.Candle) float64 {
	returns := make([]float64, 0, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		if candles[i-1].Close == 0 {
			continue
		}
		returns = append(returns, (candles[i].Close-candles[i-1].Close)/candles[i-1].Close)
	}
	if len(returns) == 0 {
		return 0
	}
	var mean float64
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	var variance float64
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
	}
	variance /= float64(len(returns))
	return math.Sqrt(variance)
}

func checkLiquidity(ctx context.Context, md marketDataReader, asset string, minLiquidity float64) RuleResult {
	candles, err := md.RecentCandles(ctx, ReferenceExchange, asset, 60)
	if err != nil {
		return RuleResult{Rule: "liquidity", Passed: false, Limit: minLiquidity, Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if len(candles) == 0 {
		return RuleResult{Rule: "liquidity", Passed: false, Limit: minLiquidity, Detail: "insufficient market data"}
	}
	var quoteVolume float64
	for _, c := range candles {
		quoteVolume += c.Volume * c.Close
	}
	return RuleResult{
		Rule: "liquidity", Passed: quoteVolume >= minLiquidity,
		Measured: quoteVolume, Limit: minLiquidity,
		Detail: fmt.Sprintf("quote volume over last %d candles: %.2f", len(candles), quoteVolume),
	}
}
