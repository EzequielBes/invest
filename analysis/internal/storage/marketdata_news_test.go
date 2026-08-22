// analysis/internal/storage/marketdata_news_test.go
package storage

import (
	"context"
	"testing"
	"time"
)

func TestRecentNews_IncludesSource(t *testing.T) {
	s := testAnalysisStore(t)
	ctx := context.Background()
	url := "https://example.com/test-article-" + time.Now().Format("150405.000000")
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM news_items WHERE url = $1`, url)
	})

	_, err := s.pool.Exec(ctx, `
		INSERT INTO news_items (source, published_at, title, body, url)
		VALUES ('marketwatch', now(), 'Test Article', 'body text', $1)
	`, url)
	if err != nil {
		t.Fatalf("seed news_items: %v", err)
	}

	items, err := s.RecentNews(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("RecentNews: %v", err)
	}
	var found bool
	for _, it := range items {
		if it.URL == url {
			found = true
			if it.Source != "marketwatch" {
				t.Errorf("Source = %q, want %q", it.Source, "marketwatch")
			}
		}
	}
	if !found {
		t.Fatal("seeded item not found in RecentNews result")
	}
}
