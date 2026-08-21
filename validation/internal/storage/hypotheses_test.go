package storage

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	validation "validation/internal/validation"
)

func TestValidateHypothesis_RejectsIncompleteContract(t *testing.T) {
	hypothesis := validHypothesis()
	hypothesis.CostPolicy = ""
	if err := ValidateHypothesis(hypothesis); err == nil || !strings.Contains(err.Error(), "cost policy") {
		t.Fatalf("ValidateHypothesis error = %v, want missing cost policy", err)
	}
}

func TestCanonicalJSON_IsDeterministic(t *testing.T) {
	first, err := CanonicalJSON(json.RawMessage(`{"z":1,"a":{"b":2,"a":3}}`))
	if err != nil {
		t.Fatalf("CanonicalJSON first: %v", err)
	}
	second, err := CanonicalJSON(json.RawMessage(` { "a" : { "a" : 3, "b" : 2 }, "z" : 1 } `))
	if err != nil {
		t.Fatalf("CanonicalJSON second: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("canonical JSON differs: %s != %s", first, second)
	}
}

func TestStorage_RoundTripsContractRunSplitsAndFindings(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Registered before the delete cleanup below so it runs last (t.Cleanup
	// is LIFO) — closing the pool first left the delete cleanup silently
	// failing against a closed pool, leaking a hypothesis (and, via
	// ON DELETE CASCADE, its run/splits/findings) into the shared database
	// on every test run.
	t.Cleanup(store.Close)

	hypothesis := validHypothesis()
	hypothesis.ID = "validation-test-" + uuid.NewString()
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(), "DELETE FROM validation_hypotheses WHERE id = $1", hypothesis.ID); err != nil {
			t.Errorf("cleanup: delete hypothesis %s: %v", hypothesis.ID, err)
		}
	})
	hypothesis, err = store.CreateHypothesis(context.Background(), hypothesis)
	if err != nil {
		t.Fatalf("CreateHypothesis: %v", err)
	}

	run, err := CreateRun(context.Background(), store, Run{ID: "validation-test-" + uuid.NewString(), HypothesisID: hypothesis.ID, Config: json.RawMessage(`{"symbols":["BTCUSDT"],"window":"1h"}`)})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.Status != "running" || run.ConfigHash == "" {
		t.Fatalf("run = %+v, want running status and config hash", run)
	}

	gotHypothesis, err := store.Hypothesis(context.Background(), hypothesis.ID)
	if err != nil {
		t.Fatalf("Hypothesis: %v", err)
	}
	if gotHypothesis.Description != hypothesis.Description || gotHypothesis.AvailabilityRule != hypothesis.AvailabilityRule {
		t.Errorf("hypothesis = %+v, want %+v", gotHypothesis, hypothesis)
	}
	gotRun, err := store.Run(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	gotConfig, err := CanonicalJSON(gotRun.Config)
	if err != nil {
		t.Fatalf("CanonicalJSON stored config: %v", err)
	}
	if string(gotConfig) != string(run.Config) || gotRun.ConfigHash != run.ConfigHash {
		t.Errorf("run = %+v, want %+v", gotRun, run)
	}
	if err := store.FinishRun(context.Background(), run.ID, "completed", ""); err == nil {
		t.Fatal("FinishRun completed a run without temporal splits")
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	splits := []validation.Split{{Kind: validation.SplitTrain, Start: start, End: start.Add(24 * time.Hour)}, {Kind: validation.SplitValidation, Start: start.Add(24 * time.Hour), End: start.Add(48 * time.Hour)}, {Kind: validation.SplitHoldout, Start: start.Add(48 * time.Hour), End: start.Add(72 * time.Hour)}}
	if err := store.SaveSplits(context.Background(), run.ID, splits); err != nil {
		t.Fatalf("SaveSplits: %v", err)
	}
	findings := validation.ValidateAvailability(start, []time.Time{start.Add(time.Minute)})
	if err := store.SaveFindings(context.Background(), run.ID, findings); err != nil {
		t.Fatalf("SaveFindings: %v", err)
	}
	var splitCount, findingCount int
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM validation_splits WHERE validation_run_id = $1", run.ID).Scan(&splitCount); err != nil {
		t.Fatalf("count splits: %v", err)
	}
	if err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM validation_findings WHERE validation_run_id = $1", run.ID).Scan(&findingCount); err != nil {
		t.Fatalf("count findings: %v", err)
	}
	if splitCount != 3 || findingCount != 1 {
		t.Errorf("counts = splits:%d findings:%d, want 3 and 1", splitCount, findingCount)
	}
}

func validHypothesis() Hypothesis {
	return Hypothesis{Description: "A declared test hypothesis", Universe: "BTCUSDT", Horizon: "1h", CostPolicy: "0.1 percent fee", PrimaryMetrics: []string{"max_drawdown_pct"}, AvailabilityRule: "inputs must precede the decision timestamp"}
}
