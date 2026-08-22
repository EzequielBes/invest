package agents

import (
	"context"
	"fmt"
	"time"

	"analysis/internal/storage"
)

// CycleNarrative is the persisted narrative context made available to the
// two cycle-level agents. It keeps the agents independent of storage.
type CycleNarrative struct {
	AgentType string
	Asset     string
	Narrative string
}

// MacroIndicators is real macro-economic data read from FRED (via
// market-data's poller), not a fabricated placeholder. ObservedAt reflects
// the underlying data's own date, which can lag today by weeks (e.g. CPI)
// — the narrative must present this honestly rather than as live data.
type MacroIndicators struct {
	FedFundsRate     float64   `json:"fed_funds_rate"`
	FedFundsAsOf     time.Time `json:"fed_funds_as_of"`
	CPI              float64   `json:"cpi"`
	CPIAsOf          time.Time `json:"cpi_as_of"`
	UnemploymentRate float64   `json:"unemployment_rate"`
	UnemploymentAsOf time.Time `json:"unemployment_as_of"`
}

// Macro reports real macro-economic indicators (Fed funds rate, CPI,
// unemployment) collected from FRED. It is cycle-level, not per-asset —
// like RiskContext, called once per analysis run.
func Macro(ctx context.Context, store *storage.Store) (Output, error) {
	observations, err := store.LatestMacroObservations(ctx)
	if err != nil {
		return Output{}, fmt.Errorf("agents: macro: fetch observations: %w", err)
	}

	var ind MacroIndicators
	if obs, ok := observations["FEDFUNDS"]; ok {
		ind.FedFundsRate = obs.Value
		ind.FedFundsAsOf = obs.ObservedAt
	}
	if obs, ok := observations["CPIAUCSL"]; ok {
		ind.CPI = obs.Value
		ind.CPIAsOf = obs.ObservedAt
	}
	if obs, ok := observations["UNRATE"]; ok {
		ind.UnemploymentRate = obs.Value
		ind.UnemploymentAsOf = obs.ObservedAt
	}
	return Output{Indicators: ind}, nil
}
