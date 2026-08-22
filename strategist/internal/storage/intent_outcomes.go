package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"risk-engine/risk"
)

// DueIntent is one distinct non-hold subscription intent awaiting a horizon.
type DueIntent struct {
	AnalysisRunID string
	IntentID      string
	Asset         string
	Side          string
	CreatedAt     time.Time
	Horizon       time.Duration
}

// DueIntentOutcomes returns only horizons whose evaluation time has passed and
// which have not already been stored.
func (s *Store) DueIntentOutcomes(ctx context.Context, now time.Time) ([]DueIntent, error) {
	rows, err := s.pool.Query(ctx, `
		WITH intents AS (
			SELECT analysis_run_id, intent_id, min(asset) AS asset, min(side) AS side, min(created_at) AS created_at
			FROM strategist_intent_applications
			WHERE side IN ('buy', 'sell')
			GROUP BY analysis_run_id, intent_id
		)
		SELECT i.analysis_run_id, i.intent_id, i.asset, i.side, i.created_at, h.hours
		FROM intents i
		CROSS JOIN (VALUES (1), (4), (24)) AS h(hours)
		WHERE i.created_at + make_interval(hours => h.hours) <= $1
		  AND NOT EXISTS (
				SELECT 1 FROM strategist_intent_outcomes o
				WHERE o.analysis_run_id = i.analysis_run_id AND o.intent_id = i.intent_id AND o.horizon_hours = h.hours
			)
		ORDER BY i.created_at, i.analysis_run_id, i.intent_id, h.hours
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var intents []DueIntent
	for rows.Next() {
		var intent DueIntent
		var hours int
		if err := rows.Scan(&intent.AnalysisRunID, &intent.IntentID, &intent.Asset, &intent.Side, &intent.CreatedAt, &hours); err != nil {
			return nil, err
		}
		intent.Horizon = time.Duration(hours) * time.Hour
		intents = append(intents, intent)
	}
	return intents, rows.Err()
}

// CloseAfter returns the first 1m candle close available after at, at
// asset's configured exchange. candles.ts is its open time, so its close
// is one minute later.
func (s *Store) CloseAfter(ctx context.Context, asset string, at time.Time) (float64, bool, error) {
	var close float64
	err := s.pool.QueryRow(ctx, `
		SELECT close FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
		  AND ts + interval '1 minute' > $3
		ORDER BY ts
		LIMIT 1
	`, risk.ExchangeFor(asset), asset, at).Scan(&close)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return close, true, nil
}

func (s *Store) SaveIntentOutcome(ctx context.Context, intent DueIntent, returnPct float64, correct bool) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		INSERT INTO strategist_intent_outcomes
			(analysis_run_id, intent_id, horizon_hours, direction_return_pct, correct, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING
	`, intent.AnalysisRunID, intent.IntentID, int(intent.Horizon.Hours()), returnPct, correct, intent.CreatedAt)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}
