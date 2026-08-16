// risk-engine/internal/storage/state.go
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

func (s *Store) GetState(ctx context.Context) (State, error) {
	var st State
	err := s.pool.QueryRow(ctx, `SELECT status, reason, changed_at FROM risk_state WHERE id = 1`).
		Scan(&st.Status, &st.Reason, &st.ChangedAt)
	return st, err
}

// SetState updates the operational state using db, which may be the
// Store's pool (standalone call) or a transaction, so the state change and
// a related risk_decisions row can commit or roll back together.
func SetState(ctx context.Context, db querier, status, reason string) error {
	_, err := db.Exec(ctx, `UPDATE risk_state SET status = $1, reason = $2, changed_at = now() WHERE id = 1`, status, reason)
	return err
}

func (s *Store) SetState(ctx context.Context, status, reason string) error {
	return SetState(ctx, s.pool, status, reason)
}

// Reset manually clears a paused/kill_switch state back to normal. The risk
// engine never does this on its own — it is a deliberate operator action.
func (s *Store) Reset(ctx context.Context, reason string) error {
	return s.SetState(ctx, StatusNormal, reason)
}
