// Package alpaca implements a market-data collector for US stocks via
// Alpaca's free-tier historical bars API. Alpaca has no perpetual-futures
// concepts (funding rate, open interest) — those methods return an empty
// result, not an error, the same way market-data's other collectors
// legitimately no-op parts of the Collector interface that don't apply.
package alpaca

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"market-data/internal/exchange"
	"market-data/internal/httpclient"
)

// Compile-time assertion that *Collector satisfies exchange.Collector.
var _ exchange.Collector = (*Collector)(nil)

type Collector struct {
	client    *httpclient.Client
	baseURL   string
	apiKey    string
	apiSecret string
}

func New(client *httpclient.Client, apiKey, apiSecret string) *Collector {
	return &Collector{client: client, baseURL: "https://data.alpaca.markets", apiKey: apiKey, apiSecret: apiSecret}
}

func (c *Collector) Name() string { return "alpaca" }

func (c *Collector) authHeaders() map[string]string {
	return map[string]string{"APCA-API-KEY-ID": c.apiKey, "APCA-API-SECRET-KEY": c.apiSecret}
}

var timeframeCode = map[exchange.Timeframe]string{
	exchange.Timeframe1m: "1Min",
	exchange.Timeframe1h: "1Hour",
	exchange.Timeframe1d: "1Day",
}

type barsResponse struct {
	Bars map[string][]bar `json:"bars"`
}

type bar struct {
	Time   time.Time `json:"t"`
	Open   float64   `json:"o"`
	High   float64   `json:"h"`
	Low    float64   `json:"l"`
	Close  float64   `json:"c"`
	Volume float64   `json:"v"`
}

// FetchCandles reads historical bars from Alpaca's free (IEX feed) tier —
// real-time SIP data requires a paid subscription; IEX is 15-minute
// delayed, acceptable for this system's swing-frequency trading.
func (c *Collector) FetchCandles(ctx context.Context, symbol string, tf exchange.Timeframe, from, to time.Time) ([]exchange.Candle, error) {
	code, ok := timeframeCode[tf]
	if !ok {
		return nil, fmt.Errorf("alpaca: unsupported timeframe %q", tf)
	}
	url := fmt.Sprintf("%s/v2/stocks/bars?symbols=%s&timeframe=%s&feed=iex&limit=10000", c.baseURL, symbol, code)
	if !from.IsZero() {
		url += "&start=" + from.UTC().Format(time.RFC3339)
	}
	if !to.IsZero() {
		url += "&end=" + to.UTC().Format(time.RFC3339)
	}

	body, err := c.client.GetWithHeaders(ctx, url, c.authHeaders())
	if err != nil {
		return nil, err
	}
	var resp barsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("alpaca: decode candles: %w", err)
	}

	bars := resp.Bars[symbol]
	candles := make([]exchange.Candle, 0, len(bars))
	for _, b := range bars {
		candles = append(candles, exchange.Candle{
			Symbol: symbol, Timeframe: tf, Time: b.Time,
			Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume,
		})
	}
	return candles, nil
}

// FetchFunding always returns empty: funding rate is a perpetual-futures
// concept that doesn't apply to stocks.
func (c *Collector) FetchFunding(ctx context.Context, symbol string, from, to time.Time) ([]exchange.FundingRate, error) {
	return nil, nil
}

// FetchOpenInterest always returns empty: open interest is a perpetual-
// futures concept that doesn't apply to stocks.
func (c *Collector) FetchOpenInterest(ctx context.Context, symbol string, from, to time.Time) ([]exchange.OpenInterest, error) {
	return nil, nil
}

// pollInterval and candleWidth mirror binance's collector exactly — same
// REST-polling fallback shape, since Alpaca's free tier has no WebSocket
// streaming either.
var pollInterval = map[exchange.Timeframe]time.Duration{
	exchange.Timeframe1m: 30 * time.Second,
	exchange.Timeframe1h: 10 * time.Minute,
	exchange.Timeframe1d: 30 * time.Minute,
}

var candleWidth = map[exchange.Timeframe]time.Duration{
	exchange.Timeframe1m: time.Minute,
	exchange.Timeframe1h: time.Hour,
	exchange.Timeframe1d: 24 * time.Hour,
}

// StreamCandles polls FetchCandles on an interval — Alpaca's free tier has
// no WebSocket streaming, following the same REST-polling fallback
// pattern binance/bybit/okx already use when their native streaming isn't
// available in this environment.
func (c *Collector) StreamCandles(ctx context.Context, symbols []string, tf exchange.Timeframe) (<-chan exchange.Candle, error) {
	if _, ok := timeframeCode[tf]; !ok {
		return nil, fmt.Errorf("alpaca: unsupported timeframe %q", tf)
	}
	interval := pollInterval[tf]
	lookback := 3 * interval
	if floor := 2 * candleWidth[tf]; lookback < floor {
		lookback = floor
	}

	out := make(chan exchange.Candle)
	go func() {
		defer close(out)
		log.Printf("alpaca: polling REST bars for live candles (tf=%s, interval=%s)", tf, interval)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		poll := func() {
			from := time.Now().Add(-lookback)
			for _, symbol := range symbols {
				candles, err := c.FetchCandles(ctx, symbol, tf, from, time.Time{})
				if err != nil {
					log.Printf("alpaca: poll candles for %s: %v", symbol, err)
					continue
				}
				for _, candle := range candles {
					select {
					case out <- candle:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		poll()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				poll()
			}
		}
	}()
	return out, nil
}

// StreamLiquidations always returns an empty, immediately-closed stream:
// liquidations are a perpetual-futures concept that doesn't apply to
// stocks.
func (c *Collector) StreamLiquidations(ctx context.Context, symbols []string) (<-chan exchange.Liquidation, error) {
	ch := make(chan exchange.Liquidation)
	close(ch)
	return ch, nil
}
