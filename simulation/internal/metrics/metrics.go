// simulation/internal/metrics/metrics.go
package metrics

import "math"

type Results struct {
	TotalReturnPct          float64
	MaxDrawdownPct          float64
	SharpeRatio             float64
	SortinoRatio            float64
	AnnualizedVolatilityPct float64
	WinRatePct              float64
	TotalTrades             int
	AvgTradePct             float64
}

// Compute derives final backtest metrics. equity is the total-equity value
// recorded at each driving-timeframe candle, chronological order.
// tradeReturnsPct is the realized P&L percentage of each closed, allowed
// trade. periodsPerYear is how many driving-timeframe candles occur in one
// year (e.g. 365*24 for 1h candles) — the caller computes this from the
// driving timeframe's duration. Risk-free rate is assumed zero.
func Compute(equity []float64, tradeReturnsPct []float64, periodsPerYear float64) Results {
	var r Results
	r.TotalTrades = len(tradeReturnsPct)

	if len(equity) >= 2 && equity[0] != 0 {
		r.TotalReturnPct = (equity[len(equity)-1] - equity[0]) / equity[0] * 100
	}
	r.MaxDrawdownPct = maxDrawdownPct(equity) * 100

	if r.TotalTrades > 0 {
		var wins int
		var sum float64
		for _, p := range tradeReturnsPct {
			if p > 0 {
				wins++
			}
			sum += p
		}
		r.WinRatePct = float64(wins) / float64(r.TotalTrades) * 100
		r.AvgTradePct = sum / float64(r.TotalTrades)
	}

	returns := periodReturns(equity)
	mean, stddev := meanStddev(returns)
	annualizer := math.Sqrt(periodsPerYear)
	r.AnnualizedVolatilityPct = stddev * annualizer * 100
	if stddev > 0 {
		r.SharpeRatio = mean / stddev * annualizer
	}
	if downside := downsideDeviation(returns); downside > 0 {
		r.SortinoRatio = mean / downside * annualizer
	}
	return r
}

func periodReturns(equity []float64) []float64 {
	if len(equity) < 2 {
		return nil
	}
	returns := make([]float64, 0, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1] == 0 {
			continue
		}
		returns = append(returns, (equity[i]-equity[i-1])/equity[i-1])
	}
	return returns
}

func meanStddev(xs []float64) (mean, stddev float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))

	var variance float64
	for _, x := range xs {
		variance += (x - mean) * (x - mean)
	}
	variance /= float64(len(xs))
	return mean, math.Sqrt(variance)
}

// downsideDeviation is the RMS of negative returns computed over ALL of
// xs, not just the losing subset — the spec's precise Sortino definition.
func downsideDeviation(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sumSq float64
	for _, x := range xs {
		if x < 0 {
			sumSq += x * x
		}
	}
	return math.Sqrt(sumSq / float64(len(xs)))
}

func maxDrawdownPct(equity []float64) float64 {
	if len(equity) == 0 {
		return 0
	}
	peak := equity[0]
	var maxDD float64
	for _, e := range equity {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			if dd := (peak - e) / peak; dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}
