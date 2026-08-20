package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AnalysisRun struct {
	ID         string     `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Timeframe  string     `json:"timeframe"`
	Status     string     `json:"status"`
	Error      *string    `json:"error,omitempty"`
}

type AnalysisResult struct {
	ID         string         `json:"id"`
	RunID      string         `json:"run_id"`
	AgentType  string         `json:"agent_type"`
	Asset      string         `json:"asset"`
	Indicators map[string]any `json:"indicators"`
	Narrative  string         `json:"narrative"`
	CreatedAt  time.Time      `json:"created_at"`
}

type AnalysisRunDetail struct {
	Run     AnalysisRun      `json:"run"`
	Results []AnalysisResult `json:"results"`
}

func (s *Store) RecentAnalysisRuns(ctx context.Context, limit int) ([]AnalysisRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, started_at, finished_at, timeframe, status, error
		FROM analysis_runs
		ORDER BY started_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []AnalysisRun{}
	for rows.Next() {
		var r AnalysisRun
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Timeframe, &r.Status, &r.Error); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) AnalysisRunDetail(ctx context.Context, id string) (AnalysisRunDetail, error) {
	var run AnalysisRun
	err := s.pool.QueryRow(ctx, `
		SELECT id, started_at, finished_at, timeframe, status, error
		FROM analysis_runs
		WHERE id = $1
	`, id).Scan(&run.ID, &run.StartedAt, &run.FinishedAt, &run.Timeframe, &run.Status, &run.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnalysisRunDetail{}, ErrNotFound
	}
	if err != nil {
		return AnalysisRunDetail{}, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, agent_type, asset, indicators, narrative, created_at
		FROM analysis_results
		WHERE run_id = $1
		ORDER BY created_at
	`, id)
	if err != nil {
		return AnalysisRunDetail{}, err
	}
	defer rows.Close()

	results := []AnalysisResult{}
	for rows.Next() {
		var r AnalysisResult
		var indicatorsRaw []byte
		if err := rows.Scan(&r.ID, &r.RunID, &r.AgentType, &r.Asset, &indicatorsRaw, &r.Narrative, &r.CreatedAt); err != nil {
			return AnalysisRunDetail{}, err
		}
		if err := json.Unmarshal(indicatorsRaw, &r.Indicators); err != nil {
			return AnalysisRunDetail{}, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return AnalysisRunDetail{}, err
	}

	return AnalysisRunDetail{Run: run, Results: results}, nil
}
