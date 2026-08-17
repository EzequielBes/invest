// simulation/internal/marketview/marketview_test.go
package marketview

import (
	"context"
	"os"
	"testing"
	"time"

	"risk-engine/risk"

	"simulation/internal/storage"
)

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping marketview tests")
	}
	s, err := storage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestView_Candles_UsesAdvancedNowAsCutoff(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	base := time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed candle: %v", err)
		}
	}
	must(store.InsertCandleForTest(ctx, risk.ReferenceExchange, "MVCOIN", "1h", base, 100, 100, 100, 100, 1))
	must(store.InsertCandleForTest(ctx, risk.ReferenceExchange, "MVCOIN", "1h", base.Add(time.Hour), 101, 101, 101, 101, 1))
	must(store.InsertCandleForTest(ctx, risk.ReferenceExchange, "MVCOIN", "1h", base.Add(2*time.Hour), 999, 999, 999, 999, 1))
	t.Cleanup(func() {
		store.DeleteCandlesForTest(context.Background(), risk.ReferenceExchange, "MVCOIN", "1h")
	})

	view := New(store)
	view.Advance(base.Add(2 * time.Hour)) // the [base+1h, base+2h) candle just closed

	candles, err := view.Candles(ctx, "1h", "MVCOIN", 10)
	if err != nil {
		t.Fatalf("Candles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len(candles) = %d, want 2, got %+v", len(candles), candles)
	}
	if candles[len(candles)-1].Close != 101 {
		t.Errorf("most recent visible close = %v, want 101", candles[len(candles)-1].Close)
	}
}
