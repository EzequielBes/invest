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

// TestRecoverGaps_ContinuesAfterFailure confirms I3: a FetchCandles failure
// for one asset/timeframe (BTC/1h here, via failCandlesOn) doesn't abort
// recovery for the remaining stale pair (ETH/1h) — mirroring
// backfill_test.go's TestBackfill_ContinuesAfterFailure.
func TestRecoverGaps_ContinuesAfterFailure(t *testing.T) {
	fc := &fakeCollector{name: "fake", maxCalls: 100, failCandlesOn: 1}
	store := &fakeLatestStore{
		latest: map[string]time.Time{
			"fake|BTC|1h": time.Now().UTC().Add(-5 * time.Hour),
			"fake|ETH|1h": time.Now().UTC().Add(-5 * time.Hour),
		},
		found: map[string]bool{
			"fake|BTC|1h": true,
			"fake|ETH|1h": true,
		},
	}

	err := RecoverGaps(context.Background(), store, []exchange.Collector{fc}, []string{"BTC", "ETH"})
	if err != nil {
		t.Fatalf("RecoverGaps: %v", err)
	}
	// BTC's 1h fails on the very first FetchCandles call (failCandlesOn: 1);
	// ETH's 1h must still be attempted afterward rather than recovery
	// aborting entirely on BTC's error.
	if fc.calls < 2 {
		t.Errorf("calls = %d, want >= 2 (BTC's failing call plus at least one call for ETH)", fc.calls)
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
