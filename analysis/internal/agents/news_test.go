package agents

import (
	"context"
	"os"
	"testing"
	"time"

	"analysis/internal/news"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedNewsItem(ctx context.Context, source, title, body, url string) error {
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		INSERT INTO news_items (source, published_at, title, body, url)
		VALUES ($1, now(), $2, $3, $4)
	`, source, title, body, url)
	return err
}

func deleteNewsByURL(ctx context.Context, url string) {
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		return
	}
	defer pool.Close()
	pool.Exec(ctx, `DELETE FROM news_items WHERE url = $1`, url)
}

// TestNews_FiltersByAssetClassCategory proves category filtering by using
// a made-up, collision-proof search term that only appears in a crypto-
// sourced fixture article's body — a real word like "Bitcoin" risks
// matching real MarketWatch articles that legitimately mention crypto in
// stock-market context, which would make this test flaky against live
// collected data. A stock-class News call for the fixture term must find
// nothing, even though the text would match — the category gate runs
// before the keyword search, not after.
func TestNews_FiltersByAssetClassCategory(t *testing.T) {
	store := testMacroStore(t)
	ctx := context.Background()
	needle := "Zqxvunicorn" + time.Now().Format("150405")

	cryptoURL := "https://example.com/crypto-" + time.Now().Format("150405.000000")
	t.Cleanup(func() { deleteNewsByURL(context.Background(), cryptoURL) })

	if err := seedNewsItem(ctx, "coindesk", needle+" surges", needle+" rallied today on strong volume", cryptoURL); err != nil {
		t.Fatalf("seed crypto news: %v", err)
	}

	cryptoOutput, err := News(ctx, store, needle, needle, "crypto")
	if err != nil {
		t.Fatalf("News (crypto class): %v", err)
	}
	cryptoResult := cryptoOutput.Indicators.(news.Result)
	if cryptoResult.ArticleCount == 0 {
		t.Fatal("expected the crypto-class query to find the coindesk article (sanity check before testing exclusion)")
	}

	stockOutput, err := News(ctx, store, needle, needle, "stock")
	if err != nil {
		t.Fatalf("News (stock class): %v", err)
	}
	stockResult := stockOutput.Indicators.(news.Result)
	if stockResult.ArticleCount != 0 {
		t.Errorf("ArticleCount = %d, want 0 — a stock-class query must not surface a crypto-category article even though the text matches", stockResult.ArticleCount)
	}
}
