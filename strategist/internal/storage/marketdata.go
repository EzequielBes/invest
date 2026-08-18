// strategist/internal/storage/marketdata.go
package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// LatestPrice returns the most recent closed candle's close price for
// exchange/symbol/timeframe (the market-data module's candles table,
// read here directly — no Go dependency on that module). found is false
// if no candle has been collected yet.
func (s *Store) LatestPrice(ctx context.Context, exchange, symbol, timeframe string) (price float64, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT close FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = $3
		ORDER BY ts DESC LIMIT 1
	`, exchange, symbol, timeframe).Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return price, true, nil
}
