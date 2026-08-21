// risk-engine/risk/quality_test.go
package risk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"risk-engine/storage"
)

// fakeMarketData lets quality rule tests run without a database.
type fakeMarketData struct {
	latest      storage.Candle
	latestFound bool
	latestErr   error
	recent      []storage.Candle
	recentErr   error
}

func (f *fakeMarketData) LatestCandle(ctx context.Context, exchange, symbol string, asOf *time.Time) (storage.Candle, bool, error) {
	return f.latest, f.latestFound, f.latestErr
}
func (f *fakeMarketData) RecentCandles(ctx context.Context, exchange, symbol string, n int, asOf *time.Time) ([]storage.Candle, error) {
	return f.recent, f.recentErr
}

func TestCheckDataFreshness_RejectsStaleData(t *testing.T) {
	md := &fakeMarketData{
		latest:      storage.Candle{Time: time.Now().UTC().Add(-45 * time.Minute)},
		latestFound: true,
	}
	result := checkDataFreshness(context.Background(), md, "BTC", 30, nil)
	if result.Passed {
		t.Fatal("expected rejection: candle is 45 minutes old, limit 30")
	}
}

func TestCheckDataFreshness_RejectsMissingData(t *testing.T) {
	md := &fakeMarketData{latestFound: false}
	result := checkDataFreshness(context.Background(), md, "BTC", 30, nil)
	if result.Passed {
		t.Fatal("expected rejection when no candle data exists (fail-safe)")
	}
}

func TestCheckDataFreshness_RejectsOnLookupError(t *testing.T) {
	md := &fakeMarketData{latestErr: errors.New("connection refused")}
	result := checkDataFreshness(context.Background(), md, "BTC", 30, nil)
	if result.Passed {
		t.Fatal("expected rejection when the market data lookup itself fails")
	}
	if result.Limit != 30 {
		t.Errorf("Limit = %v, want 30 (the configured limit, not the zero value)", result.Limit)
	}
	if !strings.Contains(result.Detail, "market data lookup failed") {
		t.Errorf("Detail = %q, want it to distinguish a lookup error from missing data", result.Detail)
	}
}

func TestCheckDataFreshness_AllowsFreshData(t *testing.T) {
	md := &fakeMarketData{
		latest:      storage.Candle{Time: time.Now().UTC().Add(-5 * time.Minute)},
		latestFound: true,
	}
	result := checkDataFreshness(context.Background(), md, "BTC", 30, nil)
	if !result.Passed {
		t.Fatal("expected approval: candle is 5 minutes old, limit 30")
	}
}

func TestCheckDataFreshness_AsOf_MeasuresAgeRelativeToAsOf(t *testing.T) {
	// The candle is "ancient" relative to real wall-clock now, but fresh
	// relative to a simulated asOf a year in the past — freshness must be
	// judged against asOf, not time.Now(), or every backtest over
	// historical data would reject on data_freshness immediately.
	candleTime := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	asOf := candleTime.Add(5 * time.Minute)
	md := &fakeMarketData{
		latest:      storage.Candle{Time: candleTime},
		latestFound: true,
	}
	result := checkDataFreshness(context.Background(), md, "BTC", 30, &asOf)
	if !result.Passed {
		t.Fatalf("expected approval: candle is 5 minutes old relative to asOf, limit 30, got Detail=%q", result.Detail)
	}
}

func TestCheckVolatility_RejectsHighVolatility(t *testing.T) {
	// Alternating +10%/-9% moves produce high volatility.
	base := time.Now().UTC().Add(-10 * time.Minute)
	md := &fakeMarketData{recent: []storage.Candle{
		{Time: base, Close: 100},
		{Time: base.Add(time.Minute), Close: 110},
		{Time: base.Add(2 * time.Minute), Close: 100},
		{Time: base.Add(3 * time.Minute), Close: 110},
		{Time: base.Add(4 * time.Minute), Close: 100},
	}}
	result := checkVolatility(context.Background(), md, "BTC", 0.02, nil)
	if result.Passed {
		t.Fatalf("expected rejection: measured volatility %.4f should exceed limit 0.02", result.Measured)
	}
}

func TestCheckVolatility_RejectsInsufficientData(t *testing.T) {
	md := &fakeMarketData{recent: []storage.Candle{{Close: 100}}}
	result := checkVolatility(context.Background(), md, "BTC", 0.5, nil)
	if result.Passed {
		t.Fatal("expected rejection with fewer than 2 candles (fail-safe)")
	}
}

func TestCheckVolatility_RejectsOnLookupError(t *testing.T) {
	md := &fakeMarketData{recentErr: errors.New("connection refused")}
	result := checkVolatility(context.Background(), md, "BTC", 0.5, nil)
	if result.Passed {
		t.Fatal("expected rejection when the market data lookup itself fails")
	}
	if result.Limit != 0.5 {
		t.Errorf("Limit = %v, want 0.5 (the configured limit, not the zero value)", result.Limit)
	}
	if !strings.Contains(result.Detail, "market data lookup failed") {
		t.Errorf("Detail = %q, want it to distinguish a lookup error from insufficient data", result.Detail)
	}
}

func TestCheckLiquidity_RejectsLowVolume(t *testing.T) {
	base := time.Now().UTC().Add(-2 * time.Minute)
	md := &fakeMarketData{recent: []storage.Candle{
		{Time: base, Close: 100, Volume: 1},
		{Time: base.Add(time.Minute), Close: 100, Volume: 1},
	}}
	result := checkLiquidity(context.Background(), md, "BTC", 1000000, nil)
	if result.Passed {
		t.Fatalf("expected rejection: measured liquidity %.2f should be under limit 1000000", result.Measured)
	}
}

func TestCheckLiquidity_RejectsOnLookupError(t *testing.T) {
	md := &fakeMarketData{recentErr: errors.New("connection refused")}
	result := checkLiquidity(context.Background(), md, "BTC", 100000, nil)
	if result.Passed {
		t.Fatal("expected rejection when the market data lookup itself fails")
	}
	if result.Limit != 100000 {
		t.Errorf("Limit = %v, want 100000 (the configured limit, not the zero value)", result.Limit)
	}
	if !strings.Contains(result.Detail, "market data lookup failed") {
		t.Errorf("Detail = %q, want it to distinguish a lookup error from insufficient data", result.Detail)
	}
}

func TestCheckLiquidity_AllowsHighVolume(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Minute).Add(-60 * time.Minute)
	candles := make([]storage.Candle, 60)
	for i := range candles {
		candles[i] = storage.Candle{Time: base.Add(time.Duration(i) * time.Minute), Close: 100, Volume: 10000}
	}
	md := &fakeMarketData{recent: candles}
	result := checkLiquidity(context.Background(), md, "BTC", 100000, nil)
	if !result.Passed {
		t.Fatalf("expected approval: measured liquidity %.2f should meet limit 100000", result.Measured)
	}
}

func TestCheckVolatility_RejectsCandleGap(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Minute).Add(-61 * time.Minute)
	candles := make([]storage.Candle, 60)
	for i := range candles {
		candles[i] = storage.Candle{Time: base.Add(time.Duration(i) * time.Minute), Close: 100}
	}
	candles[30].Time = candles[29].Time.Add(2 * time.Minute)
	result := checkVolatility(context.Background(), &fakeMarketData{recent: candles}, "BTC", 0.02, nil)
	if result.Passed || !strings.Contains(result.Detail, "consecutive") {
		t.Fatalf("result = %+v, want rejected gap", result)
	}
}

func TestAssessEligibilityRejectsIncompleteEvidence(t *testing.T) {
	result := AssessEligibility("NEW", storage.EligibilityMetrics{}, time.Now().UTC())
	if result.Eligible || len(result.Reasons) != 4 {
		t.Fatalf("result = %+v, want all missing-evidence gates rejected", result)
	}
}

func TestAssessEligibilityAllowsCompleteEvidence(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	candles := make([]storage.Candle, 60)
	for i := range candles {
		candles[i] = storage.Candle{Time: now.Add(time.Duration(i-60) * time.Minute), Close: 100}
	}
	result := AssessEligibility("BTC", storage.EligibilityMetrics{
		HistoryStartedAt:       now.Add(-eligibilityHistory),
		ThirtyDayClosedCandles: int(float64(eligibilityWindow/time.Minute) * minimumCoverage),
		ActiveExchangeCount:    minimumActiveExchanges,
		RecentCandles:          candles,
	}, now)
	if !result.Eligible {
		t.Fatalf("result = %+v, want eligible", result)
	}
}
