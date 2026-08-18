package agents

import (
	"context"
	"fmt"
	"time"

	riskstorage "risk-engine/storage"

	"analysis/internal/llm"
)

const riskContextSystemPrompt = `Você é um analista de risco de portfólio. Dado o estado atual do motor de risco e os limites configurados, escreva um resumo curto (2-4 frases) em português sobre a situação de risco do portfólio no momento. Seja direto, sem recomendação de compra ou venda.`

type RiskContextIndicators struct {
	Status    string             `json:"risk_status"`
	Reason    string             `json:"risk_reason"`
	ChangedAt string             `json:"risk_changed_at"`
	Limits    riskstorage.Limits `json:"limits"`
}

func RiskContext(ctx context.Context, riskStore *riskstorage.Store, client llm.Client) (Output, error) {
	state, err := riskStore.GetState(ctx, nil)
	if err != nil {
		return Output{}, fmt.Errorf("agents: risk_context: fetch state: %w", err)
	}
	limits, err := riskStore.GetLimits(ctx)
	if err != nil {
		return Output{}, fmt.Errorf("agents: risk_context: fetch limits: %w", err)
	}
	ind := RiskContextIndicators{Status: state.Status, Reason: state.Reason, ChangedAt: state.ChangedAt.Format(time.RFC3339), Limits: limits}
	userPrompt := fmt.Sprintf("Status: %s\nMotivo: %s\nDesde: %s\nLimite por ativo: %.1f%%\nLimite total em cripto: %.1f%%\nPerda diária máxima: %.1f%%\nPerda semanal máxima: %.1f%%\nDrawdown máximo: %.1f%%\nPerdas consecutivas máximas: %d", ind.Status, ind.Reason, ind.ChangedAt, limits.MaxPctPerAsset*100, limits.MaxPctCryptoTotal*100, limits.MaxDailyLoss*100, limits.MaxWeeklyLoss*100, limits.MaxDrawdown*100, limits.MaxConsecutiveLosses)
	narrative, err := client.Summarize(ctx, riskContextSystemPrompt, userPrompt)
	if err != nil {
		return Output{Indicators: ind, Err: err}, nil
	}
	return Output{Indicators: ind, Narrative: narrative}, nil
}
