package scheduler

import (
	"context"
	"testing"
	"time"

	"market-data/internal/exchange"
)

// consistentFakeCollector generates synthetic OHLCV data from a simple
// monotonically-increasing price ramp (price(t) = whole minutes since the
// Unix epoch), so that whatever period a requested candle covers, Open is
// the price at the period's start, Close is the price at the period's last
// minute, Low/High are the min/max over the period (trivially Open/Close
// here since the ramp is monotonic), and Volume is the number of 1-minute
// bars aggregated into it. This lets I5's test assert a genuine
// cross-timeframe consistency property — aggregating the 1m candles over an
// hour must reproduce the 1h candle for that same hour — rather than just
// counting that candles were inserted per timeframe.
type consistentFakeCollector struct{}

func (consistentFakeCollector) Name() string { return "consistent-fake" }

func rampPrice(t time.Time) float64 {
	return float64(t.Unix() / 60) // whole minutes since the Unix epoch
}

func (consistentFakeCollector) FetchCandles(ctx context.Context, symbol string, tf exchange.Timeframe, from, to time.Time) ([]exchange.Candle, error) {
	step := timeframeDuration(tf)
	var candles []exchange.Candle
	for cursor := from.Truncate(step); cursor.Before(to); cursor = cursor.Add(step) {
		periodEnd := cursor.Add(step)
		if periodEnd.After(to) {
			break // only emit fully-covered periods, as a real exchange would for the requested range
		}
		lastMinute := periodEnd.Add(-time.Minute)
		candles = append(candles, exchange.Candle{
			Symbol: symbol, Timeframe: tf, Time: cursor,
			Open: rampPrice(cursor), Close: rampPrice(lastMinute),
			Low: rampPrice(cursor), High: rampPrice(lastMinute),
			Volume: step.Minutes(),
		})
	}
	return candles, nil
}
func (consistentFakeCollector) FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]exchange.FundingRate, error) {
	return nil, nil
}
func (consistentFakeCollector) FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]exchange.OpenInterest, error) {
	return nil, nil
}
func (consistentFakeCollector) StreamCandles(ctx context.Context, symbols []string, tf exchange.Timeframe) (<-chan exchange.Candle, error) {
	return nil, nil
}
func (consistentFakeCollector) StreamLiquidations(ctx context.Context, symbols []string) (<-chan exchange.Liquidation, error) {
	return nil, nil
}

// capturingStore records every inserted candle (not just a count), so
// TestBackfill_CrossTimeframeConsistency can inspect actual OHLCV values.
type capturingStore struct {
	candles []exchange.Candle
}

func (s *capturingStore) InsertCandles(ctx context.Context, exchangeName, symbol string, candles []exchange.Candle) error {
	s.candles = append(s.candles, candles...)
	return nil
}
func (s *capturingStore) EarliestCandleTime(ctx context.Context, exchangeName, symbol string, tf exchange.Timeframe) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func (s *capturingStore) byTimeframe(tf exchange.Timeframe) []exchange.Candle {
	var out []exchange.Candle
	for _, c := range s.candles {
		if c.Timeframe == tf {
			out = append(out, c)
		}
	}
	return out
}

// TestBackfill_CrossTimeframeConsistency is the spec-required "integration
// test for backfill, validating consistency between 1m/1h/1d candles" (I5).
// It backfills a clean, minute-boundary-aligned 25-hour window across all
// three timeframes using a fake collector whose synthetic data is
// internally consistent across timeframes, then asserts (1) candles were
// inserted for every timeframe, and (2) aggregating the 1m candles over one
// full hour reproduces that hour's 1h candle exactly (open = first minute's
// open, close = last minute's close, high/low = the extremes, volume = the
// sum) — a real cross-timeframe consistency check, not just a row count.
func TestBackfill_CrossTimeframeConsistency(t *testing.T) {
	fc := consistentFakeCollector{}
	store := &capturingStore{}

	to := time.Date(2026, 1, 2, 1, 0, 0, 0, time.UTC)
	from := to.Add(-25 * time.Hour) // > 24h so a full 1d candle is produced too

	for _, tf := range []exchange.Timeframe{exchange.Timeframe1m, exchange.Timeframe1h, exchange.Timeframe1d} {
		if err := backfillCandles(context.Background(), store, fc, "BTC", tf, from, to, pageWindowFor(tf)); err != nil {
			t.Fatalf("backfillCandles(%s): %v", tf, err)
		}
	}

	m1 := store.byTimeframe(exchange.Timeframe1m)
	h1 := store.byTimeframe(exchange.Timeframe1h)
	d1 := store.byTimeframe(exchange.Timeframe1d)
	if len(m1) == 0 {
		t.Fatal("expected 1m candles to be inserted")
	}
	if len(h1) == 0 {
		t.Fatal("expected 1h candles to be inserted")
	}
	if len(d1) == 0 {
		t.Fatal("expected 1d candles to be inserted")
	}

	// Pick the first hour and confirm aggregating its 60 1m candles
	// reproduces the 1h candle stored for that same hour.
	hourStart := h1[0].Time
	var minutesInHour []exchange.Candle
	for _, c := range m1 {
		if !c.Time.Before(hourStart) && c.Time.Before(hourStart.Add(time.Hour)) {
			minutesInHour = append(minutesInHour, c)
		}
	}
	if len(minutesInHour) != 60 {
		t.Fatalf("minutesInHour = %d, want 60", len(minutesInHour))
	}

	aggOpen := minutesInHour[0].Open
	aggClose := minutesInHour[len(minutesInHour)-1].Close
	aggLow, aggHigh := minutesInHour[0].Low, minutesInHour[0].High
	aggVolume := 0.0
	for _, c := range minutesInHour {
		if c.Low < aggLow {
			aggLow = c.Low
		}
		if c.High > aggHigh {
			aggHigh = c.High
		}
		aggVolume += c.Volume
	}

	if aggOpen != h1[0].Open || aggClose != h1[0].Close || aggLow != h1[0].Low || aggHigh != h1[0].High || aggVolume != h1[0].Volume {
		t.Errorf("aggregated 1m OHLCV = {open:%v close:%v low:%v high:%v vol:%v}, want 1h candle {open:%v close:%v low:%v high:%v vol:%v}",
			aggOpen, aggClose, aggLow, aggHigh, aggVolume,
			h1[0].Open, h1[0].Close, h1[0].Low, h1[0].High, h1[0].Volume)
	}
}
