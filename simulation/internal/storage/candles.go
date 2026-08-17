// simulation/internal/storage/candles.go
package storage

import (
	"context"
	"fmt"
	"time"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// TimeframeDuration returns the fixed wall-clock duration of one candle in
// tf, for the timeframes market-data (sub-project 1) collects.
func TimeframeDuration(tf string) (time.Duration, error) {
	switch tf {
	case "1m":
		return time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("storage: unknown timeframe %q", tf)
	}
}

// RecentCandles returns the last n candles for exchange/symbol/timeframe
// whose close time (open + duration) is <= asOf, oldest first — this is
// how the simulation guarantees it never sees a candle that hasn't closed
// yet at the current simulated instant.
func (s *Store) RecentCandles(ctx context.Context, exchange, symbol, timeframe string, n int, asOf time.Time) ([]Candle, error) {
	dur, err := TimeframeDuration(timeframe)
	if err != nil {
		return nil, err
	}
	cutoff := asOf.Add(-dur)
	rows, err := s.pool.Query(ctx, `
		SELECT ts, open, high, low, close, volume FROM (
			SELECT ts, open, high, low, close, volume FROM candles
			WHERE exchange = $1 AND symbol = $2 AND timeframe = $3 AND ts <= $4
			ORDER BY ts DESC LIMIT $5
		) sub ORDER BY ts ASC
	`, exchange, symbol, timeframe, cutoff, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candles []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.Time, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume); err != nil {
			return nil, err
		}
		candles = append(candles, c)
	}
	return candles, rows.Err()
}

// InsertCandleForTest inserts or updates one candle row directly — a thin
// seeding seam for this module's own tests (marketview, engine) that need
// fixture data. Not used by any production code path.
func (s *Store) InsertCandleForTest(ctx context.Context, exchange, symbol, timeframe string, ts time.Time, open, high, low, close, volume float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
		SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
		    close = EXCLUDED.close, volume = EXCLUDED.volume
	`, exchange, symbol, timeframe, ts, open, high, low, close, volume)
	return err
}

// DeleteCandlesForTest removes every candle for exchange/symbol/timeframe —
// paired with InsertCandleForTest for fixture cleanup in this module's tests.
func (s *Store) DeleteCandlesForTest(ctx context.Context, exchange, symbol, timeframe string) {
	s.pool.Exec(ctx, `DELETE FROM candles WHERE exchange = $1 AND symbol = $2 AND timeframe = $3`, exchange, symbol, timeframe)
}
