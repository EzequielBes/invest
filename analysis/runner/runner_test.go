package runner

import (
	"context"
	"os"
	"testing"
	"time"

	"analysis/internal/storage"
)

func TestValidateRequest(t *testing.T) {
	assets, err := validateRequest(PrepareRequest{Assets: []string{" BTC ", "ETH"}, Timeframe: "1h"})
	if err != nil || len(assets) != 2 || assets[0] != "BTC" {
		t.Fatalf("validateRequest = %v, %v", assets, err)
	}
	if _, err := validateRequest(PrepareRequest{Assets: []string{"BTC", "BTC"}, Timeframe: "1h"}); err == nil {
		t.Fatal("duplicate assets accepted")
	}
}

func TestGetContextWithDSNRequiresRunID(t *testing.T) {
	if _, err := GetContextWithDSN(context.Background(), "", " "); err == nil {
		t.Fatal("missing run ID accepted")
	}
}

func TestValidateNarrativesRequiresCompleteStage(t *testing.T) {
	results := []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC"},
		{AgentType: "technical", Asset: "ETH"},
		{AgentType: "derivatives", Asset: "BTC"},
		{AgentType: "derivatives", Asset: "ETH"},
		{AgentType: "news", Asset: "BTC"},
		{AgentType: "news", Asset: "ETH"},
		{AgentType: "risk_context"},
	}
	if err := validateNarratives("technical", []NarrativeSubmission{{Asset: "BTC", Narrative: "x"}}, results); err == nil {
		t.Fatal("partial stage accepted")
	}
	if err := validateNarratives("technical", []NarrativeSubmission{{Asset: "BTC", Narrative: "x"}, {Asset: "ETH", Narrative: "y"}}, results); err != nil {
		t.Fatalf("complete stage rejected: %v", err)
	}
	if err := validateNarratives("macro", []NarrativeSubmission{{Narrative: "x"}}, results); err == nil {
		t.Fatal("macro accepted before source narratives")
	}
}

func TestCleanupStalePendingWithDSN(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	store, err := storage.New(ctx, dsn)
	if err != nil {
		t.Skipf("connect test database: %v", err)
	}
	// Cleanups must run before the store closes so fixture rows are removed.
	t.Cleanup(store.Close)

	stale := storage.Run{ID: "cleanup-stale", StartedAt: time.Now().UTC().Add(-stalePendingTimeout - time.Minute), Timeframe: "1h"}
	fresh := storage.Run{ID: "cleanup-fresh", StartedAt: time.Now().UTC(), Timeframe: "1h"}
	for _, run := range []storage.Run{stale, fresh} {
		store.DeleteRunForTest(ctx, run.ID)
		if err := store.CreateRun(ctx, run); err != nil {
			t.Fatalf("create run %s: %v", run.ID, err)
		}
		t.Cleanup(func() { store.DeleteRunForTest(ctx, run.ID) })
	}

	if err := CleanupStalePendingWithDSN(ctx, dsn); err != nil {
		t.Fatalf("cleanup stale pending runs: %v", err)
	}
	if status, err := store.RunStatus(ctx, stale.ID); err != nil || status != "failed" {
		t.Fatalf("stale status = %q, %v; want failed", status, err)
	}
	if message, err := store.RunErrorForTest(ctx, stale.ID); err != nil || message != stalePendingError {
		t.Fatalf("stale error = %q, %v; want %q", message, err, stalePendingError)
	}
	if status, err := store.RunStatus(ctx, fresh.ID); err != nil || status != "pending" {
		t.Fatalf("fresh status = %q, %v; want pending", status, err)
	}
	if err := CleanupStalePendingWithDSN(ctx, dsn); err != nil {
		t.Fatalf("repeat cleanup stale pending runs: %v", err)
	}
	if status, err := store.RunStatus(ctx, stale.ID); err != nil || status != "failed" {
		t.Fatalf("stale status after repeat = %q, %v; want failed", status, err)
	}
}
