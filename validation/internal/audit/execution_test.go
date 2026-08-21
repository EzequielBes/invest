package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"validation/internal/storage"
)

func TestExecution_AuditsOnlyCompleteValidFills(t *testing.T) {
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

	prefix := "validation-execution-audit-" + uuid.NewString()
	hypothesisID := prefix + "-hypothesis"
	executionIDs := make([]string, 0, 4)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		for _, id := range executionIDs {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM executions WHERE id = $1`, id)
		}
		deleteValidationRunsForHypothesis(cleanupCtx, pool, hypothesisID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM validation_hypotheses WHERE id = $1`, hypothesisID)
	})
	if _, err := store.CreateHypothesis(ctx, storage.Hypothesis{
		ID: hypothesisID, Description: "Audit real execution fills", Universe: "BTCUSDT", Horizon: "1h",
		CostPolicy: "reported exchange fills", PrimaryMetrics: []string{"realized_slippage_bps"}, AvailabilityRule: "execution record must exist before audit",
	}); err != nil {
		t.Fatalf("CreateHypothesis: %v", err)
	}

	for _, test := range []struct {
		name            string
		status          string
		price           float64
		filledPrice     float64
		filledQuantity  float64
		wantRunStatus   string
		wantFindingRule string
		wantSlippage    bool
	}{
		{name: "filled", status: "filled", price: 100, filledPrice: 101, filledQuantity: 1, wantRunStatus: "completed", wantSlippage: true},
		{name: "partial", status: "partial", price: 100, filledPrice: 101, filledQuantity: 0.5, wantRunStatus: "inconclusive", wantFindingRule: "partial_fill"},
		{name: "cancelled", status: "cancelled", price: 100, filledPrice: 0, filledQuantity: 0, wantRunStatus: "inconclusive", wantFindingRule: "cancelled_execution"},
		{name: "missing price", status: "filled", price: 100, filledPrice: 0, filledQuantity: 1, wantRunStatus: "inconclusive", wantFindingRule: "missing_fill_price"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executionID := prefix + "-row-" + uuid.NewString()
			clientOrderID := prefix + "-client-" + uuid.NewString()
			executionIDs = append(executionIDs, executionID)
			if _, err := pool.Exec(ctx, `
				INSERT INTO executions (id, asset, side, requested_quantity, price, order_id, client_order_id, status, filled_quantity, filled_price, created_at)
				VALUES ($1, 'BTCUSDT', 'buy', 1, $2, 'exchange-order', $3, $4, $5, $6, $7)`,
				executionID, test.price, clientOrderID, test.status, test.filledQuantity, test.filledPrice, time.Now().UTC()); err != nil {
				t.Fatalf("insert execution: %v", err)
			}

			run, err := Execution(ctx, store, ExecutionInput{
				HypothesisID: hypothesisID, ClientOrderID: clientOrderID, Config: json.RawMessage(`{"source":"integration-test"}`), Splits: validSplits(time.Now().UTC()),
			})
			if err != nil {
				t.Fatalf("Execution: %v", err)
			}
			if run.Status != test.wantRunStatus {
				t.Fatalf("run status = %q, want %q", run.Status, test.wantRunStatus)
			}

			var metricCount int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM validation_metrics WHERE validation_run_id = $1 AND name = 'realized_slippage_bps'`, run.ID).Scan(&metricCount); err != nil {
				t.Fatalf("count slippage metrics: %v", err)
			}
			if got := metricCount == 1; got != test.wantSlippage {
				t.Errorf("slippage saved = %t, want %t", got, test.wantSlippage)
			}
			if test.wantFindingRule != "" {
				var findingCount int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM validation_findings WHERE validation_run_id = $1 AND rule = $2`, run.ID, test.wantFindingRule).Scan(&findingCount); err != nil {
					t.Fatalf("count findings: %v", err)
				}
				if findingCount != 1 {
					t.Errorf("%s findings = %d, want 1", test.wantFindingRule, findingCount)
				}
			}
		})
	}
}

func TestExecution_MissingClientOrderIDIsFinding(t *testing.T) {
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

	hypothesisID := "validation-execution-audit-" + uuid.NewString()
	t.Cleanup(func() {
		deleteValidationRunsForHypothesis(context.Background(), pool, hypothesisID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM validation_hypotheses WHERE id = $1`, hypothesisID)
	})
	if _, err := store.CreateHypothesis(ctx, storage.Hypothesis{
		ID: hypothesisID, Description: "Audit missing execution", Universe: "BTCUSDT", Horizon: "1h",
		CostPolicy: "reported exchange fills", PrimaryMetrics: []string{"realized_slippage_bps"}, AvailabilityRule: "execution record must exist before audit",
	}); err != nil {
		t.Fatalf("CreateHypothesis: %v", err)
	}

	run, err := Execution(ctx, store, ExecutionInput{
		HypothesisID: hypothesisID, ClientOrderID: "missing-" + uuid.NewString(), Config: json.RawMessage(`{"source":"integration-test"}`), Splits: validSplits(time.Now().UTC()),
	})
	if err != nil {
		t.Fatalf("Execution: %v", err)
	}
	if run.Status != "inconclusive" {
		t.Fatalf("run status = %q, want inconclusive", run.Status)
	}
	findings, err := store.Findings(ctx, run.ID)
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 1 || findings[0].Rule != "missing_execution" {
		t.Errorf("findings = %+v, want missing_execution", findings)
	}
}

func TestExecution_DuplicateClientOrderIDIsInconclusive(t *testing.T) {
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

	prefix := "validation-duplicate-execution-" + uuid.NewString()
	hypothesisID, clientOrderID := prefix+"-hypothesis", prefix+"-client"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM executions WHERE client_order_id = $1`, clientOrderID)
		deleteValidationRunsForHypothesis(context.Background(), pool, hypothesisID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM validation_hypotheses WHERE id = $1`, hypothesisID)
	})
	if _, err := store.CreateHypothesis(ctx, storage.Hypothesis{ID: hypothesisID, Description: "Duplicate execution audit", Universe: "BTCUSDT", Horizon: "1h", CostPolicy: "reported fills", PrimaryMetrics: []string{"realized_slippage_bps"}, AvailabilityRule: "execution record must exist"}); err != nil {
		t.Fatalf("CreateHypothesis: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO executions (id, asset, side, requested_quantity, price, order_id, client_order_id, status, filled_quantity, filled_price, created_at) VALUES ($1, 'BTCUSDT', 'buy', 1, 100, $2, $3, 'filled', 1, 101, $4)`, fmt.Sprintf("%s-%d", prefix, i), fmt.Sprintf("%s-order-%d", prefix, i), clientOrderID, time.Now().UTC()); err != nil {
			t.Fatalf("insert execution: %v", err)
		}
	}
	run, err := Execution(ctx, store, ExecutionInput{HypothesisID: hypothesisID, ClientOrderID: clientOrderID, Config: json.RawMessage(`{"source":"integration-test"}`), Splits: validSplits(time.Now().UTC())})
	if err != nil {
		t.Fatalf("Execution: %v", err)
	}
	if run.Status != "inconclusive" {
		t.Fatalf("status = %q, want inconclusive", run.Status)
	}
	findings, err := store.Findings(ctx, run.ID)
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 1 || findings[0].Rule != "duplicate_client_order_id" {
		t.Errorf("findings = %+v, want duplicate_client_order_id", findings)
	}
}

func deleteValidationRunsForHypothesis(ctx context.Context, pool *pgxpool.Pool, hypothesisID string) {
	_, _ = pool.Exec(ctx, `DELETE FROM validation_metrics WHERE validation_run_id IN (SELECT id FROM validation_runs WHERE hypothesis_id = $1)`, hypothesisID)
	_, _ = pool.Exec(ctx, `DELETE FROM validation_findings WHERE validation_run_id IN (SELECT id FROM validation_runs WHERE hypothesis_id = $1)`, hypothesisID)
	_, _ = pool.Exec(ctx, `DELETE FROM validation_splits WHERE validation_run_id IN (SELECT id FROM validation_runs WHERE hypothesis_id = $1)`, hypothesisID)
	_, _ = pool.Exec(ctx, `DELETE FROM validation_runs WHERE hypothesis_id = $1`, hypothesisID)
}
