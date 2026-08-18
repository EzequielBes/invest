// strategist/cmd/strategist/main_test.go
package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"

	riskstorage "risk-engine/storage"

	"strategist/internal/llm"
	"strategist/internal/storage"
)

type fakeLLMClient struct {
	decision llm.Decision
}

func (f *fakeLLMClient) Decide(context.Context, string, string) (llm.Decision, error) {
	return f.decision, nil
}

func testStores(t *testing.T) (*storage.Store, *riskstorage.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration tests")
	}
	ctx := context.Background()
	store, err := storage.New(ctx, dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(store.Close)
	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	t.Cleanup(riskStore.Close)
	return store, riskStore
}

// seedAnalysisRun writes a minimal analysis_runs + analysis_results
// fixture directly via SQL (this module never writes those tables in
// production — they belong to the analysis module — so a test-only
// insert here is the right way to set up a fixture, not a shortcut
// around a missing helper).
func seedAnalysisRun(t *testing.T, store *storage.Store, runID, asset string, includeAllThreeAgents bool) {
	t.Helper()
	execSQL(t, store, `INSERT INTO analysis_runs (id, started_at, timeframe, status) VALUES ($1, now(), '1h', 'completed')`, runID)
	t.Cleanup(func() {
		execSQLIgnoreError(store, `DELETE FROM analysis_results WHERE run_id = $1`, runID)
		execSQLIgnoreError(store, `DELETE FROM analysis_runs WHERE id = $1`, runID)
	})

	agentTypes := []string{"technical", "derivatives", "news"}
	if !includeAllThreeAgents {
		agentTypes = agentTypes[:1]
	}
	for _, agentType := range agentTypes {
		execSQL(t, store, `
			INSERT INTO analysis_results (id, run_id, agent_type, asset, indicators, narrative, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
		`, uuid.NewString(), runID, agentType, asset, jsonObj(t, map[string]any{"seeded": true}), agentType+" narrative for "+asset)
	}
	execSQL(t, store, `
		INSERT INTO analysis_results (id, run_id, agent_type, asset, indicators, narrative, created_at)
		VALUES ($1, $2, 'risk_context', '', $3, 'risk context narrative', now())
	`, uuid.NewString(), runID, jsonObj(t, map[string]any{"risk_status": "normal"}))
}

// seedCandle inserts one candle at ts=now() and registers its own cleanup.
// Always call this with a clearly-fake symbol (see the TESTASSET*
// constants below), never a real one like "BTC" — this candle would
// otherwise become the *latest* row for that symbol (ts=now()) in the
// shared dev TimescaleDB for as long as the test is running, ahead of any
// real collected data, and could confuse a concurrent manual run of
// cmd/analysis or cmd/strategist against that symbol.
func seedCandle(t *testing.T, store *storage.Store, symbol string, price float64) {
	t.Helper()
	execSQL(t, store, `
		INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
		VALUES ('binance', $1, '1h', now(), $2, $2, $2, $2, 100)
		ON CONFLICT (exchange, symbol, timeframe, ts) DO NOTHING
	`, symbol, price)
	t.Cleanup(func() {
		execSQLIgnoreError(store, `DELETE FROM candles WHERE exchange = 'binance' AND symbol = $1`, symbol)
	})
}

// Fake asset symbols for these tests — never real ones (see seedCandle).
const (
	testAssetBuy        = "TESTASSETBUY"
	testAssetHold       = "TESTASSETHOLD"
	testAssetIncomplete = "TESTASSETINCOMPLETE"
)

// execSQL is a tiny helper so fixtures above can run arbitrary SQL through
// the *storage.Store without a dedicated exported method — storage.Store
// only exposes the reads/writes production code needs, not a generic
// query escape hatch, so tests reach the pool through this instead.
func execSQL(t *testing.T, store *storage.Store, sql string, args ...any) {
	t.Helper()
	if err := storage.ExecForTest(context.Background(), store, sql, args...); err != nil {
		t.Fatalf("seed SQL %q: %v", sql, err)
	}
}

// execSQLIgnoreError is for best-effort cleanup in t.Cleanup callbacks —
// mirrors analysis/internal/storage/runs.go's DeleteRunForTest, which
// discards its Exec errors the same way, since a cleanup failure
// shouldn't mask the actual test failure it ran after.
func execSQLIgnoreError(store *storage.Store, sql string, args ...any) {
	_ = storage.ExecForTest(context.Background(), store, sql, args...)
}

func jsonObj(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestRun_BuyDecisionIsValidatedAndPersisted(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID := uuid.NewString()
	seedAnalysisRun(t, store, runID, testAssetBuy, true)
	seedCandle(t, store, testAssetBuy, 50000)
	t.Cleanup(func() {
		store.DeleteDecisionsForRunForTest(ctx, runID)
	})

	client := &fakeLLMClient{decision: llm.Decision{Side: "buy", Confidence: 0.8, SizingPct: 0.1, Rationale: "uptrend"}}
	if err := Run(ctx, store, riskStore, client, runID, []string{testAssetBuy}, "1h", 10000, nil, 0, 0, 0, 0); err != nil {
		t.Fatalf("Run: %v", err)
	}

	decisions, err := store.DecisionsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("DecisionsForTest: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	d := decisions[0]
	if d.Side != "buy" || d.Asset != testAssetBuy {
		t.Errorf("decision = %+v, want side=buy asset=%s", d, testAssetBuy)
	}
	if d.RiskAllowed == nil {
		t.Error("RiskAllowed is nil, want risk.Evaluate to have run and recorded a verdict")
	}
	wantQuantity := 0.1 * 10000 / 50000
	if d.ProposedQuantity != wantQuantity {
		t.Errorf("ProposedQuantity = %v, want %v (sizing_pct * portfolio value / price)", d.ProposedQuantity, wantQuantity)
	}
}

func TestRun_HoldSkipsRiskEvaluateButIsPersisted(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID := uuid.NewString()
	seedAnalysisRun(t, store, runID, testAssetHold, true)
	seedCandle(t, store, testAssetHold, 3000)
	t.Cleanup(func() {
		store.DeleteDecisionsForRunForTest(ctx, runID)
	})

	client := &fakeLLMClient{decision: llm.Decision{Side: "hold", Rationale: "no clear signal"}}
	if err := Run(ctx, store, riskStore, client, runID, []string{testAssetHold}, "1h", 10000, nil, 0, 0, 0, 0); err != nil {
		t.Fatalf("Run: %v", err)
	}

	decisions, err := store.DecisionsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("DecisionsForTest: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	if decisions[0].Side != "hold" || decisions[0].RiskAllowed != nil {
		t.Errorf("decision = %+v, want side=hold and RiskAllowed=nil", decisions[0])
	}
}

func TestRun_IncompleteAnalysisSkipsAssetWithoutPersisting(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID := uuid.NewString()
	seedAnalysisRun(t, store, runID, testAssetIncomplete, false) // only "technical" seeded
	seedCandle(t, store, testAssetIncomplete, 150)
	t.Cleanup(func() {
		store.DeleteDecisionsForRunForTest(ctx, runID)
	})

	client := &fakeLLMClient{decision: llm.Decision{Side: "hold"}}
	if err := Run(ctx, store, riskStore, client, runID, []string{testAssetIncomplete}, "1h", 10000, nil, 0, 0, 0, 0); err != nil {
		t.Fatalf("Run: %v", err)
	}

	decisions, err := store.DecisionsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("DecisionsForTest: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("len(decisions) = %d, want 0 (incomplete analysis data must not produce an implicit decision)", len(decisions))
	}
}

func TestRun_MissingHeldPositionPriceFailsTheWholeRun(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID := uuid.NewString()
	seedAnalysisRun(t, store, runID, testAssetBuy, true)
	seedCandle(t, store, testAssetBuy, 50000)
	t.Cleanup(func() {
		store.DeleteDecisionsForRunForTest(ctx, runID)
	})

	client := &fakeLLMClient{decision: llm.Decision{Side: "hold"}}
	positions := map[string]float64{"TESTASSETNOPRICE": 1}
	err := Run(ctx, store, riskStore, client, runID, []string{testAssetBuy}, "1h", 10000, positions, 0, 0, 0, 0)
	if err == nil {
		t.Fatal("expected an error when a held position has no price data, got nil")
	}
}
