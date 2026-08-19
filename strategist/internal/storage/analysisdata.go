// strategist/internal/storage/analysisdata.go
package storage

import (
	"context"
	"encoding/json"
)

// AgentResult is one analysis_results row (owned by the analysis module,
// read here from the same shared database — no Go dependency on that
// module). AgentType is "technical", "derivatives", "news", or
// "risk_context"; Asset is "" for risk_context, which is portfolio-level.
type AgentResult struct {
	AgentType  string
	Asset      string
	Indicators map[string]any
	Narrative  string
}

// ResultsForRun returns every analysis_results row for runID, oldest first.
// The ordering makes the caller's last risk_context selection deterministic
// when a run has more than one such row.
func (s *Store) ResultsForRun(ctx context.Context, runID string) ([]AgentResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT agent_type, asset, indicators, narrative
		FROM analysis_results
		WHERE run_id = $1
		ORDER BY created_at ASC, id ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AgentResult
	for rows.Next() {
		var r AgentResult
		var raw []byte
		if err := rows.Scan(&r.AgentType, &r.Asset, &raw, &r.Narrative); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &r.Indicators); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
