package agents

import (
	"context"
	"fmt"
	"time"

	"risk-engine/risk"

	"analysis/internal/derivatives"
	"analysis/internal/llm"
	"analysis/internal/storage"
)

const derivativesSystemPrompt = `Você é um analista de derivativos de criptomoedas. Dado um conjunto de indicadores de funding rate, open interest e liquidações para um ativo, escreva um resumo curto (2-4 frases) em português explicando o que eles indicam. Seja direto, sem recomendação de compra ou venda.`

func Derivatives(ctx context.Context, store *storage.Store, client llm.Client, asset string) (Output, error) {
	fundingRate, _, err := store.LatestFundingRate(ctx, risk.ReferenceExchange, asset)
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch funding rate: %w", err)
	}
	now := time.Now().UTC()
	currentOI, _, err := store.OpenInterestNear(ctx, risk.ReferenceExchange, asset, now)
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch open interest: %w", err)
	}
	oi24hAgo, _, err := store.OpenInterestNear(ctx, risk.ReferenceExchange, asset, now.Add(-24*time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch open interest 24h ago: %w", err)
	}
	rawLiqs, err := store.RecentLiquidations(ctx, risk.ReferenceExchange, asset, now.Add(-time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch liquidations: %w", err)
	}
	liqs := make([]derivatives.Liquidation, len(rawLiqs))
	for i, l := range rawLiqs {
		liqs[i] = derivatives.Liquidation{Price: l.Price, Quantity: l.Quantity}
	}
	signals := derivatives.Compute(fundingRate, currentOI, oi24hAgo, liqs)
	userPrompt := fmt.Sprintf("Ativo: %s\nFunding rate: %.4f%% (extremo: %v)\nVariação de OI (24h): %.2f%%\nVolume liquidado (1h): $%.2f (cascata: %v)", asset, signals.FundingRate*100, signals.FundingExtreme, signals.OIChangePct, signals.LiquidationVolume1h, signals.LiquidationCascade)
	narrative, err := client.Summarize(ctx, derivativesSystemPrompt, userPrompt)
	if err != nil {
		return Output{Indicators: signals, Err: err}, nil
	}
	return Output{Indicators: signals, Narrative: narrative}, nil
}
