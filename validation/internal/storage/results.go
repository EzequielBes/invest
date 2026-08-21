package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	validation "validation/internal/validation"
)

type Metric struct {
	Name     string
	Value    float64
	Segment  string
	Unit     string
	Evidence map[string]any
}

func (s *Store) SaveMetrics(ctx context.Context, runID string, metrics []Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, metric := range metrics {
		if metric.Name == "" || metric.Segment == "" || metric.Unit == "" {
			return fmt.Errorf("metric name, segment, and unit are required")
		}
		evidence, err := json.Marshal(metric.Evidence)
		if err != nil {
			return fmt.Errorf("marshal metric evidence: %w", err)
		}
		batch.Queue(`INSERT INTO validation_metrics (id, validation_run_id, name, value, segment, unit, evidence) VALUES ($1, $2, $3, $4, $5, $6, $7)`, uuid.NewString(), runID, metric.Name, metric.Value, metric.Segment, metric.Unit, evidence)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range metrics {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) FinishRun(ctx context.Context, runID, status, runError string) error {
	splits, err := s.splits(ctx, runID)
	if err != nil {
		return err
	}
	if err := validation.ValidateSplits(splits); err != nil {
		return fmt.Errorf("finish validation run: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE validation_runs
		SET status = $1, error = NULLIF($2, ''), completed_at = now()
		WHERE id = $3`, status, runError, runID)
	return err
}

func (s *Store) splits(ctx context.Context, runID string) ([]validation.Split, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, start_at, end_at, embargo_minutes
		FROM validation_splits
		WHERE validation_run_id = $1`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	splits := []validation.Split{}
	for rows.Next() {
		var split validation.Split
		if err := rows.Scan(&split.Kind, &split.Start, &split.End, &split.EmbargoMinutes); err != nil {
			return nil, err
		}
		splits = append(splits, split)
	}
	return splits, rows.Err()
}

func (s *Store) SaveSplits(ctx context.Context, runID string, splits []validation.Split) error {
	if err := validation.ValidateSplits(splits); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, split := range splits {
		batch.Queue(`INSERT INTO validation_splits (id, validation_run_id, kind, start_at, end_at, embargo_minutes) VALUES ($1, $2, $3, $4, $5, $6)`, uuid.NewString(), runID, split.Kind, split.Start, split.End, split.EmbargoMinutes)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range splits {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveFindings(ctx context.Context, runID string, findings []validation.Finding) error {
	batch := &pgx.Batch{}
	for _, finding := range findings {
		if finding.Severity == "" || finding.Rule == "" || finding.Message == "" {
			return fmt.Errorf("finding severity, rule, and message are required")
		}
		evidence, err := json.Marshal(finding.Evidence)
		if err != nil {
			return fmt.Errorf("marshal finding evidence: %w", err)
		}
		batch.Queue(`INSERT INTO validation_findings (id, validation_run_id, severity, rule, message, evidence) VALUES ($1, $2, $3, $4, $5, $6)`, uuid.NewString(), runID, finding.Severity, finding.Rule, finding.Message, evidence)
	}
	if len(findings) == 0 {
		return nil
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range findings {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
}
