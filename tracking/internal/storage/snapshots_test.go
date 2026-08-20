package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSaveSnapshot_RoundTrips(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	store := newIsolatedSnapshotStore(t, dsn)

	want := Snapshot{
		Timestamp:      time.Now().UTC().Truncate(time.Microsecond),
		Cash:           1000,
		PositionsValue: 500,
		TotalEquity:    1500,
	}
	if err := store.SaveSnapshot(context.Background(), want); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	got, err := store.RecentSnapshotForTest(context.Background())
	if err != nil {
		t.Fatalf("RecentSnapshotForTest: %v", err)
	}
	if !got.Timestamp.Equal(want.Timestamp) || got.Cash != want.Cash || got.PositionsValue != want.PositionsValue || got.TotalEquity != want.TotalEquity {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}

func TestLatestPrice_NotFoundReturnsFalseNotError(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	_, found, err := store.LatestPrice(context.Background(), "binance", "TEST-TRACKING-NO-SUCH-SYMBOL", "1m")
	if err != nil {
		t.Fatalf("LatestPrice: %v", err)
	}
	if found {
		t.Error("found = true, want false for a symbol with no candles")
	}
}

func newIsolatedSnapshotStore(t *testing.T, dsn string) *Store {
	t.Helper()
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := fmt.Sprintf("tracking_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "CREATE TABLE "+schema+".equity_snapshots (LIKE public.equity_snapshots INCLUDING ALL)"); err != nil {
		t.Cleanup(func() { _, _ = adminPool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
		t.Fatalf("create isolated snapshots table: %v", err)
	}

	store, err := New(ctx, dsn+"&search_path="+schema)
	if err != nil {
		t.Cleanup(func() { _, _ = adminPool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
		t.Fatalf("connect isolated store: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
		_, _ = adminPool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	})
	return store
}
