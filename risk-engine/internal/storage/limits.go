// risk-engine/internal/storage/limits.go
package storage

import "context"

type Limits struct {
	MaxPctPerAsset       float64
	MaxPctCryptoTotal    float64
	MaxValuePerTrade     float64
	MaxDailyLoss         float64
	MaxWeeklyLoss        float64
	MaxDrawdown          float64
	MaxConsecutiveLosses int
	MaxVolatility        float64
	MinLiquidity         float64
	MaxDataAgeMinutes    int
}

func (s *Store) GetLimits(ctx context.Context) (Limits, error) {
	var l Limits
	err := s.pool.QueryRow(ctx, `
		SELECT max_pct_per_asset, max_pct_crypto_total, max_value_per_trade,
		       max_daily_loss, max_weekly_loss, max_drawdown, max_consecutive_losses,
		       max_volatility, min_liquidity, max_data_age_minutes
		FROM risk_limits WHERE id = 1
	`).Scan(&l.MaxPctPerAsset, &l.MaxPctCryptoTotal, &l.MaxValuePerTrade,
		&l.MaxDailyLoss, &l.MaxWeeklyLoss, &l.MaxDrawdown, &l.MaxConsecutiveLosses,
		&l.MaxVolatility, &l.MinLiquidity, &l.MaxDataAgeMinutes)
	return l, err
}

func (s *Store) SetLimits(ctx context.Context, l Limits) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE risk_limits SET
			max_pct_per_asset = $1, max_pct_crypto_total = $2, max_value_per_trade = $3,
			max_daily_loss = $4, max_weekly_loss = $5, max_drawdown = $6, max_consecutive_losses = $7,
			max_volatility = $8, min_liquidity = $9, max_data_age_minutes = $10, updated_at = now()
		WHERE id = 1
	`, l.MaxPctPerAsset, l.MaxPctCryptoTotal, l.MaxValuePerTrade,
		l.MaxDailyLoss, l.MaxWeeklyLoss, l.MaxDrawdown, l.MaxConsecutiveLosses,
		l.MaxVolatility, l.MinLiquidity, l.MaxDataAgeMinutes)
	return err
}
