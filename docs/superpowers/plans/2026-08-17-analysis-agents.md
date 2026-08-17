# Agentes de Análise Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `analysis` Go module: four domain agents (technical, derivatives, news, risk-context) that read data already collected by `market-data`/`risk-engine`, compute structured indicators, ask Claude to narrate them, and persist both to a new unified schema — run on demand via CLI.

**Architecture:** New independent Go module `analysis/`, same shape as `simulation/` (`cmd/`, `internal/`, `migrations/`), depending on `risk-engine` via a local `replace` for the `risk.ReferenceExchange` constant and for reading `risk_state`/`risk_limits` directly through `risk-engine/storage`. Four pure calculation packages (`indicators`, `derivatives`, `news`) with no database dependency, a thin `llm` wrapper around the Anthropic Go SDK behind an interface, and an `agents` package that orchestrates each domain (collect → compute → prompt → summarize).

**Tech Stack:** Go 1.22, `github.com/jackc/pgx/v5` (TimescaleDB), `github.com/google/uuid`, `github.com/anthropics/anthropic-sdk-go`.

**Spec:** `docs/superpowers/specs/2026-08-17-analysis-agents-design.md`

## Global Constraints

- Reference exchange for all market-data reads: `risk.ReferenceExchange` (currently `"binance"`), imported from `risk-engine/risk` — never hardcode `"binance"` directly in `analysis` code.
- LLM model: `claude-sonnet-5`, `MaxTokens: 512`, no `thinking` config, no beta headers — plain `client.Messages.New`.
- API key: read implicitly from the `ANTHROPIC_API_KEY` environment variable via `anthropic.NewClient()` — never pass it as a flag or read it manually.
- All row IDs (`analysis_runs.id`, `analysis_results.id`) are `uuid.NewString()` stored as Postgres `TEXT`, generated in Go — never a native `UUID` column, matching `simulation`'s established convention.
- Database DSN: `DATABASE_URL` env var for production code, `TEST_DATABASE_URL` for integration tests (tests `t.Skip` if unset) — same convention as `simulation`.
- A failed LLM call for one agent/asset is not a fatal error: indicators (already computed before the LLM call) are still persisted, the run continues to the next agent/asset, and `analysis_runs.status` only becomes `'failed'` if **every** agent/asset failed to produce a narrative.
- Reduced test rigor per current project preference: unit tests only for the three pure calculation packages (`indicators`, `derivatives`, `news`) and one end-to-end integration test for the CLI. No dedicated unit tests for storage read wrappers or agent orchestration glue — the integration test exercises those paths.

---

### Task 1: Module scaffold — go.mod, docker-compose, migration, DB connection

**Files:**
- Create: `analysis/go.mod`
- Create: `analysis/docker-compose.yml`
- Create: `analysis/migrations/001_init.sql`
- Create: `analysis/internal/storage/db.go`

**Interfaces:**
- Produces: `storage.Store` (holds a `*pgxpool.Pool`), `storage.New(ctx, dsn) (*Store, error)`, `(*Store) Close()` — every later storage task adds methods to this same `Store` type in the same package.

- [ ] **Step 1: Write `go.mod` with just the module line**

```go
module analysis

go 1.22
```

- [ ] **Step 2: Write `docker-compose.yml`**

```yaml
services:
  go:
    image: golang:1.22
    working_dir: /app
    volumes:
      - .:/app
      - ../risk-engine:/risk-engine
      - go-mod-cache:/go/pkg/mod
    environment:
      DATABASE_URL: postgres://marketdata:marketdata@timescaledb:5432/marketdata?sslmode=disable
      TEST_DATABASE_URL: postgres://marketdata:marketdata@timescaledb:5432/marketdata?sslmode=disable
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY:-}
    networks:
      - market-data_default
    command: ["sleep", "infinity"]

networks:
  market-data_default:
    external: true

volumes:
  go-mod-cache:
```

- [ ] **Step 3: Write `migrations/001_init.sql`**

```sql
-- analysis/migrations/001_init.sql
CREATE TABLE IF NOT EXISTS analysis_runs (
    id          TEXT PRIMARY KEY,
    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    timeframe   TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'running',
    error       TEXT
);

CREATE TABLE IF NOT EXISTS analysis_results (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES analysis_runs(id),
    agent_type  TEXT NOT NULL,
    asset       TEXT NOT NULL DEFAULT '',
    indicators  JSONB NOT NULL,
    narrative   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS analysis_results_run_id ON analysis_results (run_id);
```

- [ ] **Step 4: Write `internal/storage/db.go`**

```go
// analysis/internal/storage/db.go
package storage

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}
```

- [ ] **Step 5: Bring up the dev container and pin dependency versions**

Run (from `analysis/`):

```bash
COMPOSE_PROJECT_NAME=analysis-dev docker compose up -d
COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go get github.com/jackc/pgx/v5@v5.6.0
COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go get github.com/google/uuid@v1.6.0
COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go get github.com/anthropics/anthropic-sdk-go@latest
```

Then add a local replace for `risk-engine` by appending to `go.mod`:

```go
require risk-engine v0.0.0

replace risk-engine => ../risk-engine
```

Run `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go mod tidy` and verify `go build ./...` succeeds (it will build nothing yet, but must not error).

**Note for every later task in this plan:** every `docker compose exec go ...` command assumes the same `COMPOSE_PROJECT_NAME` prefix used here (`analysis-dev` in the main checkout; use a worktree-specific name instead if working in a git worktree — see the project's docker-compose worktree-collision convention). Verify the container is bound to the right directory with `COMPOSE_PROJECT_NAME=<name> docker compose exec go pwd` if in doubt.

Apply the migration against the shared TimescaleDB:

```bash
docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/001_init.sql
```

- [ ] **Step 6: Commit**

```bash
git add analysis/go.mod analysis/go.sum analysis/docker-compose.yml analysis/migrations/001_init.sql analysis/internal/storage/db.go
git commit -m "feat(analysis): scaffold module, docker-compose, and migration"
```

---

### Task 2: Market-data read helpers

**Files:**
- Create: `analysis/internal/storage/marketdata.go`

**Interfaces:**
- Consumes: `Store` from Task 1.
- Produces: `storage.Candle{Time, Close, Volume}`, `storage.NewsItem{Title, Body, URL, PublishedAt}`, and five read methods on `*Store` — `RecentCandles`, `LatestFundingRate`, `OpenInterestNear`, `RecentLiquidations`, `RecentNews` — consumed by Task 8/9's agents.

- [ ] **Step 1: Write `internal/storage/marketdata.go`**

```go
// analysis/internal/storage/marketdata.go
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Candle is the OHLCV shape read from market-data's candles table.
type Candle struct {
	Time   time.Time
	Close  float64
	Volume float64
}

// RecentCandles returns the last n closed candles for exchange/symbol/
// timeframe, oldest first.
func (s *Store) RecentCandles(ctx context.Context, exchange, symbol, timeframe string, n int) ([]Candle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, close, volume FROM (
			SELECT ts, close, volume FROM candles
			WHERE exchange = $1 AND symbol = $2 AND timeframe = $3
			ORDER BY ts DESC LIMIT $4
		) sub ORDER BY ts ASC
	`, exchange, symbol, timeframe, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candles []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.Time, &c.Close, &c.Volume); err != nil {
			return nil, err
		}
		candles = append(candles, c)
	}
	return candles, rows.Err()
}

// LatestFundingRate returns the most recent funding rate for exchange/
// symbol. found is false if none has been collected yet.
func (s *Store) LatestFundingRate(ctx context.Context, exchange, symbol string) (rate float64, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT rate FROM funding_rates WHERE exchange = $1 AND symbol = $2 ORDER BY ts DESC LIMIT 1
	`, exchange, symbol).Scan(&rate)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return rate, true, nil
}

// OpenInterestNear returns the open_interest value at or before at, for
// exchange/symbol. found is false if no such row exists.
func (s *Store) OpenInterestNear(ctx context.Context, exchange, symbol string, at time.Time) (value float64, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT value FROM open_interest
		WHERE exchange = $1 AND symbol = $2 AND ts <= $3
		ORDER BY ts DESC LIMIT 1
	`, exchange, symbol, at).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

// RecentLiquidations returns liquidations for exchange/symbol at or after
// since.
func (s *Store) RecentLiquidations(ctx context.Context, exchange, symbol string, since time.Time) ([]struct {
	Price    float64
	Quantity float64
}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT price, quantity FROM liquidations
		WHERE exchange = $1 AND symbol = $2 AND ts >= $3
	`, exchange, symbol, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []struct {
		Price    float64
		Quantity float64
	}
	for rows.Next() {
		var p, q float64
		if err := rows.Scan(&p, &q); err != nil {
			return nil, err
		}
		out = append(out, struct {
			Price    float64
			Quantity float64
		}{p, q})
	}
	return out, rows.Err()
}

// NewsItem is the shape read from market-data's news_items table.
type NewsItem struct {
	Title       string
	Body        string
	URL         string
	PublishedAt time.Time
}

// RecentNews returns news items published at or after since, across all
// sources — callers filter by asset in-memory.
func (s *Store) RecentNews(ctx context.Context, since time.Time) ([]NewsItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT title, body, url, published_at FROM news_items WHERE published_at >= $1
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []NewsItem
	for rows.Next() {
		var it NewsItem
		if err := rows.Scan(&it.Title, &it.Body, &it.URL, &it.PublishedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
```

- [ ] **Step 2: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go build ./...` (from `analysis/`; use a worktree-specific name instead of `analysis-dev` if working in a worktree). Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add analysis/internal/storage/marketdata.go
git commit -m "feat(analysis): market-data read helpers"
```

---

### Task 3: Run/result write helpers

**Files:**
- Create: `analysis/internal/storage/runs.go`

**Interfaces:**
- Consumes: `Store` from Task 1.
- Produces: `storage.Run{ID, StartedAt, Timeframe}`, `storage.Result{ID, RunID, AgentType, Asset, Indicators, Narrative, CreatedAt}`, `(*Store) CreateRun`, `(*Store) FinishRun`, `(*Store) SaveResult` — consumed by Task 10's CLI. Plus test-only helpers `DeleteRunForTest`, `RunStatus`, `ResultCount` consumed by Task 11's integration test.

- [ ] **Step 1: Write `internal/storage/runs.go`**

```go
// analysis/internal/storage/runs.go
package storage

import (
	"context"
	"encoding/json"
	"time"
)

type Run struct {
	ID        string
	StartedAt time.Time
	Timeframe string
}

func (s *Store) CreateRun(ctx context.Context, r Run) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO analysis_runs (id, started_at, timeframe, status)
		VALUES ($1, $2, $3, 'running')
	`, r.ID, r.StartedAt, r.Timeframe)
	return err
}

// FinishRun marks runID 'completed' (runErr nil) or 'failed' with runErr's
// message recorded, and stamps finished_at either way.
func (s *Store) FinishRun(ctx context.Context, runID string, runErr error) error {
	status := "completed"
	var errMsg *string
	if runErr != nil {
		status = "failed"
		msg := runErr.Error()
		errMsg = &msg
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE analysis_runs SET status = $1, error = $2, finished_at = now() WHERE id = $3
	`, status, errMsg, runID)
	return err
}

type Result struct {
	ID         string
	RunID      string
	AgentType  string
	Asset      string
	Indicators any
	Narrative  string
	CreatedAt  time.Time
}

// SaveResult marshals Indicators to JSON and inserts one analysis_results
// row.
func (s *Store) SaveResult(ctx context.Context, r Result) error {
	indicatorsJSON, err := json.Marshal(r.Indicators)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO analysis_results (id, run_id, agent_type, asset, indicators, narrative, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, r.ID, r.RunID, r.AgentType, r.Asset, indicatorsJSON, r.Narrative, r.CreatedAt)
	return err
}

// DeleteRunForTest removes a run and its results — test-only cleanup, not
// used by production code.
func (s *Store) DeleteRunForTest(ctx context.Context, runID string) {
	s.pool.Exec(ctx, `DELETE FROM analysis_results WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM analysis_runs WHERE id = $1`, runID)
}

// RunStatus reads back a run's current status — used by tests.
func (s *Store) RunStatus(ctx context.Context, runID string) (string, error) {
	var status string
	err := s.pool.QueryRow(ctx, `SELECT status FROM analysis_runs WHERE id = $1`, runID).Scan(&status)
	return status, err
}

// ResultCount counts analysis_results rows for runID — used by tests.
func (s *Store) ResultCount(ctx context.Context, runID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM analysis_results WHERE run_id = $1`, runID).Scan(&count)
	return count, err
}
```

- [ ] **Step 2: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go build ./...` (from `analysis/`; use a worktree-specific name instead of `analysis-dev` if working in a worktree). Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add analysis/internal/storage/runs.go
git commit -m "feat(analysis): analysis_runs/analysis_results write helpers"
```

---

### Task 4: Technical indicators

**Files:**
- Create: `analysis/internal/indicators/technical.go`
- Test: `analysis/internal/indicators/technical_test.go`

**Interfaces:**
- Produces: `indicators.Candle{Close, Volume}`, `indicators.Technical{SMAShort, SMALong, Trend, RSI, Volatility, RelativeVolume}` (all with `json` tags), `indicators.Compute(candles []Candle) (Technical, error)`, `indicators.MinCandles` (exported constant, 51) — consumed by Task 8's technical agent.

- [ ] **Step 1: Write the failing test**

```go
// analysis/internal/indicators/technical_test.go
package indicators

import (
	"math"
	"testing"
)

func TestCompute_InsufficientData(t *testing.T) {
	_, err := Compute(make([]Candle, MinCandles-1))
	if err == nil {
		t.Fatal("expected error for insufficient candles, got nil")
	}
}

func TestCompute_UptrendBullish(t *testing.T) {
	// 51 candles, strictly increasing close, flat volume except the last
	// candle doubles — exercises trend, RSI, volatility, and relative
	// volume all landing on the "obviously bullish, obviously spiking
	// volume" side, checkable by hand.
	candles := make([]Candle, MinCandles)
	price := 100.0
	for i := range candles {
		candles[i] = Candle{Close: price, Volume: 10}
		price += 1
	}
	candles[len(candles)-1].Volume = 50

	got, err := Compute(candles)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got.Trend != "bullish" {
		t.Errorf("Trend = %q, want bullish", got.Trend)
	}
	if got.SMAShort <= got.SMALong {
		t.Errorf("SMAShort (%.2f) should be > SMALong (%.2f) in a steady uptrend", got.SMAShort, got.SMALong)
	}
	if got.RSI <= 50 {
		t.Errorf("RSI = %.2f, want > 50 for a strictly increasing series", got.RSI)
	}
	if got.RelativeVolume <= 1 {
		t.Errorf("RelativeVolume = %.2f, want > 1 after the volume spike", got.RelativeVolume)
	}
	if got.Volatility < 0 {
		t.Errorf("Volatility = %.4f, want >= 0", got.Volatility)
	}
}

func TestCompute_FlatIsNeutral(t *testing.T) {
	candles := make([]Candle, MinCandles)
	for i := range candles {
		candles[i] = Candle{Close: 100, Volume: 10}
	}

	got, err := Compute(candles)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got.Trend != "neutral" {
		t.Errorf("Trend = %q, want neutral for a flat series", got.Trend)
	}
	if math.Abs(got.Volatility) > 1e-9 {
		t.Errorf("Volatility = %.6f, want ~0 for a flat series", got.Volatility)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./internal/indicators/... -v` (from `analysis/`).
Expected: FAIL — `Compute`, `Candle`, `MinCandles` not defined.

- [ ] **Step 3: Write `internal/indicators/technical.go`**

```go
// analysis/internal/indicators/technical.go
package indicators

import (
	"fmt"
	"math"
)

const (
	smaShortPeriod = 20
	smaLongPeriod  = 50
	rsiPeriod      = 14
	volWindow      = 20

	// MinCandles is the fewest closed candles Compute needs (the SMA-long
	// window plus one extra point for its own comparison basis).
	MinCandles = smaLongPeriod + 1
)

// Candle is the minimal OHLCV shape indicators need — decoupled from any
// storage package so this file has no database dependency.
type Candle struct {
	Close  float64
	Volume float64
}

// Technical holds the structured technical indicators computed for one
// asset over a series of closed candles.
type Technical struct {
	SMAShort       float64 `json:"sma_short"`
	SMALong        float64 `json:"sma_long"`
	Trend          string  `json:"trend"`
	RSI            float64 `json:"rsi"`
	Volatility     float64 `json:"volatility"`
	RelativeVolume float64 `json:"relative_volume"`
}

// Compute calculates technical indicators from closed candles, oldest
// first. Returns an error if fewer than MinCandles are supplied — callers
// should treat that as "insufficient data", not a bug.
func Compute(candles []Candle) (Technical, error) {
	if len(candles) < MinCandles {
		return Technical{}, fmt.Errorf("indicators: need at least %d candles, got %d", MinCandles, len(candles))
	}

	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	smaShort := sma(closes, smaShortPeriod)
	smaLong := sma(closes, smaLongPeriod)

	trend := "neutral"
	diff := (smaShort - smaLong) / smaLong
	switch {
	case diff > 0.001:
		trend = "bullish"
	case diff < -0.001:
		trend = "bearish"
	}

	return Technical{
		SMAShort:       smaShort,
		SMALong:        smaLong,
		Trend:          trend,
		RSI:            rsi(closes, rsiPeriod),
		Volatility:     volatility(closes, volWindow),
		RelativeVolume: relativeVolume(candles, volWindow),
	}, nil
}

func sma(closes []float64, n int) float64 {
	sum := 0.0
	for _, c := range closes[len(closes)-n:] {
		sum += c
	}
	return sum / float64(n)
}

// rsi computes Wilder's RSI over the last period+1 closes.
func rsi(closes []float64, period int) float64 {
	window := closes[len(closes)-period-1:]
	var gainSum, lossSum float64
	for i := 1; i < len(window); i++ {
		delta := window[i] - window[i-1]
		if delta > 0 {
			gainSum += delta
		} else {
			lossSum += -delta
		}
	}
	avgGain := gainSum / float64(period)
	avgLoss := lossSum / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// volatility is the standard deviation of percentage returns over the last
// n closes.
func volatility(closes []float64, n int) float64 {
	window := closes[len(closes)-n-1:]
	returns := make([]float64, 0, n)
	for i := 1; i < len(window); i++ {
		returns = append(returns, (window[i]-window[i-1])/window[i-1])
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	sumSq := 0.0
	for _, r := range returns {
		sumSq += (r - mean) * (r - mean)
	}
	return math.Sqrt(sumSq / float64(len(returns)))
}

// relativeVolume is the most recent candle's volume divided by the average
// volume of the preceding n candles.
func relativeVolume(candles []Candle, n int) float64 {
	last := candles[len(candles)-1]
	window := candles[len(candles)-1-n : len(candles)-1]
	sum := 0.0
	for _, c := range window {
		sum += c.Volume
	}
	avg := sum / float64(n)
	if avg == 0 {
		return 0
	}
	return last.Volume / avg
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./internal/indicators/... -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add analysis/internal/indicators/technical.go analysis/internal/indicators/technical_test.go
git commit -m "feat(analysis): technical indicators (SMA, RSI, volatility, relative volume)"
```

---

### Task 5: Derivatives signals

**Files:**
- Create: `analysis/internal/derivatives/signals.go`
- Test: `analysis/internal/derivatives/signals_test.go`

**Interfaces:**
- Produces: `derivatives.Liquidation{Price, Quantity}`, `derivatives.Signals{FundingRate, FundingExtreme, OIChangePct, LiquidationVolume1h, LiquidationCascade}` (with `json` tags), `derivatives.Compute(fundingRate, currentOI, oi24hAgo float64, liqs []Liquidation) Signals` — consumed by Task 8's derivatives agent.

- [ ] **Step 1: Write the failing test**

```go
// analysis/internal/derivatives/signals_test.go
package derivatives

import "testing"

func TestCompute_NormalFundingNoCascade(t *testing.T) {
	got := Compute(0.0002, 1000, 900, nil)
	if got.FundingExtreme {
		t.Error("FundingExtreme = true, want false for 0.02% funding")
	}
	if got.LiquidationCascade {
		t.Error("LiquidationCascade = true, want false with no liquidations")
	}
	wantOIChange := (1000.0 - 900.0) / 900.0 * 100
	if diff := got.OIChangePct - wantOIChange; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("OIChangePct = %.6f, want %.6f", got.OIChangePct, wantOIChange)
	}
}

func TestCompute_ExtremeFundingAndCascade(t *testing.T) {
	liqs := []Liquidation{
		{Price: 50000, Quantity: 15},
		{Price: 50000, Quantity: 10},
	} // 25 * 50000 = 1,250,000 > threshold
	got := Compute(-0.005, 1000, 1000, liqs)
	if !got.FundingExtreme {
		t.Error("FundingExtreme = false, want true for -0.5% funding")
	}
	if !got.LiquidationCascade {
		t.Error("LiquidationCascade = false, want true for $1.25M in liquidations")
	}
	if got.LiquidationVolume1h != 1_250_000 {
		t.Errorf("LiquidationVolume1h = %.2f, want 1250000", got.LiquidationVolume1h)
	}
}

func TestCompute_ZeroOI24hAgoNoDivideByZero(t *testing.T) {
	got := Compute(0, 1000, 0, nil)
	if got.OIChangePct != 0 {
		t.Errorf("OIChangePct = %.2f, want 0 when oi24hAgo is 0 (avoid divide-by-zero)", got.OIChangePct)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./internal/derivatives/... -v`.
Expected: FAIL — `Compute`, `Liquidation` not defined.

- [ ] **Step 3: Write `internal/derivatives/signals.go`**

```go
// analysis/internal/derivatives/signals.go
package derivatives

const (
	fundingExtremeThreshold     = 0.001   // 0.1%
	liquidationCascadeThreshold = 1000000 // $1,000,000 notional in the last hour
)

// Liquidation is the minimal shape signals needs from one liquidation
// event — decoupled from any storage package.
type Liquidation struct {
	Price    float64
	Quantity float64
}

// Signals holds the structured derivatives indicators computed for one
// asset from its most recent funding rate, open interest, and liquidation
// data.
type Signals struct {
	FundingRate         float64 `json:"funding_rate"`
	FundingExtreme      bool    `json:"funding_extreme"`
	OIChangePct         float64 `json:"oi_change_pct"`
	LiquidationVolume1h float64 `json:"liquidation_volume_1h"`
	LiquidationCascade  bool    `json:"liquidation_cascade"`
}

// Compute derives derivatives signals from the latest funding rate, the
// current and 24h-ago open interest, and liquidations in the last hour.
// oi24hAgo of 0 means no comparison point was found — OIChangePct is 0 in
// that case rather than a divide-by-zero.
func Compute(fundingRate, currentOI, oi24hAgo float64, recentLiquidations []Liquidation) Signals {
	var oiChangePct float64
	if oi24hAgo != 0 {
		oiChangePct = (currentOI - oi24hAgo) / oi24hAgo * 100
	}

	var liqVolume float64
	for _, l := range recentLiquidations {
		liqVolume += l.Price * l.Quantity
	}

	return Signals{
		FundingRate:         fundingRate,
		FundingExtreme:      absFloat(fundingRate) > fundingExtremeThreshold,
		OIChangePct:         oiChangePct,
		LiquidationVolume1h: liqVolume,
		LiquidationCascade:  liqVolume > liquidationCascadeThreshold,
	}
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./internal/derivatives/... -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add analysis/internal/derivatives/signals.go analysis/internal/derivatives/signals_test.go
git commit -m "feat(analysis): derivatives signals (funding, OI change, liquidation cascade)"
```

---

### Task 6: News keyword search

**Files:**
- Create: `analysis/internal/news/search.go`
- Test: `analysis/internal/news/search_test.go`

**Interfaces:**
- Produces: `news.Item{Title, Body, URL, PublishedAt}`, `news.Article{Title, URL, PublishedAt}` (with `json` tags), `news.Result{ArticleCount, Articles}` (with `json` tags), `news.Search(items []Item, symbol, name string) Result` — consumed by Task 9's news agent.

- [ ] **Step 1: Write the failing test**

```go
// analysis/internal/news/search_test.go
package news

import (
	"testing"
	"time"
)

func TestSearch_MatchesSymbolAndNameCaseInsensitive(t *testing.T) {
	now := time.Now()
	items := []Item{
		{Title: "Bitcoin hits new high", Body: "...", URL: "u1", PublishedAt: now},
		{Title: "market update", Body: "BTC dropped 5% today", URL: "u2", PublishedAt: now.Add(-time.Hour)},
		{Title: "Ethereum news", Body: "unrelated content", URL: "u3", PublishedAt: now},
	}

	got := Search(items, "BTC", "Bitcoin")
	if got.ArticleCount != 2 {
		t.Fatalf("ArticleCount = %d, want 2", got.ArticleCount)
	}
}

func TestSearch_NoMatch(t *testing.T) {
	items := []Item{
		{Title: "Ethereum news", Body: "unrelated content", URL: "u1", PublishedAt: time.Now()},
	}
	got := Search(items, "BTC", "Bitcoin")
	if got.ArticleCount != 0 {
		t.Fatalf("ArticleCount = %d, want 0", got.ArticleCount)
	}
	if len(got.Articles) != 0 {
		t.Fatalf("len(Articles) = %d, want 0", len(got.Articles))
	}
}

func TestSearch_CapsArticlesAtTenButCountsAll(t *testing.T) {
	now := time.Now()
	items := make([]Item, 15)
	for i := range items {
		items[i] = Item{Title: "Bitcoin update", Body: "...", URL: "u", PublishedAt: now.Add(-time.Duration(i) * time.Minute)}
	}
	got := Search(items, "BTC", "Bitcoin")
	if got.ArticleCount != 15 {
		t.Errorf("ArticleCount = %d, want 15", got.ArticleCount)
	}
	if len(got.Articles) != 10 {
		t.Errorf("len(Articles) = %d, want 10 (capped)", len(got.Articles))
	}
	if !got.Articles[0].PublishedAt.After(got.Articles[len(got.Articles)-1].PublishedAt) {
		t.Error("Articles should be newest first")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./internal/news/... -v`.
Expected: FAIL — `Item`, `Search` not defined.

- [ ] **Step 3: Write `internal/news/search.go`**

```go
// analysis/internal/news/search.go
package news

import (
	"sort"
	"strings"
	"time"
)

const maxArticles = 10

// Item is the minimal shape search needs from one news item — decoupled
// from any storage package.
type Item struct {
	Title       string
	Body        string
	URL         string
	PublishedAt time.Time
}

// Article is one matched item surfaced in a Result, trimmed to what the
// LLM prompt and the persisted indicators need.
type Article struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

// Result is the structured indicator the news agent produces: how many
// matching articles were found, and up to maxArticles of the most recent
// ones.
type Result struct {
	ArticleCount int       `json:"article_count"`
	Articles     []Article `json:"articles"`
}

// Search filters items to those mentioning symbol or name (case-
// insensitive) in the title or body, newest first. ArticleCount reflects
// every match; Articles is capped at maxArticles. Callers are expected to
// have already restricted items to the desired time window (e.g. the last
// 24h) — Search does not filter by time itself.
func Search(items []Item, symbol, name string) Result {
	needles := []string{strings.ToLower(symbol), strings.ToLower(name)}

	var matched []Item
	for _, it := range items {
		haystack := strings.ToLower(it.Title + " " + it.Body)
		for _, needle := range needles {
			if needle != "" && strings.Contains(haystack, needle) {
				matched = append(matched, it)
				break
			}
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].PublishedAt.After(matched[j].PublishedAt)
	})

	articles := make([]Article, 0, min(len(matched), maxArticles))
	for _, it := range matched[:min(len(matched), maxArticles)] {
		articles = append(articles, Article{Title: it.Title, URL: it.URL, PublishedAt: it.PublishedAt})
	}

	return Result{ArticleCount: len(matched), Articles: articles}
}
```

`min` is the Go 1.21+ builtin — no import needed for it.

- [ ] **Step 4: Run test to verify it passes**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./internal/news/... -v`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add analysis/internal/news/search.go analysis/internal/news/search_test.go
git commit -m "feat(analysis): news keyword search by asset symbol/name"
```

---

### Task 7: LLM client wrapper

**Files:**
- Create: `analysis/internal/llm/client.go`

**Interfaces:**
- Consumes: `github.com/anthropics/anthropic-sdk-go` (pinned in Task 1).
- Produces: `llm.Client` interface with `Summarize(ctx, systemPrompt, userPrompt string) (string, error)`, and `llm.AnthropicClient` (implements `llm.Client`) with constructor `llm.NewAnthropicClient() *AnthropicClient` — consumed by Task 8/9's agents (via the interface) and Task 10's CLI (via the constructor). Task 11's integration test implements `llm.Client` with a fake.

- [ ] **Step 1: Write `internal/llm/client.go`**

```go
// analysis/internal/llm/client.go
package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	model     = "claude-sonnet-5"
	maxTokens = 512
)

// Client narrates structured indicators into a short natural-language
// summary. AnthropicClient is the production implementation; tests inject
// a fake implementing the same interface.
type Client interface {
	Summarize(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// AnthropicClient calls the Claude API to generate analysis narratives.
type AnthropicClient struct {
	client anthropic.Client
}

// NewAnthropicClient builds a client that reads its API key from the
// ANTHROPIC_API_KEY environment variable, per the SDK's default
// credential resolution.
func NewAnthropicClient() *AnthropicClient {
	return &AnthropicClient{client: anthropic.NewClient()}
}

func (c *AnthropicClient) Summarize(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm: summarize: %w", err)
	}
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			return text.Text, nil
		}
	}
	return "", fmt.Errorf("llm: summarize: no text block in response")
}
```

- [ ] **Step 2: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go build ./...`. Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add analysis/internal/llm/client.go
git commit -m "feat(analysis): Anthropic LLM client wrapper"
```

---

### Task 8: Technical and derivatives agents

**Files:**
- Create: `analysis/internal/agents/agents.go`
- Create: `analysis/internal/agents/technical.go`
- Create: `analysis/internal/agents/derivatives.go`

**Interfaces:**
- Consumes: `storage.Store` (Tasks 2-3), `indicators.Compute`/`indicators.Candle` (Task 4), `derivatives.Compute`/`derivatives.Liquidation` (Task 5), `llm.Client` (Task 7), `risk.ReferenceExchange` from `risk-engine/risk`.
- Produces: `agents.Output{Indicators, Narrative, Err}`, `agents.Technical(ctx, store, client, asset, timeframe) (Output, error)`, `agents.Derivatives(ctx, store, client, asset) (Output, error)` — consumed by Task 10's CLI.

- [ ] **Step 1: Write `internal/agents/agents.go`**

```go
// analysis/internal/agents/agents.go
package agents

// Output is what one agent produces for one asset (or, for RiskContext,
// for the portfolio as a whole). Err is set when data collection
// succeeded but the LLM call itself failed — Indicators is still valid
// and should be persisted; Narrative is empty in that case.
type Output struct {
	Indicators any
	Narrative  string
	Err        error
}
```

- [ ] **Step 2: Write `internal/agents/technical.go`**

```go
// analysis/internal/agents/technical.go
package agents

import (
	"context"
	"fmt"

	"risk-engine/risk"

	"analysis/internal/indicators"
	"analysis/internal/llm"
	"analysis/internal/storage"
)

const technicalSystemPrompt = `Você é um analista técnico de criptomoedas. Dado um conjunto de indicadores técnicos calculados para um ativo, escreva um resumo curto (2-4 frases) em português explicando o que eles indicam sobre a tendência atual. Seja direto, sem recomendação de compra ou venda.`

// Technical computes technical indicators for asset over timeframe and
// asks the LLM to narrate them. Returns a plain error only if data
// collection fails; insufficient candles or an LLM failure are reported
// through Output, not error, so the caller can still persist what was
// computed.
func Technical(ctx context.Context, store *storage.Store, client llm.Client, asset, timeframe string) (Output, error) {
	candles, err := store.RecentCandles(ctx, risk.ReferenceExchange, asset, timeframe, indicators.MinCandles)
	if err != nil {
		return Output{}, fmt.Errorf("agents: technical: fetch candles: %w", err)
	}

	indicatorCandles := make([]indicators.Candle, len(candles))
	for i, c := range candles {
		indicatorCandles[i] = indicators.Candle{Close: c.Close, Volume: c.Volume}
	}

	ind, err := indicators.Compute(indicatorCandles)
	if err != nil {
		return Output{
			Indicators: map[string]string{"status": "insufficient_data"},
			Narrative:  fmt.Sprintf("Dados insuficientes para calcular indicadores técnicos de %s (mínimo de %d candles fechados necessário).", asset, indicators.MinCandles),
		}, nil
	}

	userPrompt := fmt.Sprintf(
		"Ativo: %s\nSMA curta (20): %.2f\nSMA longa (50): %.2f\nTendência: %s\nRSI (14): %.1f\nVolatilidade: %.4f\nVolume relativo: %.2f",
		asset, ind.SMAShort, ind.SMALong, ind.Trend, ind.RSI, ind.Volatility, ind.RelativeVolume,
	)
	narrative, err := client.Summarize(ctx, technicalSystemPrompt, userPrompt)
	if err != nil {
		return Output{Indicators: ind, Err: err}, nil
	}
	return Output{Indicators: ind, Narrative: narrative}, nil
}
```

- [ ] **Step 3: Write `internal/agents/derivatives.go`**

```go
// analysis/internal/agents/derivatives.go
package agents

import (
	"context"
	"fmt"
	"time"

	"risk-engine/risk"

	"analysis/internal/derivatives"
	"analysis/internal/llm"
	"analysis/internal/storage"
)

const derivativesSystemPrompt = `Você é um analista de derivativos de criptomoedas. Dado um conjunto de indicadores de funding rate, open interest e liquidações para um ativo, escreva um resumo curto (2-4 frases) em português explicando o que eles indicam. Seja direto, sem recomendação de compra ou venda.`

// Derivatives computes derivatives signals for asset and asks the LLM to
// narrate them.
func Derivatives(ctx context.Context, store *storage.Store, client llm.Client, asset string) (Output, error) {
	fundingRate, _, err := store.LatestFundingRate(ctx, risk.ReferenceExchange, asset)
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch funding rate: %w", err)
	}

	now := time.Now().UTC()
	currentOI, _, err := store.OpenInterestNear(ctx, risk.ReferenceExchange, asset, now)
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch open interest: %w", err)
	}
	oi24hAgo, _, err := store.OpenInterestNear(ctx, risk.ReferenceExchange, asset, now.Add(-24*time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch open interest 24h ago: %w", err)
	}

	rawLiqs, err := store.RecentLiquidations(ctx, risk.ReferenceExchange, asset, now.Add(-time.Hour))
	if err != nil {
		return Output{}, fmt.Errorf("agents: derivatives: fetch liquidations: %w", err)
	}
	liqs := make([]derivatives.Liquidation, len(rawLiqs))
	for i, l := range rawLiqs {
		liqs[i] = derivatives.Liquidation{Price: l.Price, Quantity: l.Quantity}
	}

	signals := derivatives.Compute(fundingRate, currentOI, oi24hAgo, liqs)

	userPrompt := fmt.Sprintf(
		"Ativo: %s\nFunding rate: %.4f%% (extremo: %v)\nVariação de OI (24h): %.2f%%\nVolume liquidado (1h): $%.2f (cascata: %v)",
		asset, signals.FundingRate*100, signals.FundingExtreme, signals.OIChangePct, signals.LiquidationVolume1h, signals.LiquidationCascade,
	)
	narrative, err := client.Summarize(ctx, derivativesSystemPrompt, userPrompt)
	if err != nil {
		return Output{Indicators: signals, Err: err}, nil
	}
	return Output{Indicators: signals, Narrative: narrative}, nil
}
```

- [ ] **Step 4: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go build ./...`. Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add analysis/internal/agents/agents.go analysis/internal/agents/technical.go analysis/internal/agents/derivatives.go
git commit -m "feat(analysis): technical and derivatives agent orchestration"
```

---

### Task 9: News and risk-context agents

**Files:**
- Create: `analysis/internal/agents/news.go`
- Create: `analysis/internal/agents/riskcontext.go`

**Interfaces:**
- Consumes: `storage.Store` (Task 2), `news.Search`/`news.Item` (Task 6), `llm.Client` (Task 7), `risk-engine/storage` (`Store.GetState`, `Store.GetLimits`, `Limits`).
- Produces: `agents.News(ctx, store, client, symbol, name) (Output, error)`, `agents.RiskContext(ctx, riskStore, client) (Output, error)`, `agents.RiskContextIndicators{Status, Reason, ChangedAt, Limits}` — consumed by Task 10's CLI.

- [ ] **Step 1: Write `internal/agents/news.go`**

```go
// analysis/internal/agents/news.go
package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"analysis/internal/llm"
	"analysis/internal/news"
	"analysis/internal/storage"
)

const newsSystemPrompt = `Você é um analista de notícias sobre criptomoedas. Dado uma lista de manchetes recentes sobre um ativo, escreva um resumo curto (2-4 frases) em português sobre o tom geral das notícias (positivo, negativo, neutro ou misto) e os principais temas. Se não houver notícias, diga isso claramente. Seja direto, sem recomendação de compra ou venda.`

// News searches for news from the last 24h mentioning asset (by symbol or
// full name) and asks the LLM to narrate the findings.
func News(ctx context.Context, store *storage.Store, client llm.Client, symbol, name string) (Output, error) {
	since := time.Now().UTC().Add(-24 * time.Hour)
	rawItems, err := store.RecentNews(ctx, since)
	if err != nil {
		return Output{}, fmt.Errorf("agents: news: fetch recent news: %w", err)
	}

	items := make([]news.Item, len(rawItems))
	for i, it := range rawItems {
		items[i] = news.Item{Title: it.Title, Body: it.Body, URL: it.URL, PublishedAt: it.PublishedAt}
	}
	result := news.Search(items, symbol, name)

	var userPrompt string
	if result.ArticleCount == 0 {
		userPrompt = fmt.Sprintf("Ativo: %s\nNenhuma notícia encontrada nas últimas 24 horas.", symbol)
	} else {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Ativo: %s\n%d notícia(s) encontrada(s) nas últimas 24 horas:\n", symbol, result.ArticleCount)
		for _, a := range result.Articles {
			fmt.Fprintf(&sb, "- %s\n", a.Title)
		}
		userPrompt = sb.String()
	}

	narrative, err := client.Summarize(ctx, newsSystemPrompt, userPrompt)
	if err != nil {
		return Output{Indicators: result, Err: err}, nil
	}
	return Output{Indicators: result, Narrative: narrative}, nil
}
```

- [ ] **Step 2: Write `internal/agents/riskcontext.go`**

```go
// analysis/internal/agents/riskcontext.go
package agents

import (
	"context"
	"fmt"
	"time"

	riskstorage "risk-engine/storage"

	"analysis/internal/llm"
)

const riskContextSystemPrompt = `Você é um analista de risco de portfólio. Dado o estado atual do motor de risco e os limites configurados, escreva um resumo curto (2-4 frases) em português sobre a situação de risco do portfólio no momento. Seja direto, sem recomendação de compra ou venda.`

// RiskContextIndicators is the structured indicator the risk-context
// agent produces: the live risk_state and the current risk_limits.
type RiskContextIndicators struct {
	Status    string             `json:"risk_status"`
	Reason    string             `json:"risk_reason"`
	ChangedAt string             `json:"risk_changed_at"`
	Limits    riskstorage.Limits `json:"limits"`
}

// RiskContext reads the live risk_state (run_id IS NULL) and current
// risk_limits and asks the LLM to narrate the portfolio's risk situation.
// It never reads risk_decisions history.
func RiskContext(ctx context.Context, riskStore *riskstorage.Store, client llm.Client) (Output, error) {
	state, err := riskStore.GetState(ctx, nil)
	if err != nil {
		return Output{}, fmt.Errorf("agents: risk_context: fetch state: %w", err)
	}
	limits, err := riskStore.GetLimits(ctx)
	if err != nil {
		return Output{}, fmt.Errorf("agents: risk_context: fetch limits: %w", err)
	}

	ind := RiskContextIndicators{
		Status:    state.Status,
		Reason:    state.Reason,
		ChangedAt: state.ChangedAt.Format(time.RFC3339),
		Limits:    limits,
	}

	userPrompt := fmt.Sprintf(
		"Status: %s\nMotivo: %s\nDesde: %s\nLimite por ativo: %.1f%%\nLimite total em cripto: %.1f%%\nPerda diária máxima: %.1f%%\nPerda semanal máxima: %.1f%%\nDrawdown máximo: %.1f%%\nPerdas consecutivas máximas: %d",
		ind.Status, ind.Reason, ind.ChangedAt,
		limits.MaxPctPerAsset*100, limits.MaxPctCryptoTotal*100,
		limits.MaxDailyLoss*100, limits.MaxWeeklyLoss*100, limits.MaxDrawdown*100,
		limits.MaxConsecutiveLosses,
	)
	narrative, err := client.Summarize(ctx, riskContextSystemPrompt, userPrompt)
	if err != nil {
		return Output{Indicators: ind, Err: err}, nil
	}
	return Output{Indicators: ind, Narrative: narrative}, nil
}
```

- [ ] **Step 3: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go build ./...`. Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add analysis/internal/agents/news.go analysis/internal/agents/riskcontext.go
git commit -m "feat(analysis): news and risk-context agent orchestration"
```

---

### Task 10: CLI

**Files:**
- Create: `analysis/cmd/analysis/main.go`

**Interfaces:**
- Consumes: everything from Tasks 2-9 (`storage.Store`, `riskstorage.Store`, `llm.NewAnthropicClient`, `agents.Technical`/`Derivatives`/`News`/`RiskContext`, `agents.Output`).
- Produces: the `analysis` binary — no further consumers within this plan (Task 11 exercises the CLI's `run` function directly).

- [ ] **Step 1: Write `cmd/analysis/main.go`**

```go
// analysis/cmd/analysis/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	riskstorage "risk-engine/storage"

	"analysis/internal/agents"
	"analysis/internal/llm"
	"analysis/internal/storage"
)

var validAgents = map[string]bool{
	"technical": true, "derivatives": true, "news": true, "risk_context": true,
}

func main() {
	var (
		assetsStr = flag.String("assets", "", "comma-separated asset symbols on the reference exchange (required)")
		timeframe = flag.String("timeframe", "1h", "timeframe used by the technical agent")
		agentsStr = flag.String("agents", "technical,derivatives,news,risk_context", "comma-separated agents to run")
	)
	flag.Parse()

	if err := run(context.Background(), *assetsStr, *timeframe, *agentsStr); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, assetsStr, timeframe, agentsStr string) error {
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
	}
	requestedAgents := splitNonEmpty(agentsStr)
	if len(requestedAgents) == 0 {
		return fmt.Errorf("-agents must not be empty")
	}
	for _, a := range requestedAgents {
		if !validAgents[a] {
			return fmt.Errorf("unknown agent %q (valid: technical, derivatives, news, risk_context)", a)
		}
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect analysis storage: %w", err)
	}
	defer store.Close()

	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()

	client := llm.NewAnthropicClient()

	runID, successCount, err := Run(ctx, store, riskStore, client, assets, timeframe, requestedAgents)
	if err != nil {
		return err
	}
	fmt.Printf("analysis run %s completed (%d results recorded)\n", runID, successCount)
	return nil
}

// Run executes one analysis run against the given assets/timeframe/agent
// selection, using client for narration. Exported (capitalized) so Task
// 11's integration test can call it directly with a fake llm.Client
// without going through flag parsing or environment variables.
func Run(ctx context.Context, store *storage.Store, riskStore *riskstorage.Store, client llm.Client, assets []string, timeframe string, requestedAgents []string) (runID string, successCount int, err error) {
	runID = uuid.NewString()
	if err := store.CreateRun(ctx, storage.Run{ID: runID, StartedAt: time.Now().UTC(), Timeframe: timeframe}); err != nil {
		return runID, 0, fmt.Errorf("create run: %w", err)
	}

	for _, agentType := range requestedAgents {
		switch agentType {
		case "technical":
			for _, asset := range assets {
				out, agentErr := agents.Technical(ctx, store, client, asset, timeframe)
				if record(ctx, store, runID, "technical", asset, out, agentErr) {
					successCount++
				}
			}
		case "derivatives":
			for _, asset := range assets {
				out, agentErr := agents.Derivatives(ctx, store, client, asset)
				if record(ctx, store, runID, "derivatives", asset, out, agentErr) {
					successCount++
				}
			}
		case "news":
			for _, asset := range assets {
				out, agentErr := agents.News(ctx, store, client, asset, asset)
				if record(ctx, store, runID, "news", asset, out, agentErr) {
					successCount++
				}
			}
		case "risk_context":
			out, agentErr := agents.RiskContext(ctx, riskStore, client)
			if record(ctx, store, runID, "risk_context", "", out, agentErr) {
				successCount++
			}
		}
	}

	var runErr error
	if successCount == 0 {
		runErr = fmt.Errorf("all agents failed")
	}
	if finishErr := store.FinishRun(ctx, runID, runErr); finishErr != nil {
		return runID, successCount, fmt.Errorf("finish run: %w", finishErr)
	}
	if runErr != nil {
		return runID, successCount, fmt.Errorf("analysis run %s: %w", runID, runErr)
	}
	return runID, successCount, nil
}

// record persists an agent's output — indicators are saved even if the LLM
// call itself failed (out.Err set) — and reports the outcome on
// stdout/stderr. Returns whether the agent produced a narrative
// successfully. A data-collection error (agentErr non-nil) is not
// persisted at all, since there are no indicators to save.
func record(ctx context.Context, store *storage.Store, runID, agentType, asset string, out agents.Output, agentErr error) bool {
	if agentErr != nil {
		fmt.Fprintf(os.Stderr, "%s/%s: data collection failed: %v\n", agentType, asset, agentErr)
		return false
	}
	if saveErr := store.SaveResult(ctx, storage.Result{
		ID: uuid.NewString(), RunID: runID, AgentType: agentType, Asset: asset,
		Indicators: out.Indicators, Narrative: out.Narrative, CreatedAt: time.Now().UTC(),
	}); saveErr != nil {
		fmt.Fprintf(os.Stderr, "%s/%s: save result failed: %v\n", agentType, asset, saveErr)
		return false
	}
	if out.Err != nil {
		fmt.Printf("%s/%s: sem narrativa: %v\n", agentType, asset, out.Err)
		return false
	}
	fmt.Printf("%s/%s: %s\n", agentType, asset, out.Narrative)
	return true
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 2: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go build ./...`. Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add analysis/cmd/analysis/main.go
git commit -m "feat(analysis): CLI wiring the four agents together"
```

---

### Task 11: End-to-end integration test

**Files:**
- Test: `analysis/cmd/analysis/main_test.go`

**Interfaces:**
- Consumes: `Run` (Task 10, exported for this reason), `storage.New`/`DeleteRunForTest`/`RunStatus`/`ResultCount` (Tasks 1-3), `riskstorage.New` (`risk-engine/storage`), `llm.Client` (Task 7, implemented by a local fake).

- [ ] **Step 1: Write `cmd/analysis/main_test.go`**

```go
// analysis/cmd/analysis/main_test.go
package main

import (
	"context"
	"os"
	"testing"

	riskstorage "risk-engine/storage"

	"analysis/internal/storage"
)

// fakeLLMClient implements llm.Client without calling the real API.
// Narratives are canned; Summarize can be made to fail for specific
// agent/asset combinations by checking systemPrompt/userPrompt content,
// but this test only needs an always-fails and an always-succeeds mode.
type fakeLLMClient struct {
	fail bool
}

func (f *fakeLLMClient) Summarize(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if f.fail {
		return "", errFakeLLMFailure
	}
	return "fake narrative", nil
}

var errFakeLLMFailure = fakeErr("fake LLM failure")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func testStores(t *testing.T) (*storage.Store, *riskstorage.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration tests")
	}
	ctx := context.Background()
	store, err := storage.New(ctx, dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(store.Close)
	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	t.Cleanup(riskStore.Close)
	return store, riskStore
}

func TestRun_AllAgentsSucceed(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()

	// risk_context needs no market data — the live risk_state/risk_limits
	// rows are seeded by risk-engine's own migration. technical needs
	// candles this test doesn't seed, so it exercises the
	// insufficient-data path (still a success: a narrative is produced).
	runID, successCount, err := Run(ctx, store, riskStore, &fakeLLMClient{}, []string{"NOSUCHASSET"}, "1h", []string{"technical", "risk_context"})
	t.Cleanup(func() { store.DeleteRunForTest(ctx, runID) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if successCount != 2 {
		t.Errorf("successCount = %d, want 2", successCount)
	}

	status, err := store.RunStatus(ctx, runID)
	if err != nil {
		t.Fatalf("RunStatus: %v", err)
	}
	if status != "completed" {
		t.Errorf("status = %q, want completed", status)
	}

	count, err := store.ResultCount(ctx, runID)
	if err != nil {
		t.Fatalf("ResultCount: %v", err)
	}
	if count != 2 {
		t.Errorf("ResultCount = %d, want 2", count)
	}
}

func TestRun_PartialLLMFailureStillCompletes(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()

	// One agent (risk_context) succeeds via the fake client; the other
	// requested agent (technical, on an asset with no candles) fails at
	// data collection and is skipped by record — proving a partial
	// failure doesn't mark the whole run 'failed' as long as at least one
	// agent produced a result.
	runID, successCount, err := Run(ctx, store, riskStore, &fakeLLMClient{}, []string{"NOSUCHASSET"}, "1h", []string{"derivatives", "risk_context"})
	t.Cleanup(func() { store.DeleteRunForTest(ctx, runID) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if successCount != 2 {
		t.Errorf("successCount = %d, want 2 (derivatives on an unknown asset still returns zero-value signals, not an error)", successCount)
	}

	status, err := store.RunStatus(ctx, runID)
	if err != nil {
		t.Fatalf("RunStatus: %v", err)
	}
	if status != "completed" {
		t.Errorf("status = %q, want completed", status)
	}
}

func TestRun_AllAgentsFailMarksRunFailed(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()

	runID, successCount, err := Run(ctx, store, riskStore, &fakeLLMClient{fail: true}, []string{"NOSUCHASSET"}, "1h", []string{"risk_context"})
	t.Cleanup(func() { store.DeleteRunForTest(ctx, runID) })
	if err == nil {
		t.Fatal("Run: expected error when every agent fails, got nil")
	}
	if successCount != 0 {
		t.Errorf("successCount = %d, want 0", successCount)
	}

	status, statusErr := store.RunStatus(ctx, runID)
	if statusErr != nil {
		t.Fatalf("RunStatus: %v", statusErr)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}

	// The risk_context result is still persisted — indicators were
	// computed successfully, only the narration failed.
	count, err := store.ResultCount(ctx, runID)
	if err != nil {
		t.Fatalf("ResultCount: %v", err)
	}
	if count != 1 {
		t.Errorf("ResultCount = %d, want 1 (indicators saved despite LLM failure)", count)
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./cmd/analysis/... -v` (`TEST_DATABASE_URL` is already set inside the container per the docker-compose environment from Task 1).
Expected: PASS for all three tests.

- [ ] **Step 3: Run the full module test suite**

Run: `COMPOSE_PROJECT_NAME=analysis-dev docker compose exec go go test ./... -v`. Expected: all tests pass (indicators, derivatives, news, cmd/analysis).

- [ ] **Step 4: Commit**

```bash
git add analysis/cmd/analysis/main_test.go
git commit -m "test(analysis): end-to-end integration test with a fake LLM client"
```

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-17-analysis-agents.md`. Recommended: dispatch via `superpowers:subagent-driven-development`, one fresh implementer subagent per task, in the isolated worktree convention already established for this repo (remember the `COMPOSE_PROJECT_NAME` docker-compose collision fix for worktrees — see the project's conventions memory).
