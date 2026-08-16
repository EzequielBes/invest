// market-data/internal/scheduler/live.go
package scheduler

import (
	"context"
	"log"
	"time"

	"market-data/internal/exchange"
)

// liquidationStore is the minimal slice of storage.Store live collection
// depends on for liquidations. candleStore and fundingStore are already
// declared in backfill.go (same package) and reused here.
type liquidationStore interface {
	InsertLiquidations(ctx context.Context, exchangeName string, liqs []exchange.Liquidation) error
}

// RunLive starts, for each collector: a live-candle consumer per timeframe,
// a live-liquidation consumer, and a funding/open-interest poller (every 5
// minutes — funding settles every 8h and OI moves slowly, so this is far
// more often than needed but cheap and simple). Blocks until ctx is done.
func RunLive(ctx context.Context, cs candleStore, fs fundingStore, ls liquidationStore, collectors []exchange.Collector, assets []string) {
	for _, c := range collectors {
		c := c
		for _, tf := range []exchange.Timeframe{exchange.Timeframe1m, exchange.Timeframe1h, exchange.Timeframe1d} {
			tf := tf
			ch, err := c.StreamCandles(ctx, assets, tf)
			if err != nil {
				log.Printf("live: %s StreamCandles(%s): %v", c.Name(), tf, err)
				continue
			}
			go func() {
				for candle := range ch {
					if err := cs.InsertCandles(ctx, c.Name(), candle.Symbol, []exchange.Candle{candle}); err != nil {
						log.Printf("live: %s insert candle: %v", c.Name(), err)
					}
				}
			}()
		}

		liqCh, err := c.StreamLiquidations(ctx, assets)
		if err != nil {
			log.Printf("live: %s StreamLiquidations: %v", c.Name(), err)
		} else {
			go func() {
				for liq := range liqCh {
					if err := ls.InsertLiquidations(ctx, c.Name(), []exchange.Liquidation{liq}); err != nil {
						log.Printf("live: %s insert liquidation: %v", c.Name(), err)
					}
				}
			}()
		}

		go pollFundingAndOI(ctx, fs, c, assets)
	}
}

func pollFundingAndOI(ctx context.Context, fs fundingStore, c exchange.Collector, assets []string) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	poll := func() {
		since := time.Now().UTC().Add(-1 * time.Hour)
		for _, symbol := range assets {
			if rates, err := c.FetchFunding(ctx, symbol, since, time.Time{}); err != nil {
				log.Printf("live: %s FetchFunding(%s): %v", c.Name(), symbol, err)
			} else if len(rates) > 0 {
				if err := fs.InsertFunding(ctx, c.Name(), symbol, rates); err != nil {
					log.Printf("live: %s insert funding: %v", c.Name(), err)
				}
			}
			if points, err := c.FetchOpenInterest(ctx, symbol, since, time.Time{}); err != nil {
				log.Printf("live: %s FetchOpenInterest(%s): %v", c.Name(), symbol, err)
			} else if len(points) > 0 {
				if err := fs.InsertOpenInterest(ctx, c.Name(), symbol, points); err != nil {
					log.Printf("live: %s insert open interest: %v", c.Name(), err)
				}
			}
		}
	}

	poll()
	for {
		select {
		case <-ticker.C:
			poll()
		case <-ctx.Done():
			return
		}
	}
}
