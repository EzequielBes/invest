package storage

import (
	"context"
	"time"
)

type IntentOutcome struct {
	AnalysisRunID      string    `json:"analysis_run_id"`
	IntentID           string    `json:"intent_id"`
	Asset              string    `json:"asset"`
	Side               string    `json:"side"`
	HorizonHours       int       `json:"horizon_hours"`
	DirectionReturnPct float64   `json:"direction_return_pct"`
	Correct            bool      `json:"correct"`
	CreatedAt          time.Time `json:"created_at"`
}

// RecentIntentOutcomes reads evaluation rows while keeping strategist's
// outcome schema private to its owning module.
func (s *Store) RecentIntentOutcomes(ctx context.Context, limit int) ([]IntentOutcome, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT o.analysis_run_id, o.intent_id, i.asset, i.side, o.horizon_hours,
		       o.direction_return_pct, o.correct, o.created_at
		FROM strategist_intent_outcomes o
		JOIN (
			SELECT analysis_run_id, intent_id, min(asset) AS asset, min(side) AS side
			FROM strategist_intent_applications
			GROUP BY analysis_run_id, intent_id
		) i USING (analysis_run_id, intent_id)
		ORDER BY o.created_at DESC, o.horizon_hours
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	outcomes := []IntentOutcome{}
	for rows.Next() {
		var outcome IntentOutcome
		if err := rows.Scan(&outcome.AnalysisRunID, &outcome.IntentID, &outcome.Asset, &outcome.Side,
			&outcome.HorizonHours, &outcome.DirectionReturnPct, &outcome.Correct, &outcome.CreatedAt); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, rows.Err()
}
