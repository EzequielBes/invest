package storage

import (
	"context"
	"time"
)

// Observation is one macro-indicator reading: an official value observed
// at a point in time, plus when we actually fetched it (fetched_at can lag
// observed_at by weeks — e.g. CPI is published with a delay).
type Observation struct {
	ObservedAt time.Time
	Value      float64
	FetchedAt  time.Time
}

func (s *Store) InsertMacroObservation(ctx context.Context, seriesID string, observedAt time.Time, value float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO macro_indicators (series_id, observed_at, value, fetched_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (series_id, observed_at) DO UPDATE SET value = EXCLUDED.value, fetched_at = now()
	`, seriesID, observedAt, value)
	return err
}

// LatestMacroObservations returns, for every series_id present, the
// observation with the most recent observed_at.
func (s *Store) LatestMacroObservations(ctx context.Context) (map[string]Observation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (series_id) series_id, observed_at, value, fetched_at
		FROM macro_indicators
		ORDER BY series_id, observed_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]Observation)
	for rows.Next() {
		var seriesID string
		var obs Observation
		if err := rows.Scan(&seriesID, &obs.ObservedAt, &obs.Value, &obs.FetchedAt); err != nil {
			return nil, err
		}
		out[seriesID] = obs
	}
	return out, rows.Err()
}
