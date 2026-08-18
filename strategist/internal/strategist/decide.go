// strategist/internal/strategist/decide.go
package strategist

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"strategist/internal/llm"
	"strategist/internal/storage"
)

const systemPrompt = `Você é um estrategista de investimentos em criptomoedas. Você recebe indicadores técnicos, de derivativos, de notícias e de contexto de risco sobre um ativo, e decide se deve comprar, vender ou manter a posição atual. Nunca sugira sizing_pct acima de 0.25 (25% do portfólio) numa única operação. Responda sempre usando a ferramenta record_decision.`

// Outcome is what deciding on one asset produces. Risk is nil when
// Decision.Side is "hold" (risk.Evaluate is never called for a hold) or
// when risk.Evaluate itself failed — RiskErr is set in that second case
// so the caller can log it while still persisting the LLM's decision.
type Outcome struct {
	Decision llm.Decision
	Quantity float64
	Value    float64
	Risk     *risk.Decision
	RiskErr  error
}

// Decide asks the LLM for a decision about asset from its three per-asset
// analysis results (technical, derivatives, news) plus the shared
// risk-context result, then — for buy/sell — sizes the proposed operation
// against portfolioValue/price and validates it via risk.Evaluate.
//
// Returns an error only when there is no decision at all to persist:
// missing analysis data for asset, or an LLM failure. A risk.Evaluate
// failure is reported through Outcome.RiskErr instead of the return
// error, since the LLM's decision is still worth keeping in that case.
func Decide(
	ctx context.Context,
	riskStore *riskstorage.Store,
	client llm.Client,
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

	decision, err := client.Decide(ctx, systemPrompt, userPrompt)
	if err != nil {
		return Outcome{}, fmt.Errorf("strategist: %s: decide: %w", asset, err)
	}

	outcome := Outcome{Decision: decision}
	if decision.Side == "hold" {
		return outcome, nil
	}

	outcome.Quantity = decision.SizingPct * portfolioValue / price
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
