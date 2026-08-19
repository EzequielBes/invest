// execution/internal/binanceclient/orders.go
package binanceclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Order is the subset of Binance's order response this client needs,
// shared by PlaceLimitOrder, GetOrderStatus, and CancelOrder — all three
// endpoints return the same shape.
type Order struct {
	OrderID       int64
	ClientOrderID string
	Status        string // NEW, PARTIALLY_FILLED, FILLED, CANCELED, EXPIRED, REJECTED
	ExecutedQty   float64
	AvgPrice      float64
}

type orderResponse struct {
	OrderID       int64  `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
	Status        string `json:"status"`
	ExecutedQty   string `json:"executedQty"`
	AvgPrice      string `json:"avgPrice"`
}

func parseOrder(body []byte) (Order, error) {
	var raw orderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Order{}, fmt.Errorf("unmarshal order: %w", err)
	}
	executedQty, err := strconv.ParseFloat(raw.ExecutedQty, 64)
	if err != nil {
		return Order{}, fmt.Errorf("parse executedQty: %w", err)
	}
	avgPrice, err := strconv.ParseFloat(raw.AvgPrice, 64)
	if err != nil {
		return Order{}, fmt.Errorf("parse avgPrice: %w", err)
	}
	return Order{
		OrderID:       raw.OrderID,
		ClientOrderID: raw.ClientOrderID,
		Status:        raw.Status,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
	}, nil
}

// symbolFor converts a canonical asset symbol ("BTC") to Binance's
// USDT-margined perpetual futures symbol ("BTCUSDT") — same convention
// market-data's binance collector uses.
func symbolFor(asset string) string { return asset + "USDT" }

func (c *Client) PlaceLimitOrder(ctx context.Context, asset, side string, quantity, price float64, clientOrderID string) (Order, error) {
	params := url.Values{}
	params.Set("symbol", symbolFor(asset))
	params.Set("side", strings.ToUpper(side))
	params.Set("type", "LIMIT")
	params.Set("timeInForce", "GTC")
	params.Set("quantity", strconv.FormatFloat(quantity, 'f', -1, 64))
	params.Set("price", strconv.FormatFloat(price, 'f', -1, 64))
	params.Set("newClientOrderId", clientOrderID)

	body, err := c.signedRequest(ctx, http.MethodPost, "/fapi/v1/order", params)
	if err != nil {
		return Order{}, fmt.Errorf("binance: place order: %w", err)
	}
	return parseOrder(body)
}

func (c *Client) GetOrderStatus(ctx context.Context, asset, clientOrderID string) (Order, error) {
	params := url.Values{}
	params.Set("symbol", symbolFor(asset))
	params.Set("origClientOrderId", clientOrderID)

	body, err := c.signedRequest(ctx, http.MethodGet, "/fapi/v1/order", params)
	if err != nil {
		return Order{}, fmt.Errorf("binance: get order status: %w", err)
	}
	return parseOrder(body)
}

func (c *Client) CancelOrder(ctx context.Context, asset, clientOrderID string) (Order, error) {
	params := url.Values{}
	params.Set("symbol", symbolFor(asset))
	params.Set("origClientOrderId", clientOrderID)

	body, err := c.signedRequest(ctx, http.MethodDelete, "/fapi/v1/order", params)
	if err != nil {
		return Order{}, fmt.Errorf("binance: cancel order: %w", err)
	}
	return parseOrder(body)
}
