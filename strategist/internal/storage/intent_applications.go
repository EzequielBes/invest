package storage

import (
	"context"
	"encoding/json"
	"time"
)

// IntentApplication records one intent's result for one executor target.
// The (AnalysisRunID, IntentID, TargetID) key makes retries safe per target
// without suppressing the same stable intent in a later analysis run.
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
// sent. false means a prior attempt already owns the stable run/target pair.
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
		ON CONFLICT (analysis_run_id, intent_id, target_id) DO NOTHING
	`, a.IntentID, a.TargetID, a.AnalysisRunID, a.Asset, a.Side, a.Confidence, a.SizingPct, a.Rationale,
		a.ProposedQuantity, a.ProposedValue, a.RiskAllowed, raw, a.ExecutionStatus, a.CreatedAt)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (s *Store) CompleteIntentApplication(ctx context.Context, analysisRunID, intentID, targetID, status string, orderID *string, quantity, price *float64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE strategist_intent_applications
		SET execution_status = $4, execution_order_id = $5, execution_filled_quantity = $6, execution_filled_price = $7
		WHERE analysis_run_id = $1 AND intent_id = $2 AND target_id = $3
	`, analysisRunID, intentID, targetID, status, orderID, quantity, price)
	return err
}

func (s *Store) SetIntentApplicationRisk(ctx context.Context, analysisRunID, intentID, targetID, status string, allowed *bool, reasons []string) error {
	if reasons == nil {
		reasons = []string{}
	}
	raw, err := json.Marshal(reasons)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE strategist_intent_applications
		SET execution_status = $4, risk_allowed = $5, risk_reasons = $6
		WHERE analysis_run_id = $1 AND intent_id = $2 AND target_id = $3
	`, analysisRunID, intentID, targetID, status, allowed, raw)
	return err
}

func (s *Store) IntentApplications(ctx context.Context, analysisRunID string, intentIDs []string) ([]IntentApplication, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT intent_id, target_id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
		       proposed_quantity, proposed_value, risk_allowed, risk_reasons, execution_status,
		       execution_order_id, execution_filled_quantity, execution_filled_price, created_at
		FROM strategist_intent_applications
		WHERE analysis_run_id = $1 AND intent_id = ANY($2)
		ORDER BY created_at, intent_id, target_id
	`, analysisRunID, intentIDs)
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
