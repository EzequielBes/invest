// analysis/internal/storage/runs.go
package storage

import (
	"context"
	"encoding/json"
	"time"
)

type Run struct {
	ID        string
	StartedAt time.Time
	Timeframe string
}

func (s *Store) CreateRun(ctx context.Context, r Run) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO analysis_runs (id, started_at, timeframe, status)
		VALUES ($1, $2, $3, 'running')
	`, r.ID, r.StartedAt, r.Timeframe)
	return err
}

// FinishRun marks runID 'completed' (runErr nil) or 'failed' with runErr's
// message recorded, and stamps finished_at either way.
func (s *Store) FinishRun(ctx context.Context, runID string, runErr error) error {
	status := "completed"
	var errMsg *string
	if runErr != nil {
		status = "failed"
		msg := runErr.Error()
		errMsg = &msg
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE analysis_runs SET status = $1, error = $2, finished_at = now() WHERE id = $3
	`, status, errMsg, runID)
	return err
}

type Result struct {
	ID         string
	RunID      string
	AgentType  string
	Asset      string
	Indicators any
	Narrative  string
	CreatedAt  time.Time
}

// SaveResult marshals Indicators to JSON and inserts one analysis_results
// row.
func (s *Store) SaveResult(ctx context.Context, r Result) error {
	indicatorsJSON, err := json.Marshal(r.Indicators)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO analysis_results (id, run_id, agent_type, asset, indicators, narrative, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, r.ID, r.RunID, r.AgentType, r.Asset, indicatorsJSON, r.Narrative, r.CreatedAt)
	return err
}

// DeleteRunForTest removes a run and its results — test-only cleanup, not
// used by production code.
func (s *Store) DeleteRunForTest(ctx context.Context, runID string) {
	s.pool.Exec(ctx, `DELETE FROM analysis_results WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM analysis_runs WHERE id = $1`, runID)
}

// RunStatus reads back a run's current status — used by tests.
func (s *Store) RunStatus(ctx context.Context, runID string) (string, error) {
	var status string
	err := s.pool.QueryRow(ctx, `SELECT status FROM analysis_runs WHERE id = $1`, runID).Scan(&status)
	return status, err
}

// ResultCount counts analysis_results rows for runID — used by tests.
func (s *Store) ResultCount(ctx context.Context, runID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM analysis_results WHERE run_id = $1`, runID).Scan(&count)
	return count, err
}

// PersistedResult is the result shape read back by integration tests.
type PersistedResult struct {
	AgentType string
	Asset     string
	Narrative string
}

// ResultsForTest reads persisted results in creation order — used by tests.
func (s *Store) ResultsForTest(ctx context.Context, runID string) ([]PersistedResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT agent_type, asset, narrative
		FROM analysis_results
		WHERE run_id = $1
		ORDER BY created_at, id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []PersistedResult
	for rows.Next() {
		var result PersistedResult
		if err := rows.Scan(&result.AgentType, &result.Asset, &result.Narrative); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

// AgentResult is one analysis_results row, including indicators — used by
// production callers (e.g. the MCP server's run_analysis tool), unlike
// the narrower test-only PersistedResult.
type AgentResult struct {
	AgentType  string
	Asset      string
	Indicators json.RawMessage
	Narrative  string
}

// ResultsForRun reads persisted results (including indicators) in
// creation order — used by production callers.
func (s *Store) ResultsForRun(ctx context.Context, runID string) ([]AgentResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT agent_type, asset, indicators, narrative
		FROM analysis_results
		WHERE run_id = $1
		ORDER BY created_at, id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AgentResult
	for rows.Next() {
		var r AgentResult
		var indicatorsRaw []byte
		if err := rows.Scan(&r.AgentType, &r.Asset, &indicatorsRaw, &r.Narrative); err != nil {
			return nil, err
		}
		r.Indicators = json.RawMessage(indicatorsRaw)
		results = append(results, r)
	}
	return results, rows.Err()
}
