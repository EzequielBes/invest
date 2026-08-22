// risk-engine/risk/quality.go
package risk

import (
	"context"
	"fmt"
	"math"
	"time"

	"risk-engine/storage"
)

const (
	eligibilityHistory = 180 * 24 * time.Hour
	eligibilityWindow  = 30 * 24 * time.Hour
	minimumCoverage    = 0.95

	// minimumActiveExchanges: crypto requires redundancy across
	// independent venues (binance/bybit/okx); a single-source asset class
	// like Alpaca-only stocks has no second venue to require.
	minimumActiveExchangesCrypto = 2
	minimumActiveExchangesStock  = 1

	// minimumConsecutiveBars: crypto's abundant 1m candle data supports a
	// tight window; stocks (free-tier Alpaca, 15min-delayed) use a
	// coarser 5m candle, so the same 60-bar count spans 5 hours instead
	// of 1 — a realistic window given the data actually available.
	minimumConsecutiveBarsCrypto = 60
	minimumConsecutiveBarsStock  = 60
)

// EligibilityResult is a deterministic preflight result. It narrows the
// automation universe; it never authorizes an order by itself.
type EligibilityResult struct {
	Asset    string
	Eligible bool
	Reasons  []string
}

// barDuration maps the two candle granularities this system collects to
// their duration, for eligibility's coverage/consecutiveness math. Not a
// general timeframe parser — this system only ever uses these two.
var barDuration = map[string]time.Duration{"1m": time.Minute, "5m": 5 * time.Minute}

// AssessEligibility applies the baseline universe policy using only closed
// market data, for exchange/timeframe (the reference venue and candle
// granularity for asset's class — crypto and stocks use different
// thresholds since crypto has multi-exchange redundancy and abundant 1m
// data that a single-source, 5m-delayed stock feed doesn't). Missing data
// is a rejection, never an implicit approval.
func AssessEligibility(asset string, metrics storage.EligibilityMetrics, now time.Time, exchange, timeframe string) EligibilityResult {
	result := EligibilityResult{Asset: asset, Eligible: true}
	if metrics.HistoryStartedAt.IsZero() || metrics.HistoryStartedAt.After(now.Add(-eligibilityHistory)) {
		result.Reasons = append(result.Reasons, fmt.Sprintf("requires at least 180 days of %s candle history", exchange))
	}
	expected := int(eligibilityWindow / barDuration[timeframe])
	if float64(metrics.ThirtyDayClosedCandles)/float64(expected) < minimumCoverage {
		result.Reasons = append(result.Reasons, fmt.Sprintf("requires at least 95%% closed %s candle coverage over 30 days", timeframe))
	}

	minActiveExchanges := minimumActiveExchangesCrypto
	minConsecutiveBars := minimumConsecutiveBarsCrypto
	if !IsCrypto(asset) {
		minActiveExchanges = minimumActiveExchangesStock
		minConsecutiveBars = minimumConsecutiveBarsStock
	}
	if metrics.ActiveExchangeCount < minActiveExchanges {
		result.Reasons = append(result.Reasons, fmt.Sprintf("requires fresh candles from at least %d exchange(s)", minActiveExchanges))
	}
	if !consecutiveCandles(metrics.RecentCandles, minConsecutiveBars, barDuration[timeframe]) {
		result.Reasons = append(result.Reasons, fmt.Sprintf("requires %d consecutive closed %s %s candles", minConsecutiveBars, exchange, timeframe))
	}
	result.Eligible = len(result.Reasons) == 0
	return result
}

// marketDataReader is the slice of *storage.Store this file depends on, so
// quality rules are unit-testable with a fake instead of a real database.
type marketDataReader interface {
	LatestCandle(ctx context.Context, exchange, symbol, timeframe string, asOf *time.Time) (storage.Candle, bool, error)
	RecentCandles(ctx context.Context, exchange, symbol, timeframe string, n int, asOf *time.Time) ([]storage.Candle, error)
}

// qualityTimeframe returns the reference exchange, candle timeframe, and
// per-bar duration to check asset's data quality against — crypto and
// stocks route to different venues/granularities (see ExchangeFor).
func qualityTimeframe(asset string) (exchange, timeframe string, minConsecutiveBars int) {
	exchange = ExchangeFor(asset)
	if IsCrypto(asset) {
		return exchange, "1m", minimumConsecutiveBarsCrypto
	}
	return exchange, "5m", minimumConsecutiveBarsStock
}

func checkDataFreshness(ctx context.Context, md marketDataReader, asset string, maxAgeMinutes int, asOf *time.Time) RuleResult {
	exchange, timeframe, _ := qualityTimeframe(asset)
	candle, found, err := md.LatestCandle(ctx, exchange, asset, timeframe, asOf)
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
	exchange, timeframe, minConsecutiveBars := qualityTimeframe(asset)
	candles, err := md.RecentCandles(ctx, exchange, asset, timeframe, minConsecutiveBars, asOf)
	if err != nil {
		return RuleResult{Rule: "volatility", Passed: false, Limit: maxVolatility, Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if !consecutiveCandles(candles, minConsecutiveBars, barDuration[timeframe]) {
		return RuleResult{Rule: "volatility", Passed: false, Limit: maxVolatility, Detail: fmt.Sprintf("requires %d consecutive closed %s candles", minConsecutiveBars, timeframe)}
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
	exchange, timeframe, minConsecutiveBars := qualityTimeframe(asset)
	candles, err := md.RecentCandles(ctx, exchange, asset, timeframe, minConsecutiveBars, asOf)
	if err != nil {
		return RuleResult{Rule: "liquidity", Passed: false, Limit: minLiquidity, Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if !consecutiveCandles(candles, minConsecutiveBars, barDuration[timeframe]) {
		return RuleResult{Rule: "liquidity", Passed: false, Limit: minLiquidity, Detail: fmt.Sprintf("requires %d consecutive closed %s candles", minConsecutiveBars, timeframe)}
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

// consecutiveCandles reports whether candles has exactly want entries,
// each barDuration apart from the last — generalizes the original crypto-
// only 1-minute check to any candle granularity.
func consecutiveCandles(candles []storage.Candle, want int, barDuration time.Duration) bool {
	if len(candles) != want {
		return false
	}
	for i := 1; i < len(candles); i++ {
		if !candles[i].Time.Equal(candles[i-1].Time.Add(barDuration)) {
			return false
		}
	}
	return true
}
