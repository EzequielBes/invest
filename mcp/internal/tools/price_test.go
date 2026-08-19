// mcp/internal/tools/price_test.go
package tools

import (
	"context"
	"os"
	"testing"

	"mcp/internal/storage"
)

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration tests")
	}
	s, err := storage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestGetLatestPrice_MissingAssetIsError(t *testing.T) {
	store := testStore(t)
	if _, err := GetLatestPrice(context.Background(), store, GetLatestPriceArgs{}); err == nil {
		t.Fatal("expected an error for a missing asset, got nil")
	}
}

func TestGetLatestPrice_NoDataFound(t *testing.T) {
	store := testStore(t)
	result, err := GetLatestPrice(context.Background(), store, GetLatestPriceArgs{Asset: "MCPTESTNOPRICE"})
	if err != nil {
		t.Fatalf("GetLatestPrice: %v", err)
	}
	if result.Found {
		t.Fatalf("result = %+v, want Found=false for an asset with no collected candles", result)
	}
}
