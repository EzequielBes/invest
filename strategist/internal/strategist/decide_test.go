// strategist/internal/strategist/decide_test.go
package strategist

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"execution/executor"

	"strategist/internal/llm"
	"strategist/internal/storage"
)

func TestBuildPrompt_MissingAgentIsError(t *testing.T) {
	perAsset := []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC", Narrative: "uptrend"},
		{AgentType: "derivatives", Asset: "BTC", Narrative: "normal funding"},
		// "news" missing.
	}
	if _, err := buildPrompt("BTC", perAsset, storage.AgentResult{}); err == nil {
		t.Fatal("expected an error for a missing agent result, got nil")
	}
}

func TestBuildPrompt_IncludesAllThreeAgentsAndRiskContext(t *testing.T) {
	perAsset := []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC", Indicators: map[string]any{"trend": "bullish"}, Narrative: "uptrend narrative"},
		{AgentType: "derivatives", Asset: "BTC", Narrative: "derivatives narrative"},
		{AgentType: "news", Asset: "BTC", Narrative: "news narrative"},
	}
	riskContext := storage.AgentResult{AgentType: "risk_context", Narrative: "risk narrative"}

	prompt, err := buildPrompt("BTC", perAsset, riskContext)
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}
	for _, want := range []string{"uptrend narrative", "derivatives narrative", "news narrative", "risk narrative", "trend=bullish"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPromptWithRanking_AppendsRelativeContext(t *testing.T) {
	prompt, err := buildPromptWithRanking("BTC", validPerAsset(), storage.AgentResult{}, []storage.Ranking{
		{Asset: "BTC", Rank: 1, CompositeScore: 0.8, Thesis: "bull", Confidence: 0.7},
		{Asset: "ETH", Rank: 2, CompositeScore: 0.6, Thesis: "neutro", Confidence: 0.5},
	})
	if err != nil {
		t.Fatalf("buildPromptWithRanking: %v", err)
	}
	for _, want := range []string{"[ranking]", "Posição do ativo: 1", "1. BTC", "2. ETH"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestDecideWithRanking_WithoutCurrentRankingUsesExistingPrompt(t *testing.T) {
	client := &capturingLLMClient{decision: llm.Decision{Side: "hold"}}
	_, err := DecideWithRanking(context.Background(), nil, client, nil, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, []storage.Ranking{{Asset: "ETH", Rank: 1}}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("DecideWithRanking: %v", err)
	}
	if strings.Contains(client.prompt, "[ranking]") {
		t.Fatalf("fallback prompt unexpectedly contains ranking: %s", client.prompt)
	}
}

type fakeLLMClient struct {
	decision llm.Decision
	err      error
}

type capturingLLMClient struct {
	decision llm.Decision
	prompt   string
}

func (f *capturingLLMClient) Decide(_ context.Context, _ string, prompt string) (llm.Decision, error) {
	f.prompt = prompt
	return f.decision, nil
}

func (f *fakeLLMClient) Decide(context.Context, string, string) (llm.Decision, error) {
	return f.decision, f.err
}

type fakeExecClient struct {
	outcome executor.Outcome
	err     error
	calls   int
}

func (f *fakeExecClient) FetchPortfolio(context.Context) (float64, map[string]float64, error) {
	return 0, nil, nil
}

func (f *fakeExecClient) Execute(context.Context, string, risk.Side, float64, float64, string) (executor.Outcome, error) {
	f.calls++
	return f.outcome, f.err
}

func validPerAsset() []storage.AgentResult {
	return []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC", Narrative: "n1"},
		{AgentType: "derivatives", Asset: "BTC", Narrative: "n2"},
		{AgentType: "news", Asset: "BTC", Narrative: "n3"},
	}
}

func TestDecide_HoldNeverCallsRiskEvaluateOrExecute(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "hold", Rationale: "wait and see"}}

	// riskStore and execClient are nil on purpose: a hold must return
	// before touching either — passing nil turns any accidental call into
	// an immediate nil-pointer panic, which is exactly the assertion we
	// want.
	outcome, err := Decide(context.Background(), nil, client, nil, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Risk != nil || outcome.RiskErr != nil {
		t.Errorf("outcome = %+v, want no risk evaluation for a hold", outcome)
	}
	if outcome.Execution != nil || outcome.ExecutionErr != nil {
		t.Errorf("outcome = %+v, want no execution for a hold", outcome)
	}
	if outcome.Quantity != 0 || outcome.Value != 0 {
		t.Errorf("outcome = %+v, want zero quantity/value for a hold", outcome)
	}
}

func TestDecide_LLMFailureReturnsError(t *testing.T) {
	client := &fakeLLMClient{err: errors.New("rate limited")}

	_, err := Decide(context.Background(), nil, client, nil, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err == nil {
		t.Fatal("expected an error when the LLM call fails, got nil")
	}
}

func TestDecide_MissingAnalysisDataReturnsErrorBeforeCallingLLM(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "hold"}}
	incomplete := []storage.AgentResult{{AgentType: "technical", Asset: "BTC", Narrative: "n1"}}

	if _, err := Decide(context.Background(), nil, client, nil, "decision-1", "BTC", incomplete, storage.AgentResult{}, riskPortfolioState(), 10000, 100); err == nil {
		t.Fatal("expected an error for incomplete analysis data, got nil")
	}
}

func TestDecide_RiskEvaluationFailureIsReportedInOutcomeAndSkipsExecution(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	riskStore, err := riskstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	// Closing the concrete store is the controlled failure path available to
	// this package: risk.Evaluate accepts *storage.Store, not an interface.
	riskStore.Close()

	client := &fakeLLMClient{decision: llm.Decision{Side: "buy", SizingPct: 0.1, Rationale: "persist despite risk failure"}}
	// execClient is nil on purpose: a risk.Evaluate failure must return
	// before ever calling Execute.
	outcome, err := Decide(context.Background(), riskStore, client, nil, "decision-1", "TESTASSETRISKOUTCOME", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Risk != nil || outcome.RiskErr == nil {
		t.Fatalf("outcome = %+v, want Risk=nil and RiskErr set", outcome)
	}
	if outcome.Execution != nil || outcome.ExecutionErr != nil {
		t.Errorf("outcome = %+v, want no execution attempted after a risk.Evaluate failure", outcome)
	}
}

func TestDecide_SellClampsToHeldQuantity(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	riskStore, err := riskstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	// Closed on purpose, same reasoning as the risk-evaluation-failure
	// test above: outcome.Quantity is computed (and clamped) before
	// risk.Evaluate runs, so a controlled risk.Evaluate failure afterward
	// doesn't affect what's being asserted here.
	riskStore.Close()

	// sizing_pct=0.5 against a 10000 portfolio at price=100 would propose
	// selling 50 units — but only 2 are actually held.
	client := &fakeLLMClient{decision: llm.Decision{Side: "sell", SizingPct: 0.5, Rationale: "take profit"}}
	portfolio := riskPortfolioState()
	portfolio.Positions = map[string]risk.Position{"BTC": {Asset: "BTC", Quantity: 2, Value: 200}}

	outcome, err := Decide(context.Background(), riskStore, client, nil, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, portfolio, 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Quantity != 2 {
		t.Errorf("Quantity = %v, want 2 (clamped to the held position)", outcome.Quantity)
	}
}

func TestDecide_SellWithNoPositionClampsToZeroAndSkipsEverything(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "sell", SizingPct: 0.5, Rationale: "take profit"}}
	execClient := &fakeExecClient{}

	// riskStore is nil on purpose: a clamp to zero must skip risk.Evaluate
	// and Execute entirely — nothing to propose, nothing to trade.
	outcome, err := Decide(context.Background(), nil, client, execClient, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Quantity != 0 {
		t.Errorf("Quantity = %v, want 0 (no position held)", outcome.Quantity)
	}
	if execClient.calls != 0 {
		t.Errorf("execClient.calls = %d, want 0", execClient.calls)
	}
}

func riskPortfolioState() risk.PortfolioState {
	return risk.PortfolioState{}
}
