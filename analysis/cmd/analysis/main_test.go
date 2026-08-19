// analysis/cmd/analysis/main_test.go
package main

import "testing"

func TestParseAssetNames(t *testing.T) {
	names, err := parseAssetNames("BTC=Bitcoin, ETH = Ethereum")
	if err != nil {
		t.Fatalf("parseAssetNames: %v", err)
	}
	if names["BTC"] != "Bitcoin" || names["ETH"] != "Ethereum" {
		t.Fatalf("names = %#v, want BTC/ETH full names", names)
	}
	if _, err := parseAssetNames("BTC"); err == nil {
		t.Fatal("expected malformed mapping error, got nil")
	}
}
