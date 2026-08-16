package scheduler

import (
	"context"
	"testing"
	"time"

	"market-data/internal/exchange"
)

// fakeCollector returns one candle per call and records how many times
// FetchCandles was invoked, so the test can assert the backfill loop paginates
// instead of trusting a single big response.
type fakeCollector struct {
	name       string
	calls      int
	maxCalls   int
	pageWindow time.Duration
}

func (f *fakeCollector) Name() string { return f.name }

func (f *fakeCollector) FetchCandles(ctx context.Context, symbol string, tf exchange.Timeframe, from, to time.Time) ([]exchange.Candle, error) {
	f.calls++
	if f.calls > f.maxCalls {
		return nil, nil // no more data — backfill loop must stop
	}
	return []exchange.Candle{{Symbol: symbol, Timeframe: tf, Time: from, Open: 1, High: 1, Low: 1, Close: 1, Volume: 1}}, nil
}
func (f *fakeCollector) FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]exchange.FundingRate, error) {
	return nil, nil
}
func (f *fakeCollector) FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]exchange.OpenInterest, error) {
	return nil, nil
}
func (f *fakeCollector) StreamCandles(ctx context.Context, symbols []string, tf exchange.Timeframe) (<-chan exchange.Candle, error) {
	return nil, nil
}
func (f *fakeCollector) StreamLiquidations(ctx context.Context, symbols []string) (<-chan exchange.Liquidation, error) {
	return nil, nil
}

type recordingStore struct {
	insertedCandles int
	runsStarted     int
	runsFinished    int
}

func (r *recordingStore) InsertCandles(ctx context.Context, ex, symbol string, candles []exchange.Candle) error {
	r.insertedCandles += len(candles)
	return nil
}
func (r *recordingStore) StartRun(ctx context.Context, collector, symbol string) (int64, error) {
	r.runsStarted++
	return int64(r.runsStarted), nil
}
func (r *recordingStore) FinishRun(ctx context.Context, runID int64, status string, runErr error) error {
	r.runsFinished++
	return nil
}

func TestBackfillCandles_PaginatesUntilCollectorReturnsEmpty(t *testing.T) {
	fc := &fakeCollector{name: "fake", maxCalls: 3, pageWindow: time.Hour}
	store := &recordingStore{}

	err := backfillCandles(context.Background(), store, fc, "BTC", exchange.Timeframe1h,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 5, 0, 0, 0, time.UTC), fc.pageWindow)
	if err != nil {
		t.Fatalf("backfillCandles: %v", err)
	}
	if fc.calls != 4 { // 3 pages with data + 1 that returns empty and stops the loop
		t.Errorf("calls = %d, want 4", fc.calls)
	}
	if store.insertedCandles != 3 {
		t.Errorf("insertedCandles = %d, want 3", store.insertedCandles)
	}
}
