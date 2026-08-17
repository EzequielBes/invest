// risk-engine/storage/state.go
package storage

import (
	"context"
	"time"
)

const (
	StatusNormal     = "normal"
	StatusPaused     = "paused"
	StatusKillSwitch = "kill_switch"
)

type State struct {
	Status    string
	Reason    string
	ChangedAt time.Time
}

// GetState reads operational state for runID. nil means the live row
// (run_id IS NULL) — exactly today's behavior. A non-nil runID reads that
// backtest run's isolated row instead.
func (s *Store) GetState(ctx context.Context, runID *string) (State, error) {
	var st State
	err := s.pool.QueryRow(ctx, `
		SELECT status, reason, changed_at FROM risk_state WHERE run_id IS NOT DISTINCT FROM $1
	`, runID).Scan(&st.Status, &st.Reason, &st.ChangedAt)
	return st, err
}

// InitRunState creates a fresh 'normal' risk_state row for a backtest run,
// idempotently (a re-run with the same runID is a no-op). Every backtest
// run must call this once before its first risk.Evaluate call, since
// unlike the live row there is no pre-seeded row for a new run_id.
func (s *Store) InitRunState(ctx context.Context, runID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO risk_state (run_id, status, reason) VALUES ($1, $2, $3)
		ON CONFLICT (run_id) WHERE run_id IS NOT NULL DO NOTHING
	`, runID, StatusNormal, "run started")
	return err
}

// setState updates the operational state using db, which may be the
// Store's pool (standalone call) or a transaction, so the state change and
// a related risk_decisions row can commit or roll back together.
//
// This is deliberately package-private to prevent unguarded external use:
// an unconditional overwrite of risk_state is exactly the hazard
// SetStateIfNormal exists to guard against. SetStateIfNormal is the safe
// path for conditional writes; (*Store).SetState and (*Store).Reset are the
// intentional operator-facing manual-override paths.
func setState(ctx context.Context, db querier, runID *string, status, reason string) error {
	_, err := db.Exec(ctx, `
		UPDATE risk_state SET status = $1, reason = $2, changed_at = now()
		WHERE run_id IS NOT DISTINCT FROM $3
	`, status, reason, runID)
	return err
}

func (s *Store) SetState(ctx context.Context, runID *string, status, reason string) error {
	return setState(ctx, s.pool, runID, status, reason)
}

// SetStateIfNormal transitions status only if the current status is still
// StatusNormal, returning whether the transition was applied. This guards
// against a race where an operator (or, for a run, an earlier concurrent
// evaluation) changes state between Evaluate's initial GetState and this
// write — the engine must never silently downgrade a protective state it
// didn't set.
func SetStateIfNormal(ctx context.Context, db querier, runID *string, status, reason string) (bool, error) {
	tag, err := db.Exec(ctx, `
		UPDATE risk_state SET status = $1, reason = $2, changed_at = now()
		WHERE run_id IS NOT DISTINCT FROM $3 AND status = $4
	`, status, reason, runID, StatusNormal)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// Reset manually clears a paused/kill_switch state back to normal. The risk
// engine never does this on its own — it is a deliberate operator action.
func (s *Store) Reset(ctx context.Context, runID *string, reason string) error {
	return s.SetState(ctx, runID, StatusNormal, reason)
}
