// execution/executor/alpaca.go
package executor

import (
	"context"
	"fmt"
	"os"
	"time"

	"risk-engine/risk"

	"execution/internal/alpacaclient"
	"execution/internal/storage"
	"execution/paperstore"
)

const alpacaPaperBaseURL = "https://paper-api.alpaca.markets"

// Compile-time assertion that *AlpacaExecutor satisfies Client.
var _ Client = (*AlpacaExecutor)(nil)

// alpacaOps is the subset of *alpacaclient.Client this package calls —
// letting tests substitute a fake, same pattern as binanceOps.
type alpacaOps interface {
	GetAccount(ctx context.Context) (alpacaclient.Account, error)
	PlaceOrder(ctx context.Context, symbol, side string, qty float64, clientOrderID string) (alpacaclient.Order, error)
	GetOrderStatus(ctx context.Context, clientOrderID string) (alpacaclient.Order, error)
	CancelOrder(ctx context.Context, orderID string) error
}

// alpacaControlStore is the automation-controls gate Execute serializes
// order submission against — narrower than controlStore (which gates
// Binance's WithTestnetEnabled) since Alpaca has its own independent
// on/off switch.
type alpacaControlStore interface {
	Close()
	WithAlpacaPaperEnabled(context.Context, func() error) error
}

// AlpacaExecutor is the production implementation of Client for US stocks
// via Alpaca's paper-trading API — only paper trading, no live-money path.
type AlpacaExecutor struct {
	alpaca       alpacaOps
	store        executionStore
	controls     alpacaControlStore
	pollInterval time.Duration
	fillTimeout  time.Duration
}

// NewAlpacaClient reads ALPACA_API_KEY/ALPACA_API_SECRET_KEY from the
// environment (both required — never a silent no-op) and connects to
// storage using dsn, matching NewClient's convention.
func NewAlpacaClient(ctx context.Context, dsn string) (*AlpacaExecutor, error) {
	apiKey := os.Getenv("ALPACA_API_KEY")
	secret := os.Getenv("ALPACA_API_SECRET_KEY")
	if apiKey == "" || secret == "" {
		return nil, fmt.Errorf("executor: ALPACA_API_KEY and ALPACA_API_SECRET_KEY are required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("executor: connect storage: %w", err)
	}
	controls, err := paperstore.New(ctx, dsn)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("executor: connect automation controls: %w", err)
	}
	return &AlpacaExecutor{
		alpaca:       alpacaclient.New(apiKey, secret, alpacaPaperBaseURL),
		store:        store,
		controls:     controls,
		pollInterval: 2 * time.Second,
		fillTimeout:  30 * time.Second,
	}, nil
}

func (e *AlpacaExecutor) Close() {
	e.store.Close()
	e.controls.Close()
}

func (e *AlpacaExecutor) FetchPortfolio(ctx context.Context) (float64, map[string]float64, error) {
	account, err := e.alpaca.GetAccount(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("executor: fetch portfolio: %w", err)
	}
	positions := make(map[string]float64, len(account.Positions))
	for _, p := range account.Positions {
		positions[p.Asset] = p.Quantity
	}
	return account.Cash, positions, nil
}

// Execute places a market order and follows it until it fills or
// fillTimeout elapses, cancelling in the latter case — mirrors
// BinanceExecutor.Execute's shape exactly, swapped to alpacaOps and
// Alpaca's cancel-by-order-id API (see alpacaclient.CancelOrder).
func (e *AlpacaExecutor) Execute(ctx context.Context, asset string, side risk.Side, quantity, price float64, clientOrderID string) (Outcome, error) {
	var order alpacaclient.Order
	err := e.controls.WithAlpacaPaperEnabled(ctx, func() error {
		var err error
		order, err = e.alpaca.PlaceOrder(ctx, asset, string(side), quantity, clientOrderID)
		return err
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("executor: %s: authorize and place order: %w", asset, err)
	}

	deadline := time.Now().Add(e.fillTimeout)
	for order.Status != "filled" && time.Now().Before(deadline) {
		time.Sleep(e.pollInterval)
		next, statusErr := e.alpaca.GetOrderStatus(ctx, clientOrderID)
		if statusErr != nil {
			break
		}
		order = next
	}

	status := "filled"
	if order.Status != "filled" {
		if cancelErr := e.alpaca.CancelOrder(ctx, order.OrderID); cancelErr != nil {
			// Cancel can fail because the order filled in the race window
			// between our last poll and this call — re-check status once
			// before giving up, so a real fill is never silently lost.
			refetched, statusErr := e.alpaca.GetOrderStatus(ctx, clientOrderID)
			if statusErr != nil {
				return Outcome{}, fmt.Errorf("executor: %s: cancel order: %w (status re-check also failed: %v)", asset, cancelErr, statusErr)
			}
			if refetched.Status == "new" || refetched.Status == "partially_filled" {
				return Outcome{}, fmt.Errorf("executor: %s: cancel order: %w (order still open per status re-check, not safe to treat as cancelled)", asset, cancelErr)
			}
			order = refetched
		} else {
			// Alpaca's cancel returns 204 with no body — re-fetch to learn
			// the resulting status rather than assuming "canceled" (a
			// cancel request can still race a fill server-side).
			refetched, statusErr := e.alpaca.GetOrderStatus(ctx, clientOrderID)
			if statusErr != nil {
				return Outcome{}, fmt.Errorf("executor: %s: cancel order: re-check status: %w", asset, statusErr)
			}
			order = refetched
		}
		switch {
		case order.Status == "filled":
			status = "filled"
		case order.FilledQty > 0:
			status = "partial"
		default:
			status = "cancelled"
		}
	}

	outcome := Outcome{
		OrderID: order.OrderID, ClientOrderID: order.ClientOrderID, Status: status,
		FilledQuantity: order.FilledQty, FilledPrice: order.FilledAvgPrice,
	}
	err = e.store.SaveExecution(ctx, storage.Execution{
		ID: clientOrderID, Asset: asset, Side: string(side),
		RequestedQuantity: quantity, Price: price,
		OrderID: outcome.OrderID, ClientOrderID: clientOrderID,
		Status: status, FilledQuantity: outcome.FilledQuantity, FilledPrice: outcome.FilledPrice,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return outcome, fmt.Errorf("executor: %s: save execution: %w", asset, err)
	}
	return outcome, nil
}
