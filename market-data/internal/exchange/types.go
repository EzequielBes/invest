package exchange

import (
	"context"
	"time"
)

type Timeframe string

const (
	Timeframe1m Timeframe = "1m"
	Timeframe1h Timeframe = "1h"
	Timeframe1d Timeframe = "1d"
)

type Candle struct {
	Symbol    string
	Timeframe Timeframe
	Time      time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

type FundingRate struct {
	Symbol string
	Time   time.Time
	Rate   float64
}

type OpenInterest struct {
	Symbol string
	Time   time.Time
	Value  float64
}

type LiquidationSide string

const (
	LiquidationBuy  LiquidationSide = "buy"
	LiquidationSell LiquidationSide = "sell"
)

type Liquidation struct {
	Symbol   string
	Time     time.Time
	Side     LiquidationSide
	Price    float64
	Quantity float64
}

// Collector is implemented once per exchange. Canonical symbols (e.g. "BTC",
// "ETH") are translated to the exchange's own instrument naming internally.
type Collector interface {
	Name() string
	FetchCandles(ctx context.Context, symbol string, tf Timeframe, from, to time.Time) ([]Candle, error)
	FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]FundingRate, error)
	FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]OpenInterest, error)
	StreamCandles(ctx context.Context, symbols []string, tf Timeframe) (<-chan Candle, error)
	StreamLiquidations(ctx context.Context, symbols []string) (<-chan Liquidation, error)
}
