package agents

import (
	"context"
	"fmt"
	"time"

	"analysis/internal/news"
	"analysis/internal/storage"
)

// sourceCategory maps each collected news source to the asset class its
// content is relevant to, so a crypto analysis never sees stock-market
// noise and vice versa.
var sourceCategory = map[string]string{
	"coindesk":      "crypto",
	"cointelegraph": "crypto",
	"marketwatch":   "stock",
}

// News searches recent items for symbol/name mentions, restricted to
// sources whose category matches assetClass ("crypto" or "stock") — a
// source with no known category is excluded rather than shown to every
// asset class by default.
func News(ctx context.Context, store *storage.Store, symbol, name, assetClass string) (Output, error) {
	rawItems, err := store.RecentNews(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: news: fetch recent news: %w", err)
	}
	items := make([]news.Item, 0, len(rawItems))
	for _, it := range rawItems {
		if sourceCategory[it.Source] != assetClass {
			continue
		}
		items = append(items, news.Item{Title: it.Title, Body: it.Body, URL: it.URL, PublishedAt: it.PublishedAt})
	}
	result := news.Search(items, symbol, name)
	return Output{Indicators: result}, nil
}
