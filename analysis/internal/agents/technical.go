package agents

import (
	"context"
	"fmt"

	"risk-engine/risk"

	"analysis/internal/indicators"
	"analysis/internal/storage"
)

func Technical(ctx context.Context, store *storage.Store, asset, timeframe string) (Output, error) {
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
		return Output{Indicators: partial}, nil
	}
	return Output{Indicators: ind}, nil
}
