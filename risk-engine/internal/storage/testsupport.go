// risk-engine/internal/storage/testsupport.go
package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestOnlyInsertCandle and TestOnlyDeleteCandles exist so tests in other
// packages of this module (internal/risk) can seed and clean up fixture
// candle rows without duplicating raw SQL or exporting the full query
// surface. Not used by production code.
func TestOnlyInsertCandle(ctx context.Context, s *Store, exchange, symbol string, ts time.Time, open, high, low, close, volume float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
		VALUES ($1, $2, '1m', $3, $4, $5, $6, $7, $8)
		ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
		SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
		    close = EXCLUDED.close, volume = EXCLUDED.volume
	`, exchange, symbol, ts, open, high, low, close, volume)
	return err
}

func TestOnlyDeleteCandles(ctx context.Context, s *Store, exchange, symbol string) {
	s.pool.Exec(ctx, `DELETE FROM candles WHERE exchange = $1 AND symbol = $2`, exchange, symbol)
}

// QueryRowTestHelper exposes the pool's QueryRow for test-only ad-hoc
// assertions in other packages, avoiding a bespoke method for every
// one-off query a test needs.
func (s *Store) QueryRowTestHelper(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.pool.QueryRow(ctx, sql, args...)
}
