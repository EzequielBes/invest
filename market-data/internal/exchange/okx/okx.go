package okx

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

// wsCandle is a parsed OKX WS candle row, structurally identical to
// okxKline's fields plus the trailing confirm flag:
// [ts, open, high, low, close, vol, volCcy, volCcyQuote, confirm]. Live-
// verified in this environment (Task 12) against wss://ws.okx.com:8443/ws/v5/business:
// confirm is "0" for the still-forming candle and flips to "1" exactly once,
// on the frame emitted right at the bar boundary, after which a fresh row
// for the next bar starts back at confirm "0".
type wsCandle struct {
	Ts      int64
	Open    float64
	High    float64
	Low     float64
	Close   float64
	Volume  float64
	Confirm bool
}

func (k *wsCandle) UnmarshalJSON(data []byte) error {
	var raw []string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) < 9 {
		return fmt.Errorf("okx: unexpected ws candle shape, got %d fields", len(raw))
	}
	ts, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil {
		return fmt.Errorf("okx: ws candle ts: %w", err)
	}
	k.Ts = ts
	fields := []*float64{&k.Open, &k.High, &k.Low, &k.Close, &k.Volume}
	for i, f := range fields {
		v, err := strconv.ParseFloat(raw[i+1], 64)
		if err != nil {
			return fmt.Errorf("okx: ws candle field %d: %w", i+1, err)
		}
		*f = v
	}
	k.Confirm = raw[8] == "1"
	return nil
}

type wsCandleMsg struct {
	Arg struct {
		InstID string `json:"instId"`
	} `json:"arg"`
	Data []wsCandle `json:"data"`
}

// StreamCandles subscribes to OKX's public business WS (confirmed reachable
// from this environment — see Task 8/12) for candle{bar} channels and emits
// a Candle only for confirmed (closed) bars; in-progress bars (confirm:"0")
// are dropped to avoid re-emitting a still-forming candle on every tick.
func (c *Collector) StreamCandles(ctx context.Context, symbols []string, tf exchange.Timeframe) (<-chan exchange.Candle, error) {
	code, ok := timeframeCode[tf]
	if !ok {
		return nil, fmt.Errorf("okx: unsupported timeframe %q", tf)
	}
	channel := "candle" + code
	args := make([]map[string]string, len(symbols))
	symbolOf := map[string]string{}
	for i, s := range symbols {
		args[i] = map[string]string{"channel": channel, "instId": instID(s)}
		symbolOf[instID(s)] = s
	}

	out := make(chan exchange.Candle)
	go func() {
		defer close(out)
		wsclient.Connect(ctx, "wss://ws.okx.com:8443/ws/v5/business", func(raw []byte) {
			var msg wsCandleMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				return
			}
			sym, ok := symbolOf[msg.Arg.InstID]
			if !ok {
				return
			}
			for _, row := range msg.Data {
				if !row.Confirm {
					continue
				}
				select {
				case out <- exchange.Candle{
					Symbol: sym, Timeframe: tf, Time: time.UnixMilli(row.Ts),
					Open: row.Open, High: row.High, Low: row.Low,
					Close: row.Close, Volume: row.Volume,
				}:
				case <-ctx.Done():
				}
			}
		}, wsclient.OnConnect(func(conn *websocket.Conn) error {
			return conn.WriteJSON(map[string]any{"op": "subscribe", "args": args})
		}))
	}()
	return out, nil
}

// liquidationKey returns the in-memory dedup key for a liquidation, unique
// enough per poll cycle to distinguish distinct fills for the same symbol.
func liquidationKey(l exchange.Liquidation) string {
	return fmt.Sprintf("%s|%d|%f|%f", l.Symbol, l.Time.UnixNano(), l.Price, l.Quantity)
}

// pollLiquidationsOnce fetches liquidations for each symbol and forwards
// only those not already present in seen (mutated in place), keyed by
// liquidationKey. Factored out of StreamLiquidations so tests can drive the
// polling/dedup logic directly across multiple cycles without waiting on the
// real ticker interval.
func (c *Collector) pollLiquidationsOnce(ctx context.Context, symbols []string, seen map[string]bool, out chan<- exchange.Liquidation) {
	for _, symbol := range symbols {
		liqs, err := c.FetchLiquidations(ctx, symbol)
		if err != nil {
			continue
		}
		for _, l := range liqs {
			key := liquidationKey(l)
			if seen[key] {
				continue
			}
			seen[key] = true
			select {
			case out <- l:
			case <-ctx.Done():
				return
			}
		}
	}
}

// StreamLiquidations polls OKX's REST liquidation-orders endpoint
// (FetchLiquidations, Task 11) on a ticker, since OKX exposes liquidations
// only via REST and has no dedicated WS liquidation channel in this plan.
// Liquidations already forwarded (keyed by symbol+time+price+quantity) are
// not forwarded again on subsequent polls. seen grows unbounded for the life
// of the process — acceptable at ~100 liquidations/poll/symbol given the
// scheduler's periodic gap-recovery restarts (Task 15); add an eviction
// policy only if memory profiling shows it matters.
func (c *Collector) StreamLiquidations(ctx context.Context, symbols []string) (<-chan exchange.Liquidation, error) {
	out := make(chan exchange.Liquidation)
	go func() {
		defer close(out)
		seen := map[string]bool{}
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		c.pollLiquidationsOnce(ctx, symbols, seen, out)
		for {
			select {
			case <-ticker.C:
				c.pollLiquidationsOnce(ctx, symbols, seen, out)
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
