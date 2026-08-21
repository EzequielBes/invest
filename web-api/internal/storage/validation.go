package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ValidationRun is the read-only summary of a quantitative validation report.
type ValidationRun struct {
	ID            string     `json:"id"`
	HypothesisID  string     `json:"hypothesis_id"`
	Status        string     `json:"status"`
	ConfigHash    string     `json:"config_hash"`
	BacktestRunID *string    `json:"backtest_run_id,omitempty"`
	GitCommit     *string    `json:"git_commit,omitempty"`
	Error         *string    `json:"error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

type ValidationMetric struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Value    float64        `json:"value"`
	Segment  string         `json:"segment"`
	Unit     string         `json:"unit"`
	Evidence map[string]any `json:"evidence"`
}

type ValidationFinding struct {
	ID        string         `json:"id"`
	Severity  string         `json:"severity"`
	Rule      string         `json:"rule"`
	Message   string         `json:"message"`
	Evidence  map[string]any `json:"evidence"`
	CreatedAt time.Time      `json:"created_at"`
}

type ValidationRunDetail struct {
	Run      ValidationRun       `json:"run"`
	Metrics  []ValidationMetric  `json:"metrics"`
	Findings []ValidationFinding `json:"findings"`
}

const validationRunSelect = `
	SELECT id, hypothesis_id, status, config_hash, backtest_run_id, git_commit,
	       error, created_at, completed_at
	FROM validation_runs
`

func scanValidationRun(row pgx.Row) (ValidationRun, error) {
	var run ValidationRun
	err := row.Scan(&run.ID, &run.HypothesisID, &run.Status, &run.ConfigHash,
		&run.BacktestRunID, &run.GitCommit, &run.Error, &run.CreatedAt, &run.CompletedAt)
	return run, err
}

func (s *Store) RecentValidationRuns(ctx context.Context, limit int) ([]ValidationRun, error) {
	rows, err := s.pool.Query(ctx, validationRunSelect+`
		WHERE completed_at IS NOT NULL
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []ValidationRun{}
	for rows.Next() {
		run, err := scanValidationRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) ValidationRunDetail(ctx context.Context, id string) (ValidationRunDetail, error) {
	run, err := scanValidationRun(s.pool.QueryRow(ctx, validationRunSelect+` WHERE id = $1 AND completed_at IS NOT NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ValidationRunDetail{}, ErrNotFound
	}
	if err != nil {
		return ValidationRunDetail{}, err
	}

	metricRows, err := s.pool.Query(ctx, `
		SELECT id, name, value, segment, unit, evidence
		FROM validation_metrics
		WHERE validation_run_id = $1
		ORDER BY name, segment
	`, id)
	if err != nil {
		return ValidationRunDetail{}, err
	}
	defer metricRows.Close()

	metrics := []ValidationMetric{}
	for metricRows.Next() {
		var metric ValidationMetric
		var evidence []byte
		if err := metricRows.Scan(&metric.ID, &metric.Name, &metric.Value, &metric.Segment, &metric.Unit, &evidence); err != nil {
			return ValidationRunDetail{}, err
		}
		if err := json.Unmarshal(evidence, &metric.Evidence); err != nil {
			return ValidationRunDetail{}, err
		}
		metrics = append(metrics, metric)
	}
	if err := metricRows.Err(); err != nil {
		return ValidationRunDetail{}, err
	}

	findingRows, err := s.pool.Query(ctx, `
		SELECT id, severity, rule, message, evidence, created_at
		FROM validation_findings
		WHERE validation_run_id = $1
		ORDER BY created_at
	`, id)
	if err != nil {
		return ValidationRunDetail{}, err
	}
	defer findingRows.Close()

	findings := []ValidationFinding{}
	for findingRows.Next() {
		var finding ValidationFinding
		var evidence []byte
		if err := findingRows.Scan(&finding.ID, &finding.Severity, &finding.Rule, &finding.Message, &evidence, &finding.CreatedAt); err != nil {
			return ValidationRunDetail{}, err
		}
		if err := json.Unmarshal(evidence, &finding.Evidence); err != nil {
			return ValidationRunDetail{}, err
		}
		findings = append(findings, finding)
	}
	if err := findingRows.Err(); err != nil {
		return ValidationRunDetail{}, err
	}

	return ValidationRunDetail{Run: run, Metrics: metrics, Findings: findings}, nil
}
