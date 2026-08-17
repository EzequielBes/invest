// analysis/internal/indicators/technical_test.go
package indicators

import (
	"math"
	"testing"
)

func TestCompute_InsufficientData(t *testing.T) {
	_, err := Compute(make([]Candle, MinCandles-1))
	if err == nil {
		t.Fatal("expected error for insufficient candles, got nil")
	}
}

func TestCompute_UptrendBullish(t *testing.T) {
	// 51 candles, strictly increasing close, flat volume except the last
	// candle doubles — exercises trend, RSI, volatility, and relative
	// volume all landing on the "obviously bullish, obviously spiking
	// volume" side, checkable by hand.
	candles := make([]Candle, MinCandles)
	price := 100.0
	for i := range candles {
		candles[i] = Candle{Close: price, Volume: 10}
		price += 1
	}
	candles[len(candles)-1].Volume = 50

	got, err := Compute(candles)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got.Trend != "bullish" {
		t.Errorf("Trend = %q, want bullish", got.Trend)
	}
	if got.SMAShort <= got.SMALong {
		t.Errorf("SMAShort (%.2f) should be > SMALong (%.2f) in a steady uptrend", got.SMAShort, got.SMALong)
	}
	if got.RSI <= 50 {
		t.Errorf("RSI = %.2f, want > 50 for a strictly increasing series", got.RSI)
	}
	if got.RelativeVolume <= 1 {
		t.Errorf("RelativeVolume = %.2f, want > 1 after the volume spike", got.RelativeVolume)
	}
	if got.Volatility < 0 {
		t.Errorf("Volatility = %.4f, want >= 0", got.Volatility)
	}
}

func TestCompute_FlatIsNeutral(t *testing.T) {
	candles := make([]Candle, MinCandles)
	for i := range candles {
		candles[i] = Candle{Close: 100, Volume: 10}
	}

	got, err := Compute(candles)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got.Trend != "neutral" {
		t.Errorf("Trend = %q, want neutral for a flat series", got.Trend)
	}
	if math.Abs(got.Volatility) > 1e-9 {
		t.Errorf("Volatility = %.6f, want ~0 for a flat series", got.Volatility)
	}
}
