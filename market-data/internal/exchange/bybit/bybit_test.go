package bybit

import (
	"context"
	"encoding/json"
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
	if candles[0].Open != 63027.8 {
		t.Errorf("candles[0].Open = %v", candles[0].Open)
	}
}

func TestFetchFunding_ParsesRealFixture(t *testing.T) {
	c, srv := testCollector(t, "testdata/funding.json")
	defer srv.Close()

	rates, err := c.FetchFunding(context.Background(), "BTC", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchFunding: %v", err)
	}
	if len(rates) != 2 || rates[0].Rate != 0.00004403 {
		t.Errorf("rates = %+v", rates)
	}
}

func TestFetchOpenInterest_ParsesRealFixture(t *testing.T) {
	c, srv := testCollector(t, "testdata/open_interest.json")
	defer srv.Close()

	points, err := c.FetchOpenInterest(context.Background(), "BTC", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchOpenInterest: %v", err)
	}
	if len(points) != 2 || points[0].Value != 64992.054 {
		t.Errorf("points = %+v", points)
	}
}

// TestBybitLiquidationSide locks in the doc-confirmed mapping from Bybit's
// allLiquidation "S" field (the position side liquidated) to our own
// LiquidationSide (the side of the forced order that closed it):
// https://bybit-exchange.github.io/docs/v5/websocket/public/all-liquidation
// says "When you receive a Buy update, this means that a long position has
// been liquidated" — a long is closed by a forced sell, and vice versa.
func TestBybitLiquidationSide(t *testing.T) {
	cases := []struct {
		raw  string
		want exchange.LiquidationSide
	}{
		{"Buy", exchange.LiquidationSell}, // long liquidated, closed via forced sell
		{"Sell", exchange.LiquidationBuy}, // short liquidated, closed via forced buy
	}
	for _, tc := range cases {
		if got := bybitLiquidationSide(tc.raw); got != tc.want {
			t.Errorf("bybitLiquidationSide(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestStreamLiquidations_ParsesSyntheticMessage feeds a synthetic
// allLiquidation.BTCUSDT WS frame (shaped like Bybit's documented payload)
// through the same envelope/wsLiquidation unmarshalling StreamLiquidations
// uses, and confirms the resulting side mapping — since no live liquidation
// arrived during Task 10's WS verification window.
func TestStreamLiquidations_ParsesSyntheticMessage(t *testing.T) {
	raw := []byte(`{"topic":"allLiquidation.BTCUSDT","type":"snapshot","data":[{"T":1673251091822,"s":"BTCUSDT","S":"Buy","v":"0.003","p":"18476.34"}]}`)

	var env wsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Topic != "allLiquidation.BTCUSDT" {
		t.Fatalf("topic = %q", env.Topic)
	}

	var liqs []wsLiquidation
	if err := json.Unmarshal(env.Data, &liqs); err != nil {
		t.Fatalf("unmarshal liquidations: %v", err)
	}
	if len(liqs) != 1 {
		t.Fatalf("len(liqs) = %d, want 1", len(liqs))
	}

	l := liqs[0]
	if l.Symbol != "BTCUSDT" || l.Price != 18476.34 || l.Quantity != 0.003 {
		t.Fatalf("liq = %+v", l)
	}
	if got := bybitLiquidationSide(l.Side); got != exchange.LiquidationSell {
		t.Errorf("bybitLiquidationSide(%q) = %q, want %q (Buy = long liquidated = closed via sell)", l.Side, got, exchange.LiquidationSell)
	}

	// Reverse case: "Sell" (short liquidated) should map to LiquidationBuy.
	raw2 := []byte(`{"topic":"allLiquidation.BTCUSDT","type":"snapshot","data":[{"T":1673251091822,"s":"BTCUSDT","S":"Sell","v":"0.003","p":"18476.34"}]}`)
	var env2 wsEnvelope
	if err := json.Unmarshal(raw2, &env2); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var liqs2 []wsLiquidation
	if err := json.Unmarshal(env2.Data, &liqs2); err != nil {
		t.Fatalf("unmarshal liquidations: %v", err)
	}
	if got := bybitLiquidationSide(liqs2[0].Side); got != exchange.LiquidationBuy {
		t.Errorf("bybitLiquidationSide(%q) = %q, want %q (Sell = short liquidated = closed via buy)", liqs2[0].Side, got, exchange.LiquidationBuy)
	}
}
