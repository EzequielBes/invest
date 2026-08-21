package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Decision is one dashboard history record, from a legacy decision or an
// intent application, including execution outcomes.
type Decision struct {
	ID                      string    `json:"id"`
	AnalysisRunID           string    `json:"analysis_run_id"`
	Asset                   string    `json:"asset"`
	Side                    string    `json:"side"`
	Confidence              float64   `json:"confidence"`
	SizingPct               float64   `json:"sizing_pct"`
	Rationale               string    `json:"rationale"`
	ProposedQuantity        float64   `json:"proposed_quantity"`
	ProposedValue           float64   `json:"proposed_value"`
	RiskAllowed             *bool     `json:"risk_allowed,omitempty"`
	RiskReasons             []string  `json:"risk_reasons,omitempty"`
	ExecutionStatus         *string   `json:"execution_status,omitempty"`
	ExecutionOrderID        *string   `json:"execution_order_id,omitempty"`
	ExecutionFilledQuantity *float64  `json:"execution_filled_quantity,omitempty"`
	ExecutionFilledPrice    *float64  `json:"execution_filled_price,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
}

// RecentDecisions returns legacy non-paper decisions and new testnet intent
// applications. Paper applications remain exclusive to RecentPaperDecisions.
func (s *Store) RecentDecisions(ctx context.Context, limit int) ([]Decision, error) {
	return queryDecisions(ctx, s.pool, `
		SELECT id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
		       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
		       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
		       created_at
		FROM (
			SELECT id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
			       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
			       created_at
			FROM strategist_decisions
			WHERE id NOT IN (SELECT id FROM paper_decision_ids)
			UNION ALL
			SELECT intent_id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
			       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
			       created_at
			FROM strategist_intent_applications
			WHERE target_id = 'testnet'
		) decisions
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
}

// RecentPaperDecisions returns legacy paper decisions and paper intent
// applications, including holds and risk rejections.
func (s *Store) RecentPaperDecisions(ctx context.Context, limit int) ([]Decision, error) {
	return queryDecisions(ctx, s.pool, `
		SELECT id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
		       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
		       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
		       created_at
		FROM (
			SELECT id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
			       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
			       created_at
			FROM strategist_decisions
			WHERE id IN (SELECT id FROM paper_decision_ids)
			UNION ALL
			SELECT intent_id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
			       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
			       created_at
			FROM strategist_intent_applications
			WHERE target_id = 'paper'
		) decisions
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
}

func queryDecisions(ctx context.Context, pool *pgxpool.Pool, query string, limit int) ([]Decision, error) {
	rows, err := pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	decisions := []Decision{}
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
