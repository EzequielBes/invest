// analysis/internal/storage/marketdata.go
package storage

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// Candle is the OHLCV shape read from market-data's candles table.
type Candle struct {
	Time   time.Time
	Close  float64
	Volume float64
}

// QualitySnapshot is the exact set of market-data measurements used by the
// opportunity ranking. It mirrors risk-engine's 1m, 60-candle quality
// window, while keeping analysis independent of risk-engine internals.
type QualitySnapshot struct {
	DataAgeMinutes float64 `json:"data_age_minutes"`
	Liquidity      float64 `json:"liquidity"`
	Volatility     float64 `json:"volatility"`
}

// QualityForRanking reads the same 1m candle window used by the risk-engine
// quality rules. found is false unless enough data exists to measure both
// liquidity and volatility.
func (s *Store) QualityForRanking(ctx context.Context, exchange, symbol string) (snapshot QualitySnapshot, found bool, err error) {
	candles, err := s.RecentCandles(ctx, exchange, symbol, "1m", 60)
	if err != nil {
		return QualitySnapshot{}, false, err
	}
	if len(candles) < 2 {
		return QualitySnapshot{}, false, nil
	}
	var liquidity float64
	for _, candle := range candles {
		liquidity += candle.Volume * candle.Close
	}
	if liquidity <= 0 {
		return QualitySnapshot{}, false, nil
	}
	returns := make([]float64, 0, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		if candles[i-1].Close == 0 {
			continue
		}
		returns = append(returns, (candles[i].Close-candles[i-1].Close)/candles[i-1].Close)
	}
	if len(returns) == 0 {
		return QualitySnapshot{}, false, nil
	}
	var mean float64
	for _, value := range returns {
		mean += value
	}
	mean /= float64(len(returns))
	var variance float64
	for _, value := range returns {
		variance += (value - mean) * (value - mean)
	}
	variance /= float64(len(returns))
	age := time.Since(candles[len(candles)-1].Time).Minutes()
	if age < 0 {
		age = 0
	}
	return QualitySnapshot{DataAgeMinutes: age, Liquidity: liquidity, Volatility: math.Sqrt(variance)}, true, nil
}

// RecentCandles returns the last n closed candles for exchange/symbol/
// timeframe, oldest first.
func (s *Store) RecentCandles(ctx context.Context, exchange, symbol, timeframe string, n int) ([]Candle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, close, volume FROM (
			SELECT ts, close, volume FROM candles
			WHERE exchange = $1 AND symbol = $2 AND timeframe = $3
			ORDER BY ts DESC LIMIT $4
		) sub ORDER BY ts ASC
	`, exchange, symbol, timeframe, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candles []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.Time, &c.Close, &c.Volume); err != nil {
			return nil, err
		}
		candles = append(candles, c)
	}
	return candles, rows.Err()
}

// LatestFundingRate returns the most recent funding rate for exchange/
// symbol. found is false if none has been collected yet.
func (s *Store) LatestFundingRate(ctx context.Context, exchange, symbol string) (rate float64, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT rate FROM funding_rates WHERE exchange = $1 AND symbol = $2 ORDER BY ts DESC LIMIT 1
	`, exchange, symbol).Scan(&rate)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return rate, true, nil
}

// OpenInterestNear returns the open_interest value at or before at, for
// exchange/symbol. found is false if no such row exists.
func (s *Store) OpenInterestNear(ctx context.Context, exchange, symbol string, at time.Time) (value float64, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT value FROM open_interest
		WHERE exchange = $1 AND symbol = $2 AND ts <= $3
		ORDER BY ts DESC LIMIT 1
	`, exchange, symbol, at).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

// RecentLiquidations returns liquidations for exchange/symbol at or after
// since.
func (s *Store) RecentLiquidations(ctx context.Context, exchange, symbol string, since time.Time) ([]struct {
	Price    float64
	Quantity float64
}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT price, quantity FROM liquidations
		WHERE exchange = $1 AND symbol = $2 AND ts >= $3
	`, exchange, symbol, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []struct {
		Price    float64
		Quantity float64
	}
	for rows.Next() {
		var p, q float64
		if err := rows.Scan(&p, &q); err != nil {
			return nil, err
		}
		out = append(out, struct {
			Price    float64
			Quantity float64
		}{p, q})
	}
	return out, rows.Err()
}

// NewsItem is the shape read from market-data's news_items table.
type NewsItem struct {
	Title       string
	Body        string
	URL         string
	PublishedAt time.Time
}

// RecentNews returns news items published at or after since, across all
// sources — callers filter by asset in-memory.
func (s *Store) RecentNews(ctx context.Context, since time.Time) ([]NewsItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT title, body, url, published_at FROM news_items WHERE published_at >= $1
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []NewsItem
	for rows.Next() {
		var it NewsItem
		if err := rows.Scan(&it.Title, &it.Body, &it.URL, &it.PublishedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
