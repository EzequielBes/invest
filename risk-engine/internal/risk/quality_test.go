// risk-engine/internal/risk/quality_test.go
package risk

import (
	"context"
	"errors"
	"strings"
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

func TestCheckDataFreshness_RejectsOnLookupError(t *testing.T) {
	md := &fakeMarketData{latestErr: errors.New("connection refused")}
	result := checkDataFreshness(context.Background(), md, "BTC", 30)
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

func TestCheckVolatility_RejectsOnLookupError(t *testing.T) {
	md := &fakeMarketData{recentErr: errors.New("connection refused")}
	result := checkVolatility(context.Background(), md, "BTC", 0.5)
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
	result := checkLiquidity(context.Background(), md, "BTC", 1000000)
	if result.Passed {
		t.Fatalf("expected rejection: measured liquidity %.2f should be under limit 1000000", result.Measured)
	}
}

func TestCheckLiquidity_RejectsOnLookupError(t *testing.T) {
	md := &fakeMarketData{recentErr: errors.New("connection refused")}
	result := checkLiquidity(context.Background(), md, "BTC", 100000)
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
