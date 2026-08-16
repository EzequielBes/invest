// market-data/cmd/market-data/main.go
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"market-data/internal/config"
	"market-data/internal/exchange"
	"market-data/internal/exchange/binance"
	"market-data/internal/exchange/bybit"
	"market-data/internal/exchange/okx"
	"market-data/internal/httpclient"
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
	collectors := []exchange.Collector{
		binance.New(httpclient.New(10, 5)),
		bybit.New(httpclient.New(5, 5)),
		okx.New(httpclient.New(5, 5)),
	}

	log.Printf("starting backfill for %d assets across %d exchanges", len(cfg.Assets), len(collectors))
	if err := scheduler.Backfill(ctx, store, store, store, collectors, cfg.Assets, backfillDepth); err != nil {
		log.Printf("backfill: %v", err)
	}

	log.Print("recovering any gaps since last run")
	if err := scheduler.RecoverGaps(ctx, store, collectors, cfg.Assets); err != nil {
		log.Printf("gap recovery: %v", err)
	}

	log.Print("starting live collection")
	scheduler.RunLive(ctx, store, store, store, collectors, cfg.Assets)

	newsClient := httpclient.New(1, 2)
	go runNewsPoller(ctx, store, newsClient)

	<-ctx.Done()
	log.Print("shutting down")
}

func runNewsPoller(ctx context.Context, store *storage.Store, client *httpclient.Client) {
	feeds := map[string]string{
		"coindesk":      "https://www.coindesk.com/arc/outboundfeeds/rss/",
		"cointelegraph": "https://cointelegraph.com/rss",
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
