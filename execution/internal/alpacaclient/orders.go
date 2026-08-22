// execution/internal/alpacaclient/orders.go
package alpacaclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// Order is the subset of Alpaca's order object this client needs.
type Order struct {
	OrderID        string
	ClientOrderID  string
	Status         string // "new", "partially_filled", "filled", "canceled"
	FilledQty      float64
	FilledAvgPrice float64
}

type orderRequest struct {
	Symbol        string `json:"symbol"`
	Qty           string `json:"qty"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	TimeInForce   string `json:"time_in_force"`
	ClientOrderID string `json:"client_order_id"`
}

type orderResponse struct {
	ID             string  `json:"id"`
	ClientOrderID  string  `json:"client_order_id"`
	Status         string  `json:"status"`
	FilledQty      string  `json:"filled_qty"`
	FilledAvgPrice *string `json:"filled_avg_price"`
}

func parseOrder(body []byte) (Order, error) {
	var raw orderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Order{}, fmt.Errorf("alpaca: unmarshal order: %w", err)
	}
	filledQty, err := strconv.ParseFloat(raw.FilledQty, 64)
	if err != nil {
		return Order{}, fmt.Errorf("alpaca: parse filled_qty: %w", err)
	}
	var filledAvgPrice float64
	if raw.FilledAvgPrice != nil {
		filledAvgPrice, err = strconv.ParseFloat(*raw.FilledAvgPrice, 64)
		if err != nil {
			return Order{}, fmt.Errorf("alpaca: parse filled_avg_price: %w", err)
		}
	}
	return Order{
		OrderID: raw.ID, ClientOrderID: raw.ClientOrderID, Status: raw.Status,
		FilledQty: filledQty, FilledAvgPrice: filledAvgPrice,
	}, nil
}

// PlaceOrder submits a market order — no limit price needed since Alpaca
// stock orders fill during market hours at the prevailing price, and this
// system's decisions are already sized in shares, not notional value.
func (c *Client) PlaceOrder(ctx context.Context, symbol, side string, qty float64, clientOrderID string) (Order, error) {
	body, err := json.Marshal(orderRequest{
		Symbol: symbol, Qty: strconv.FormatFloat(qty, 'f', -1, 64), Side: side,
		Type: "market", TimeInForce: "day", ClientOrderID: clientOrderID,
	})
	if err != nil {
		return Order{}, err
	}
	resp, err := c.post(ctx, "/v2/orders", body)
	if err != nil {
		return Order{}, fmt.Errorf("alpaca: place order: %w", err)
	}
	return parseOrder(resp)
}

// GetOrderStatus reads an order by its client-supplied ID (this system's
// convention, same as binanceclient's GetOrderStatus).
func (c *Client) GetOrderStatus(ctx context.Context, clientOrderID string) (Order, error) {
	resp, err := c.get(ctx, "/v2/orders:by_client_order_id?client_order_id="+clientOrderID)
	if err != nil {
		return Order{}, fmt.Errorf("alpaca: get order status: %w", err)
	}
	return parseOrder(resp)
}

// CancelOrder requests cancellation by Alpaca's own order ID — unlike
// GetOrderStatus, Alpaca's cancel endpoint has no by-client-order-id
// variant, only DELETE /v2/orders/{order_id}. It returns 204 with no
// body on success; the caller must re-fetch via GetOrderStatus to learn
// the resulting status (a cancel can race a fill, so "accepted" doesn't
// necessarily mean "canceled").
func (c *Client) CancelOrder(ctx context.Context, orderID string) error {
	_, err := c.delete(ctx, "/v2/orders/"+orderID)
	if err != nil {
		return fmt.Errorf("alpaca: cancel order: %w", err)
	}
	return nil
}
