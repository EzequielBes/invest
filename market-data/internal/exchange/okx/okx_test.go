package okx

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
	if len(candles) != 2 || candles[0].Open != 63028 {
		t.Errorf("candles = %+v", candles)
	}
}

func TestFetchFunding_ParsesRealFixture(t *testing.T) {
	c, srv := testCollector(t, "testdata/funding.json")
	defer srv.Close()

	rates, err := c.FetchFunding(context.Background(), "BTC", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("FetchFunding: %v", err)
	}
	if len(rates) != 2 || rates[0].Rate != 0.0000647375951599 {
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
	if len(points) != 1 || points[0].Value != 3384273.06000001729 {
		t.Errorf("points = %+v", points)
	}
}

func TestFetchLiquidations_ParsesRealFixture(t *testing.T) {
	c, srv := testCollector(t, "testdata/liquidations.json")
	defer srv.Close()

	liqs, err := c.FetchLiquidations(context.Background(), "BTC")
	if err != nil {
		t.Fatalf("FetchLiquidations: %v", err)
	}
	if len(liqs) != 2 {
		t.Fatalf("len(liqs) = %d, want 2", len(liqs))
	}
	if liqs[0].Side != exchange.LiquidationBuy || liqs[0].Price != 63064.9 {
		t.Errorf("liqs[0] = %+v", liqs[0])
	}
}
