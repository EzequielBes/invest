package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"market-data/internal/exchange"
	"market-data/internal/httpclient"
)

// Compile-time assertion that *Collector satisfies exchange.Collector. If
// the interface gains or changes a method, this fails the build here
// instead of surfacing later at wiring time.
var _ exchange.Collector = (*Collector)(nil)

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

// pollInterval maps a timeframe to how often StreamCandles re-polls the
// futures klines REST endpoint for it. Every entry in timeframeCode has a
// corresponding poll interval.
var pollInterval = map[exchange.Timeframe]time.Duration{
	exchange.Timeframe1m: 30 * time.Second,
	exchange.Timeframe1h: 10 * time.Minute,
	exchange.Timeframe1d: 30 * time.Minute,
}

// StreamCandles polls the Binance futures REST klines endpoint on a timer
// rather than streaming over WebSocket.
//
// Binance's futures WebSocket (wss://fstream.binance.com) is unreachable
// from this environment: the handshake completes and SUBSCRIBE is even
// acknowledged, but no market-data frames ever arrive (live probes across
// multiple streams and IPs all end in i/o timeout, while Binance spot WS,
// Bybit WS, and OKX WS all stream fine from the same container — so this is
// a server-side restriction on Binance derivatives streams, not our
// wsclient or the network). The futures REST API works fine, so this
// reuses FetchCandles (already implemented and tested in Task 7) behind a
// simple poller. Re-emitting a candle Binance already sent is harmless:
// storage.InsertCandles upserts on conflict.
func (c *Collector) StreamCandles(ctx context.Context, symbols []string, tf exchange.Timeframe) (<-chan exchange.Candle, error) {
	if _, ok := timeframeCode[tf]; !ok {
		return nil, fmt.Errorf("binance: unsupported timeframe %q", tf)
	}
	interval := pollInterval[tf]
	lookback := 3 * interval

	out := make(chan exchange.Candle)
	go func() {
		defer close(out)
		log.Printf("binance: futures WS unavailable in this environment, polling REST klines for live candles (tf=%s, interval=%s)", tf, interval)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		poll := func() {
			from := time.Now().Add(-lookback)
			for _, symbol := range symbols {
				candles, err := c.FetchCandles(ctx, symbol, tf, from, time.Time{})
				if err != nil {
					log.Printf("binance: poll candles for %s: %v", symbol, err)
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

// StreamLiquidations returns an empty, immediately-closed stream.
//
// Binance only exposes liquidations via the futures WebSocket
// (!forceOrder@arr) — there is no REST equivalent to poll. That WebSocket
// is the same one confirmed unreachable from this environment for
// StreamCandles (handshake and SUBSCRIBE succeed, but no frames ever
// arrive). So this capability is genuinely unavailable here, not merely
// unimplemented. Do NOT "fix" this by wiring up the WS: it will connect
// successfully and silently deliver nothing, which is worse than an
// honest empty stream. Bybit and OKX still provide liquidations, so the
// system as a whole still satisfies the spec.
func (c *Collector) StreamLiquidations(ctx context.Context, symbols []string) (<-chan exchange.Liquidation, error) {
	log.Printf("binance: liquidations unavailable — only served over the futures WS (!forceOrder@arr), which is blocked in this environment; returning empty stream")
	out := make(chan exchange.Liquidation)
	go func() {
		defer close(out)
		<-ctx.Done()
	}()
	return out, nil
}
