package scheduler

import (
	"context"
	"log"
	"time"

	"market-data/internal/exchange"
)

// candleStore, fundingStore, and runStore are the minimal slices of storage.Store this
// package depends on, so backfill logic can be unit-tested without a real
// database (see backfill_test.go's recordingStore).
type candleStore interface {
	InsertCandles(ctx context.Context, exchangeName, symbol string, candles []exchange.Candle) error
}

type fundingStore interface {
	InsertFunding(ctx context.Context, exchangeName, symbol string, rates []exchange.FundingRate) error
	InsertOpenInterest(ctx context.Context, exchangeName, symbol string, points []exchange.OpenInterest) error
}

type runStore interface {
	StartRun(ctx context.Context, collector, symbol string) (int64, error)
	FinishRun(ctx context.Context, runID int64, status string, runErr error) error
}

// backfillCandles walks forward from `from` to `to` in pageWindow-sized
// chunks, stopping as soon as the collector returns no data for a window
// (either real end-of-history, or the exchange's retention limit for
// endpoints like open interest history).
func backfillCandles(ctx context.Context, store candleStore, c exchange.Collector, symbol string, tf exchange.Timeframe, from, to time.Time, pageWindow time.Duration) error {
	cursor := from
	for cursor.Before(to) {
		windowEnd := cursor.Add(pageWindow)
		if windowEnd.After(to) {
			windowEnd = to
		}
		candles, err := c.FetchCandles(ctx, symbol, tf, cursor, windowEnd)
		if err != nil {
			return err
		}
		if len(candles) == 0 {
			return nil
		}
		if err := store.InsertCandles(ctx, c.Name(), symbol, candles); err != nil {
			return err
		}
		cursor = windowEnd
	}
	return nil
}

// backfillFunding walks forward from `from` to `to` in 20-day windows,
// paginating until the collector returns no data.
func backfillFunding(ctx context.Context, store fundingStore, c exchange.Collector, symbol string, from, to time.Time) error {
	cursor := from
	const window = 20 * 24 * time.Hour // conservative: keeps rows-per-call under every exchange's funding-history page limit
	for cursor.Before(to) {
		windowEnd := cursor.Add(window)
		if windowEnd.After(to) {
			windowEnd = to
		}
		rates, err := c.FetchFunding(ctx, symbol, cursor, windowEnd)
		if err != nil {
			return err
		}
		if len(rates) > 0 {
			if err := store.InsertFunding(ctx, c.Name(), symbol, rates); err != nil {
				return err
			}
		}
		cursor = windowEnd
	}
	return nil
}

// backfillOpenInterest walks forward from `from` to `to` in 7-day windows,
// paginating until the collector returns no data.
func backfillOpenInterest(ctx context.Context, store fundingStore, c exchange.Collector, symbol string, from, to time.Time) error {
	cursor := from
	const window = 7 * 24 * time.Hour // conservative: keeps rows-per-call under every exchange's OI-history page limit
	for cursor.Before(to) {
		windowEnd := cursor.Add(window)
		if windowEnd.After(to) {
			windowEnd = to
		}
		points, err := c.FetchOpenInterest(ctx, symbol, cursor, windowEnd)
		if err != nil {
			return err
		}
		if len(points) > 0 {
			if err := store.InsertOpenInterest(ctx, c.Name(), symbol, points); err != nil {
				return err
			}
		}
		cursor = windowEnd
	}
	return nil
}

// pageWindowFor returns a conservative page size per timeframe, well under
// each exchange's max rows-per-call (Binance 1500, Bybit 1000, OKX 300) so a
// single window never risks truncation regardless of which exchange is
// backfilling.
func pageWindowFor(tf exchange.Timeframe) time.Duration {
	switch tf {
	case exchange.Timeframe1m:
		return 200 * time.Minute
	case exchange.Timeframe1h:
		return 200 * time.Hour
	case exchange.Timeframe1d:
		return 200 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// Backfill runs historical backfill for every asset across every collector
// and timeframe, going back `depth` from now. Each asset/collector pair gets
// its own collector_runs row so failures are attributable.
func Backfill(ctx context.Context, cs candleStore, fs fundingStore, rs runStore, collectors []exchange.Collector, assets []string, depth time.Duration) error {
	to := time.Now().UTC()
	from := to.Add(-depth)
	timeframes := []exchange.Timeframe{exchange.Timeframe1m, exchange.Timeframe1h, exchange.Timeframe1d}

	for _, c := range collectors {
		for _, symbol := range assets {
			runID, err := rs.StartRun(ctx, c.Name(), symbol)
			if err != nil {
				log.Printf("backfill %s/%s: StartRun failed: %v", c.Name(), symbol, err)
				continue
			}
			var runErr error
			for _, tf := range timeframes {
				if err := backfillCandles(ctx, cs, c, symbol, tf, from, to, pageWindowFor(tf)); err != nil {
					log.Printf("backfill %s/%s/%s: %v", c.Name(), symbol, tf, err)
					runErr = err
				}
			}
			if err := backfillFunding(ctx, fs, c, symbol, from, to); err != nil {
				log.Printf("backfill %s/%s/funding: %v", c.Name(), symbol, err)
				runErr = err
			}
			if err := backfillOpenInterest(ctx, fs, c, symbol, from, to); err != nil {
				log.Printf("backfill %s/%s/openinterest: %v", c.Name(), symbol, err)
				runErr = err
			}
			status := "success"
			if runErr != nil {
				status = "failed"
			}
			if err := rs.FinishRun(ctx, runID, status, runErr); err != nil {
				log.Printf("backfill %s/%s: FinishRun failed: %v", c.Name(), symbol, err)
			}
		}
	}
	return nil
}
