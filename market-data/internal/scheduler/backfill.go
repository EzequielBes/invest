package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"market-data/internal/exchange"
)

// candleStore, fundingStore, and runStore are the minimal slices of storage.Store this
// package depends on, so backfill logic can be unit-tested without a real
// database (see backfill_test.go's recordingStore).
type candleStore interface {
	InsertCandles(ctx context.Context, exchangeName, symbol string, candles []exchange.Candle) error
	EarliestCandleTime(ctx context.Context, exchangeName, symbol string, tf exchange.Timeframe) (time.Time, bool, error)
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

// maxOpenInterestBackfillDepth caps how far back open-interest backfill
// reaches, regardless of the overall backfill depth: Binance's
// futures/data/openInterestHist rejects any startTime older than ~30 days
// with a 400 ("parameter 'startTime' is invalid.", code -1130) — confirmed
// live. None of the three exchanges retain OI history anywhere near the
// ~1.5yr candle depth in practice, and the spec doesn't mandate full-depth OI
// history (candles are the primary requirement), so one conservative,
// exchange-agnostic window is used instead of per-exchange retention logic.
const maxOpenInterestBackfillDepth = 25 * 24 * time.Hour

// backfillCoverageTolerance is how close a pair/timeframe's earliest stored
// candle must be to the target `from` boundary to be treated as "already
// backfilled," letting a restart skip re-running the rate-limited candle
// backfill loop for that timeframe instead of discarding all prior progress.
const backfillCoverageTolerance = 48 * time.Hour

// Backfill runs historical backfill for every asset across every collector
// and timeframe, going back `depth` from now. Each asset/collector pair gets
// its own collector_runs row so failures are attributable. Collectors run
// concurrently — each has its own independent rate limiter and hits a
// different exchange's API, so there's no reason to serialize them, and
// serializing them was the root cause of Bybit/OKX effectively never
// starting their backfill behind Binance's ~10 hour run. Errors are logged
// per-pair inside backfillCollector rather than aggregated/returned, since
// one collector's failure shouldn't abort the others.
func Backfill(ctx context.Context, cs candleStore, fs fundingStore, rs runStore, collectors []exchange.Collector, assets []string, depth time.Duration) error {
	to := time.Now().UTC()
	from := to.Add(-depth)
	timeframes := []exchange.Timeframe{exchange.Timeframe1m, exchange.Timeframe1h, exchange.Timeframe1d}

	var wg sync.WaitGroup
	for _, c := range collectors {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			backfillCollector(ctx, cs, fs, rs, c, assets, from, to, timeframes)
		}()
	}
	wg.Wait()
	return nil
}

// backfillCollector runs the full backfill sequence (candles across all
// timeframes unless already covered, then funding, then open interest) for
// every asset of a single collector, sequentially per-asset — the same
// per-asset body Backfill used to run directly in its outer loop, just
// factored out so Backfill can fan out one of these per collector
// concurrently via goroutines.
func backfillCollector(ctx context.Context, cs candleStore, fs fundingStore, rs runStore, c exchange.Collector, assets []string, from, to time.Time, timeframes []exchange.Timeframe) {
	oiFrom := from
	if to.Sub(from) > maxOpenInterestBackfillDepth {
		oiFrom = to.Add(-maxOpenInterestBackfillDepth)
	}

	for _, symbol := range assets {
		runID, err := rs.StartRun(ctx, c.Name(), symbol)
		if err != nil {
			log.Printf("backfill %s/%s: StartRun failed: %v", c.Name(), symbol, err)
			continue
		}
		var runErr error

		for _, tf := range timeframes {
			if alreadyCovered(ctx, cs, c.Name(), symbol, tf, from) {
				log.Printf("backfill %s/%s/%s: candle history already reaches back to ~%s, skipping", c.Name(), symbol, tf, from.Format(time.RFC3339))
				continue
			}
			if err := backfillCandles(ctx, cs, c, symbol, tf, from, to, pageWindowFor(tf)); err != nil {
				log.Printf("backfill %s/%s/%s: %v", c.Name(), symbol, tf, err)
				runErr = err
			}
		}

		// Funding/OI backfill run regardless of the candle-coverage skip
		// above — they're cheap and idempotent via upsert, no need to gate
		// them behind the same check.
		if err := backfillFunding(ctx, fs, c, symbol, from, to); err != nil {
			log.Printf("backfill %s/%s/funding: %v", c.Name(), symbol, err)
			runErr = err
		}
		if err := backfillOpenInterest(ctx, fs, c, symbol, oiFrom, to); err != nil {
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

// alreadyCovered reports whether symbol's stored candle history for
// collector c, at timeframe tf specifically, already reaches back to within
// backfillCoverageTolerance of from, meaning a prior backfill run already
// covered this pair/timeframe and the (rate-limited, potentially hours-long)
// candle backfill loop can be safely skipped for it this run. Checked
// per-timeframe rather than proxying through a single timeframe (e.g. 1d):
// backfillCandles's per-timeframe loop continues past an error on one
// timeframe and still attempts the others, so one timeframe (say 1d) can
// succeed while another (say 1m or 1h) partially or fully failed in the same
// run — a single pair-level check would then permanently skip re-attempting
// the failed timeframe on every subsequent run.
func alreadyCovered(ctx context.Context, cs candleStore, collectorName, symbol string, tf exchange.Timeframe, from time.Time) bool {
	earliest, found, err := cs.EarliestCandleTime(ctx, collectorName, symbol, tf)
	if err != nil {
		log.Printf("backfill %s/%s/%s: EarliestCandleTime check failed: %v (proceeding with full candle backfill)", collectorName, symbol, tf, err)
		return false
	}
	if !found {
		return false
	}
	return !earliest.After(from.Add(backfillCoverageTolerance))
}
