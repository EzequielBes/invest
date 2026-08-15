package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"market-data/internal/exchange"
	"market-data/internal/httpclient"
)

type Collector struct {
	client  *httpclient.Client
	baseURL string
}

func New(client *httpclient.Client) *Collector {
	return &Collector{client: client, baseURL: "https://www.okx.com"}
}

func (c *Collector) Name() string { return "okx" }

func instID(symbol string) string { return symbol + "-USDT-SWAP" }

var timeframeCode = map[exchange.Timeframe]string{
	exchange.Timeframe1m: "1m",
	exchange.Timeframe1h: "1H",
	exchange.Timeframe1d: "1D",
}

type response[T any] struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

func get[T any](ctx context.Context, c *Collector, url string) (T, error) {
	var zero T
	body, err := c.client.Get(ctx, url)
	if err != nil {
		return zero, err
	}
	var resp response[T]
	if err := json.Unmarshal(body, &resp); err != nil {
		return zero, fmt.Errorf("okx: decode: %w", err)
	}
	if resp.Code != "0" {
		return zero, fmt.Errorf("okx: code %s: %s", resp.Code, resp.Msg)
	}
	return resp.Data, nil
}

// okxKline represents a parsed OKX candle row with proper error handling.
// OKX candle rows: [ts, open, high, low, close, vol, volCcy, volCcyQuote, confirm]
// We extract the first 6 fields and propagate any parse errors.
type okxKline struct {
	Ts     int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

func (k *okxKline) UnmarshalJSON(data []byte) error {
	var raw []string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) < 6 {
		return fmt.Errorf("okx: unexpected kline shape, got %d fields", len(raw))
	}
	ts, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil {
		return fmt.Errorf("okx: kline ts: %w", err)
	}
	k.Ts = ts
	fields := []*float64{&k.Open, &k.High, &k.Low, &k.Close, &k.Volume}
	for i, f := range fields {
		v, err := strconv.ParseFloat(raw[i+1], 64)
		if err != nil {
			return fmt.Errorf("okx: kline field %d: %w", i+1, err)
		}
		*f = v
	}
	return nil
}

func (c *Collector) FetchCandles(ctx context.Context, symbol string, tf exchange.Timeframe, from, to time.Time) ([]exchange.Candle, error) {
	code, ok := timeframeCode[tf]
	if !ok {
		return nil, fmt.Errorf("okx: unsupported timeframe %q", tf)
	}
	url := fmt.Sprintf("%s/api/v5/market/candles?instId=%s&bar=%s&limit=300", c.baseURL, instID(symbol), code)
	if !to.IsZero() {
		url += fmt.Sprintf("&after=%d", to.UnixMilli())
	}
	if !from.IsZero() {
		url += fmt.Sprintf("&before=%d", from.UnixMilli())
	}

	rows, err := get[[]okxKline](ctx, c, url)
	if err != nil {
		return nil, err
	}

	candles := make([]exchange.Candle, 0, len(rows))
	for _, row := range rows {
		candles = append(candles, exchange.Candle{
			Symbol: symbol, Timeframe: tf, Time: time.UnixMilli(row.Ts),
			Open: row.Open, High: row.High, Low: row.Low,
			Close: row.Close, Volume: row.Volume,
		})
	}
	return candles, nil
}

func (c *Collector) FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]exchange.FundingRate, error) {
	url := fmt.Sprintf("%s/api/v5/public/funding-rate-history?instId=%s&limit=100", c.baseURL, instID(symbol))
	if !to.IsZero() {
		url += fmt.Sprintf("&after=%d", to.UnixMilli())
	}
	if !from.IsZero() {
		url += fmt.Sprintf("&before=%d", from.UnixMilli())
	}

	entries, err := get[[]struct {
		FundingRate exchange.StringFloat `json:"fundingRate"`
		FundingTime exchange.StringInt64 `json:"fundingTime"`
	}](ctx, c, url)
	if err != nil {
		return nil, err
	}

	rates := make([]exchange.FundingRate, 0, len(entries))
	for _, e := range entries {
		rates = append(rates, exchange.FundingRate{Symbol: symbol, Time: e.FundingTime.Time(), Rate: float64(e.FundingRate)})
	}
	return rates, nil
}

func (c *Collector) FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]exchange.OpenInterest, error) {
	// OKX only exposes current open interest, not a history endpoint — the
	// scheduler (Task 14) polls this periodically to build history over time
	// rather than backfilling it, unlike candles and funding.
	url := fmt.Sprintf("%s/api/v5/public/open-interest?instType=SWAP&instId=%s", c.baseURL, instID(symbol))

	entries, err := get[[]struct {
		Oi exchange.StringFloat `json:"oi"`
		Ts exchange.StringInt64 `json:"ts"`
	}](ctx, c, url)
	if err != nil {
		return nil, err
	}

	points := make([]exchange.OpenInterest, 0, len(entries))
	for _, e := range entries {
		points = append(points, exchange.OpenInterest{Symbol: symbol, Time: e.Ts.Time(), Value: float64(e.Oi)})
	}
	return points, nil
}

func (c *Collector) FetchLiquidations(ctx context.Context, symbol string) ([]exchange.Liquidation, error) {
	family := strings.TrimSuffix(instID(symbol), "-SWAP")
	url := fmt.Sprintf("%s/api/v5/public/liquidation-orders?instType=SWAP&instFamily=%s&state=filled&limit=100", c.baseURL, family)

	entries, err := get[[]struct {
		Details []struct {
			BkPx exchange.StringFloat `json:"bkPx"`
			Side string               `json:"side"`
			Sz   exchange.StringFloat `json:"sz"`
			Ts   exchange.StringInt64 `json:"ts"`
		} `json:"details"`
	}](ctx, c, url)
	if err != nil {
		return nil, err
	}

	var liqs []exchange.Liquidation
	for _, entry := range entries {
		for _, d := range entry.Details {
			side := exchange.LiquidationSell
			if d.Side == "buy" {
				side = exchange.LiquidationBuy
			}
			liqs = append(liqs, exchange.Liquidation{Symbol: symbol, Time: d.Ts.Time(), Side: side, Price: float64(d.BkPx), Quantity: float64(d.Sz)})
		}
	}
	return liqs, nil
}
