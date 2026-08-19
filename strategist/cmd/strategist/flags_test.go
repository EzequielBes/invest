// strategist/cmd/strategist/flags_test.go
package main

import "testing"

func TestParsePositions(t *testing.T) {
	positions, err := parsePositions("BTC:0.5, ETH:2")
	if err != nil {
		t.Fatalf("parsePositions: %v", err)
	}
	if positions["BTC"] != 0.5 || positions["ETH"] != 2 {
		t.Fatalf("positions = %#v, want BTC:0.5 ETH:2", positions)
	}
	if _, err := parsePositions("BTC"); err == nil {
		t.Fatal("expected an error for a malformed entry, got nil")
	}
	if _, err := parsePositions("BTC:notanumber"); err == nil {
		t.Fatal("expected an error for a non-numeric quantity, got nil")
	}
}

func TestParsePositions_Empty(t *testing.T) {
	positions, err := parsePositions("")
	if err != nil {
		t.Fatalf("parsePositions: %v", err)
	}
	if len(positions) != 0 {
		t.Fatalf("positions = %#v, want empty", positions)
	}
}

func TestParsePositions_WhitespaceOnlyEntryIsRejected(t *testing.T) {
	if _, err := parsePositions(" : "); err == nil {
		t.Fatal("expected an error for a whitespace-only position entry, got nil")
	}
}
