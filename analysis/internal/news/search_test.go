// analysis/internal/news/search_test.go
package news

import (
	"testing"
	"time"
)

func TestSearch_MatchesSymbolAndNameCaseInsensitive(t *testing.T) {
	now := time.Now()
	items := []Item{
		{Title: "Bitcoin hits new high", Body: "...", URL: "u1", PublishedAt: now},
		{Title: "market update", Body: "BTC dropped 5% today", URL: "u2", PublishedAt: now.Add(-time.Hour)},
		{Title: "Ethereum news", Body: "unrelated content", URL: "u3", PublishedAt: now},
	}

	got := Search(items, "BTC", "Bitcoin")
	if got.ArticleCount != 2 {
		t.Fatalf("ArticleCount = %d, want 2", got.ArticleCount)
	}
}

func TestSearch_NoMatch(t *testing.T) {
	items := []Item{
		{Title: "Ethereum news", Body: "unrelated content", URL: "u1", PublishedAt: time.Now()},
	}
	got := Search(items, "BTC", "Bitcoin")
	if got.ArticleCount != 0 {
		t.Fatalf("ArticleCount = %d, want 0", got.ArticleCount)
	}
	if len(got.Articles) != 0 {
		t.Fatalf("len(Articles) = %d, want 0", len(got.Articles))
	}
}

func TestSearch_CapsArticlesAtTenButCountsAll(t *testing.T) {
	now := time.Now()
	items := make([]Item, 15)
	for i := range items {
		items[i] = Item{Title: "Bitcoin update", Body: "...", URL: "u", PublishedAt: now.Add(-time.Duration(i) * time.Minute)}
	}
	got := Search(items, "BTC", "Bitcoin")
	if got.ArticleCount != 15 {
		t.Errorf("ArticleCount = %d, want 15", got.ArticleCount)
	}
	if len(got.Articles) != 10 {
		t.Errorf("len(Articles) = %d, want 10 (capped)", len(got.Articles))
	}
	if !got.Articles[0].PublishedAt.After(got.Articles[len(got.Articles)-1].PublishedAt) {
		t.Error("Articles should be newest first")
	}
}
