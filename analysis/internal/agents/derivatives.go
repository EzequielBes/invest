package agents

import (
	"context"
	"fmt"
	"time"

	"risk-engine/risk"

	"analysis/internal/derivatives"
	"analysis/internal/storage"
)

func Derivatives(ctx context.Context, store *storage.Store, asset string) (Output, error) {
	fundingRate, _, err := store.LatestFundingRate(ctx, risk.ExchangeFor(asset), asset)
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch funding rate: %w", err)
	}
	now := time.Now().UTC()
	currentOI, _, err := store.OpenInterestNear(ctx, risk.ExchangeFor(asset), asset, now)
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch open interest: %w", err)
	}
	oi24hAgo, _, err := store.OpenInterestNear(ctx, risk.ExchangeFor(asset), asset, now.Add(-24*time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch open interest 24h ago: %w", err)
	}
	rawLiqs, err := store.RecentLiquidations(ctx, risk.ExchangeFor(asset), asset, now.Add(-time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch liquidations: %w", err)
	}
	liqs := make([]derivatives.Liquidation, len(rawLiqs))
	for i, l := range rawLiqs {
		liqs[i] = derivatives.Liquidation{Price: l.Price, Quantity: l.Quantity}
	}
	signals := derivatives.Compute(fundingRate, currentOI, oi24hAgo, liqs)
	return Output{Indicators: signals}, nil
}
