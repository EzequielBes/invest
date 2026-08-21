package audit

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"validation/internal/storage"
	validation "validation/internal/validation"
)

func TestBacktest_PersistsObservationalMetrics(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	store, err := storage.New(ctx, dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	id := "validation-audit-test-" + uuid.NewString()
	backtestID := id + "-backtest"
	hypothesisID := id + "-hypothesis"
	var validationRunID string
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if validationRunID != "" {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM validation_metrics WHERE validation_run_id = $1`, validationRunID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM validation_findings WHERE validation_run_id = $1`, validationRunID)
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM validation_runs WHERE id = $1`, validationRunID)
		}
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM validation_hypotheses WHERE id = $1`, hypothesisID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM backtest_equity_curve WHERE run_id = $1`, backtestID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM backtest_trades WHERE run_id = $1`, backtestID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM backtest_runs WHERE id = $1`, backtestID)
	})

	if _, err := store.CreateHypothesis(ctx, storage.Hypothesis{
		ID: hypothesisID, Description: "Audit stored simulation output", Universe: "BTCUSDT", Horizon: "1h",
		CostPolicy: "fee declared by simulation", PrimaryMetrics: []string{"total_return_pct"}, AvailabilityRule: "inputs precede decisions",
	}); err != nil {
		t.Fatalf("CreateHypothesis: %v", err)
	}

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO backtest_runs (id, strategy_name, period_start, period_end, timeframes, driving_timeframe, initial_cash, fee_pct, status, started_at, ended_at)
		VALUES ($1, 'fixture', $2, $3, ARRAY['1h'], '1h', 100, 0.001, 'completed', $2, $3)`, backtestID, start, start.Add(3*time.Hour)); err != nil {
		t.Fatalf("insert backtest run: %v", err)
	}
	for _, trade := range []struct {
		quantity, price float64
		allowed         bool
	}{{1, 100, true}, {2, 110, true}, {1, 0, false}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO backtest_trades (run_id, ts, asset, side, quantity, price, fee, allowed)
			VALUES ($1, $2, 'BTCUSDT', 'buy', $3, $4, 0, $5)`, backtestID, start, trade.quantity, trade.price, trade.allowed); err != nil {
			t.Fatalf("insert backtest trade: %v", err)
		}
	}
	for i, equity := range []float64{100, 80, 120} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO backtest_equity_curve (run_id, ts, cash, positions_value, total_equity)
			VALUES ($1, $2, $3, 0, $3)`, backtestID, start.Add(time.Duration(i)*time.Hour), equity); err != nil {
			t.Fatalf("insert equity point: %v", err)
		}
	}

	run, err := Backtest(ctx, store, BacktestInput{
		HypothesisID: hypothesisID, BacktestRunID: backtestID, Config: json.RawMessage(`{"source":"integration-test"}`), Splits: validSplits(start),
	})
	if err != nil {
		t.Fatalf("Backtest: %v", err)
	}
	validationRunID = run.ID
	if run.Status != "completed" {
		t.Fatalf("run status = %q, want completed", run.Status)
	}

	values := map[string]float64{}
	rows, err := pool.Query(ctx, `SELECT name, value FROM validation_metrics WHERE validation_run_id = $1`, run.ID)
	if err != nil {
		t.Fatalf("query metrics: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var value float64
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("scan metric: %v", err)
		}
		values[name] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate metrics: %v", err)
	}
	for name, want := range map[string]float64{
		"total_return_pct": 20, "max_drawdown_pct": 20, "max_recovery_duration_seconds": 7200,
		"turnover_pct": 320, "trade_count": 2,
	} {
		if got := values[name]; math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	var evidenceRaw []byte
	if err := pool.QueryRow(ctx, `SELECT evidence FROM validation_metrics WHERE validation_run_id = $1 AND name = 'dataset_row_count'`, run.ID).Scan(&evidenceRaw); err != nil {
		t.Fatalf("load dataset evidence: %v", err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(evidenceRaw, &evidence); err != nil {
		t.Fatalf("decode dataset evidence: %v", err)
	}
	if evidence["dataset_fingerprint"] == "" || evidence["equity_point_count"] != float64(3) || evidence["trade_count"] != float64(3) {
		t.Errorf("dataset evidence = %#v, want fingerprint and row counts", evidence)
	}
	var simulationWrites int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM backtest_results WHERE run_id = $1`, backtestID).Scan(&simulationWrites); err != nil {
		t.Fatalf("count simulation results: %v", err)
	}
	if simulationWrites != 0 {
		t.Errorf("backtest_results rows = %d, want audit to leave it unchanged", simulationWrites)
	}
}

func TestBacktest_RequiresValidTemporalSplits(t *testing.T) {
	_, err := Backtest(context.Background(), nil, BacktestInput{HypothesisID: "hypothesis", BacktestRunID: "backtest", Config: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("Backtest without splits succeeded")
	}

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	_, err = Backtest(context.Background(), nil, BacktestInput{
		HypothesisID: "hypothesis", BacktestRunID: "backtest", Config: json.RawMessage(`{}`),
		Splits: []validation.Split{
			{Kind: validation.SplitTrain, Start: start, End: start.Add(2 * time.Hour)},
			{Kind: validation.SplitValidation, Start: start.Add(time.Hour), End: start.Add(3 * time.Hour)},
			{Kind: validation.SplitHoldout, Start: start.Add(3 * time.Hour), End: start.Add(4 * time.Hour)},
		},
	})
	if err == nil {
		t.Fatal("Backtest with overlapping splits succeeded")
	}
}

func validSplits(start time.Time) []validation.Split {
	return []validation.Split{
		{Kind: validation.SplitTrain, Start: start, End: start.Add(time.Hour)},
		{Kind: validation.SplitValidation, Start: start.Add(time.Hour), End: start.Add(2 * time.Hour)},
		{Kind: validation.SplitHoldout, Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)},
	}
}

func TestBacktestDatasetEvidenceIsDeterministic(t *testing.T) {
	pointTime := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	backtest := storage.BacktestRun{ID: "backtest", Status: "completed", FeePct: 0.001, StartedAt: pointTime}
	points := []storage.BacktestEquityPoint{{Time: pointTime, TotalEquity: 100}}
	trades := []storage.BacktestTrade{{Time: pointTime, Quantity: 1, Price: 100, Allowed: true}}
	first, err := backtestDatasetEvidence(backtest, points, trades)
	if err != nil {
		t.Fatalf("first fingerprint: %v", err)
	}
	second, err := backtestDatasetEvidence(backtest, points, trades)
	if err != nil {
		t.Fatalf("second fingerprint: %v", err)
	}
	if first["dataset_fingerprint"] != second["dataset_fingerprint"] {
		t.Errorf("fingerprints differ: %q and %q", first["dataset_fingerprint"], second["dataset_fingerprint"])
	}
}
