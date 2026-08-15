package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"market-data/internal/exchange"
	"market-data/internal/httpclient"
	"market-data/internal/wsclient"
)

// Compile-time assertion that *Collector satisfies exchange.Collector.
var _ exchange.Collector = (*Collector)(nil)

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

type bybitKline struct {
	Start  int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

func (k *bybitKline) UnmarshalJSON(data []byte) error {
	var raw []string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) < 6 {
		return fmt.Errorf("bybit: unexpected kline shape, got %d fields", len(raw))
	}
	start, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil {
		return fmt.Errorf("bybit: kline start: %w", err)
	}
	k.Start = start
	fields := []*float64{&k.Open, &k.High, &k.Low, &k.Close, &k.Volume}
	for i, f := range fields {
		v, err := strconv.ParseFloat(raw[i+1], 64)
		if err != nil {
			return fmt.Errorf("bybit: kline field %d: %w", i+1, err)
		}
		*f = v
	}
	return nil
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
		List []bybitKline `json:"list"`
	}](ctx, c, url)
	if err != nil {
		return nil, err
	}

	candles := make([]exchange.Candle, 0, len(result.List))
	for _, row := range result.List {
		candles = append(candles, exchange.Candle{
			Symbol: symbol, Timeframe: tf, Time: time.UnixMilli(row.Start),
			Open: row.Open, High: row.High, Low: row.Low,
			Close: row.Close, Volume: row.Volume,
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

type wsEnvelope struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

type wsKline struct {
	Start   exchange.StringInt64 `json:"start"`
	Open    exchange.StringFloat `json:"open"`
	High    exchange.StringFloat `json:"high"`
	Low     exchange.StringFloat `json:"low"`
	Close   exchange.StringFloat `json:"close"`
	Volume  exchange.StringFloat `json:"volume"`
	Confirm bool                 `json:"confirm"`
}

// StreamCandles subscribes to Bybit's public linear WS (confirmed reachable
// from this environment, unlike Binance futures WS — see Task 8) for
// kline.{interval}.{symbol} topics and emits a Candle only for confirmed
// (closed) bars; in-progress bars (confirm:false) are dropped to avoid
// re-emitting a still-forming candle on every tick.
func (c *Collector) StreamCandles(ctx context.Context, symbols []string, tf exchange.Timeframe) (<-chan exchange.Candle, error) {
	code, ok := timeframeCode[tf]
	if !ok {
		return nil, fmt.Errorf("bybit: unsupported timeframe %q", tf)
	}
	topics := make([]string, len(symbols))
	symbolOf := map[string]string{}
	for i, s := range symbols {
		topics[i] = "kline." + code + "." + instrument(s)
		symbolOf[instrument(s)] = s
	}

	out := make(chan exchange.Candle)
	go func() {
		defer close(out)
		wsclient.Connect(ctx, "wss://stream.bybit.com/v5/public/linear", func(raw []byte) {
			var env wsEnvelope
			if err := json.Unmarshal(raw, &env); err != nil || !strings.HasPrefix(env.Topic, "kline.") {
				return
			}
			parts := strings.Split(env.Topic, ".")
			sym, ok := symbolOf[parts[len(parts)-1]]
			if !ok {
				return
			}
			var klines []wsKline
			if err := json.Unmarshal(env.Data, &klines); err != nil {
				return
			}
			for _, k := range klines {
				if !k.Confirm {
					continue
				}
				select {
				case out <- exchange.Candle{
					Symbol: sym, Timeframe: tf, Time: k.Start.Time(),
					Open: float64(k.Open), High: float64(k.High), Low: float64(k.Low), Close: float64(k.Close), Volume: float64(k.Volume),
				}:
				case <-ctx.Done():
				}
			}
		}, wsclient.OnConnect(func(conn *websocket.Conn) error {
			return conn.WriteJSON(map[string]any{"op": "subscribe", "args": topics})
		}))
	}()
	return out, nil
}

type wsLiquidation struct {
	Time     exchange.StringInt64 `json:"T"`
	Symbol   string               `json:"s"`
	Side     string               `json:"S"` // "Sell" = long liquidated, "Buy" = short liquidated
	Quantity exchange.StringFloat `json:"v"`
	Price    exchange.StringFloat `json:"p"`
}

// StreamLiquidations subscribes to Bybit's allLiquidation.{symbol} topic.
// Bybit renamed this from liquidation.{symbol}; live-verified in this
// environment (Task 10) that allLiquidation.BTCUSDT subscribes successfully
// while liquidation.BTCUSDT is rejected outright with
// "error:handler not found,topic:liquidation.BTCUSDT" — confirming the old
// name is retired, even though no live liquidation event arrived in the
// verification window (they are infrequent).
func (c *Collector) StreamLiquidations(ctx context.Context, symbols []string) (<-chan exchange.Liquidation, error) {
	topics := make([]string, len(symbols))
	symbolOf := map[string]string{}
	for i, s := range symbols {
		topics[i] = "allLiquidation." + instrument(s)
		symbolOf[instrument(s)] = s
	}

	out := make(chan exchange.Liquidation)
	go func() {
		defer close(out)
		wsclient.Connect(ctx, "wss://stream.bybit.com/v5/public/linear", func(raw []byte) {
			var env wsEnvelope
			if err := json.Unmarshal(raw, &env); err != nil || !strings.HasPrefix(env.Topic, "allLiquidation.") {
				return
			}
			var liqs []wsLiquidation
			if err := json.Unmarshal(env.Data, &liqs); err != nil {
				return
			}
			for _, l := range liqs {
				sym, ok := symbolOf[l.Symbol]
				if !ok {
					continue
				}
				side := exchange.LiquidationSell
				if l.Side == "Buy" {
					side = exchange.LiquidationBuy
				}
				select {
				case out <- exchange.Liquidation{Symbol: sym, Time: l.Time.Time(), Side: side, Price: float64(l.Price), Quantity: float64(l.Quantity)}:
				case <-ctx.Done():
				}
			}
		}, wsclient.OnConnect(func(conn *websocket.Conn) error {
			return conn.WriteJSON(map[string]any{"op": "subscribe", "args": topics})
		}))
	}()
	return out, nil
}
