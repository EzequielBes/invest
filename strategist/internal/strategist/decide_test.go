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

type fakeLLMClient struct {
	decision llm.Decision
	err      error
}

func (f *fakeLLMClient) Decide(context.Context, string, string) (llm.Decision, error) {
	return f.decision, f.err
}

func validPerAsset() []storage.AgentResult {
	return []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC", Narrative: "n1"},
		{AgentType: "derivatives", Asset: "BTC", Narrative: "n2"},
		{AgentType: "news", Asset: "BTC", Narrative: "n3"},
	}
}

func TestDecide_HoldNeverCallsRiskEvaluate(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "hold", Rationale: "wait and see"}}

	// riskStore is nil on purpose: a hold must return before touching it —
	// passing nil here turns any accidental risk.Evaluate call into an
	// immediate nil-pointer panic, which is exactly the assertion we want.
	outcome, err := Decide(context.Background(), nil, client, "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Risk != nil || outcome.RiskErr != nil {
		t.Errorf("outcome = %+v, want no risk evaluation for a hold", outcome)
	}
	if outcome.Quantity != 0 || outcome.Value != 0 {
		t.Errorf("outcome = %+v, want zero quantity/value for a hold", outcome)
	}
}

func TestDecide_LLMFailureReturnsError(t *testing.T) {
	client := &fakeLLMClient{err: errors.New("rate limited")}

	_, err := Decide(context.Background(), nil, client, "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err == nil {
		t.Fatal("expected an error when the LLM call fails, got nil")
	}
}

func TestDecide_MissingAnalysisDataReturnsErrorBeforeCallingLLM(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "hold"}}
	incomplete := []storage.AgentResult{{AgentType: "technical", Asset: "BTC", Narrative: "n1"}}

	if _, err := Decide(context.Background(), nil, client, "BTC", incomplete, storage.AgentResult{}, riskPortfolioState(), 10000, 100); err == nil {
		t.Fatal("expected an error for incomplete analysis data, got nil")
	}
}

func TestDecide_RiskEvaluationFailureIsReportedInOutcome(t *testing.T) {
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
	outcome, err := Decide(context.Background(), riskStore, client, "TESTASSETRISKOUTCOME", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Risk != nil || outcome.RiskErr == nil {
		t.Fatalf("outcome = %+v, want Risk=nil and RiskErr set", outcome)
	}
}

func riskPortfolioState() risk.PortfolioState {
	return risk.PortfolioState{}
}
