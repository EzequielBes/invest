package storage

import (
	"context"

	"market-data/internal/exchange"
)

func (s *Store) InsertLiquidations(ctx context.Context, ex string, liqs []exchange.Liquidation) error {
	for _, l := range liqs {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO liquidations (exchange, symbol, ts, side, price, quantity)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (exchange, symbol, ts, side, price, quantity) DO NOTHING
		`, ex, l.Symbol, l.Time, string(l.Side), l.Price, l.Quantity)
		if err != nil {
			return err
		}
	}
	return nil
}
