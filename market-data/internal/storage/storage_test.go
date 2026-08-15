package storage

import (
	"context"
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
