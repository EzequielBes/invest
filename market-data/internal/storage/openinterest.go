package storage

import (
	"context"

	"market-data/internal/exchange"
)

func (s *Store) InsertOpenInterest(ctx context.Context, ex, symbol string, points []exchange.OpenInterest) error {
	for _, p := range points {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO open_interest (exchange, symbol, ts, value)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (exchange, symbol, ts) DO UPDATE SET value = EXCLUDED.value
		`, ex, symbol, p.Time, p.Value)
		if err != nil {
			return err
		}
	}
	return nil
}
