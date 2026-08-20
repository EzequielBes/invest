package storage

import (
	"context"
	"time"
)

type RiskState struct {
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	ChangedAt time.Time `json:"changed_at"`
}

type RiskLimits struct {
	MaxPctPerAsset       float64 `json:"max_pct_per_asset"`
	MaxPctCryptoTotal    float64 `json:"max_pct_crypto_total"`
	MaxValuePerTrade     float64 `json:"max_value_per_trade"`
	MaxDailyLoss         float64 `json:"max_daily_loss"`
	MaxWeeklyLoss        float64 `json:"max_weekly_loss"`
	MaxDrawdown          float64 `json:"max_drawdown"`
	MaxConsecutiveLosses int     `json:"max_consecutive_losses"`
	MaxVolatility        float64 `json:"max_volatility"`
	MinLiquidity         float64 `json:"min_liquidity"`
	MaxDataAgeMinutes    int     `json:"max_data_age_minutes"`
}

type RiskStateResponse struct {
	State  RiskState  `json:"state"`
	Limits RiskLimits `json:"limits"`
}

// LiveRiskState reads the live row (run_id IS NULL) and configured limits.
func (s *Store) LiveRiskState(ctx context.Context) (RiskStateResponse, error) {
	var resp RiskStateResponse
	err := s.pool.QueryRow(ctx, `
		SELECT status, reason, changed_at
		FROM risk_state
		WHERE run_id IS NULL
	`).Scan(&resp.State.Status, &resp.State.Reason, &resp.State.ChangedAt)
	if err != nil {
		return RiskStateResponse{}, err
	}

	err = s.pool.QueryRow(ctx, `
		SELECT max_pct_per_asset, max_pct_crypto_total, max_value_per_trade,
		       max_daily_loss, max_weekly_loss, max_drawdown, max_consecutive_losses,
		       max_volatility, min_liquidity, max_data_age_minutes
		FROM risk_limits
		WHERE id = 1
	`).Scan(&resp.Limits.MaxPctPerAsset, &resp.Limits.MaxPctCryptoTotal, &resp.Limits.MaxValuePerTrade,
		&resp.Limits.MaxDailyLoss, &resp.Limits.MaxWeeklyLoss, &resp.Limits.MaxDrawdown, &resp.Limits.MaxConsecutiveLosses,
		&resp.Limits.MaxVolatility, &resp.Limits.MinLiquidity, &resp.Limits.MaxDataAgeMinutes)
	if err != nil {
		return RiskStateResponse{}, err
	}
	return resp, nil
}
