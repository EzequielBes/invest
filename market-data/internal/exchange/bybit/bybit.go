package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"market-data/internal/exchange"
	"market-data/internal/httpclient"
)

type Collector struct {
	client  *httpclient.Client
	baseURL string
}

func New(client *httpclient.Client) *Collector {
	return &Collector{client: client, baseURL: "https://api.bybit.com"}
}

func (c *Collector) Name() string { return "bybit" }

func instrument(symbol string) string { return symbol + "USDT" }

var timeframeCode = map[exchange.Timeframe]string{
	exchange.Timeframe1m: "1",
	exchange.Timeframe1h: "60",
	exchange.Timeframe1d: "D",
}

type envelope[T any] struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  T      `json:"result"`
}

func get[T any](ctx context.Context, c *Collector, url string) (T, error) {
	var zero T
	body, err := c.client.Get(ctx, url)
	if err != nil {
		return zero, err
	}
	var env envelope[T]
	if err := json.Unmarshal(body, &env); err != nil {
		return zero, fmt.Errorf("bybit: decode: %w", err)
	}
	if env.RetCode != 0 {
		return zero, fmt.Errorf("bybit: retCode %d: %s", env.RetCode, env.RetMsg)
	}
	return env.Result, nil
}

func (c *Collector) FetchCandles(ctx context.Context, symbol string, tf exchange.Timeframe, from, to time.Time) ([]exchange.Candle, error) {
	code, ok := timeframeCode[tf]
	if !ok {
		return nil, fmt.Errorf("bybit: unsupported timeframe %q", tf)
	}
	url := fmt.Sprintf("%s/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=1000", c.baseURL, instrument(symbol), code)
	if !from.IsZero() {
		url += fmt.Sprintf("&start=%d", from.UnixMilli())
	}
	if !to.IsZero() {
		url += fmt.Sprintf("&end=%d", to.UnixMilli())
	}

	result, err := get[struct {
		List [][]string `json:"list"`
	}](ctx, c, url)
	if err != nil {
		return nil, err
	}

	candles := make([]exchange.Candle, 0, len(result.List))
	for _, row := range result.List {
		if len(row) < 6 {
			continue
		}
		candles = append(candles, exchange.Candle{
			Symbol: symbol, Timeframe: tf, Time: parseMillis(row[0]),
			Open: parseFloat(row[1]), High: parseFloat(row[2]), Low: parseFloat(row[3]),
			Close: parseFloat(row[4]), Volume: parseFloat(row[5]),
		})
	}
	return candles, nil
}

func (c *Collector) FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]exchange.FundingRate, error) {
	url := fmt.Sprintf("%s/v5/market/funding/history?category=linear&symbol=%s&limit=200", c.baseURL, instrument(symbol))
	if !from.IsZero() {
		url += fmt.Sprintf("&startTime=%d", from.UnixMilli())
	}
	if !to.IsZero() {
		url += fmt.Sprintf("&endTime=%d", to.UnixMilli())
	}

	result, err := get[struct {
		List []struct {
			FundingRate          exchange.StringFloat `json:"fundingRate"`
			FundingRateTimestamp exchange.StringInt64 `json:"fundingRateTimestamp"`
		} `json:"list"`
	}](ctx, c, url)
	if err != nil {
		return nil, err
	}

	rates := make([]exchange.FundingRate, 0, len(result.List))
	for _, e := range result.List {
		rates = append(rates, exchange.FundingRate{Symbol: symbol, Time: e.FundingRateTimestamp.Time(), Rate: float64(e.FundingRate)})
	}
	return rates, nil
}

func (c *Collector) FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]exchange.OpenInterest, error) {
	url := fmt.Sprintf("%s/v5/market/open-interest?category=linear&symbol=%s&intervalTime=1h&limit=200", c.baseURL, instrument(symbol))
	if !from.IsZero() {
		url += fmt.Sprintf("&startTime=%d", from.UnixMilli())
	}
	if !to.IsZero() {
		url += fmt.Sprintf("&endTime=%d", to.UnixMilli())
	}

	result, err := get[struct {
		List []struct {
			OpenInterest exchange.StringFloat `json:"openInterest"`
			Timestamp    exchange.StringInt64 `json:"timestamp"`
		} `json:"list"`
	}](ctx, c, url)
	if err != nil {
		return nil, err
	}

	points := make([]exchange.OpenInterest, 0, len(result.List))
	for _, e := range result.List {
		points = append(points, exchange.OpenInterest{Symbol: symbol, Time: e.Timestamp.Time(), Value: float64(e.OpenInterest)})
	}
	return points, nil
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseMillis(s string) time.Time {
	v, _ := strconv.ParseInt(s, 10, 64)
	return time.UnixMilli(v)
}
