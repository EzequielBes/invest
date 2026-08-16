package storage

import (
	"context"
	"encoding/json"
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

	// Query back the inserted row to verify all content
	var (
		quantity    float64
		value       float64
		reasonsJSON []byte
		rulesJSON   []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT quantity, value, reasons, rules_checked
		FROM risk_decisions WHERE asset = 'BTC' AND allowed = false
		ORDER BY id DESC LIMIT 1
	`).Scan(&quantity, &value, &reasonsJSON, &rulesJSON)
	if err != nil {
		t.Fatalf("query inserted row: %v", err)
	}

	// Verify quantity and value
	if quantity != 0.01 {
		t.Errorf("quantity = %v, want 0.01", quantity)
	}
	if value != 500 {
		t.Errorf("value = %v, want 500", value)
	}

	// Verify reasons JSON
	var gotReasons []string
	if err := json.Unmarshal(reasonsJSON, &gotReasons); err != nil {
		t.Fatalf("unmarshal reasons: %v", err)
	}
	if len(gotReasons) != 1 || gotReasons[0] != "daily_loss: daily loss so far: 0.0800" {
		t.Errorf("reasons = %v, want [\"daily_loss: daily loss so far: 0.0800\"]", gotReasons)
	}

	// Verify rules_checked JSON
	var gotRules []RuleResultRecord
	if err := json.Unmarshal(rulesJSON, &gotRules); err != nil {
		t.Fatalf("unmarshal rules_checked: %v", err)
	}
	if len(gotRules) != 2 {
		t.Errorf("rules_checked len = %d, want 2", len(gotRules))
	}
	if len(gotRules) >= 2 {
		if gotRules[0].Rule != "daily_loss" || gotRules[0].Passed != false || gotRules[0].Measured != 0.08 || gotRules[0].Limit != 0.05 {
			t.Errorf("rules_checked[0] = %+v, want {Rule:daily_loss, Passed:false, Measured:0.08, Limit:0.05, ...}", gotRules[0])
		}
		if gotRules[1].Rule != "asset_concentration" || gotRules[1].Passed != true || gotRules[1].Measured != 0.10 || gotRules[1].Limit != 0.20 {
			t.Errorf("rules_checked[1] = %+v, want {Rule:asset_concentration, Passed:true, Measured:0.10, Limit:0.20, ...}", gotRules[1])
		}
	}
}
