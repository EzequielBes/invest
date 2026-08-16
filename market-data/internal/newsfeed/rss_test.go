package newsfeed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"market-data/internal/httpclient"
)

func serveFixture(t *testing.T, path string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
}

func TestFetch_ParsesCointelegraphFixture(t *testing.T) {
	srv := serveFixture(t, "testdata/cointelegraph.xml")
	defer srv.Close()

	items, err := Fetch(context.Background(), httpclient.New(100, 10), "cointelegraph", srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	first := items[0]
	if first.Source != "cointelegraph" {
		t.Errorf("Source = %q", first.Source)
	}
	if first.Title != "Tokenized stock holders more than double as monthly volume surges" {
		t.Errorf("Title = %q", first.Title)
	}
	if first.URL != "https://cointelegraph.com/news/tokenized-stock-holders?utm_source=rss_feed" {
		t.Errorf("URL = %q", first.URL)
	}
	wantTime := time.Date(2026, 8, 15, 17, 26, 43, 0, time.UTC)
	if !first.PublishedAt.Equal(wantTime) {
		t.Errorf("PublishedAt = %v, want %v", first.PublishedAt, wantTime)
	}
}

func TestFetch_ParsesCoindeskFixture(t *testing.T) {
	srv := serveFixture(t, "testdata/coindesk.xml")
	defer srv.Close()

	items, err := Fetch(context.Background(), httpclient.New(100, 10), "coindesk", srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Title != "Robot maker Unitree is going public. Hyperliquid traders see 4x upside from IPO price" {
		t.Errorf("Title = %q", items[0].Title)
	}
}
