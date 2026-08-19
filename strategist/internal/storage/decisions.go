// strategist/internal/storage/decisions.go
package storage

import (
	"context"
	"encoding/json"
	"time"
)

// Decision is one persisted strategist_decisions row: the LLM's decision
// (Side/Confidence/SizingPct/Rationale) plus the sized proposal, the
// risk-engine's verdict on it, and — since sub-project 8 — the real
// execution outcome. RiskAllowed is nil when Side is "hold" or
// risk.Evaluate itself failed. The Execution* fields are nil whenever
// Execution wasn't attempted (hold, risk rejection, sell-clamped to
// zero) or the execution call itself failed — see strategist.Decide.
type Decision struct {
	ID                      string
	AnalysisRunID           string
	Asset                   string
	Side                    string
	Confidence              float64
	SizingPct               float64
	Rationale               string
	ProposedQuantity        float64
	ProposedValue           float64
	RiskAllowed             *bool
	RiskReasons             []string
	ExecutionStatus         *string
	ExecutionOrderID        *string
	ExecutionFilledQuantity *float64
	ExecutionFilledPrice    *float64
	CreatedAt               time.Time
}

// SaveDecision marshals RiskReasons to JSON and inserts one
// strategist_decisions row.
func (s *Store) SaveDecision(ctx context.Context, d Decision) error {
	reasons := d.RiskReasons
	if reasons == nil {
		reasons = []string{}
	}
	raw, err := json.Marshal(reasons)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO strategist_decisions
			(id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			 proposed_quantity, proposed_value, risk_allowed, risk_reasons,
			 execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
			 created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`, d.ID, d.AnalysisRunID, d.Asset, d.Side, d.Confidence, d.SizingPct, d.Rationale,
		d.ProposedQuantity, d.ProposedValue, d.RiskAllowed, raw,
		d.ExecutionStatus, d.ExecutionOrderID, d.ExecutionFilledQuantity, d.ExecutionFilledPrice,
		d.CreatedAt)
	return err
}

// DecisionsForTest reads persisted decisions for a run, in insertion
// order — used by tests.
func (s *Store) DecisionsForTest(ctx context.Context, runID string) ([]Decision, error) {
	return s.decisionsForRun(ctx, runID)
}

// DecisionsForRun reads persisted decisions for an analysis run, in
// creation order — used by production callers (e.g. the MCP server's
// run_strategist tool, which reads back what Run just persisted).
func (s *Store) DecisionsForRun(ctx context.Context, analysisRunID string) ([]Decision, error) {
	return s.decisionsForRun(ctx, analysisRunID)
}

func (s *Store) decisionsForRun(ctx context.Context, runID string) ([]Decision, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
		       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
		       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
		       created_at
		FROM strategist_decisions
		WHERE analysis_run_id = $1
		ORDER BY created_at, id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []Decision
	for rows.Next() {
		var d Decision
		var reasonsRaw []byte
		if err := rows.Scan(&d.ID, &d.AnalysisRunID, &d.Asset, &d.Side, &d.Confidence, &d.SizingPct,
			&d.Rationale, &d.ProposedQuantity, &d.ProposedValue, &d.RiskAllowed, &reasonsRaw,
			&d.ExecutionStatus, &d.ExecutionOrderID, &d.ExecutionFilledQuantity, &d.ExecutionFilledPrice,
			&d.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(reasonsRaw, &d.RiskReasons); err != nil {
			return nil, err
		}
		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}

// DeleteDecisionsForRunForTest removes strategist_decisions rows for
// runID — used by tests to clean up after themselves.
func (s *Store) DeleteDecisionsForRunForTest(ctx context.Context, runID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM strategist_decisions WHERE analysis_run_id = $1`, runID)
	return err
}
