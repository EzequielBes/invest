package macrofeed

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

func TestFetch_ParsesObservationsSkippingMissingValues(t *testing.T) {
	srv := serveFixture(t, "testdata/fedfunds.json")
	defer srv.Close()

	obs, err := fetchFrom(context.Background(), httpclient.New(100, 10), srv.URL)
	if err != nil {
		t.Fatalf("fetchFrom: %v", err)
	}
	// The fixture has 3 observations, one with value "." (missing) — that
	// one must be skipped, not parsed as a bogus zero.
	if len(obs) != 2 {
		t.Fatalf("len(obs) = %d, want 2 (missing-value row skipped)", len(obs))
	}
	if obs[0].Value != 5.25 {
		t.Errorf("obs[0].Value = %v, want 5.25", obs[0].Value)
	}
	wantDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if !obs[0].ObservedAt.Equal(wantDate) {
		t.Errorf("obs[0].ObservedAt = %v, want %v", obs[0].ObservedAt, wantDate)
	}
	if obs[1].Value != 5.50 {
		t.Errorf("obs[1].Value = %v, want 5.50", obs[1].Value)
	}
}
