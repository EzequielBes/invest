// analysis/internal/news/search.go
package news

import (
	"sort"
	"strings"
	"time"
)

const maxArticles = 10

// Item is the minimal shape search needs from one news item — decoupled
// from any storage package.
type Item struct {
	Title       string
	Body        string
	URL         string
	PublishedAt time.Time
}

// Article is one matched item surfaced in a Result, trimmed to what the
// LLM prompt and the persisted indicators need.
type Article struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

// Result is the structured indicator the news agent produces: how many
// matching articles were found, and up to maxArticles of the most recent
// ones.
type Result struct {
	ArticleCount int       `json:"article_count"`
	Articles     []Article `json:"articles"`
}

// Search filters items to those mentioning symbol or name (case-
// insensitive) in the title or body, newest first. ArticleCount reflects
// every match; Articles is capped at maxArticles. Callers are expected to
// have already restricted items to the desired time window (e.g. the last
// 24h) — Search does not filter by time itself.
func Search(items []Item, symbol, name string) Result {
	needles := []string{strings.ToLower(symbol), strings.ToLower(name)}

	var matched []Item
	for _, it := range items {
		haystack := strings.ToLower(it.Title + " " + it.Body)
		for _, needle := range needles {
			if needle != "" && strings.Contains(haystack, needle) {
				matched = append(matched, it)
				break
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].PublishedAt.After(matched[j].PublishedAt)
	})

	articles := make([]Article, 0, min(len(matched), maxArticles))
	for _, it := range matched[:min(len(matched), maxArticles)] {
		articles = append(articles, Article{Title: it.Title, URL: it.URL, PublishedAt: it.PublishedAt})
	}

	return Result{ArticleCount: len(matched), Articles: articles}
}
