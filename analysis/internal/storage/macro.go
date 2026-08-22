// analysis/internal/storage/macro.go
package storage

import (
	"context"
	"time"
)

// MacroObservation is one macro-indicator reading as read from
// market-data's macro_indicators table — a pure cross-module data read
// (no Go dependency on market-data), same pattern as candles/news.
type MacroObservation struct {
	ObservedAt time.Time
	Value      float64
}

// LatestMacroObservations returns, for every series_id present, the
// observation with the most recent observed_at.
func (s *Store) LatestMacroObservations(ctx context.Context) (map[string]MacroObservation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (series_id) series_id, observed_at, value
		FROM macro_indicators
		ORDER BY series_id, observed_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]MacroObservation)
	for rows.Next() {
		var seriesID string
		var obs MacroObservation
		if err := rows.Scan(&seriesID, &obs.ObservedAt, &obs.Value); err != nil {
			return nil, err
		}
		out[seriesID] = obs
	}
	return out, rows.Err()
}
