package storage

import (
	"context"
	"os"
	"testing"
)

func TestRecentNews_ReturnsEmptySliceNotNilWhenNoRows(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	news, err := store.RecentNews(context.Background(), 1)
	if err != nil {
		t.Fatalf("RecentNews: %v", err)
	}
	if news == nil {
		t.Error("news is nil, want a non-nil (possibly empty) slice so it JSON-encodes as [] not null")
	}
}
