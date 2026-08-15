package binance

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
	return &Collector{client: client, baseURL: "https://fapi.binance.com"}
}

func (c *Collector) Name() string { return "binance" }

// instrument converts a canonical symbol ("BTC") to Binance's USDT-margined
// perpetual futures symbol ("BTCUSDT") — the only instrument type this
// collector tracks.
func instrument(symbol string) string { return symbol + "USDT" }

var timeframeCode = map[exchange.Timeframe]string{
	exchange.Timeframe1m: "1m",
	exchange.Timeframe1h: "1h",
	exchange.Timeframe1d: "1d",
}

type kline struct {
	OpenTime int64
	Open     float64
	High     float64
	Low      float64
	Close    float64
	Volume   float64
}

func (k *kline) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) < 6 {
		return fmt.Errorf("binance: unexpected kline shape, got %d fields", len(raw))
	}
	if err := json.Unmarshal(raw[0], &k.OpenTime); err != nil {
		return fmt.Errorf("binance: open time: %w", err)
	}
	fields := []*float64{&k.Open, &k.High, &k.Low, &k.Close, &k.Volume}
	for i, f := range fields {
		var s string
		if err := json.Unmarshal(raw[i+1], &s); err != nil {
			return fmt.Errorf("binance: kline field %d: %w", i+1, err)
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("binance: kline field %d parse: %w", i+1, err)
		}
		*f = v
	}
	return nil
}

func (c *Collector) FetchCandles(ctx context.Context, symbol string, tf exchange.Timeframe, from, to time.Time) ([]exchange.Candle, error) {
	code, ok := timeframeCode[tf]
	if !ok {
		return nil, fmt.Errorf("binance: unsupported timeframe %q", tf)
	}
	url := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=%s&limit=1500", c.baseURL, instrument(symbol), code)
	if !from.IsZero() {
		url += fmt.Sprintf("&startTime=%d", from.UnixMilli())
	}
	if !to.IsZero() {
		url += fmt.Sprintf("&endTime=%d", to.UnixMilli())
	}

	body, err := c.client.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var klines []kline
	if err := json.Unmarshal(body, &klines); err != nil {
		return nil, fmt.Errorf("binance: decode candles: %w", err)
	}

	candles := make([]exchange.Candle, 0, len(klines))
	for _, k := range klines {
		candles = append(candles, exchange.Candle{
			Symbol: symbol, Timeframe: tf, Time: time.UnixMilli(k.OpenTime),
			Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume,
		})
	}
	return candles, nil
}

type fundingEntry struct {
	FundingTime exchange.StringInt64 `json:"fundingTime"`
	FundingRate exchange.StringFloat `json:"fundingRate"`
}

func (c *Collector) FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]exchange.FundingRate, error) {
	url := fmt.Sprintf("%s/fapi/v1/fundingRate?symbol=%s&limit=1000", c.baseURL, instrument(symbol))
	if !from.IsZero() {
		url += fmt.Sprintf("&startTime=%d", from.UnixMilli())
	}
	if !to.IsZero() {
		url += fmt.Sprintf("&endTime=%d", to.UnixMilli())
	}

	body, err := c.client.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var entries []fundingEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("binance: decode funding: %w", err)
	}

	rates := make([]exchange.FundingRate, 0, len(entries))
	for _, e := range entries {
		rates = append(rates, exchange.FundingRate{Symbol: symbol, Time: e.FundingTime.Time(), Rate: float64(e.FundingRate)})
	}
	return rates, nil
}

type openInterestEntry struct {
	Timestamp       exchange.StringInt64 `json:"timestamp"`
	SumOpenInterest exchange.StringFloat `json:"sumOpenInterest"`
}

func (c *Collector) FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]exchange.OpenInterest, error) {
	url := fmt.Sprintf("%s/futures/data/openInterestHist?symbol=%s&period=1h&limit=500", c.baseURL, instrument(symbol))
	if !from.IsZero() {
		url += fmt.Sprintf("&startTime=%d", from.UnixMilli())
	}
	if !to.IsZero() {
		url += fmt.Sprintf("&endTime=%d", to.UnixMilli())
	}

	body, err := c.client.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var entries []openInterestEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("binance: decode open interest: %w", err)
	}

	points := make([]exchange.OpenInterest, 0, len(entries))
	for _, e := range entries {
		points = append(points, exchange.OpenInterest{Symbol: symbol, Time: e.Timestamp.Time(), Value: float64(e.SumOpenInterest)})
	}
	return points, nil
}
