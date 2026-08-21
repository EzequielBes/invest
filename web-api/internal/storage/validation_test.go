package storage

import (
	"context"
	"errors"
	"testing"
)

func TestValidationRunsExposeOnlyFinalizedRuns(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	hypothesisID := testID(t, "validation-hypothesis")
	runningID := testID(t, "validation-running")
	completedID := testID(t, "validation-completed")

	if _, err := store.pool.Exec(ctx, `INSERT INTO validation_hypotheses (id, description, universe, horizon, cost_policy, primary_metrics, availability_rule) VALUES ($1, 'test', 'BTCUSDT', '1h', 'declared', '["metric"]', 'inputs precede decisions')`, hypothesisID); err != nil {
		t.Fatalf("seed hypothesis: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM validation_hypotheses WHERE id = $1`, hypothesisID)
	})
	for _, run := range []struct {
		id        string
		status    string
		completed bool
	}{{runningID, "running", false}, {completedID, "completed", true}} {
		if _, err := store.pool.Exec(ctx, `INSERT INTO validation_runs (id, hypothesis_id, status, config, config_hash, completed_at) VALUES ($1, $2, $3, '{}', 'hash', CASE WHEN $4 THEN now() END)`, run.id, hypothesisID, run.status, run.completed); err != nil {
			t.Fatalf("seed validation run: %v", err)
		}
	}

	runs, err := store.RecentValidationRuns(ctx, 100)
	if err != nil {
		t.Fatalf("RecentValidationRuns: %v", err)
	}
	for _, run := range runs {
		if run.ID == runningID {
			t.Fatal("RecentValidationRuns returned a running run")
		}
	}
	if _, err := store.ValidationRunDetail(ctx, runningID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ValidationRunDetail(running) error = %v, want ErrNotFound", err)
	}
	if _, err := store.ValidationRunDetail(ctx, completedID); err != nil {
		t.Fatalf("ValidationRunDetail(completed): %v", err)
	}
}
