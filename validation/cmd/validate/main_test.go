package main

import (
	"encoding/json"
	"testing"

	validation "validation/internal/validation"
)

func TestAuditModeRequiresExactlyOneAuditTarget(t *testing.T) {
	for _, test := range []struct {
		name, backtestRunID, clientOrderID, want string
	}{
		{name: "neither", want: ""},
		{name: "backtest", backtestRunID: "backtest-1", want: "backtest"},
		{name: "execution", clientOrderID: "client-1", want: "execution"},
		{name: "both", backtestRunID: "backtest-1", clientOrderID: "client-1", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := auditMode(test.backtestRunID, test.clientOrderID); got != test.want {
				t.Errorf("auditMode(%q, %q) = %q, want %q", test.backtestRunID, test.clientOrderID, got, test.want)
			}
		})
	}
}

func TestSplitsJSONUsesDocumentedFieldNames(t *testing.T) {
	var splits []validation.Split
	if err := json.Unmarshal([]byte(`[
 		{"kind":"train","start":"2026-08-01T00:00:00Z","end":"2026-08-02T00:00:00Z","embargo_minutes":30},
 		{"kind":"validation","start":"2026-08-02T00:30:00Z","end":"2026-08-03T00:00:00Z","embargo_minutes":0},
 		{"kind":"holdout","start":"2026-08-03T00:00:00Z","end":"2026-08-04T00:00:00Z","embargo_minutes":0}
 	]`), &splits); err != nil {
		t.Fatalf("parse splits: %v", err)
	}
	if err := validation.ValidateSplits(splits); err != nil {
		t.Fatalf("ValidateSplits: %v", err)
	}
}
