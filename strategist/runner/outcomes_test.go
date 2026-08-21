package runner

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"strategist/internal/storage"
)

func TestOutcomeAdjustsDirection(t *testing.T) {
	for _, test := range []struct {
		side        string
		entry, exit float64
		want        float64
		correct     bool
	}{
		{"buy", 100, 110, 10, true},
		{"sell", 100, 90, 10, true},
		{"buy", 100, 100, 0, false},
	} {
		got, correct := outcome(test.side, test.entry, test.exit)
		if got != test.want || correct != test.correct {
			t.Errorf("outcome(%q, %v, %v) = %v, %v; want %v, %v", test.side, test.entry, test.exit, got, correct, test.want, test.correct)
		}
	}
}

func TestEvaluateOutcomesIsIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	ctx := context.Background()
	store, err := storage.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	runID, asset := uuid.NewString(), "TESTOUTCOME"
	intentID := "test-outcome"
	createdAt := time.Now().UTC().Add(-25 * time.Hour).Truncate(time.Minute)
	execSQL(t, store, `INSERT INTO strategist_intent_applications (intent_id, target_id, analysis_run_id, asset, side, confidence, sizing_pct, rationale, execution_status, created_at) VALUES ($1, 'paper', $2, $3, 'buy', 1, 0.1, '', 'filled', $4)`, intentID, runID, asset, createdAt)
	for _, point := range []struct {
		ts    time.Time
		close float64
	}{
		{createdAt, 100},
		{createdAt.Add(time.Hour), 110},
		{createdAt.Add(4 * time.Hour), 90},
		{createdAt.Add(24 * time.Hour), 120},
	} {
		execSQL(t, store, `INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume) VALUES ('binance', $1, '1m', $2, $3, $3, $3, $3, 1)`, asset, point.ts, point.close)
	}
	t.Cleanup(func() {
		_ = storage.ExecForTest(ctx, store, `DELETE FROM strategist_intent_outcomes WHERE analysis_run_id = $1`, runID)
		_ = storage.ExecForTest(ctx, store, `DELETE FROM strategist_intent_applications WHERE analysis_run_id = $1`, runID)
		_ = storage.ExecForTest(ctx, store, `DELETE FROM candles WHERE exchange = 'binance' AND symbol = $1`, asset)
	})

	count, err := EvaluateOutcomesWithDSN(ctx, dsn)
	if err != nil || count != 3 {
		t.Fatalf("first evaluation = %d, %v; want 3, nil", count, err)
	}
	count, err = EvaluateOutcomesWithDSN(ctx, dsn)
	if err != nil || count != 0 {
		t.Fatalf("second evaluation = %d, %v; want 0, nil", count, err)
	}
}
