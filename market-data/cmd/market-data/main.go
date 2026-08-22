// market-data/cmd/market-data/main.go
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"os"

	"market-data/internal/config"
	"market-data/internal/exchange"
	"market-data/internal/exchange/alpaca"
	"market-data/internal/exchange/binance"
	"market-data/internal/exchange/bybit"
	"market-data/internal/exchange/okx"
	"market-data/internal/httpclient"
	"market-data/internal/macrofeed"
	"market-data/internal/newsfeed"
	"market-data/internal/scheduler"
	"market-data/internal/storage"
)

const backfillDepth = 365 * 24 * time.Hour * 3 / 2 // ~1.5 years, within the spec's 1-2 year range

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	store, err := storage.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer store.Close()

	// Rate limits are conservative fractions of each exchange's published
	// public-endpoint limits, leaving headroom for backfill + live polling
	// to run concurrently without tripping a 429.
	//
	// Binance's real IP-wide limit is REQUEST_WEIGHT: 2400/minute (from
	// GET /fapi/v1/exchangeInfo), and FetchCandles's klines call (the
	// dominant call pattern, always limit=1500) costs weight 11 per call
	// (observed via the x-mbx-used-weight-1m response header). 3 req/s =
	// 180 req/min x 11 = 1980 weight/min, ~82% of budget — leaves headroom
	// since the backgrounded Backfill goroutine and RunLive's live/funding/
	// OI polling all share this same rate limiter concurrently. The
	// previous 10 req/s (6600 weight/min, ~2.75x the real budget) would
	// very likely get 429/418'd or IP-banned under a real backfill run.
	collectors := []exchange.Collector{
		binance.New(httpclient.New(3, 5)),
		bybit.New(httpclient.New(5, 5)),
		okx.New(httpclient.New(5, 5)),
	}
	assetsByCollector := map[string][]string{
		"binance": cfg.Assets,
		"bybit":   cfg.Assets,
		"okx":     cfg.Assets,
	}

	// Alpaca only joins the pipeline once both credentials and a stock
	// asset list are configured — an empty asset list would mean the
	// collector runs and does nothing, which is just noise.
	alpacaKey, alpacaSecret := os.Getenv("ALPACA_API_KEY"), os.Getenv("ALPACA_API_SECRET_KEY")
	if len(cfg.StockAssets) > 0 && alpacaKey != "" && alpacaSecret != "" {
		collectors = append(collectors, alpaca.New(httpclient.New(3, 5), alpacaKey, alpacaSecret))
		assetsByCollector["alpaca"] = cfg.StockAssets
	} else if len(cfg.StockAssets) > 0 {
		log.Print("ALPACA_ASSETS set but ALPACA_API_KEY/ALPACA_API_SECRET_KEY missing, skipping stock collection")
	}

	// Backfill runs in the background rather than blocking startup: with the
	// real ~1.5-year depth across every asset/exchange it can take hours,
	// and live streaming + news polling shouldn't wait on it. RecoverGaps
	// reads LatestCandleTime from the *previous* run's data and correctly
	// no-ops for assets with none yet (that's Backfill's job), and live vs.
	// backfilled candles both go through InsertCandles's upsert, so the two
	// can run concurrently without a correctness issue — worst case is a
	// redundant overwrite of the same data.
	log.Printf("starting backfill for %d exchanges (running in background)", len(collectors))
	go func() {
		if err := scheduler.Backfill(ctx, store, store, store, collectors, assetsByCollector, backfillDepth); err != nil {
			log.Printf("backfill: %v", err)
		} else {
			log.Print("backfill complete")
		}
	}()

	log.Print("recovering any gaps since last run")
	if err := scheduler.RecoverGaps(ctx, store, collectors, assetsByCollector); err != nil {
		log.Printf("gap recovery: %v", err)
	}

	// Also run gap recovery periodically for the life of the process, not
	// just once at startup (I2): a WS drop that goes undetected between
	// keepalive pings (or any other transient cause) can otherwise leave a
	// gap that would only get healed at the next restart. RecoverGaps itself
	// logs and continues past a single pair's failure (I3), so a failure
	// here doesn't need to be fatal.
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := scheduler.RecoverGaps(ctx, store, collectors, assetsByCollector); err != nil {
					log.Printf("periodic gap recovery: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	log.Print("starting live collection")
	scheduler.RunLive(ctx, store, store, store, collectors, assetsByCollector)

	newsClient := httpclient.New(1, 2)
	go runNewsPoller(ctx, store, newsClient)

	if cfg.FredAPIKey != "" {
		macroClient := httpclient.New(1, 2)
		go runMacroPoller(ctx, store, macroClient, cfg.FredAPIKey)
	} else {
		log.Print("FRED_API_KEY not set, skipping macro indicator polling")
	}

	<-ctx.Done()
	log.Print("shutting down")
}

func runNewsPoller(ctx context.Context, store *storage.Store, client *httpclient.Client) {
	feeds := map[string]string{
		"coindesk":      "https://www.coindesk.com/arc/outboundfeeds/rss/",
		"cointelegraph": "https://cointelegraph.com/rss",
		"marketwatch":   "https://www.marketwatch.com/rss/topstories",
	}
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	poll := func() {
		for source, url := range feeds {
			items, err := newsfeed.Fetch(ctx, client, source, url)
			if err != nil {
				log.Printf("news: fetch %s: %v", source, err)
				continue
			}
			for _, item := range items {
				if _, err := store.InsertNewsItem(ctx, item.Source, item.Title, item.Body, item.URL, item.PublishedAt); err != nil {
					log.Printf("news: insert %s: %v", source, err)
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

// runMacroPoller polls FRED daily — these series update monthly, so
// the news poller's 10-minute cadence would be wasteful here.
func runMacroPoller(ctx context.Context, store *storage.Store, client *httpclient.Client, apiKey string) {
	series := []string{"FEDFUNDS", "CPIAUCSL", "UNRATE"}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	poll := func() {
		for _, seriesID := range series {
			observations, err := macrofeed.Fetch(ctx, client, seriesID, apiKey)
			if err != nil {
				log.Printf("macro: fetch %s: %v", seriesID, err)
				continue
			}
			for _, obs := range observations {
				if err := store.InsertMacroObservation(ctx, seriesID, obs.ObservedAt, obs.Value); err != nil {
					log.Printf("macro: insert %s: %v", seriesID, err)
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
