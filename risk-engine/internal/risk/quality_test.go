// risk-engine/internal/risk/quality_test.go
package risk

import (
	"context"
	"testing"
	"time"

	"risk-engine/internal/storage"
)

// fakeMarketData lets quality rule tests run without a database.
type fakeMarketData struct {
	latest      storage.Candle
	latestFound bool
	latestErr   error
	recent      []storage.Candle
	recentErr   error
}

func (f *fakeMarketData) LatestCandle(ctx context.Context, exchange, symbol string) (storage.Candle, bool, error) {
	return f.latest, f.latestFound, f.latestErr
}
func (f *fakeMarketData) RecentCandles(ctx context.Context, exchange, symbol string, n int) ([]storage.Candle, error) {
	return f.recent, f.recentErr
}

func TestCheckDataFreshness_RejectsStaleData(t *testing.T) {
	md := &fakeMarketData{
		latest:      storage.Candle{Time: time.Now().UTC().Add(-45 * time.Minute)},
		latestFound: true,
	}
	result := checkDataFreshness(context.Background(), md, "BTC", 30)
	if result.Passed {
		t.Fatal("expected rejection: candle is 45 minutes old, limit 30")
	}
}

func TestCheckDataFreshness_RejectsMissingData(t *testing.T) {
	md := &fakeMarketData{latestFound: false}
	result := checkDataFreshness(context.Background(), md, "BTC", 30)
	if result.Passed {
		t.Fatal("expected rejection when no candle data exists (fail-safe)")
	}
}

func TestCheckDataFreshness_AllowsFreshData(t *testing.T) {
	md := &fakeMarketData{
		latest:      storage.Candle{Time: time.Now().UTC().Add(-5 * time.Minute)},
		latestFound: true,
	}
	result := checkDataFreshness(context.Background(), md, "BTC", 30)
	if !result.Passed {
		t.Fatal("expected approval: candle is 5 minutes old, limit 30")
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
	result := checkVolatility(context.Background(), md, "BTC", 0.02)
	if result.Passed {
		t.Fatalf("expected rejection: measured volatility %.4f should exceed limit 0.02", result.Measured)
	}
}

func TestCheckVolatility_RejectsInsufficientData(t *testing.T) {
	md := &fakeMarketData{recent: []storage.Candle{{Close: 100}}}
	result := checkVolatility(context.Background(), md, "BTC", 0.5)
	if result.Passed {
		t.Fatal("expected rejection with fewer than 2 candles (fail-safe)")
	}
}

func TestCheckLiquidity_RejectsLowVolume(t *testing.T) {
	base := time.Now().UTC().Add(-2 * time.Minute)
	md := &fakeMarketData{recent: []storage.Candle{
		{Time: base, Close: 100, Volume: 1},
		{Time: base.Add(time.Minute), Close: 100, Volume: 1},
	}}
	result := checkLiquidity(context.Background(), md, "BTC", 1000000)
	if result.Passed {
		t.Fatalf("expected rejection: measured liquidity %.2f should be under limit 1000000", result.Measured)
	}
}

func TestCheckLiquidity_AllowsHighVolume(t *testing.T) {
	base := time.Now().UTC().Add(-2 * time.Minute)
	md := &fakeMarketData{recent: []storage.Candle{
		{Time: base, Close: 100, Volume: 10000},
		{Time: base.Add(time.Minute), Close: 100, Volume: 10000},
	}}
	result := checkLiquidity(context.Background(), md, "BTC", 100000)
	if !result.Passed {
		t.Fatalf("expected approval: measured liquidity %.2f should meet limit 100000", result.Measured)
	}
}
