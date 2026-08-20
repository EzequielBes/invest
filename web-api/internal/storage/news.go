package storage

import (
	"context"
	"time"
)

// NewsItem is one market-data news_items row, read here directly — no
// Go dependency on the market-data module, same pattern every other
// cross-module read in this file uses.
type NewsItem struct {
	ID          int64     `json:"id"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"published_at"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	URL         string    `json:"url"`
}

func (s *Store) RecentNews(ctx context.Context, limit int) ([]NewsItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, source, published_at, title, body, url
		FROM news_items
		ORDER BY published_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []NewsItem{}
	for rows.Next() {
		var n NewsItem
		if err := rows.Scan(&n.ID, &n.Source, &n.PublishedAt, &n.Title, &n.Body, &n.URL); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}
