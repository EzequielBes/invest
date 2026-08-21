package agents

import (
	"context"
	"fmt"
	"time"

	riskstorage "risk-engine/storage"
)

type RiskContextIndicators struct {
	Status    string             `json:"risk_status"`
	Reason    string             `json:"risk_reason"`
	ChangedAt string             `json:"risk_changed_at"`
	Limits    riskstorage.Limits `json:"limits"`
}

func RiskContext(ctx context.Context, riskStore *riskstorage.Store) (Output, error) {
	state, err := riskStore.GetState(ctx, nil)
	if err != nil {
		return Output{}, fmt.Errorf("agents: risk_context: fetch state: %w", err)
	}
	limits, err := riskStore.GetLimits(ctx)
	if err != nil {
		return Output{}, fmt.Errorf("agents: risk_context: fetch limits: %w", err)
	}
	ind := RiskContextIndicators{Status: state.Status, Reason: state.Reason, ChangedAt: state.ChangedAt.Format(time.RFC3339), Limits: limits}
	return Output{Indicators: ind}, nil
}
