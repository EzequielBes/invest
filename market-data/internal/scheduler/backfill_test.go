package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"market-data/internal/exchange"
)

// fakeCollector returns one candle per call and records how many times
// FetchCandles was invoked, so the test can assert the backfill loop paginates
// instead of trusting a single big response.
type fakeCollector struct {
	name                 string
	calls                int
	maxCalls             int
	pageWindow           time.Duration
	fundingCalls         int
	fundingMaxCalls      int
	openInterestCalls    int
	openInterestMaxCalls int
	failCandlesOn        int // fail FetchCandles on this call number (0 = never)
	failFundingOn        int // fail FetchFunding on this call number (0 = never)
}

func (f *fakeCollector) Name() string { return f.name }

func (f *fakeCollector) FetchCandles(ctx context.Context, symbol string, tf exchange.Timeframe, from, to time.Time) ([]exchange.Candle, error) {
	f.calls++
	if f.failCandlesOn > 0 && f.calls == f.failCandlesOn {
		return nil, errors.New("simulated candle fetch failure")
	}
	if f.calls > f.maxCalls {
		return nil, nil // no more data — backfill loop must stop
	}
	return []exchange.Candle{{Symbol: symbol, Timeframe: tf, Time: from, Open: 1, High: 1, Low: 1, Close: 1, Volume: 1}}, nil
}
func (f *fakeCollector) FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]exchange.FundingRate, error) {
	f.fundingCalls++
	if f.failFundingOn > 0 && f.fundingCalls == f.failFundingOn {
		return nil, errors.New("simulated funding fetch failure")
	}
	if f.fundingCalls > f.fundingMaxCalls {
		return nil, nil // no more data
	}
	return []exchange.FundingRate{{Symbol: symbol, Time: from, Rate: 0.0001}}, nil
}
func (f *fakeCollector) FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]exchange.OpenInterest, error) {
	f.openInterestCalls++
	if f.openInterestCalls > f.openInterestMaxCalls {
		return nil, nil // no more data
	}
	return []exchange.OpenInterest{{Symbol: symbol, Time: from, Value: 1000}}, nil
}
func (f *fakeCollector) StreamCandles(ctx context.Context, symbols []string, tf exchange.Timeframe) (<-chan exchange.Candle, error) {
	return nil, nil
}
func (f *fakeCollector) StreamLiquidations(ctx context.Context, symbols []string) (<-chan exchange.Liquidation, error) {
	return nil, nil
}

// recordingStore is shared across goroutines now that Backfill fans out one
// goroutine per collector (C3), so every method locks mu — go test -race
// would otherwise flag concurrent increments when a test exercises more than
// one collector.
type recordingStore struct {
	mu sync.Mutex

	insertedCandles int
	insertedFunding int
	insertedOI      int
	runsStarted     int
	runsFinished    int
	lastStatus      string

	// earliest/earliestFound back EarliestCandleTime, keyed by
	// "exchange|symbol|timeframe", so tests can simulate a pair whose candle
	// history already reaches back close to (or further than) the target
	// depth and confirm Backfill skips re-running the candle backfill loop
	// for it.
	earliest      map[string]time.Time
	earliestFound map[string]bool
}

func (r *recordingStore) InsertCandles(ctx context.Context, ex, symbol string, candles []exchange.Candle) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insertedCandles += len(candles)
	return nil
}
func (r *recordingStore) InsertFunding(ctx context.Context, exchangeName, symbol string, rates []exchange.FundingRate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insertedFunding += len(rates)
	return nil
}
func (r *recordingStore) InsertOpenInterest(ctx context.Context, exchangeName, symbol string, points []exchange.OpenInterest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insertedOI += len(points)
	return nil
}
func (r *recordingStore) StartRun(ctx context.Context, collector, symbol string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runsStarted++
	return int64(r.runsStarted), nil
}
func (r *recordingStore) FinishRun(ctx context.Context, runID int64, status string, runErr error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runsFinished++
	r.lastStatus = status
	return nil
}
func (r *recordingStore) EarliestCandleTime(ctx context.Context, ex, symbol string, tf exchange.Timeframe) (time.Time, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ex + "|" + symbol + "|" + string(tf)
	return r.earliest[key], r.earliestFound[key], nil
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

func TestBackfill_Orchestration(t *testing.T) {
	// Test that Backfill has exactly one StartRun/FinishRun pair per collector×asset,
	// not per timeframe. With 1 collector and 2 assets, should have 2 runs total.
	fc := &fakeCollector{name: "test", maxCalls: 1, fundingMaxCalls: 1, openInterestMaxCalls: 1}
	store := &recordingStore{}
	collectors := []exchange.Collector{fc}
	assets := []string{"BTC", "ETH"}

	err := Backfill(context.Background(), store, store, store, collectors, assets, 24*time.Hour)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if store.runsStarted != 2 {
		t.Errorf("runsStarted = %d, want 2", store.runsStarted)
	}
	if store.runsFinished != 2 {
		t.Errorf("runsFinished = %d, want 2", store.runsFinished)
	}
	if store.lastStatus != "success" {
		t.Errorf("lastStatus = %q, want 'success'", store.lastStatus)
	}
}

func TestBackfill_FailureStatus(t *testing.T) {
	// Test that when any part of a pair fails, the status is "failed".
	fc := &fakeCollector{name: "test", maxCalls: 1, fundingMaxCalls: 1, openInterestMaxCalls: 1, failCandlesOn: 1}
	store := &recordingStore{}
	collectors := []exchange.Collector{fc}
	assets := []string{"BTC"}

	err := Backfill(context.Background(), store, store, store, collectors, assets, 24*time.Hour)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if store.runsStarted != 1 {
		t.Errorf("runsStarted = %d, want 1", store.runsStarted)
	}
	if store.runsFinished != 1 {
		t.Errorf("runsFinished = %d, want 1", store.runsFinished)
	}
	if store.lastStatus != "failed" {
		t.Errorf("lastStatus = %q, want 'failed'", store.lastStatus)
	}
}

func TestBackfill_ContinuesAfterFailure(t *testing.T) {
	// Test that Backfill continues to next asset even if one fails.
	// First collector fails on first asset, should still process second asset.
	fc := &fakeCollector{name: "test", maxCalls: 1, fundingMaxCalls: 1, openInterestMaxCalls: 1, failCandlesOn: 1}
	store := &recordingStore{}
	collectors := []exchange.Collector{fc}
	assets := []string{"BTC", "ETH"}

	err := Backfill(context.Background(), store, store, store, collectors, assets, 24*time.Hour)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	// Should have 2 runs (one per asset) even though first one failed
	if store.runsStarted != 2 {
		t.Errorf("runsStarted = %d, want 2", store.runsStarted)
	}
	if store.runsFinished != 2 {
		t.Errorf("runsFinished = %d, want 2", store.runsFinished)
	}
}

// TestBackfill_RunsCollectorsConcurrently exercises Backfill with more than
// one collector so `go test -race` can catch any unsynchronized access to
// the shared recordingStore introduced by C3's goroutine-per-collector
// fan-out — and confirms every collector's assets still get their own run.
func TestBackfill_RunsCollectorsConcurrently(t *testing.T) {
	fc1 := &fakeCollector{name: "c1", maxCalls: 1, fundingMaxCalls: 1, openInterestMaxCalls: 1}
	fc2 := &fakeCollector{name: "c2", maxCalls: 1, fundingMaxCalls: 1, openInterestMaxCalls: 1}
	store := &recordingStore{}
	collectors := []exchange.Collector{fc1, fc2}
	assets := []string{"BTC", "ETH"}

	err := Backfill(context.Background(), store, store, store, collectors, assets, 24*time.Hour)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if store.runsStarted != 4 {
		t.Errorf("runsStarted = %d, want 4 (2 collectors x 2 assets)", store.runsStarted)
	}
	if store.runsFinished != 4 {
		t.Errorf("runsFinished = %d, want 4", store.runsFinished)
	}
	if store.lastStatus != "success" {
		t.Errorf("lastStatus = %q, want success", store.lastStatus)
	}
}

// TestBackfill_SkipsCandlesWhenAlreadyCovered confirms Part B of C3: when a
// pair's earliest stored 1d candle already reaches back close to (or beyond)
// the target depth, Backfill skips the rate-limited FetchCandles loop for
// that pair entirely rather than discarding the prior progress and
// re-backfilling from scratch — while funding/OI backfill (cheap and
// idempotent) still run as normal.
func TestBackfill_SkipsCandlesWhenAlreadyCovered(t *testing.T) {
	fc := &fakeCollector{name: "test", maxCalls: 5, fundingMaxCalls: 1, openInterestMaxCalls: 1}
	store := &recordingStore{
		earliest:      map[string]time.Time{"test|BTC|1d": time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
		earliestFound: map[string]bool{"test|BTC|1d": true},
	}
	collectors := []exchange.Collector{fc}
	assets := []string{"BTC"}

	err := Backfill(context.Background(), store, store, store, collectors, assets, 24*time.Hour)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if fc.calls != 0 {
		t.Errorf("FetchCandles calls = %d, want 0 (candle history already covers target depth)", fc.calls)
	}
	if fc.fundingCalls == 0 {
		t.Error("expected FetchFunding to still be called even when candle backfill is skipped")
	}
	if fc.openInterestCalls == 0 {
		t.Error("expected FetchOpenInterest to still be called even when candle backfill is skipped")
	}
	if store.lastStatus != "success" {
		t.Errorf("lastStatus = %q, want success", store.lastStatus)
	}
}

// TestBackfill_DoesNotSkipCandlesWhenCoverageIsRecentOnly confirms the flip
// side: when the earliest stored 1d candle is more recent than
// from+backfillCoverageTolerance, Backfill treats the pair as not yet
// covered and runs the full candle backfill loop.
func TestBackfill_DoesNotSkipCandlesWhenCoverageIsRecentOnly(t *testing.T) {
	fc := &fakeCollector{name: "test", maxCalls: 1, fundingMaxCalls: 1, openInterestMaxCalls: 1}
	store := &recordingStore{
		earliest:      map[string]time.Time{"test|BTC|1d": time.Now().UTC()}, // far more recent than the 24h-depth target `from`
		earliestFound: map[string]bool{"test|BTC|1d": true},
	}
	collectors := []exchange.Collector{fc}
	assets := []string{"BTC"}

	err := Backfill(context.Background(), store, store, store, collectors, assets, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if fc.calls == 0 {
		t.Error("expected FetchCandles to be called: earliest candle doesn't reach back close to the target depth")
	}
}
