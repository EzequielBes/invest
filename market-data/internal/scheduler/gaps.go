package scheduler

import (
	"context"
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
func RecoverGaps(ctx context.Context, store latestCandleStore, collectors []exchange.Collector, assets []string) error {
	now := time.Now().UTC()
	timeframes := []exchange.Timeframe{exchange.Timeframe1m, exchange.Timeframe1h, exchange.Timeframe1d}

	for _, c := range collectors {
		for _, symbol := range assets {
			for _, tf := range timeframes {
				latest, found, err := store.LatestCandleTime(ctx, c.Name(), symbol, tf)
				if err != nil {
					return err
				}
				if !found {
					continue
				}
				if now.Sub(latest) <= timeframeDuration(tf) {
					continue // up to date, nothing to recover
				}
				if err := backfillCandles(ctx, store, c, symbol, tf, latest, now, pageWindowFor(tf)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
