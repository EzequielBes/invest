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

// TestPollLiquidationsOnce_DedupsAcrossCycles drives StreamLiquidations'
// polling logic directly (bypassing the real 30s ticker, per the task's
// testing note) against a fixture that returns the same two liquidations on
// every poll, and confirms each liquidation is only forwarded once even
// though pollLiquidationsOnce is invoked twice with a shared seen map — the
// same dedup behavior StreamLiquidations relies on to avoid re-emitting a
// liquidation seen on a prior poll cycle.
func TestPollLiquidationsOnce_DedupsAcrossCycles(t *testing.T) {
	c, srv := testCollector(t, "testdata/liquidations.json")
	defer srv.Close()

	out := make(chan exchange.Liquidation, 10)
	seen := map[string]bool{}
	ctx := context.Background()

	c.pollLiquidationsOnce(ctx, []string{"BTC"}, seen, out)
	c.pollLiquidationsOnce(ctx, []string{"BTC"}, seen, out)
	close(out)

	var got []exchange.Liquidation
	for l := range out {
		got = append(got, l)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (fixture's 2 liquidations forwarded once each, not duplicated across the 2 poll cycles)", len(got))
	}
	if got[0].Price != 63064.9 || got[1].Price != 63060 {
		t.Errorf("got = %+v", got)
	}
}

// TestLiquidationKey_DistinguishesFieldsAndIsStable confirms liquidationKey
// treats two liquidations differing in any of symbol/time/price/quantity as
// distinct, and produces the identical key for identical liquidations (the
// property pollLiquidationsOnce's dedup relies on).
func TestLiquidationKey_DistinguishesFieldsAndIsStable(t *testing.T) {
	base := exchange.Liquidation{Symbol: "BTC", Time: time.UnixMilli(1786810247378), Price: 63064.9, Quantity: 2.41}
	if liquidationKey(base) != liquidationKey(base) {
		t.Error("liquidationKey should be stable for identical liquidations")
	}

	variants := []exchange.Liquidation{
		{Symbol: "ETH", Time: base.Time, Price: base.Price, Quantity: base.Quantity},
		{Symbol: base.Symbol, Time: time.UnixMilli(1786810247379), Price: base.Price, Quantity: base.Quantity},
		{Symbol: base.Symbol, Time: base.Time, Price: 1, Quantity: base.Quantity},
		{Symbol: base.Symbol, Time: base.Time, Price: base.Price, Quantity: 1},
	}
	for _, v := range variants {
		if liquidationKey(v) == liquidationKey(base) {
			t.Errorf("liquidationKey(%+v) collided with base key", v)
		}
	}
}
