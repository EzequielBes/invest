// Package ranking calculates the deterministic opportunity order from
// committee assessments and the existing risk-engine data-quality limits.
package ranking

import (
	"fmt"
	"math"
	"sort"
)

type Limits struct {
	MaxDataAgeMinutes int
	MinLiquidity      float64
	MaxVolatility     float64
}

// Input contains only persisted/recalculable values. Quality values are
// recorded in the corresponding committee analysis_result.
type Input struct {
	Asset            string
	OpportunityScore float64
	DataAgeMinutes   float64
	Liquidity        float64
	Volatility       float64
}

type Result struct {
	Input
	FreshnessFactor  float64
	LiquidityFactor  float64
	VolatilityFactor float64
	CompositeScore   float64
	Rank             int
}

// Compute applies the documented multiplicative formula. Scores are ordered
// descending; asset is the deterministic tie-breaker.
func Compute(inputs []Input, limits Limits) ([]Result, error) {
	if limits.MaxDataAgeMinutes <= 0 || limits.MinLiquidity <= 0 || limits.MaxVolatility <= 0 {
		return nil, fmt.Errorf("ranking: quality limits must be positive")
	}
	results := make([]Result, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input.Asset == "" {
			return nil, fmt.Errorf("ranking: asset is required")
		}
		if _, ok := seen[input.Asset]; ok {
			return nil, fmt.Errorf("ranking: duplicate asset %q", input.Asset)
		}
		seen[input.Asset] = struct{}{}
		if !finite(input.OpportunityScore) || input.OpportunityScore < 0 || input.OpportunityScore > 1 {
			return nil, fmt.Errorf("ranking: %s has invalid opportunity score", input.Asset)
		}
		if !finite(input.DataAgeMinutes) || input.DataAgeMinutes < 0 || !finite(input.Liquidity) || input.Liquidity <= 0 || !finite(input.Volatility) || input.Volatility < 0 {
			return nil, fmt.Errorf("ranking: %s has invalid quality inputs", input.Asset)
		}
		freshness := freshnessFactor(input.DataAgeMinutes, limits.MaxDataAgeMinutes)
		liquidity := liquidityFactor(input.Liquidity, limits.MinLiquidity)
		volatility := volatilityFactor(input.Volatility, limits.MaxVolatility)
		results = append(results, Result{
			Input: input, FreshnessFactor: freshness, LiquidityFactor: liquidity, VolatilityFactor: volatility,
			CompositeScore: input.OpportunityScore * freshness * liquidity * volatility,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].CompositeScore == results[j].CompositeScore {
			return results[i].Asset < results[j].Asset
		}
		return results[i].CompositeScore > results[j].CompositeScore
	})
	for i := range results {
		results[i].Rank = i + 1
	}
	return results, nil
}

// freshnessFactor returns 1 for data within the risk limit and linearly
// penalizes older positive ages.
func freshnessFactor(ageMinutes float64, maxAgeMinutes int) float64 {
	return cappedRatio(float64(maxAgeMinutes), ageMinutes)
}

// liquidityFactor returns 1 at or above the risk minimum and linearly
// penalizes lower positive quote volume.
func liquidityFactor(liquidity, minLiquidity float64) float64 {
	return cappedRatio(liquidity, minLiquidity)
}

// volatilityFactor returns 1 at or below the risk limit and linearly
// penalizes volatility above it.
func volatilityFactor(volatility, maxVolatility float64) float64 {
	return cappedRatio(maxVolatility, volatility)
}

// cappedRatio returns a quality multiplier in (0, 1]. Values at or better
// than the limit receive 1; poorer positive values are linearly penalized.
func cappedRatio(good, observed float64) float64 {
	if observed == 0 {
		return 1
	}
	if good >= observed {
		return 1
	}
	return good / observed
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
