// Package paperexec is a simulated implementation of execution/executor's
// Client interface — it never places a real or testnet order. FetchPortfolio
// reads execution/paperstore's own ledger instead of a Binance account, and
// Execute "fills" instantly at the given price and updates that same
// ledger. Passing this into strategist/runner.RunWithExecutor runs the
// exact real decision pipeline (analysis results, LLM call, risk-engine
// validation) with nothing but the money at stake swapped out.
package paperexec

import (
	"context"
	"fmt"

	"risk-engine/risk"

	"execution/executor"
	"execution/paperstore"
)

var _ executor.Client = (*Client)(nil)

type Client struct {
	store paperStore
}

type paperStore interface {
	Close()
	Portfolio(context.Context) (float64, map[string]float64, error)
	Enabled(context.Context) (bool, error)
	ApplyFill(context.Context, string, string, string, float64, float64) error
}

func New(ctx context.Context, dsn string) (*Client, error) {
	store, err := paperstore.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("paperexec: connect storage: %w", err)
	}
	return &Client{store: store}, nil
}

func (c *Client) Close() {
	c.store.Close()
}

func (c *Client) FetchPortfolio(ctx context.Context) (float64, map[string]float64, error) {
	return c.store.Portfolio(ctx)
}

// Execute never touches an exchange — it immediately "fills" at the
// given price and applies the result to the paper ledger. clientOrderID
// is the strategist decision ID (see strategist/runner.Run), which
// ApplyFill also records into paper_decision_ids so the real Decisions
// dashboard can exclude it.
func (c *Client) Execute(ctx context.Context, asset string, side risk.Side, quantity, price float64, clientOrderID string) (executor.Outcome, error) {
	enabled, err := c.store.Enabled(ctx)
	if err != nil {
		return executor.Outcome{}, fmt.Errorf("paperexec: %s: check enabled: %w", asset, err)
	}
	if !enabled {
		return executor.Outcome{}, fmt.Errorf("paperexec: %s: paper trading is disabled", asset)
	}
	if err := c.store.ApplyFill(ctx, clientOrderID, asset, string(side), quantity, price); err != nil {
		return executor.Outcome{}, fmt.Errorf("paperexec: %s: apply fill: %w", asset, err)
	}
	return executor.Outcome{
		OrderID:        clientOrderID,
		ClientOrderID:  clientOrderID,
		Status:         "filled",
		FilledQuantity: quantity,
		FilledPrice:    price,
	}, nil
}
