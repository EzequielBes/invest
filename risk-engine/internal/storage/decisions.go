package storage

import (
	"context"
	"encoding/json"
	"time"
)

type RuleResultRecord struct {
	Rule     string  `json:"rule"`
	Passed   bool    `json:"passed"`
	Measured float64 `json:"measured"`
	Limit    float64 `json:"limit"`
	Detail   string  `json:"detail"`
}

type DecisionRecord struct {
	Asset        string
	Side         string
	Quantity     float64
	Value        float64
	Allowed      bool
	Reasons      []string
	RulesChecked []RuleResultRecord
}

func RecordDecision(ctx context.Context, db querier, d DecisionRecord) error {
	reasonsJSON, err := json.Marshal(d.Reasons)
	if err != nil {
		return err
	}
	rulesJSON, err := json.Marshal(d.RulesChecked)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO risk_decisions (ts, asset, side, quantity, value, allowed, reasons, rules_checked)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, time.Now().UTC(), d.Asset, d.Side, d.Quantity, d.Value, d.Allowed, reasonsJSON, rulesJSON)
	return err
}

func (s *Store) RecordDecision(ctx context.Context, d DecisionRecord) error {
	return RecordDecision(ctx, s.pool, d)
}
