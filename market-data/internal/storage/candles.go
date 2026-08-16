package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"market-data/internal/exchange"
)

func (s *Store) InsertCandles(ctx context.Context, ex, symbol string, candles []exchange.Candle) error {
	batch := make([][]any, 0, len(candles))
	for _, c := range candles {
		batch = append(batch, []any{ex, symbol, string(c.Timeframe), c.Time, c.Open, c.High, c.Low, c.Close, c.Volume})
	}
	for _, row := range batch {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
			SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
			    close = EXCLUDED.close, volume = EXCLUDED.volume
		`, row...)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LatestCandleTime(ctx context.Context, ex, symbol string, tf exchange.Timeframe) (time.Time, bool, error) {
	var ts time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT ts FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = $3
		ORDER BY ts DESC LIMIT 1
	`, ex, symbol, string(tf)).Scan(&ts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return ts, true, nil
}

// EarliestCandleTime returns the timestamp of the oldest stored candle for
// exchange/symbol/timeframe, used by Backfill (Part B of C3) to detect
// whether a pair's history already reaches back close to the target depth,
// so a restart doesn't discard prior backfill progress and start over.
func (s *Store) EarliestCandleTime(ctx context.Context, ex, symbol string, tf exchange.Timeframe) (time.Time, bool, error) {
	var ts time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT ts FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = $3
		ORDER BY ts ASC LIMIT 1
	`, ex, symbol, string(tf)).Scan(&ts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return ts, true, nil
}
