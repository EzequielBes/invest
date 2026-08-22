// risk-engine/internal/storagetest/storagetest.go
// Package storagetest provides fixture-seeding helpers for tests in other
// packages of this module (internal/risk's evaluate_test.go end-to-end
// scenario). It is deliberately independent of storage.Store — it opens its
// own connection rather than reaching into Store's private pool — so this
// package, and the write access to market-data's candles table it needs for
// test fixtures, never compiles into anything that imports storage.Store for
// production use.
package storagetest

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Seeder struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Seeder, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Seeder{pool: pool}, nil
}

func (s *Seeder) Close() {
	s.pool.Close()
}

func (s *Seeder) InsertCandle(ctx context.Context, exchange, symbol string, ts time.Time, open, high, low, close, volume float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
		VALUES ($1, $2, '1m', $3, $4, $5, $6, $7, $8)
		ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
		SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
		    close = EXCLUDED.close, volume = EXCLUDED.volume
	`, exchange, symbol, ts, open, high, low, close, volume)
	return err
}

func (s *Seeder) DeleteCandles(ctx context.Context, exchange, symbol string) {
	s.pool.Exec(ctx, `DELETE FROM candles WHERE exchange = $1 AND symbol = $2`, exchange, symbol)
}

// InsertPaperFill seeds a closed sell fill (with cost_basis/realized_pnl)
// against execution's paper_fills table, for tests of Kelly sizing that
// need closed-trade history for an asset.
func (s *Seeder) InsertPaperFill(ctx context.Context, asset string, quantity, price, costBasis, realizedPnL float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO paper_fills (id, asset, side, quantity, price, cost_basis, realized_pnl, created_at)
		VALUES (gen_random_uuid()::text, $1, 'sell', $2, $3, $4, $5, now())
	`, asset, quantity, price, costBasis, realizedPnL)
	return err
}

func (s *Seeder) DeletePaperFills(ctx context.Context, asset string) {
	s.pool.Exec(ctx, `DELETE FROM paper_fills WHERE asset = $1`, asset)
}

// CountApprovedDecisions counts risk_decisions rows recorded as approved for
// asset. This is a narrow, purpose-built method for one test assertion —
// not a general ad-hoc query escape hatch.
func (s *Seeder) CountApprovedDecisions(ctx context.Context, asset string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM risk_decisions WHERE asset = $1 AND allowed = true`, asset).Scan(&count)
	return count, err
}

// CountDecisions counts risk_decisions rows recorded for asset with the
// given allowed value. Used by rejection-path tests to confirm an audit row
// was actually written, not just that Evaluate returned a rejection.
func (s *Seeder) CountDecisions(ctx context.Context, asset string, allowed bool) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM risk_decisions WHERE asset = $1 AND allowed = $2`, asset, allowed).Scan(&count)
	return count, err
}
