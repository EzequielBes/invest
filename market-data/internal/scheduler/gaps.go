package scheduler

import (
	"context"
	"log"
	"time"

	"market-data/internal/exchange"
)

type latestCandleStore interface {
	candleStore
	LatestCandleTime(ctx context.Context, exchangeName, symbol string, tf exchange.Timeframe) (time.Time, bool, error)
}

func timeframeDuration(tf exchange.Timeframe) time.Duration {
	switch tf {
	case exchange.Timeframe1m:
		return time.Minute
	case exchange.Timeframe1h:
		return time.Hour
	case exchange.Timeframe1d:
		return 24 * time.Hour
	default:
		return time.Hour
	}
}

// RecoverGaps checks, for each asset/collector/timeframe with prior data,
// whether the most recent stored candle is older than one interval — meaning
// the service was down and missed live updates — and backfills the missing
// window. Assets with no prior data are left to the initial Backfill (Task
// 14); this only recovers gaps in existing history.
//
// A failure for one collector/asset/timeframe is logged and does not abort
// recovery for the rest: RecoverGaps runs on the startup critical path and
// (as of I2) on a recurring 15-minute ticker, so a single transient error
// (e.g. a rate-limit blip) must not skip recovery for every other
// asset/exchange/timeframe combination — mirroring how Backfill already
// handles per-pair errors.
// assetsByCollector is keyed by collector.Name() — each collector's gaps
// are only checked against its own asset list, not a single list shared
// by every collector.
func RecoverGaps(ctx context.Context, store latestCandleStore, collectors []exchange.Collector, assetsByCollector map[string][]string) error {
	now := time.Now().UTC()
	timeframes := []exchange.Timeframe{exchange.Timeframe1m, exchange.Timeframe1h, exchange.Timeframe1d}

	for _, c := range collectors {
		for _, symbol := range assetsByCollector[c.Name()] {
			for _, tf := range timeframes {
				latest, found, err := store.LatestCandleTime(ctx, c.Name(), symbol, tf)
				if err != nil {
					log.Printf("gap recovery %s/%s/%s: LatestCandleTime failed: %v", c.Name(), symbol, tf, err)
					continue
				}
				if !found {
					continue
				}
				if now.Sub(latest) <= timeframeDuration(tf) {
					continue // up to date, nothing to recover
				}
				if err := backfillCandles(ctx, store, c, symbol, tf, latest, now, pageWindowFor(tf)); err != nil {
					log.Printf("gap recovery %s/%s/%s: %v", c.Name(), symbol, tf, err)
					continue
				}
			}
		}
	}
	return nil
}
