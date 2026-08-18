package agents

import (
	"context"
	"fmt"
	"strings"

	"risk-engine/risk"

	"analysis/internal/indicators"
	"analysis/internal/llm"
	"analysis/internal/storage"
)

const technicalSystemPrompt = `Você é um analista técnico de criptomoedas. Dado um conjunto de indicadores técnicos calculados para um ativo, escreva um resumo curto (2-4 frases) em português explicando o que eles indicam sobre a tendência atual. Seja direto, sem recomendação de compra ou venda.`

func Technical(ctx context.Context, store *storage.Store, client llm.Client, asset, timeframe string) (Output, error) {
	candles, err := store.RecentCandles(ctx, risk.ReferenceExchange, asset, timeframe, indicators.MinCandles)
	if err != nil {
		return Output{}, fmt.Errorf("agents: technical: fetch candles: %w", err)
	}

	indicatorCandles := make([]indicators.Candle, len(candles))
	for i, c := range candles {
		indicatorCandles[i] = indicators.Candle{Close: c.Close, Volume: c.Volume}
	}
	ind, err := indicators.Compute(indicatorCandles)
	if err != nil {
		partial := indicators.ComputePartial(indicatorCandles)
		userPrompt := fmt.Sprintf(
			"Ativo: %s\nHá somente %d candles fechados; são necessários %d para o conjunto completo. Indicadores parciais disponíveis: %s",
			asset, len(indicatorCandles), indicators.MinCandles, formatPartialTechnical(partial),
		)
		narrative, summarizeErr := client.Summarize(ctx, technicalSystemPrompt, userPrompt)
		if summarizeErr != nil {
			return Output{Indicators: partial, Err: summarizeErr}, nil
		}
		return Output{Indicators: partial, Narrative: narrative}, nil
	}

	userPrompt := fmt.Sprintf("Ativo: %s\nSMA curta (20): %.2f\nSMA longa (50): %.2f\nTendência: %s\nRSI (14): %.1f\nVolatilidade: %.4f\nVolume relativo: %.2f", asset, ind.SMAShort, ind.SMALong, ind.Trend, ind.RSI, ind.Volatility, ind.RelativeVolume)
	narrative, err := client.Summarize(ctx, technicalSystemPrompt, userPrompt)
	if err != nil {
		return Output{Indicators: ind, Err: err}, nil
	}
	return Output{Indicators: ind, Narrative: narrative}, nil
}

func formatPartialTechnical(partial indicators.PartialTechnical) string {
	parts := make([]string, 0, 6)
	if partial.SMAShort != nil {
		parts = append(parts, fmt.Sprintf("SMA curta %.2f", *partial.SMAShort))
	}
	if partial.SMALong != nil {
		parts = append(parts, fmt.Sprintf("SMA longa %.2f", *partial.SMALong))
	}
	if partial.Trend != nil {
		parts = append(parts, "tendência "+*partial.Trend)
	}
	if partial.RSI != nil {
		parts = append(parts, fmt.Sprintf("RSI %.1f", *partial.RSI))
	}
	if partial.Volatility != nil {
		parts = append(parts, fmt.Sprintf("volatilidade %.4f", *partial.Volatility))
	}
	if partial.RelativeVolume != nil {
		parts = append(parts, fmt.Sprintf("volume relativo %.2f", *partial.RelativeVolume))
	}
	if len(parts) == 0 {
		return "nenhum indicador calculável"
	}
	return strings.Join(parts, ", ")
}
