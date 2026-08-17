package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// LatestCandle reads the most recent 1m candle for exchange/symbol from the
// candles table owned by the market-data-foundation sub-project — this
// module only ever reads it, never writes. asOf, if non-nil, excludes any
// candle not yet closed at that instant (ts <= asOf - 1 minute) — used by a
// backtest to prevent seeing data from its own simulated future. nil means
// no cutoff (today's live behavior).
func (s *Store) LatestCandle(ctx context.Context, exchange, symbol string, asOf *time.Time) (Candle, bool, error) {
	var c Candle
	err := s.pool.QueryRow(ctx, `
		SELECT ts, open, high, low, close, volume FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
		  AND ($3::timestamptz IS NULL OR ts <= $3::timestamptz - interval '1 minute')
		ORDER BY ts DESC LIMIT 1
	`, exchange, symbol, asOf).Scan(&c.Time, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Candle{}, false, nil
		}
		return Candle{}, false, err
	}
	return c, true, nil
}

// RecentCandles returns the last n 1m candles for exchange/symbol, oldest
// first, used to compute recent volatility and liquidity. See LatestCandle
// for asOf's semantics.
func (s *Store) RecentCandles(ctx context.Context, exchange, symbol string, n int, asOf *time.Time) ([]Candle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, open, high, low, close, volume FROM (
			SELECT ts, open, high, low, close, volume FROM candles
			WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
			  AND ($4::timestamptz IS NULL OR ts <= $4::timestamptz - interval '1 minute')
			ORDER BY ts DESC LIMIT $3
		) sub ORDER BY ts ASC
	`, exchange, symbol, n, asOf)
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
