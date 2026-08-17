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

	c, found, err := s.LatestCandle(context.Background(), "test-exchange", "TESTCOIN", nil)
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
	_, found, err := s.LatestCandle(context.Background(), "test-exchange", "NOSUCHASSET", nil)
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
	// Seed 5 candles but request only the most recent 3 (n=3 < 5 seeded), so
	// the LIMIT actually truncates. This distinguishes the correct
	// implementation (most recent n, oldest-first among themselves) from a
	// naive `ORDER BY ts ASC LIMIT n` which would wrongly return the OLDEST
	// n instead.
	seedCandles(t, s, "test-exchange", "TESTCOIN2", []Candle{
		{Time: now.Add(-5 * time.Minute), Close: 100, Volume: 1},
		{Time: now.Add(-4 * time.Minute), Close: 101, Volume: 2},
		{Time: now.Add(-3 * time.Minute), Close: 102, Volume: 3},
		{Time: now.Add(-2 * time.Minute), Close: 103, Volume: 4},
		{Time: now.Add(-1 * time.Minute), Close: 104, Volume: 5},
	})

	candles, err := s.RecentCandles(context.Background(), "test-exchange", "TESTCOIN2", 3, nil)
	if err != nil {
		t.Fatalf("RecentCandles: %v", err)
	}
	if len(candles) != 3 {
		t.Fatalf("len(candles) = %d, want 3", len(candles))
	}
	want := []float64{102, 103, 104}
	for i, w := range want {
		if candles[i].Close != w {
			t.Errorf("candles[%d].Close = %v, want %v (most recent 3, oldest-first): got %+v", i, candles[i].Close, w, candles)
			break
		}
	}
}

func TestLatestCandle_AsOf_IgnoresFutureCandles(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	seedCandles(t, s, "test-exchange", "TESTCOIN3", []Candle{
		{Time: now.Add(-3 * time.Minute), Close: 100, Volume: 1},
		{Time: now.Add(-2 * time.Minute), Close: 101, Volume: 1},
		{Time: now, Close: 999, Volume: 1}, // "future" relative to asOf below
	})

	asOf := now.Add(-1 * time.Minute)
	c, found, err := s.LatestCandle(context.Background(), "test-exchange", "TESTCOIN3", &asOf)
	if err != nil {
		t.Fatalf("LatestCandle: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if c.Close != 101 {
		t.Errorf("Close = %v, want 101 (the -2min candle; the -3min-cutoff excludes the -1min-cutoff's own not-yet-closed candle and the future one)", c.Close)
	}
}

func TestRecentCandles_AsOf_IgnoresFutureCandles(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	seedCandles(t, s, "test-exchange", "TESTCOIN4", []Candle{
		{Time: now.Add(-3 * time.Minute), Close: 100, Volume: 1},
		{Time: now.Add(-2 * time.Minute), Close: 101, Volume: 1},
		{Time: now.Add(-1 * time.Minute), Close: 102, Volume: 1},
		{Time: now, Close: 999, Volume: 1}, // must never be visible
	})

	asOf := now.Add(-1 * time.Minute)
	candles, err := s.RecentCandles(context.Background(), "test-exchange", "TESTCOIN4", 10, &asOf)
	if err != nil {
		t.Fatalf("RecentCandles: %v", err)
	}
	for _, c := range candles {
		if c.Close == 999 {
			t.Fatalf("RecentCandles returned a candle at or after asOf's cutoff: %+v", candles)
		}
	}
	if len(candles) != 2 {
		t.Fatalf("len(candles) = %d, want 2 (closes 100, 101 — the -1min candle's own close time equals asOf, so it's excluded too)", len(candles))
	}
}
