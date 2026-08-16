package scheduler

import (
	"context"
	"testing"
	"time"

	"market-data/internal/exchange"
)

type fakeLatestStore struct {
	recordingStore
	latest map[string]time.Time // key: exchange|symbol|timeframe
	found  map[string]bool
}

func (f *fakeLatestStore) LatestCandleTime(ctx context.Context, ex, symbol string, tf exchange.Timeframe) (time.Time, bool, error) {
	key := ex + "|" + symbol + "|" + string(tf)
	return f.latest[key], f.found[key], nil
}

func TestRecoverGaps_BackfillsWhenLatestCandleIsStale(t *testing.T) {
	fc := &fakeCollector{name: "fake", maxCalls: 100}
	store := &fakeLatestStore{
		latest: map[string]time.Time{"fake|BTC|1h": time.Now().UTC().Add(-5 * time.Hour)},
		found:  map[string]bool{"fake|BTC|1h": true},
	}

	err := RecoverGaps(context.Background(), store, []exchange.Collector{fc}, []string{"BTC"})
	if err != nil {
		t.Fatalf("RecoverGaps: %v", err)
	}
	if fc.calls == 0 {
		t.Error("expected FetchCandles to be called to fill the gap")
	}
}

func TestRecoverGaps_SkipsWhenNoPriorData(t *testing.T) {
	fc := &fakeCollector{name: "fake", maxCalls: 100}
	store := &fakeLatestStore{latest: map[string]time.Time{}, found: map[string]bool{}}

	err := RecoverGaps(context.Background(), store, []exchange.Collector{fc}, []string{"BTC"})
	if err != nil {
		t.Fatalf("RecoverGaps: %v", err)
	}
	if fc.calls != 0 {
		t.Error("expected no gap-fill call when there's no prior data — that's the initial backfill's job (Task 14), not gap recovery")
	}
}
