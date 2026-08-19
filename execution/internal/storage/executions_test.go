package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestSaveExecution_RoundTrips(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	id := "test-execution-roundtrip"
	defer store.DeleteExecutionForTest(context.Background(), id)

	want := Execution{
		ID: id, Asset: "BTC", Side: "buy", RequestedQuantity: 1.5, Price: 50000,
		OrderID: "12345", ClientOrderID: id, Status: "filled",
		FilledQuantity: 1.5, FilledPrice: 50001.2, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := store.SaveExecution(context.Background(), want); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	got, err := store.ExecutionForTest(context.Background(), id)
	if err != nil {
		t.Fatalf("ExecutionForTest: %v", err)
	}
	if got != want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}
