package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"tracking/internal/storage"
)

type fakeExecClient struct {
	cash      float64
	positions map[string]float64
	err       error
}

func (f *fakeExecClient) FetchPortfolio(context.Context) (float64, map[string]float64, error) {
	return f.cash, f.positions, f.err
}

type fakeStore struct {
	prices  map[string]float64
	saved   []storage.Snapshot
	saveErr error
}

func (f *fakeStore) LatestPrice(_ context.Context, _, symbol, _ string) (float64, bool, error) {
	price, ok := f.prices[symbol]
	return price, ok, nil
}

func (f *fakeStore) SaveSnapshot(_ context.Context, s storage.Snapshot) error {
	f.saved = append(f.saved, s)
	return f.saveErr
}

func TestSnapshotOnce_ComputesTotalEquityAndSaves(t *testing.T) {
	exec := &fakeExecClient{cash: 1000, positions: map[string]float64{"BTC": 0.5}}
	store := &fakeStore{prices: map[string]float64{"BTC": 50000}}

	snapshotOnce(context.Background(), store, exec)

	if len(store.saved) != 1 {
		t.Fatalf("saved = %+v, want exactly one snapshot", store.saved)
	}
	got := store.saved[0]
	if got.Cash != 1000 || got.PositionsValue != 25000 || got.TotalEquity != 26000 {
		t.Errorf("snapshot = %+v, want cash=1000 positions_value=25000 total_equity=26000", got)
	}
}

func TestSnapshotOnce_FetchPortfolioFailureSkipsCycleWithoutSaving(t *testing.T) {
	exec := &fakeExecClient{err: errors.New("binance unreachable")}
	store := &fakeStore{}

	snapshotOnce(context.Background(), store, exec)

	if len(store.saved) != 0 {
		t.Errorf("saved = %+v, want none (portfolio fetch failed)", store.saved)
	}
}

func TestSnapshotOnce_MissingPriceSkipsCycleWithoutSaving(t *testing.T) {
	exec := &fakeExecClient{cash: 1000, positions: map[string]float64{"BTC": 0.5}}
	store := &fakeStore{prices: map[string]float64{}}

	snapshotOnce(context.Background(), store, exec)

	if len(store.saved) != 0 {
		t.Errorf("saved = %+v, want none (missing price)", store.saved)
	}
}

func TestRunLoop_SnapshotsImmediatelyAndStopsOnContextCancellation(t *testing.T) {
	exec := &fakeExecClient{cash: 100, positions: map[string]float64{}}
	store := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		runLoop(ctx, store, exec, time.Hour)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("runLoop returned before context was cancelled")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runLoop did not stop after context cancellation")
	}

	if len(store.saved) != 1 {
		t.Errorf("saved = %+v, want exactly one snapshot from the immediate poll at start", store.saved)
	}
}
