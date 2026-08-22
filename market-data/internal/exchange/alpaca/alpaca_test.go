package alpaca

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"market-data/internal/exchange"
	"market-data/internal/httpclient"
)

func serveFixture(t *testing.T, path string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
}

func TestFetchCandles_ParsesBarsFixture(t *testing.T) {
	srv := serveFixture(t, "testdata/bars.json")
	defer srv.Close()

	c := New(httpclient.New(100, 10), "key", "secret")
	c.baseURL = srv.URL

	candles, err := c.FetchCandles(context.Background(), "AAPL", exchange.Timeframe1m, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchCandles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len(candles) = %d, want 2", len(candles))
	}
	if candles[0].Close != 231.0 {
		t.Errorf("candles[0].Close = %v, want 231.0", candles[0].Close)
	}
	if candles[0].Symbol != "AAPL" {
		t.Errorf("candles[0].Symbol = %q, want AAPL", candles[0].Symbol)
	}
	wantTime := time.Date(2026, 8, 22, 14, 30, 0, 0, time.UTC)
	if !candles[0].Time.Equal(wantTime) {
		t.Errorf("candles[0].Time = %v, want %v", candles[0].Time, wantTime)
	}
}

func TestFetchFunding_ReturnsEmpty(t *testing.T) {
	c := New(httpclient.New(100, 10), "key", "secret")
	rates, err := c.FetchFunding(context.Background(), "AAPL", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchFunding: %v", err)
	}
	if len(rates) != 0 {
		t.Errorf("len(rates) = %d, want 0 (funding rate is a perpetual-futures concept, stocks don't have it)", len(rates))
	}
}

func TestFetchOpenInterest_ReturnsEmpty(t *testing.T) {
	c := New(httpclient.New(100, 10), "key", "secret")
	points, err := c.FetchOpenInterest(context.Background(), "AAPL", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchOpenInterest: %v", err)
	}
	if len(points) != 0 {
		t.Errorf("len(points) = %d, want 0 (open interest is a perpetual-futures concept, stocks don't have it)", len(points))
	}
}

func TestName(t *testing.T) {
	c := New(httpclient.New(100, 10), "key", "secret")
	if c.Name() != "alpaca" {
		t.Errorf("Name() = %q, want alpaca", c.Name())
	}
}
