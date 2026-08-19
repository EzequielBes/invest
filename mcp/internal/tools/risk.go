// mcp/internal/tools/risk.go
package tools

import (
	"context"
	"fmt"

	riskstorage "risk-engine/storage"
)

// RiskStateResult is the shape both get_risk_state and set_risk_state
// return — set_risk_state re-reads state after writing so its result is
// exactly what get_risk_state would now report.
type RiskStateResult struct {
	Status               string  `json:"status"`
	Reason               string  `json:"reason"`
	ChangedAt            string  `json:"changed_at"`
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

// GetRiskStateArgs is the get_risk_state tool's input — empty, it always
// reads the live (non-backtest) state.
type GetRiskStateArgs struct{}

// GetRiskState reads the risk-engine's live operational status and
// configured limits. Always the live row (run_id IS NULL) — this module
// never reads or writes backtest-scoped risk state.
func GetRiskState(ctx context.Context, store *riskstorage.Store) (RiskStateResult, error) {
	return readRiskState(ctx, store)
}

// SetRiskStateArgs is the set_risk_state tool's input.
type SetRiskStateArgs struct {
	Status string `json:"status" jsonschema:"one of normal, paused, kill_switch"`
	Reason string `json:"reason" jsonschema:"why the state is being changed"`
}

var validRiskStatuses = map[string]bool{
	riskstorage.StatusNormal:     true,
	riskstorage.StatusPaused:     true,
	riskstorage.StatusKillSwitch: true,
}

// SetRiskState manually sets the risk-engine's live operational status —
// always the live row (run_id IS NULL). Returns the state as re-read
// after the write, so the caller sees the confirmed result, not just an
// echo of what it asked for.
func SetRiskState(ctx context.Context, store *riskstorage.Store, args SetRiskStateArgs) (RiskStateResult, error) {
	if !validRiskStatuses[args.Status] {
		return RiskStateResult{}, fmt.Errorf("invalid status %q (valid: normal, paused, kill_switch)", args.Status)
	}
	if args.Reason == "" {
		return RiskStateResult{}, fmt.Errorf("reason is required")
	}
	if err := store.SetState(ctx, nil, args.Status, args.Reason); err != nil {
		return RiskStateResult{}, fmt.Errorf("set risk state: %w", err)
	}
	return readRiskState(ctx, store)
}

func readRiskState(ctx context.Context, store *riskstorage.Store) (RiskStateResult, error) {
	state, err := store.GetState(ctx, nil)
	if err != nil {
		return RiskStateResult{}, fmt.Errorf("get risk state: %w", err)
	}
	limits, err := store.GetLimits(ctx)
	if err != nil {
		return RiskStateResult{}, fmt.Errorf("get risk limits: %w", err)
	}
	return RiskStateResult{
		Status: state.Status, Reason: state.Reason, ChangedAt: state.ChangedAt.Format("2006-01-02T15:04:05Z07:00"),
		MaxPctPerAsset: limits.MaxPctPerAsset, MaxPctCryptoTotal: limits.MaxPctCryptoTotal,
		MaxValuePerTrade: limits.MaxValuePerTrade, MaxDailyLoss: limits.MaxDailyLoss,
		MaxWeeklyLoss: limits.MaxWeeklyLoss, MaxDrawdown: limits.MaxDrawdown,
		MaxConsecutiveLosses: limits.MaxConsecutiveLosses, MaxVolatility: limits.MaxVolatility,
		MinLiquidity: limits.MinLiquidity, MaxDataAgeMinutes: limits.MaxDataAgeMinutes,
	}, nil
}
