package storage

import (
	"context"
	"testing"
)

func TestRecordDecision(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	d := DecisionRecord{
		Asset: "BTC", Side: "buy", Quantity: 0.01, Value: 500,
		Allowed: false,
		Reasons: []string{"daily_loss: daily loss so far: 0.0800"},
		RulesChecked: []RuleResultRecord{
			{Rule: "daily_loss", Passed: false, Measured: 0.08, Limit: 0.05, Detail: "daily loss so far: 0.0800"},
			{Rule: "asset_concentration", Passed: true, Measured: 0.10, Limit: 0.20, Detail: "BTC would be 10.0% of portfolio"},
		},
	}

	if err := s.RecordDecision(ctx, d); err != nil {
		t.Fatalf("RecordDecision: %v", err)
	}

	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM risk_decisions WHERE asset = 'BTC' AND allowed = false`).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one matching row in risk_decisions")
	}
}
