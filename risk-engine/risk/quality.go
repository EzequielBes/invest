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

const (
	eligibilityHistory     = 180 * 24 * time.Hour
	eligibilityWindow      = 30 * 24 * time.Hour
	minimumCoverage        = 0.95
	minimumActiveExchanges = 2
	minimumConsecutiveBars = 60
)

// EligibilityResult is a deterministic preflight result. It narrows the
// automation universe; it never authorizes an order by itself.
type EligibilityResult struct {
	Asset    string
	Eligible bool
	Reasons  []string
}

// AssessEligibility applies the baseline universe policy using only closed
// market data. Missing data is a rejection, never an implicit approval.
func AssessEligibility(asset string, metrics storage.EligibilityMetrics, now time.Time) EligibilityResult {
	result := EligibilityResult{Asset: asset, Eligible: true}
	if metrics.HistoryStartedAt.IsZero() || metrics.HistoryStartedAt.After(now.Add(-eligibilityHistory)) {
		result.Reasons = append(result.Reasons, "requires at least 180 days of Binance candle history")
	}
	expected := int(eligibilityWindow / time.Minute)
	if float64(metrics.ThirtyDayClosedCandles)/float64(expected) < minimumCoverage {
		result.Reasons = append(result.Reasons, "requires at least 95% closed 1m candle coverage over 30 days")
	}
	if metrics.ActiveExchangeCount < minimumActiveExchanges {
		result.Reasons = append(result.Reasons, "requires fresh candles from at least two exchanges")
	}
	if !consecutiveMinuteCandles(metrics.RecentCandles, minimumConsecutiveBars) {
		result.Reasons = append(result.Reasons, "requires 60 consecutive closed Binance 1m candles")
	}
	result.Eligible = len(result.Reasons) == 0
	return result
}

// marketDataReader is the slice of *storage.Store this file depends on, so
// quality rules are unit-testable with a fake instead of a real database.
type marketDataReader interface {
	LatestCandle(ctx context.Context, exchange, symbol string, asOf *time.Time) (storage.Candle, bool, error)
	RecentCandles(ctx context.Context, exchange, symbol string, n int, asOf *time.Time) ([]storage.Candle, error)
}

func checkDataFreshness(ctx context.Context, md marketDataReader, asset string, maxAgeMinutes int, asOf *time.Time) RuleResult {
	candle, found, err := md.LatestCandle(ctx, ReferenceExchange, asset, asOf)
	if err != nil {
		return RuleResult{Rule: "data_freshness", Passed: false, Limit: float64(maxAgeMinutes), Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if !found {
		return RuleResult{Rule: "data_freshness", Passed: false, Limit: float64(maxAgeMinutes), Detail: "no market data available"}
	}
	reference := time.Now()
	if asOf != nil {
		reference = *asOf
	}
	age := reference.Sub(candle.Time).Minutes()
	return RuleResult{
		Rule: "data_freshness", Passed: age <= float64(maxAgeMinutes),
		Measured: age, Limit: float64(maxAgeMinutes),
		Detail: fmt.Sprintf("latest candle is %.1f minutes old", age),
	}
}

func checkVolatility(ctx context.Context, md marketDataReader, asset string, maxVolatility float64, asOf *time.Time) RuleResult {
	candles, err := md.RecentCandles(ctx, ReferenceExchange, asset, 60, asOf)
	if err != nil {
		return RuleResult{Rule: "volatility", Passed: false, Limit: maxVolatility, Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if !consecutiveMinuteCandles(candles, minimumConsecutiveBars) {
		return RuleResult{Rule: "volatility", Passed: false, Limit: maxVolatility, Detail: "requires 60 consecutive closed 1m candles"}
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

func checkLiquidity(ctx context.Context, md marketDataReader, asset string, minLiquidity float64, asOf *time.Time) RuleResult {
	candles, err := md.RecentCandles(ctx, ReferenceExchange, asset, 60, asOf)
	if err != nil {
		return RuleResult{Rule: "liquidity", Passed: false, Limit: minLiquidity, Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if !consecutiveMinuteCandles(candles, minimumConsecutiveBars) {
		return RuleResult{Rule: "liquidity", Passed: false, Limit: minLiquidity, Detail: "requires 60 consecutive closed 1m candles"}
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

func consecutiveMinuteCandles(candles []storage.Candle, want int) bool {
	if len(candles) != want {
		return false
	}
	for i := 1; i < len(candles); i++ {
		if !candles[i].Time.Equal(candles[i-1].Time.Add(time.Minute)) {
			return false
		}
	}
	return true
}
