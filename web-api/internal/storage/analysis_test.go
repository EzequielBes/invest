package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnalysisRunDetail_UnknownIDReturnsErrNotFound(t *testing.T) {
	store := testStore(t)

	_, err := store.AnalysisRunDetail(context.Background(), testID(t, "nonexistent-analysis-run"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAnalysisRunDetail_ReturnsRunAndResults(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	runID := testID(t, "analysis-run")
	resultID := testID(t, "analysis-result")

	_, err := store.pool.Exec(ctx, `
		INSERT INTO analysis_runs (id, started_at, timeframe, status)
		VALUES ($1, $2, '1h', 'completed')
	`, runID, time.Now().UTC())
	if err != nil {
		t.Fatalf("seed analysis_runs: %v", err)
	}
	// Register parent cleanup first: t.Cleanup runs in LIFO order.
	t.Cleanup(func() { deleteAnalysisRunForTest(t, store, runID) })

	_, err = store.pool.Exec(ctx, `
		INSERT INTO analysis_results (id, run_id, agent_type, asset, indicators, narrative, created_at)
		VALUES ($1, $2, 'technical', 'BTC', '{"trend":"bullish"}', 'uptrend', $3)
	`, resultID, runID, time.Now().UTC())
	if err != nil {
		t.Fatalf("seed analysis_results: %v", err)
	}
	t.Cleanup(func() { deleteAnalysisResultForTest(t, store, resultID) })

	detail, err := store.AnalysisRunDetail(ctx, runID)
	if err != nil {
		t.Fatalf("AnalysisRunDetail: %v", err)
	}
	if detail.Run.ID != runID {
		t.Errorf("Run.ID = %q, want %q", detail.Run.ID, runID)
	}
	if len(detail.Results) != 1 || detail.Results[0].Narrative != "uptrend" {
		t.Errorf("Results = %+v, want one result with narrative uptrend", detail.Results)
	}
	if detail.Results[0].Indicators["trend"] != "bullish" {
		t.Errorf("Results[0].Indicators[trend] = %v, want bullish", detail.Results[0].Indicators["trend"])
	}
}

func deleteAnalysisResultForTest(t *testing.T, store *Store, id string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM analysis_results WHERE id = $1`, id); err != nil {
		t.Errorf("cleanup analysis result %s: %v", id, err)
	}
}

func deleteAnalysisRunForTest(t *testing.T, store *Store, id string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM analysis_runs WHERE id = $1`, id); err != nil {
		t.Errorf("cleanup analysis run %s: %v", id, err)
	}
}
