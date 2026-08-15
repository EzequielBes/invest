package binance

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

func testCollector(t *testing.T, fixture string) (*Collector, *httptest.Server) {
	t.Helper()
	body, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	c := New(httpclient.New(100, 10))
	c.baseURL = srv.URL
	return c, srv
}

func TestFetchCandles_ParsesRealFixture(t *testing.T) {
	c, srv := testCollector(t, "testdata/candles.json")
	defer srv.Close()

	candles, err := c.FetchCandles(context.Background(), "BTC", exchange.Timeframe1h, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchCandles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len(candles) = %d, want 2", len(candles))
	}
	first := candles[0]
	if first.Open != 63043.40 || first.Close != 63026.30 {
		t.Errorf("first candle = %+v", first)
	}
	wantTime := time.UnixMilli(1786816800000).UTC()
	if !first.Time.UTC().Equal(wantTime) {
		t.Errorf("first.Time = %v, want %v", first.Time, wantTime)
	}
}

func TestFetchFunding_ParsesRealFixture(t *testing.T) {
	c, srv := testCollector(t, "testdata/funding.json")
	defer srv.Close()

	rates, err := c.FetchFunding(context.Background(), "BTC", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchFunding: %v", err)
	}
	if len(rates) != 2 {
		t.Fatalf("len(rates) = %d, want 2", len(rates))
	}
	if rates[0].Rate != 0.00006523 {
		t.Errorf("rates[0].Rate = %v", rates[0].Rate)
	}
}

func TestFetchOpenInterest_ParsesRealFixture(t *testing.T) {
	c, srv := testCollector(t, "testdata/open_interest.json")
	defer srv.Close()

	points, err := c.FetchOpenInterest(context.Background(), "BTC", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchOpenInterest: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("len(points) = %d, want 2", len(points))
	}
	if points[0].Value != 111856.664 {
		t.Errorf("points[0].Value = %v", points[0].Value)
	}
}

func TestStreamCandles_PollsRESTAndEmitsCandles(t *testing.T) {
	c, srv := testCollector(t, "testdata/candles.json")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch, err := c.StreamCandles(ctx, []string{"BTC"}, exchange.Timeframe1h)
	if err != nil {
		t.Fatalf("StreamCandles: %v", err)
	}

	select {
	case candle, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before any candle arrived")
		}
		if candle.Symbol != "BTC" || candle.Open != 63043.40 {
			t.Errorf("candle = %+v", candle)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first candle from poller")
	}

	cancel()

	// Drain until the channel closes to confirm ctx cancellation stops the
	// goroutine and closes the channel, rather than leaking it.
	closed := false
	deadline := time.After(2 * time.Second)
	for !closed {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for channel to close after ctx cancellation")
		}
	}
}

func TestStreamCandles_UnsupportedTimeframe(t *testing.T) {
	c, srv := testCollector(t, "testdata/candles.json")
	defer srv.Close()

	_, err := c.StreamCandles(context.Background(), []string{"BTC"}, exchange.Timeframe("5m"))
	if err == nil {
		t.Fatal("expected error for unsupported timeframe, got nil")
	}
}
