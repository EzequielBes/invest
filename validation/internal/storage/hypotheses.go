package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Hypothesis struct {
	ID               string
	Description      string
	Universe         string
	Horizon          string
	CostPolicy       string
	PrimaryMetrics   []string
	AvailabilityRule string
	CreatedAt        time.Time
}

func ValidateHypothesis(h Hypothesis) error {
	if strings.TrimSpace(h.Description) == "" {
		return fmt.Errorf("hypothesis description is required")
	}
	if strings.TrimSpace(h.Universe) == "" {
		return fmt.Errorf("hypothesis universe is required")
	}
	if strings.TrimSpace(h.Horizon) == "" {
		return fmt.Errorf("hypothesis horizon is required")
	}
	if strings.TrimSpace(h.CostPolicy) == "" {
		return fmt.Errorf("hypothesis cost policy is required")
	}
	if len(h.PrimaryMetrics) == 0 {
		return fmt.Errorf("hypothesis primary metrics are required")
	}
	for _, metric := range h.PrimaryMetrics {
		if strings.TrimSpace(metric) == "" {
			return fmt.Errorf("hypothesis primary metrics cannot be blank")
		}
	}
	if strings.TrimSpace(h.AvailabilityRule) == "" {
		return fmt.Errorf("hypothesis availability rule is required")
	}
	return nil
}

func (s *Store) CreateHypothesis(ctx context.Context, hypothesis Hypothesis) (Hypothesis, error) {
	if err := ValidateHypothesis(hypothesis); err != nil {
		return Hypothesis{}, err
	}
	if hypothesis.ID == "" {
		hypothesis.ID = uuid.NewString()
	}
	metrics, err := json.Marshal(hypothesis.PrimaryMetrics)
	if err != nil {
		return Hypothesis{}, fmt.Errorf("marshal primary metrics: %w", err)
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO validation_hypotheses
			(id, description, universe, horizon, cost_policy, primary_metrics, availability_rule)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`, hypothesis.ID, hypothesis.Description, hypothesis.Universe,
		hypothesis.Horizon, hypothesis.CostPolicy, metrics, hypothesis.AvailabilityRule).Scan(&hypothesis.CreatedAt)
	if err != nil {
		return Hypothesis{}, err
	}
	return hypothesis, nil
}

func (s *Store) Hypothesis(ctx context.Context, id string) (Hypothesis, error) {
	var hypothesis Hypothesis
	err := s.pool.QueryRow(ctx, `
		SELECT id, description, universe, horizon, cost_policy, primary_metrics, availability_rule, created_at
		FROM validation_hypotheses WHERE id = $1`, id).Scan(&hypothesis.ID, &hypothesis.Description,
		&hypothesis.Universe, &hypothesis.Horizon, &hypothesis.CostPolicy, &hypothesis.PrimaryMetrics,
		&hypothesis.AvailabilityRule, &hypothesis.CreatedAt)
	return hypothesis, err
}

type Run struct {
	ID            string
	HypothesisID  string
	Status        string
	Config        json.RawMessage
	ConfigHash    string
	BacktestRunID string
	GitCommit     string
	Error         string
	CreatedAt     time.Time
}

func CreateRun(ctx context.Context, store *Store, run Run) (Run, error) {
	canonical, err := CanonicalJSON(run.Config)
	if err != nil {
		return Run{}, err
	}
	if run.HypothesisID == "" {
		return Run{}, fmt.Errorf("hypothesis ID is required")
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	run.Status = "running"
	run.Config = canonical
	hash := sha256.Sum256(canonical)
	run.ConfigHash = hex.EncodeToString(hash[:])
	err = store.pool.QueryRow(ctx, `
		INSERT INTO validation_runs
			(id, hypothesis_id, status, config, config_hash, backtest_run_id, git_commit, error)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''))
		RETURNING created_at`, run.ID, run.HypothesisID, run.Status, run.Config, run.ConfigHash,
		run.BacktestRunID, run.GitCommit, run.Error).Scan(&run.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Store) Run(ctx context.Context, id string) (Run, error) {
	var run Run
	err := s.pool.QueryRow(ctx, `
		SELECT id, hypothesis_id, status, config, config_hash, COALESCE(backtest_run_id, ''),
			COALESCE(git_commit, ''), COALESCE(error, ''), created_at
		FROM validation_runs WHERE id = $1`, id).Scan(&run.ID, &run.HypothesisID, &run.Status,
		&run.Config, &run.ConfigHash, &run.BacktestRunID, &run.GitCommit, &run.Error, &run.CreatedAt)
	return run, err
}

func CanonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("run config is required")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode run config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode run config: multiple JSON values")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical config: %w", err)
	}
	return canonical, nil
}
