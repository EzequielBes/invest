// strategist/internal/strategist/decide.go
package strategist

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"execution/executor"

	"strategist/internal/llm"
	"strategist/internal/storage"
)

const systemPrompt = `Você é um estrategista de investimentos em criptomoedas. Você recebe indicadores técnicos, de derivativos, de notícias e de contexto de risco sobre um ativo, e decide se deve comprar, vender ou manter a posição atual. Nunca sugira sizing_pct acima de 0.25 (25% do portfólio) numa única operação. Responda sempre usando a ferramenta record_decision.`

// Outcome is what deciding on one asset produces. Risk is nil when
// Decision.Side is "hold", when the sizing clamp reduces Quantity to
// <= 0 (nothing to propose), or when risk.Evaluate itself failed —
// RiskErr is set in that last case so the caller can log it while still
// persisting the LLM's decision. Execution is nil whenever Risk is nil,
// whenever risk.Evaluate rejected the proposal, or when the execution
// call itself failed — ExecutionErr is set in that last case, same
// isolated-failure treatment as RiskErr.
type Outcome struct {
	Decision     llm.Decision
	Quantity     float64
	Value        float64
	Risk         *risk.Decision
	RiskErr      error
	Execution    *executor.Outcome
	ExecutionErr error
}

// Decide asks the LLM for a decision about asset from its three per-asset
// analysis results (technical, derivatives, news) plus the shared
// risk-context result, sizes the proposed operation against
// portfolioValue/price (clamped to the actually-held quantity for a
// sell), validates it via risk.Evaluate, and — if approved — executes it
// for real via execClient using decisionID as the exchange's client
// order ID (so a retry of the same decision never places a duplicate
// order).
//
// Returns an error only when there is no decision at all to persist:
// missing analysis data for asset, or an LLM failure. A risk.Evaluate or
// execution failure is reported through Outcome.RiskErr/ExecutionErr
// instead of the return error, since the LLM's decision is still worth
// keeping in both cases.
func Decide(
	ctx context.Context,
	riskStore *riskstorage.Store,
	llmClient llm.Client,
	execClient executor.Client,
	decisionID string,
	asset string,
	perAsset []storage.AgentResult,
	riskContext storage.AgentResult,
	portfolio risk.PortfolioState,
	portfolioValue, price float64,
) (Outcome, error) {
	userPrompt, err := buildPrompt(asset, perAsset, riskContext)
	if err != nil {
		return Outcome{}, err
	}
	return decideWithPrompt(ctx, riskStore, llmClient, execClient, decisionID, asset, userPrompt, portfolio, portfolioValue, price)
}

// DecideWithRanking is the paper-only variation of Decide. When a ranking is
// unavailable for asset, it deliberately delegates to Decide so its prompt is
// byte-for-byte the established non-ranking prompt.
func DecideWithRanking(
	ctx context.Context,
	riskStore *riskstorage.Store,
	llmClient llm.Client,
	execClient executor.Client,
	decisionID string,
	asset string,
	perAsset []storage.AgentResult,
	riskContext storage.AgentResult,
	rankings []storage.Ranking,
	portfolio risk.PortfolioState,
	portfolioValue, price float64,
) (Outcome, error) {
	if !hasRanking(asset, rankings) {
		return Decide(ctx, riskStore, llmClient, execClient, decisionID, asset, perAsset, riskContext, portfolio, portfolioValue, price)
	}
	userPrompt, err := buildPromptWithRanking(asset, perAsset, riskContext, rankings)
	if err != nil {
		return Outcome{}, err
	}
	return decideWithPrompt(ctx, riskStore, llmClient, execClient, decisionID, asset, userPrompt, portfolio, portfolioValue, price)
}

func decideWithPrompt(
	ctx context.Context,
	riskStore *riskstorage.Store,
	llmClient llm.Client,
	execClient executor.Client,
	decisionID, asset, userPrompt string,
	portfolio risk.PortfolioState,
	portfolioValue, price float64,
) (Outcome, error) {

	decision, err := llmClient.Decide(ctx, systemPrompt, userPrompt)
	if err != nil {
		return Outcome{}, fmt.Errorf("strategist: %s: decide: %w", asset, err)
	}

	outcome := Outcome{Decision: decision}
	if decision.Side == "hold" {
		return outcome, nil
	}

	outcome.Quantity = decision.SizingPct * portfolioValue / price
	if decision.Side == "sell" {
		outcome.Quantity = math.Min(outcome.Quantity, portfolio.Positions[asset].Quantity)
	}
	if outcome.Quantity <= 0 {
		return outcome, nil
	}
	outcome.Value = outcome.Quantity * price

	proposed := risk.ProposedOperation{
		Asset:    asset,
		Side:     risk.Side(decision.Side),
		Quantity: outcome.Quantity,
		Value:    outcome.Value,
	}
	riskDecision, err := risk.Evaluate(ctx, riskStore, portfolio, proposed, risk.EvalOptions{})
	if err != nil {
		outcome.RiskErr = fmt.Errorf("strategist: %s: risk.Evaluate: %w", asset, err)
		return outcome, nil
	}
	outcome.Risk = &riskDecision

	if !riskDecision.Allowed {
		return outcome, nil
	}

	execOutcome, err := execClient.Execute(ctx, asset, risk.Side(decision.Side), outcome.Quantity, price, decisionID)
	if err != nil {
		outcome.ExecutionErr = fmt.Errorf("strategist: %s: execute: %w", asset, err)
		return outcome, nil
	}
	outcome.Execution = &execOutcome
	return outcome, nil
}

// buildPrompt requires all three per-asset agent types to be present —
// an analysis run that only covers some agents for this asset isn't
// enough context to decide, and the caller should skip the asset instead
// of deciding on partial information.
func buildPrompt(asset string, perAsset []storage.AgentResult, riskContext storage.AgentResult) (string, error) {
	byType := make(map[string]storage.AgentResult, len(perAsset))
	for _, r := range perAsset {
		byType[r.AgentType] = r
	}
	for _, want := range []string{"technical", "derivatives", "news"} {
		if _, ok := byType[want]; !ok {
			return "", fmt.Errorf("strategist: %s: missing %q analysis result for this run", asset, want)
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Ativo: %s\n\n", asset)
	for _, agentType := range []string{"technical", "derivatives", "news"} {
		r := byType[agentType]
		fmt.Fprintf(&sb, "[%s]\nIndicadores: %s\nNarrativa: %s\n\n", agentType, formatIndicators(r.Indicators), r.Narrative)
	}
	fmt.Fprintf(&sb, "[risk_context]\nIndicadores: %s\nNarrativa: %s\n", formatIndicators(riskContext.Indicators), riskContext.Narrative)
	return sb.String(), nil
}

func buildPromptWithRanking(asset string, perAsset []storage.AgentResult, riskContext storage.AgentResult, rankings []storage.Ranking) (string, error) {
	prompt, err := buildPrompt(asset, perAsset, riskContext)
	if err != nil {
		return "", err
	}
	var current storage.Ranking
	for _, ranking := range rankings {
		if ranking.Asset == asset {
			current = ranking
			break
		}
	}
	if current.Asset == "" {
		return prompt, nil
	}
	var sb strings.Builder
	sb.WriteString(prompt)
	fmt.Fprintf(&sb, "\n[ranking]\nPosição do ativo: %d\nScore composto: %.6f\nTese do comitê: %s\nConfiança do comitê: %.2f\nRanking do ciclo:\n", current.Rank, current.CompositeScore, current.Thesis, current.Confidence)
	for _, ranking := range rankings {
		fmt.Fprintf(&sb, "%d. %s — score %.6f, tese %s\n", ranking.Rank, ranking.Asset, ranking.CompositeScore, ranking.Thesis)
	}
	return sb.String(), nil
}

func hasRanking(asset string, rankings []storage.Ranking) bool {
	for _, ranking := range rankings {
		if ranking.Asset == asset {
			return true
		}
	}
	return false
}

func formatIndicators(indicators map[string]any) string {
	if len(indicators) == 0 {
		return "(nenhum)"
	}
	keys := make([]string, 0, len(indicators))
	for k := range indicators {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, indicators[k]))
	}
	return strings.Join(parts, ", ")
}
