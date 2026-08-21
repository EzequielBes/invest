package storage

import (
	"context"
	"encoding/json"
	"time"
)

// IntentApplication records one intent's result for one executor target.
// The (IntentID, TargetID) key makes retries safe per target.
type IntentApplication struct {
	IntentID                string
	TargetID                string
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
	ExecutionStatus         string
	ExecutionOrderID        *string
	ExecutionFilledQuantity *float64
	ExecutionFilledPrice    *float64
	CreatedAt               time.Time
}

// CreateIntentApplication reserves an intent for a target before any order is
// sent. false means a prior attempt already owns the stable pair.
func (s *Store) CreateIntentApplication(ctx context.Context, a IntentApplication) (bool, error) {
	reasons := a.RiskReasons
	if reasons == nil {
		reasons = []string{}
	}
	raw, err := json.Marshal(reasons)
	if err != nil {
		return false, err
	}
	result, err := s.pool.Exec(ctx, `
		INSERT INTO strategist_intent_applications
			(intent_id, target_id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			 proposed_quantity, proposed_value, risk_allowed, risk_reasons, execution_status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (intent_id, target_id) DO NOTHING
	`, a.IntentID, a.TargetID, a.AnalysisRunID, a.Asset, a.Side, a.Confidence, a.SizingPct, a.Rationale,
		a.ProposedQuantity, a.ProposedValue, a.RiskAllowed, raw, a.ExecutionStatus, a.CreatedAt)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (s *Store) CompleteIntentApplication(ctx context.Context, intentID, targetID, status string, orderID *string, quantity, price *float64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE strategist_intent_applications
		SET execution_status = $3, execution_order_id = $4, execution_filled_quantity = $5, execution_filled_price = $6
		WHERE intent_id = $1 AND target_id = $2
	`, intentID, targetID, status, orderID, quantity, price)
	return err
}

func (s *Store) SetIntentApplicationRisk(ctx context.Context, intentID, targetID, status string, allowed *bool, reasons []string) error {
	if reasons == nil {
		reasons = []string{}
	}
	raw, err := json.Marshal(reasons)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE strategist_intent_applications
		SET execution_status = $3, risk_allowed = $4, risk_reasons = $5
		WHERE intent_id = $1 AND target_id = $2
	`, intentID, targetID, status, allowed, raw)
	return err
}

func (s *Store) IntentApplications(ctx context.Context, intentIDs []string) ([]IntentApplication, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT intent_id, target_id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
		       proposed_quantity, proposed_value, risk_allowed, risk_reasons, execution_status,
		       execution_order_id, execution_filled_quantity, execution_filled_price, created_at
		FROM strategist_intent_applications WHERE intent_id = ANY($1) ORDER BY created_at, intent_id, target_id
	`, intentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var applications []IntentApplication
	for rows.Next() {
		var a IntentApplication
		var reasons []byte
		if err := rows.Scan(&a.IntentID, &a.TargetID, &a.AnalysisRunID, &a.Asset, &a.Side, &a.Confidence, &a.SizingPct, &a.Rationale,
			&a.ProposedQuantity, &a.ProposedValue, &a.RiskAllowed, &reasons, &a.ExecutionStatus,
			&a.ExecutionOrderID, &a.ExecutionFilledQuantity, &a.ExecutionFilledPrice, &a.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(reasons, &a.RiskReasons); err != nil {
			return nil, err
		}
		applications = append(applications, a)
	}
	return applications, rows.Err()
}
