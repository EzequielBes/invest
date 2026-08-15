package storage

import (
	"context"

	"market-data/internal/exchange"
)

func (s *Store) InsertFunding(ctx context.Context, ex, symbol string, rates []exchange.FundingRate) error {
	for _, r := range rates {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO funding_rates (exchange, symbol, ts, rate)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (exchange, symbol, ts) DO UPDATE SET rate = EXCLUDED.rate
		`, ex, symbol, r.Time, r.Rate)
		if err != nil {
			return err
		}
	}
	return nil
}
