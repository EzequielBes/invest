package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"market-data/internal/exchange"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping storage tests")
	}
	s, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertAndLatestCandleTime(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	ts := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	candles := []exchange.Candle{
		{Symbol: "BTC", Timeframe: exchange.Timeframe1h, Time: ts, Open: 63000, High: 63100, Low: 62900, Close: 63050, Volume: 100},
	}
	if err := s.InsertCandles(ctx, "test-exchange", "BTC", candles); err != nil {
		t.Fatalf("InsertCandles: %v", err)
	}
	// Insert again to confirm the PK conflict is handled idempotently.
	if err := s.InsertCandles(ctx, "test-exchange", "BTC", candles); err != nil {
		t.Fatalf("InsertCandles (repeat): %v", err)
	}

	latest, ok, err := s.LatestCandleTime(ctx, "test-exchange", "BTC", exchange.Timeframe1h)
	if err != nil {
		t.Fatalf("LatestCandleTime: %v", err)
	}
	if !ok {
		t.Fatal("LatestCandleTime: expected ok=true")
	}
	if !latest.Equal(ts) {
		t.Errorf("latest = %v, want %v", latest, ts)
	}
}

func TestEarliestCandleTime(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	early := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	candles := []exchange.Candle{
		{Symbol: "ETH", Timeframe: exchange.Timeframe1d, Time: late, Open: 1, High: 1, Low: 1, Close: 1, Volume: 1},
		{Symbol: "ETH", Timeframe: exchange.Timeframe1d, Time: early, Open: 1, High: 1, Low: 1, Close: 1, Volume: 1},
	}
	if err := s.InsertCandles(ctx, "test-exchange", "ETH", candles); err != nil {
		t.Fatalf("InsertCandles: %v", err)
	}

	earliest, ok, err := s.EarliestCandleTime(ctx, "test-exchange", "ETH", exchange.Timeframe1d)
	if err != nil {
		t.Fatalf("EarliestCandleTime: %v", err)
	}
	if !ok {
		t.Fatal("EarliestCandleTime: expected ok=true")
	}
	if !earliest.Equal(early) {
		t.Errorf("earliest = %v, want %v", earliest, early)
	}
}

func TestEarliestCandleTime_NoData(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, ok, err := s.EarliestCandleTime(ctx, "test-exchange", "NOPE", exchange.Timeframe1d)
	if err != nil {
		t.Fatalf("EarliestCandleTime: %v", err)
	}
	if ok {
		t.Error("EarliestCandleTime: expected ok=false for symbol with no candles")
	}
}

func TestStartAndFinishRun(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	runID, err := s.StartRun(ctx, "test-collector", "BTC")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if runID == 0 {
		t.Fatal("StartRun: expected non-zero runID")
	}
	if err := s.FinishRun(ctx, runID, "success", nil); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
}

func TestInsertFundingAndOpenInterest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ts := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)

	err := s.InsertFunding(ctx, "test-exchange", "BTC", []exchange.FundingRate{{Symbol: "BTC", Time: ts, Rate: 0.0001}})
	if err != nil {
		t.Fatalf("InsertFunding: %v", err)
	}
	err = s.InsertOpenInterest(ctx, "test-exchange", "BTC", []exchange.OpenInterest{{Symbol: "BTC", Time: ts, Value: 12345.6}})
	if err != nil {
		t.Fatalf("InsertOpenInterest: %v", err)
	}
}

func TestInsertLiquidations(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ts := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)

	liqs := []exchange.Liquidation{
		{Symbol: "BTC", Time: ts, Side: exchange.LiquidationSell, Price: 63000, Quantity: 0.5},
	}
	err := s.InsertLiquidations(ctx, "test-exchange", liqs)
	if err != nil {
		t.Fatalf("InsertLiquidations: %v", err)
	}

	// Inserting the exact same liquidation again must be a silent no-op
	// (ON CONFLICT DO NOTHING against the natural-key unique constraint from
	// migration 002), not an error — this is what lets StreamLiquidations'
	// per-process-restart re-ingestion of trailing fills be deduped at the
	// DB layer instead of only via the in-memory seen map.
	if err := s.InsertLiquidations(ctx, "test-exchange", liqs); err != nil {
		t.Fatalf("InsertLiquidations (duplicate): %v", err)
	}
}

func TestInsertNewsItem_DedupesByURL(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	url := fmt.Sprintf("https://example.com/test-article-dedup-%d", time.Now().UnixNano())

	inserted, err := s.InsertNewsItem(ctx, "test-source", "Title", "Body", url, time.Now().UTC())
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if !inserted {
		t.Fatal("first insert: expected inserted=true")
	}

	inserted, err = s.InsertNewsItem(ctx, "test-source", "Title", "Body", url, time.Now().UTC())
	if err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}
	if inserted {
		t.Fatal("duplicate insert: expected inserted=false")
	}
}
