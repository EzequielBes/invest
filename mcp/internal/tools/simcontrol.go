// mcp/internal/tools/simcontrol.go
package tools

import (
	"context"

	"execution/paperstore"
)

// SimulationStatusResult is what both get_simulation_status and
// set_simulation_enabled return.
type SimulationStatusResult struct {
	Enabled   bool               `json:"enabled"`
	Cash      float64            `json:"cash"`
	Positions map[string]float64 `json:"positions"`
}

type GetSimulationStatusArgs struct{}

// GetSimulationStatus reads whether paper/simulation mode is currently
// on, plus the simulated portfolio's current state.
func GetSimulationStatus(ctx context.Context, store *paperstore.Store) (SimulationStatusResult, error) {
	return readSimulationStatus(ctx, store)
}

type SetSimulationEnabledArgs struct {
	Enabled bool `json:"enabled" jsonschema:"true to turn simulation on, false to turn it off"`
}

// SetSimulationEnabled turns run_paper_strategist on or off — off means
// it refuses to run at all, so no LLM call is spent on cycles the user
// doesn't want.
func SetSimulationEnabled(ctx context.Context, store *paperstore.Store, args SetSimulationEnabledArgs) (SimulationStatusResult, error) {
	if err := store.SetEnabled(ctx, args.Enabled); err != nil {
		return SimulationStatusResult{}, err
	}
	return readSimulationStatus(ctx, store)
}

func readSimulationStatus(ctx context.Context, store *paperstore.Store) (SimulationStatusResult, error) {
	enabled, err := store.Enabled(ctx)
	if err != nil {
		return SimulationStatusResult{}, err
	}
	cash, positions, err := store.Portfolio(ctx)
	if err != nil {
		return SimulationStatusResult{}, err
	}
	return SimulationStatusResult{Enabled: enabled, Cash: cash, Positions: positions}, nil
}
