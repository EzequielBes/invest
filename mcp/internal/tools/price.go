// mcp/internal/tools/price.go
package tools

import (
	"context"
	"fmt"

	"risk-engine/risk"

	"mcp/internal/storage"
)

// GetLatestPriceArgs is the get_latest_price tool's input.
type GetLatestPriceArgs struct {
	Asset     string `json:"asset" jsonschema:"the asset symbol, e.g. BTC"`
	Timeframe string `json:"timeframe,omitempty" jsonschema:"candle timeframe to read, defaults to 1h"`
	Exchange  string `json:"exchange,omitempty" jsonschema:"exchange to read from, defaults to the platform's reference exchange (binance)"`
}

// GetLatestPriceResult is the get_latest_price tool's output.
type GetLatestPriceResult struct {
	Found bool    `json:"found"`
	Price float64 `json:"price,omitempty"`
}

// GetLatestPrice reads the most recent closed candle's close price. It is
// a plain Go function (not tied to the MCP SDK's handler signature) so it
// can be tested directly; Task 9's wiring in cmd/mcp-server/main.go adapts
// it to mcp.AddTool's handler shape.
func GetLatestPrice(ctx context.Context, store *storage.Store, args GetLatestPriceArgs) (GetLatestPriceResult, error) {
	if args.Asset == "" {
		return GetLatestPriceResult{}, fmt.Errorf("asset is required")
	}
	timeframe := args.Timeframe
	if timeframe == "" {
		timeframe = "1h"
	}
	exchange := args.Exchange
	if exchange == "" {
		exchange = risk.ReferenceExchange
	}
	price, found, err := store.LatestPrice(ctx, exchange, args.Asset, timeframe)
	if err != nil {
		return GetLatestPriceResult{}, fmt.Errorf("read latest price: %w", err)
	}
	return GetLatestPriceResult{Found: found, Price: price}, nil
}
