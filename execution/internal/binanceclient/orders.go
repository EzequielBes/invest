// execution/internal/binanceclient/orders.go
package binanceclient

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type lotSize struct {
	minQty    *big.Rat
	stepSize  *big.Rat
	precision int
}

func decimalPrecision(value string) int {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) == 1 {
		return 0
	}
	return len(strings.TrimRight(parts[1], "0"))
}

func parseLotSize(minQty, stepSize string) (lotSize, error) {
	min, ok := new(big.Rat).SetString(minQty)
	if !ok {
		return lotSize{}, fmt.Errorf("invalid minQty %q", minQty)
	}
	step, ok := new(big.Rat).SetString(stepSize)
	if !ok || step.Sign() <= 0 {
		return lotSize{}, fmt.Errorf("invalid stepSize %q", stepSize)
	}
	return lotSize{minQty: min, stepSize: step, precision: decimalPrecision(stepSize)}, nil
}

func (c *Client) lotSizeFor(ctx context.Context, symbol string) (lotSize, error) {
	c.filterMu.Lock()
	defer c.filterMu.Unlock()
	if filter, ok := c.filters[symbol]; ok {
		return filter, nil
	}
	body, err := c.publicRequest(ctx, "/fapi/v1/exchangeInfo", url.Values{"symbol": {symbol}})
	if err != nil {
		return lotSize{}, fmt.Errorf("get exchange info: %w", err)
	}
	var response struct {
		Symbols []struct {
			Symbol  string `json:"symbol"`
			Filters []struct {
				FilterType string `json:"filterType"`
				MinQty     string `json:"minQty"`
				StepSize   string `json:"stepSize"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return lotSize{}, fmt.Errorf("unmarshal exchange info: %w", err)
	}
	for _, item := range response.Symbols {
		if item.Symbol != symbol {
			continue
		}
		for _, filter := range item.Filters {
			if filter.FilterType == "LOT_SIZE" {
				parsed, err := parseLotSize(filter.MinQty, filter.StepSize)
				if err != nil {
					return lotSize{}, err
				}
				c.filters[symbol] = parsed
				return parsed, nil
			}
		}
	}
	return lotSize{}, fmt.Errorf("LOT_SIZE filter not found for %s", symbol)
}

func (l lotSize) roundQuantity(quantity float64) (string, error) {
	quantityRat, ok := new(big.Rat).SetString(strconv.FormatFloat(quantity, 'f', -1, 64))
	if !ok || quantityRat.Sign() <= 0 {
		return "", fmt.Errorf("quantity must be positive")
	}
	steps := new(big.Rat).Quo(quantityRat, l.stepSize)
	units := new(big.Int).Quo(steps.Num(), steps.Denom())
	rounded := new(big.Rat).Mul(new(big.Rat).SetInt(units), l.stepSize)
	if rounded.Cmp(l.minQty) < 0 {
		return "", fmt.Errorf("quantity is below minimum")
	}
	return rounded.FloatString(l.precision), nil
}

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
	symbol := symbolFor(asset)
	filter, err := c.lotSizeFor(ctx, symbol)
	if err != nil {
		return Order{}, fmt.Errorf("binance: place order: %w", err)
	}
	roundedQuantity, err := filter.roundQuantity(quantity)
	if err != nil {
		return Order{}, fmt.Errorf("binance: place order: %s quantity: %w", symbol, err)
	}
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", strings.ToUpper(side))
	params.Set("type", "LIMIT")
	params.Set("timeInForce", "GTC")
	params.Set("quantity", roundedQuantity)
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
