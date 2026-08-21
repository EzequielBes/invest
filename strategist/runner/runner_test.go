package runner

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"execution/executor"
	"strategist/internal/storage"
)

type fakeExecutor struct {
	cash  float64
	calls int
}

func (f *fakeExecutor) FetchPortfolio(context.Context) (float64, map[string]float64, error) {
	return f.cash, nil, nil
}
func (f *fakeExecutor) Execute(context.Context, string, risk.Side, float64, float64, string) (executor.Outcome, error) {
	f.calls++
	return executor.Outcome{}, nil
}

func TestValidateRejectsInvalidIntentAndTarget(t *testing.T) {
	strategy := StrategyContext{AnalysisRunID: "run", Rankings: []Ranking{{Asset: "BTC", Rank: 1}}}
	if err := validate(strategy, []Intent{{ID: "intent", Asset: "ETH", Side: "buy"}}, []Target{{ID: "paper", Executor: &fakeExecutor{}}}); err == nil {
		t.Fatal("unranked intent accepted")
	}
	if err := validate(strategy, []Intent{{ID: "intent", Asset: "BTC", Side: "buy"}}, []Target{{ID: "paper"}}); err == nil {
		t.Fatal("target without executor accepted")
	}
}

func TestApplyIntentsIsIdempotentPerTarget(t *testing.T) {
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
	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(riskStore.Close)

	runID, asset := uuid.NewString(), "TESTINTENTIDEMPOTENT"
	execSQL(t, store, `INSERT INTO analysis_runs (id, started_at, timeframe, status) VALUES ($1, now(), '1m', 'completed')`, runID)
	execSQL(t, store, `INSERT INTO analysis_rankings (run_id, asset, rank, composite_score, opportunity_score_raw, thesis, confidence, evidence, computed_at) VALUES ($1, $2, 1, 1, 1, 'test', 1, '[]', now())`, runID, asset)
	for i := 59; i >= 0; i-- {
		execSQL(t, store, `INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume) VALUES ('binance', $1, '1m', now() - ($2 * interval '1 minute'), 100, 100, 100, 100, 2000)`, asset, i)
	}
	t.Cleanup(func() {
		_ = storage.ExecForTest(ctx, store, `DELETE FROM strategist_intent_applications WHERE analysis_run_id = $1`, runID)
		_ = storage.ExecForTest(ctx, store, `DELETE FROM analysis_rankings WHERE run_id = $1`, runID)
		_ = storage.ExecForTest(ctx, store, `DELETE FROM analysis_runs WHERE id = $1`, runID)
		_ = storage.ExecForTest(ctx, store, `DELETE FROM candles WHERE exchange = 'binance' AND symbol = $1`, asset)
	})

	paper := &fakeExecutor{cash: 10000}
	testnet := &fakeExecutor{cash: 5000}
	strategy := StrategyContext{AnalysisRunID: runID, Rankings: []Ranking{{Asset: asset, Rank: 1}}}
	intents := []Intent{{ID: "stable-intent", Asset: asset, Side: "buy", Confidence: 0.8, SizingPct: 0.1}}
	targets := []Target{{ID: "paper", Executor: paper}, {ID: "testnet", Executor: testnet}}
	applications, err := ApplyIntentsWithDSN(ctx, dsn, riskStore, strategy, intents, targets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyIntentsWithDSN(ctx, dsn, riskStore, strategy, intents, targets); err != nil {
		t.Fatal(err)
	}
	if paper.calls != 1 || testnet.calls != 1 {
		t.Fatalf("executor calls = paper %d, testnet %d; want one each", paper.calls, testnet.calls)
	}
	if len(applications) != 2 || applications[0].Quantity == applications[1].Quantity {
		t.Fatalf("applications = %+v, want independently sized targets", applications)
	}
}

func execSQL(t *testing.T, store *storage.Store, query string, args ...any) {
	t.Helper()
	if err := storage.ExecForTest(context.Background(), store, query, args...); err != nil {
		t.Fatal(err)
	}
}
