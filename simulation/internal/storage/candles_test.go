// simulation/internal/storage/candles_test.go
package storage

import (
	"context"
	"os"
	"testing"
	"time"
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

func TestTimeframeDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"1m": time.Minute, "5m": 5 * time.Minute, "15m": 15 * time.Minute,
		"1h": time.Hour, "4h": 4 * time.Hour, "1d": 24 * time.Hour,
	}
	for tf, want := range cases {
		got, err := TimeframeDuration(tf)
		if err != nil {
			t.Errorf("TimeframeDuration(%q): %v", tf, err)
		}
		if got != want {
			t.Errorf("TimeframeDuration(%q) = %v, want %v", tf, got, want)
		}
	}
}

func TestTimeframeDuration_RejectsUnknown(t *testing.T) {
	if _, err := TimeframeDuration("3m"); err == nil {
		t.Fatal("expected an error for an uncollected timeframe")
	}
}

func seedCandles(t *testing.T, s *Store, exchange, symbol, timeframe string, candles []Candle) {
	t.Helper()
	ctx := context.Background()
	for _, c := range candles {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
			SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
			    close = EXCLUDED.close, volume = EXCLUDED.volume
		`, exchange, symbol, timeframe, c.Time, c.Open, c.High, c.Low, c.Close, c.Volume)
		if err != nil {
			t.Fatalf("seedCandles insert: %v", err)
		}
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM candles WHERE exchange = $1 AND symbol = $2 AND timeframe = $3`, exchange, symbol, timeframe)
	})
}

func TestRecentCandles_ExcludesNotYetClosedAndFutureCandles(t *testing.T) {
	s := testStore(t)
	base := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	seedCandles(t, s, "test-exchange", "SIMCOIN", "1h", []Candle{
		{Time: base, Close: 100},
		{Time: base.Add(time.Hour), Close: 101},
		{Time: base.Add(2 * time.Hour), Close: 102}, // not yet closed at asOf below
		{Time: base.Add(3 * time.Hour), Close: 999}, // future
	})

	asOf := base.Add(2 * time.Hour) // the [base+1h, base+2h) candle just closed
	candles, err := s.RecentCandles(context.Background(), "test-exchange", "SIMCOIN", "1h", 10, asOf)
	if err != nil {
		t.Fatalf("RecentCandles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len(candles) = %d, want 2, got %+v", len(candles), candles)
	}
	if candles[len(candles)-1].Close != 101 {
		t.Errorf("most recent visible close = %v, want 101 (the 102 and 999 candles haven't closed yet at asOf)", candles[len(candles)-1].Close)
	}
}
