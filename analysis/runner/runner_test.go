// analysis/runner/runner_test.go
package runner

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	riskstorage "risk-engine/storage"

	"analysis/internal/agents"
	"analysis/internal/storage"
)

type fakeLLMClient struct {
	fail  bool
	calls int
}

func (f *fakeLLMClient) Summarize(context.Context, string, string) (string, error) {
	f.calls++
	if f.fail {
		return "", errFakeLLMFailure
	}
	return "fake narrative", nil
}

var errFakeLLMFailure = fakeErr("fake LLM failure")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

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

func TestRun_AllAgentsSucceed(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	client := &fakeLLMClient{}
	runID, successCount, err := Run(ctx, store, riskStore, client, []string{"NOSUCHASSET"}, nil, "1h", []string{"technical", "risk_context"})
	t.Cleanup(func() { store.DeleteRunForTest(ctx, runID) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if successCount != 2 {
		t.Errorf("successCount = %d, want 2", successCount)
	}
	if client.calls != 2 {
		t.Errorf("LLM calls = %d, want 2 (including insufficient technical data)", client.calls)
	}
	status, err := store.RunStatus(ctx, runID)
	if err != nil {
		t.Fatalf("RunStatus: %v", err)
	}
	if status != "completed" {
		t.Errorf("status = %q, want completed", status)
	}
	count, err := store.ResultCount(ctx, runID)
	if err != nil {
		t.Fatalf("ResultCount: %v", err)
	}
	if count != 2 {
		t.Errorf("ResultCount = %d, want 2", count)
	}
	results, err := store.ResultsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("ResultsForTest: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	byAgent := resultsByAgent(results)
	if result := byAgent["technical"]; result.Asset != "NOSUCHASSET" || result.Narrative == "" {
		t.Errorf("technical result = %+v, want asset and narrative persisted", result)
	}
	if result := byAgent["risk_context"]; result.Asset != "" || result.Narrative == "" {
		t.Errorf("risk-context result = %+v, want portfolio result and narrative persisted", result)
	}
}

func TestRun_PartialLLMFailureStillCompletes(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	client := &selectiveFakeLLMClient{}
	runID, successCount, err := Run(ctx, store, riskStore, client, []string{"NOSUCHASSET"}, nil, "1h", []string{"risk_context", "news"})
	t.Cleanup(func() { store.DeleteRunForTest(ctx, runID) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if successCount != 1 {
		t.Errorf("successCount = %d, want 1", successCount)
	}
	status, err := store.RunStatus(ctx, runID)
	if err != nil {
		t.Fatalf("RunStatus: %v", err)
	}
	if status != "completed" {
		t.Errorf("status = %q, want completed", status)
	}
	count, err := store.ResultCount(ctx, runID)
	if err != nil {
		t.Fatalf("ResultCount: %v", err)
	}
	if count != 2 {
		t.Errorf("ResultCount = %d, want 2", count)
	}
	results, err := store.ResultsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("ResultsForTest: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	byAgent := resultsByAgent(results)
	if result := byAgent["risk_context"]; result.Narrative != "fake narrative" {
		t.Errorf("successful result = %+v, want persisted narrative", result)
	}
	if result := byAgent["news"]; result.Narrative != "" {
		t.Errorf("failed-LLM result = %+v, want indicators row with empty narrative", result)
	}
}

func TestRun_CycleAgentsRunInFixedOrderAndCommitteeFailureIsIsolated(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	client := &committeeFakeLLMClient{}
	runID, successCount, err := Run(ctx, store, riskStore, client, []string{"NOSUCHASSET"}, nil, "1h", []string{"committee", "macro", "news", "risk_context", "derivatives", "technical"})
	t.Cleanup(func() { store.DeleteRunForTest(ctx, runID) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if successCount != 5 {
		t.Errorf("successCount = %d, want 5 (committee failure is isolated)", successCount)
	}
	results, err := store.ResultsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("ResultsForTest: %v", err)
	}
	gotOrder := make([]string, len(results))
	for i, result := range results {
		gotOrder[i] = result.AgentType
	}
	wantOrder := []string{"technical", "derivatives", "news", "risk_context", "macro"}
	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("result order = %v, want %v", gotOrder, wantOrder)
	}
}

type selectiveFakeLLMClient struct{}

func (*selectiveFakeLLMClient) Summarize(_ context.Context, systemPrompt, _ string) (string, error) {
	if strings.Contains(systemPrompt, "notícias") {
		return "", errFakeLLMFailure
	}
	return "fake narrative", nil
}

type committeeFakeLLMClient struct{}

func (*committeeFakeLLMClient) Summarize(_ context.Context, systemPrompt, _ string) (string, error) {
	if strings.Contains(systemPrompt, "chefe de mesa") {
		return "", errFakeLLMFailure
	}
	return "fake narrative", nil
}

func TestRun_AllAgentsFailMarksRunFailed(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID, successCount, err := Run(ctx, store, riskStore, &fakeLLMClient{fail: true}, []string{"NOSUCHASSET"}, nil, "1h", []string{"risk_context"})
	t.Cleanup(func() { store.DeleteRunForTest(ctx, runID) })
	if err == nil {
		t.Fatal("Run: expected error when every agent fails, got nil")
	}
	if successCount != 0 {
		t.Errorf("successCount = %d, want 0", successCount)
	}
	status, err := store.RunStatus(ctx, runID)
	if err != nil {
		t.Fatalf("RunStatus: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	count, err := store.ResultCount(ctx, runID)
	if err != nil {
		t.Fatalf("ResultCount: %v", err)
	}
	if count != 1 {
		t.Errorf("ResultCount = %d, want 1", count)
	}
}

type failingResultSaver struct{ err error }

func (f failingResultSaver) SaveResult(context.Context, storage.Result) error { return f.err }

func TestRecord_SaveFailureIsFatal(t *testing.T) {
	wantErr := errors.New("database unavailable")
	succeeded, err := record(context.Background(), failingResultSaver{err: wantErr}, "run-id", "news", "BTC", agents.Output{Indicators: map[string]int{"article_count": 1}, Narrative: "text"}, nil)
	if succeeded {
		t.Fatal("record succeeded after SaveResult failure")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("record error = %v, want wrapped %v", err, wantErr)
	}
}

func resultsByAgent(results []storage.PersistedResult) map[string]storage.PersistedResult {
	byAgent := make(map[string]storage.PersistedResult, len(results))
	for _, result := range results {
		byAgent[result.AgentType] = result
	}
	return byAgent
}
