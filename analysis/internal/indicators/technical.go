// analysis/internal/indicators/technical.go
package indicators

import (
	"fmt"
	"math"
)

const (
	smaShortPeriod = 20
	smaLongPeriod  = 50
	rsiPeriod      = 14
	volWindow      = 20

	// MinCandles is the fewest closed candles Compute needs (the SMA-long
	// window plus one extra point for its own comparison basis).
	MinCandles = smaLongPeriod + 1
)

// Candle is the minimal OHLCV shape indicators need — decoupled from any
// storage package so this file has no database dependency.
type Candle struct {
	Close  float64
	Volume float64
}

// Technical holds the structured technical indicators computed for one
// asset over a series of closed candles.
type Technical struct {
	SMAShort       float64 `json:"sma_short"`
	SMALong        float64 `json:"sma_long"`
	Trend          string  `json:"trend"`
	RSI            float64 `json:"rsi"`
	Volatility     float64 `json:"volatility"`
	RelativeVolume float64 `json:"relative_volume"`
}

// PartialTechnical contains every indicator that can be calculated from an
// undersized candle set. Missing values are omitted when marshalled to JSON.
type PartialTechnical struct {
	Status         string   `json:"status"`
	SMAShort       *float64 `json:"sma_short,omitempty"`
	SMALong        *float64 `json:"sma_long,omitempty"`
	Trend          *string  `json:"trend,omitempty"`
	RSI            *float64 `json:"rsi,omitempty"`
	Volatility     *float64 `json:"volatility,omitempty"`
	RelativeVolume *float64 `json:"relative_volume,omitempty"`
}

// ComputePartial calculates only the indicators supported by the available
// candles. It is intended for the insufficient-data path in the agent.
func ComputePartial(candles []Candle) PartialTechnical {
	result := PartialTechnical{Status: "insufficient_data"}
	closes := make([]float64, len(candles))
	for i, candle := range candles {
		closes[i] = candle.Close
	}
	if len(closes) >= smaShortPeriod {
		value := sma(closes, smaShortPeriod)
		result.SMAShort = &value
	}
	if len(closes) >= smaLongPeriod {
		value := sma(closes, smaLongPeriod)
		result.SMALong = &value
		if result.SMAShort != nil && value != 0 {
			trend := "neutral"
			diff := (*result.SMAShort - value) / value
			switch {
			case diff > 0.001:
				trend = "bullish"
			case diff < -0.001:
				trend = "bearish"
			}
			result.Trend = &trend
		}
	}
	if len(closes) >= rsiPeriod+1 {
		value := rsi(closes, rsiPeriod)
		result.RSI = &value
	}
	if len(closes) >= volWindow+1 {
		value := volatility(closes, volWindow)
		result.Volatility = &value
		value = relativeVolume(candles, volWindow)
		result.RelativeVolume = &value
	}
	return result
}

// Compute calculates technical indicators from closed candles, oldest
// first. Returns an error if fewer than MinCandles are supplied — callers
// should treat that as "insufficient data", not a bug.
func Compute(candles []Candle) (Technical, error) {
	if len(candles) < MinCandles {
		return Technical{}, fmt.Errorf("indicators: need at least %d candles, got %d", MinCandles, len(candles))
	}

	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	smaShort := sma(closes, smaShortPeriod)
	smaLong := sma(closes, smaLongPeriod)

	trend := "neutral"
	diff := (smaShort - smaLong) / smaLong
	switch {
	case diff > 0.001:
		trend = "bullish"
	case diff < -0.001:
		trend = "bearish"
	}

	return Technical{
		SMAShort:       smaShort,
		SMALong:        smaLong,
		Trend:          trend,
		RSI:            rsi(closes, rsiPeriod),
		Volatility:     volatility(closes, volWindow),
		RelativeVolume: relativeVolume(candles, volWindow),
	}, nil
}

func sma(closes []float64, n int) float64 {
	sum := 0.0
	for _, c := range closes[len(closes)-n:] {
		sum += c
	}
	return sum / float64(n)
}

// rsi computes Wilder's RSI over the last period+1 closes.
func rsi(closes []float64, period int) float64 {
	window := closes[len(closes)-period-1:]
	var gainSum, lossSum float64
	for i := 1; i < len(window); i++ {
		delta := window[i] - window[i-1]
		if delta > 0 {
			gainSum += delta
		} else {
			lossSum += -delta
		}
	}
	avgGain := gainSum / float64(period)
	avgLoss := lossSum / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// volatility is the standard deviation of percentage returns over the last
// n closes.
func volatility(closes []float64, n int) float64 {
	window := closes[len(closes)-n-1:]
	returns := make([]float64, 0, n)
	for i := 1; i < len(window); i++ {
		returns = append(returns, (window[i]-window[i-1])/window[i-1])
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	sumSq := 0.0
	for _, r := range returns {
		sumSq += (r - mean) * (r - mean)
	}
	return math.Sqrt(sumSq / float64(len(returns)))
}

// relativeVolume is the most recent candle's volume divided by the average
// volume of the preceding n candles.
func relativeVolume(candles []Candle, n int) float64 {
	last := candles[len(candles)-1]
	window := candles[len(candles)-1-n : len(candles)-1]
	sum := 0.0
	for _, c := range window {
		sum += c.Volume
	}
	avg := sum / float64(n)
	if avg == 0 {
		return 0
	}
	return last.Volume / avg
}
