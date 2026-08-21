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

// EligibilityMetrics is the closed-candle evidence needed to decide whether
// an asset may enter an automated analysis cycle.
type EligibilityMetrics struct {
	HistoryStartedAt       time.Time
	ThirtyDayClosedCandles int
	ActiveExchangeCount    int
	RecentCandles          []Candle
}

func (s *Store) EligibilityMetrics(ctx context.Context, symbol string) (EligibilityMetrics, error) {
	var metrics EligibilityMetrics
	var historyStartedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT
			MIN(ts),
			COUNT(*) FILTER (WHERE ts >= date_trunc('minute', now()) - interval '30 days' AND ts < date_trunc('minute', now())),
			COUNT(DISTINCT exchange) FILTER (WHERE ts >= date_trunc('minute', now()) - interval '24 hours' AND ts < date_trunc('minute', now()))
		FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
	`, "binance", symbol).Scan(&historyStartedAt, &metrics.ThirtyDayClosedCandles, &metrics.ActiveExchangeCount)
	if err != nil {
		return EligibilityMetrics{}, err
	}
	if historyStartedAt != nil {
		metrics.HistoryStartedAt = *historyStartedAt
	}

	// The exchange count must reflect independent venues, not multiple Binance
	// rows. Fetch it separately because the reference exchange above owns the
	// historical and coverage checks.
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT exchange)
		FROM candles
		WHERE symbol = $1 AND timeframe = '1m'
		  AND ts >= date_trunc('minute', now()) - interval '24 hours'
		  AND ts < date_trunc('minute', now())
	`, symbol).Scan(&metrics.ActiveExchangeCount)
	if err != nil {
		return EligibilityMetrics{}, err
	}
	metrics.RecentCandles, err = s.RecentCandles(ctx, "binance", symbol, 60, nil)
	if err != nil {
		return EligibilityMetrics{}, err
	}
	return metrics, nil
}

// LatestCandle reads the most recent 1m candle for exchange/symbol from the
// candles table owned by the market-data-foundation sub-project — this
// module only ever reads it, never writes. asOf, if non-nil, excludes any
// candle not yet closed at that instant (ts <= asOf - 1 minute) — used by a
// backtest to prevent seeing data from its own simulated future. Live reads
// also exclude the currently open minute candle.
func (s *Store) LatestCandle(ctx context.Context, exchange, symbol string, asOf *time.Time) (Candle, bool, error) {
	var c Candle
	err := s.pool.QueryRow(ctx, `
		SELECT ts, open, high, low, close, volume FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
		  AND (CASE WHEN $3::timestamptz IS NULL THEN ts < date_trunc('minute', now()) ELSE ts <= $3::timestamptz - interval '1 minute' END)
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
			  AND (CASE WHEN $4::timestamptz IS NULL THEN ts < date_trunc('minute', now()) ELSE ts <= $4::timestamptz - interval '1 minute' END)
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
