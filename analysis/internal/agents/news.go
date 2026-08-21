package agents

import (
	"context"
	"fmt"
	"time"

	"analysis/internal/news"
	"analysis/internal/storage"
)

func News(ctx context.Context, store *storage.Store, symbol, name string) (Output, error) {
	rawItems, err := store.RecentNews(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: news: fetch recent news: %w", err)
	}
	items := make([]news.Item, len(rawItems))
	for i, it := range rawItems {
		items[i] = news.Item{Title: it.Title, Body: it.Body, URL: it.URL, PublishedAt: it.PublishedAt}
	}
	result := news.Search(items, symbol, name)
	return Output{Indicators: result}, nil
}
