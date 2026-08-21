package agents

import (
	"context"
	"fmt"
	"strings"

	"analysis/internal/llm"
)

const macroSystemPrompt = `Você é um analista de regime de mercado. Sintetize, em 2-4 frases em português, a volatilidade e dispersão observadas entre os ativos analisados e o tom geral das notícias. Descreva somente o contexto atual, sem recomendar compra ou venda e sem fazer previsão.`

// CycleNarrative is the persisted narrative context made available to the
// two cycle-level agents. It keeps the agents independent of storage.
type CycleNarrative struct {
	AgentType string
	Asset     string
	Narrative string
}

type MacroIndicators struct {
	AssetsAnalyzed int `json:"assets_analyzed"`
	SourceCount    int `json:"source_count"`
}

// Macro summarizes the regime from narratives already produced in this run.
func Macro(ctx context.Context, client llm.Client, assets []string, sources []CycleNarrative) (Output, error) {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "Ativos analisados: %s\n\n", strings.Join(assets, ", "))
	for _, source := range sources {
		if source.Narrative == "" || source.AgentType == "risk_context" || source.AgentType == "macro" || source.AgentType == "committee" {
			continue
		}
		fmt.Fprintf(&prompt, "[%s/%s] %s\n", source.AgentType, source.Asset, source.Narrative)
	}
	indicators := MacroIndicators{AssetsAnalyzed: len(assets), SourceCount: len(sources)}
	narrative, err := client.Summarize(ctx, macroSystemPrompt, prompt.String())
	if err != nil {
		return Output{Indicators: indicators, Err: err}, nil
	}
	return Output{Indicators: indicators, Narrative: narrative}, nil
}
