// execution/executor/client.go
package executor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"risk-engine/risk"

	"execution/internal/binanceclient"
	"execution/internal/storage"
)

const testnetBaseURL = "https://testnet.binancefuture.com"

// Compile-time assertion that *BinanceExecutor satisfies Client — same
// convention used in market-data/internal/exchange/binance/binance.go.
var _ Client = (*BinanceExecutor)(nil)

// Outcome is the real result of attempting to execute one order on the
// exchange — filled, partially filled (then cancelled at timeout), or
// cancelled with nothing filled. Never an error on its own; a timeout
// without any fill is still a valid Outcome, not a failure.
type Outcome struct {
	OrderID        string
	ClientOrderID  string
	Status         string // "filled", "partial", "cancelled"
	FilledQuantity float64
	FilledPrice    float64
}

// Client is the executor's public interface — FetchPortfolio to read the
// exchange's real balance/positions before sizing a decision, Execute to
// place and follow one order through to fill or cancellation. A fake
// implementing this interface lets strategist's tests exercise the sell
// clamp and execution wiring without a real exchange connection.
type Client interface {
	// FetchPortfolio returns cash and a map of asset symbol to held
	// quantity — the same shape strategist's existing buildPortfolio
	// already expects, so callers price and value positions themselves.
	FetchPortfolio(ctx context.Context) (cash float64, positions map[string]float64, err error)
	Execute(ctx context.Context, asset string, side risk.Side, quantity, price float64, clientOrderID string) (Outcome, error)
}

// binanceOps is the subset of *binanceclient.Client this package calls —
// letting tests substitute a fake instead of hitting the real Binance API.
type binanceOps interface {
	GetAccount(ctx context.Context) (binanceclient.Account, error)
	PlaceLimitOrder(ctx context.Context, asset, side string, quantity, price float64, clientOrderID string) (binanceclient.Order, error)
	GetOrderStatus(ctx context.Context, asset, clientOrderID string) (binanceclient.Order, error)
	CancelOrder(ctx context.Context, asset, clientOrderID string) (binanceclient.Order, error)
}

// executionStore is the subset of *storage.Store this package calls —
// same reason as binanceOps: fakeable in tests.
type executionStore interface {
	SaveExecution(ctx context.Context, e storage.Execution) error
	Close()
}

// BinanceExecutor is the production implementation of Client.
type BinanceExecutor struct {
	binance      binanceOps
	store        executionStore
	pollInterval time.Duration
	fillTimeout  time.Duration
}

// NewClient reads BINANCE_API_KEY/BINANCE_API_SECRET from the
// environment (both required — never a silent no-op) and connects to
// storage using dsn, matching this repo's existing storage.New(ctx, dsn)
// convention (the caller reads DATABASE_URL and passes it in).
func NewClient(ctx context.Context, dsn string) (*BinanceExecutor, error) {
	apiKey := os.Getenv("BINANCE_API_KEY")
	secret := os.Getenv("BINANCE_API_SECRET")
	if apiKey == "" || secret == "" {
		return nil, fmt.Errorf("executor: BINANCE_API_KEY and BINANCE_API_SECRET are required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("executor: connect storage: %w", err)
	}
	return &BinanceExecutor{
		binance:      binanceclient.New(apiKey, secret, testnetBaseURL),
		store:        store,
		pollInterval: 2 * time.Second,
		fillTimeout:  30 * time.Second,
	}, nil
}

func (e *BinanceExecutor) Close() {
	e.store.Close()
}

func (e *BinanceExecutor) FetchPortfolio(ctx context.Context) (float64, map[string]float64, error) {
	account, err := e.binance.GetAccount(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("executor: fetch portfolio: %w", err)
	}
	positions := make(map[string]float64, len(account.Positions))
	for _, p := range account.Positions {
		positions[p.Asset] = p.Quantity
	}
	return account.AvailableBalance, positions, nil
}

// Execute places a limit order and follows it until it fills or
// fillTimeout elapses, cancelling in the latter case. The resulting
// Outcome — filled, partial, or cancelled — is always persisted via
// SaveExecution before returning, using clientOrderID as the row's ID
// (the same value the caller minted as the decision's own ID).
func (e *BinanceExecutor) Execute(ctx context.Context, asset string, side risk.Side, quantity, price float64, clientOrderID string) (Outcome, error) {
	order, err := e.binance.PlaceLimitOrder(ctx, asset, string(side), quantity, price, clientOrderID)
	if err != nil {
		return Outcome{}, fmt.Errorf("executor: %s: place order: %w", asset, err)
	}

	deadline := time.Now().Add(e.fillTimeout)
	for order.Status != "FILLED" && time.Now().Before(deadline) {
		time.Sleep(e.pollInterval)
		order, err = e.binance.GetOrderStatus(ctx, asset, clientOrderID)
		if err != nil {
			return Outcome{}, fmt.Errorf("executor: %s: get order status: %w", asset, err)
		}
	}

	status := "filled"
	if order.Status != "FILLED" {
		cancelled, err := e.binance.CancelOrder(ctx, asset, clientOrderID)
		if err != nil {
			return Outcome{}, fmt.Errorf("executor: %s: cancel order: %w", asset, err)
		}
		order = cancelled
		if order.ExecutedQty > 0 {
			status = "partial"
		} else {
			status = "cancelled"
		}
	}

	outcome := Outcome{
		OrderID:        strconv.FormatInt(order.OrderID, 10),
		ClientOrderID:  order.ClientOrderID,
		Status:         status,
		FilledQuantity: order.ExecutedQty,
		FilledPrice:    order.AvgPrice,
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
