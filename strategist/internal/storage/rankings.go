package storage

import (
	"context"
	"encoding/json"
)

// Ranking is an analysis-owned ranking read directly from the shared
// database. Strategist intentionally has no Go dependency on analysis.
type Ranking struct {
	Asset          string
	Rank           int
	CompositeScore float64
	Thesis         string
	Confidence     float64
	Evidence       json.RawMessage
}

// RankingsForRun returns a deterministic opportunity order for an analysis
// run. An empty result is normal for older or committee-failed runs.
func (s *Store) RankingsForRun(ctx context.Context, runID string) ([]Ranking, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asset, rank, composite_score, thesis, confidence, evidence
		FROM analysis_rankings
		WHERE run_id = $1
		ORDER BY rank ASC, asset ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rankings []Ranking
	for rows.Next() {
		var ranking Ranking
		if err := rows.Scan(&ranking.Asset, &ranking.Rank, &ranking.CompositeScore, &ranking.Thesis, &ranking.Confidence, &ranking.Evidence); err != nil {
			return nil, err
		}
		rankings = append(rankings, ranking)
	}
	return rankings, rows.Err()
}
