package storage

import (
	"context"
	"testing"
	"time"
)

func seedCandles(t *testing.T, s *Store, exchange, symbol string, candles []Candle) {
	t.Helper()
	ctx := context.Background()
	for _, c := range candles {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
			VALUES ($1, $2, '1m', $3, $4, $5, $6, $7, $8)
			ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
			SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
			    close = EXCLUDED.close, volume = EXCLUDED.volume
		`, exchange, symbol, c.Time, c.Open, c.High, c.Low, c.Close, c.Volume)
		if err != nil {
			t.Fatalf("seedCandles insert: %v", err)
		}
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM candles WHERE exchange = $1 AND symbol = $2`, exchange, symbol)
	})
}

func TestLatestCandle(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	seedCandles(t, s, "test-exchange", "TESTCOIN", []Candle{
		{Time: now.Add(-2 * time.Minute), Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 10},
		{Time: now.Add(-1 * time.Minute), Open: 100.5, High: 102, Low: 100, Close: 101, Volume: 12},
	})

	c, found, err := s.LatestCandle(context.Background(), "test-exchange", "TESTCOIN")
	if err != nil {
		t.Fatalf("LatestCandle: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if c.Close != 101 {
		t.Errorf("Close = %v, want 101 (the most recent candle)", c.Close)
	}
}

func TestLatestCandle_NotFound(t *testing.T) {
	s := testStore(t)
	_, found, err := s.LatestCandle(context.Background(), "test-exchange", "NOSUCHASSET")
	if err != nil {
		t.Fatalf("LatestCandle: %v", err)
	}
	if found {
		t.Fatal("expected found=false for an asset with no candles")
	}
}

func TestRecentCandles_OldestFirst(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	seedCandles(t, s, "test-exchange", "TESTCOIN2", []Candle{
		{Time: now.Add(-3 * time.Minute), Close: 100, Volume: 1},
		{Time: now.Add(-2 * time.Minute), Close: 101, Volume: 2},
		{Time: now.Add(-1 * time.Minute), Close: 102, Volume: 3},
	})

	candles, err := s.RecentCandles(context.Background(), "test-exchange", "TESTCOIN2", 10)
	if err != nil {
		t.Fatalf("RecentCandles: %v", err)
	}
	if len(candles) != 3 {
		t.Fatalf("len(candles) = %d, want 3", len(candles))
	}
	if candles[0].Close != 100 || candles[2].Close != 102 {
		t.Errorf("candles not oldest-first: %+v", candles)
	}
}
