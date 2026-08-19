# Ambiente Real (Execução em Exchanges/Corretoras) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add real order execution against the Binance Futures testnet — a new `execution` module authenticates to Binance, fetches the real account portfolio, and places/tracks/cancels limit orders — wired into `strategist` so every approved decision executes automatically, replacing the manual `-cash`/`-positions`/`-timeframe` flags with real exchange data.

**Architecture:** A new Go module `execution` owns Binance authentication (HMAC-SHA256 request signing), order lifecycle (place → poll → fill or timeout-cancel), and its own audit table. `strategist` imports `execution`'s public `executor` package directly (a normal Go import — `executor` is public from the start, no `internal/` boundary to work around). `strategist.Decide` gains a sell-quantity clamp against the real held position and, after `risk.Evaluate` approves, calls `execClient.Execute` inline. `mcp`'s existing `run_strategist` tool call site is updated to match the simplified `RunWithDSN` signature.

**Tech Stack:** Go 1.22 (unchanged), `github.com/jackc/pgx/v5` (execution's own storage, same as every other module), `golang.org/x/time/rate` (rate limiting, mirrors `market-data/internal/httpclient`), stdlib `crypto/hmac`/`crypto/sha256` for request signing — no new external dependency for Binance itself, this repo talks to exchanges via raw HTTP (see `market-data/internal/exchange/binance`), not a vendored SDK.

**Spec:** `docs/superpowers/specs/2026-08-19-real-execution-design.md`

## Global Constraints

- **Binance Futures Testnet base URL is `https://testnet.binancefuture.com`, hardcoded** — no production toggle, no configuration mechanism. Migrating to production is explicitly a future phase (per spec).
- **Credentials via `BINANCE_API_KEY`/`BINANCE_API_SECRET` environment variables, both required.** Missing either is a fatal error at `executor.NewClient` construction time — never a silent no-op or a delayed failure on first use.
- **Limit orders only**, at the same price already used for sizing (the latest `1m` candle close). `timeInForce=GTC`. Poll order status every `pollInterval` (default `2*time.Second`) until `fillTimeout` (default `30*time.Second`) elapses, then cancel if not fully filled. A full fill, a partial fill (cancelled at timeout), and a clean cancel with nothing filled are all valid, non-error `Outcome`s — never treated as failures.
- **`newClientOrderId` is the decision's own ID.** The decision ID (`uuid.NewString()`) is minted once, before calling `strategist.Decide`, and reused both as the persisted `strategist_decisions.id` primary key AND as the exchange's client order ID — a retried decision can never place a duplicate order.
- **Sell quantity is clamped to the real held quantity** (`portfolio.Positions[asset].Quantity`) immediately after computing the raw sizing, before `risk.Evaluate` is ever called. Any decision (buy or sell) whose quantity is `<= 0` after this clamp skips `risk.Evaluate` and execution entirely — treated exactly like a "hold" (no `Risk`, no `Execution`, both stay `nil`).
- **Price is always looked up on the `1m` timeframe** — for sizing and for valuing every held position — matching the fixed `1m` timeframe risk-engine's quality checks already read (`risk-engine/risk/quality.go` via `risk-engine/storage/marketdata.go`). The `-timeframe` CLI flag and the `timeframe` parameter threaded through `strategist`'s `Run`/`RunWithDSN`/`buildPortfolio`/`cmd/strategist/main.go` are removed entirely — there is no longer a configurable timeframe anywhere in `strategist`.
- **`-cash`/`-positions` CLI flags and `RunStrategistArgs.Cash`/`.Positions` are removed entirely.** Portfolio always comes from `execution.Client.FetchPortfolio`. `-daily-loss`/`-weekly-loss`/`-drawdown`/`-consecutive-losses` (and their MCP equivalents) are unchanged — out of scope for this phase, still manual inputs.
- **No kill switch, no market orders, no other exchanges besides Binance.** Explicitly out of scope per the spec's "Fora de escopo" section.
- **Binance's exact JSON response field names in this plan's code (`availableBalance`, `positionAmt`, `orderId`, `clientOrderId`, `status`, `executedQty`, `avgPrice`) reflect the Binance Futures API's documented response shape, but have not been verified against a live call during planning** (no test credentials available at plan-writing time). Task 1's implementer must cross-check `account.go`'s response struct against Binance's current `GET /fapi/v2/account` documentation before finalizing it, and Task 3's implementer must do the same for `orders.go` against `POST/GET/DELETE /fapi/v1/order`. If a field name has changed, update the struct tags — the parsing/business logic around them stays the same.
- **This plan's changes are NOT invisible to `mcp`, unlike sub-project 7's.** `mcp/internal/tools/strategist.go` calls `strategist/runner.RunWithDSN` directly with the flags this plan removes (`Cash`, `Positions`, implicit `Timeframe`) — Task 6 updates that call site to compile against the new signature. `mcp/go.mod` also needs `go mod tidy` for the new transitive `execution` dependency (via `strategist/runner` → `execution/executor`), and `mcp/docker-compose.yml` needs `../execution:/execution` mounted, exactly like its existing `../analysis`, `../strategist`, `../simulation`, `../risk-engine` mounts (see sub-project 7's final review for why a plan's "no downstream change" claim needs verifying, not assuming).

---

### Task 1: `execution` module scaffold + Binance authenticated client (signing + account)

**Files:**
- Create: `execution/go.mod`
- Create: `execution/docker-compose.yml`
- Create: `execution/internal/binanceclient/client.go`
- Create: `execution/internal/binanceclient/account.go`
- Create: `execution/internal/binanceclient/client_test.go`
- Create: `execution/internal/binanceclient/account_test.go`

**Interfaces:**
- Produces: `binanceclient.Client` (unexported fields, constructed via `binanceclient.New(apiKey, secret, baseURL string) *Client`), `binanceclient.Account`, `binanceclient.AccountPosition`, `(*Client).GetAccount(ctx) (Account, error)` — consumed by Task 3's `executor` package.

- [ ] **Step 1: Create the module scaffold**

```bash
mkdir -p execution/internal/binanceclient
```

`execution/go.mod`:
```
module execution

go 1.22

require (
	golang.org/x/time v0.10.0
	risk-engine v0.0.0-00010101000000-000000000000
)

replace risk-engine => ../risk-engine
```

`execution/docker-compose.yml`:
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
      BINANCE_API_KEY: ${BINANCE_API_KEY:-}
      BINANCE_API_SECRET: ${BINANCE_API_SECRET:-}
    networks:
      - market-data_default
    command: ["sleep", "infinity"]

networks:
  market-data_default:
    external: true

volumes:
  go-mod-cache:
```

Bring the container up: `COMPOSE_PROJECT_NAME=<worktree-specific-name> docker compose up -d` (see this repo's worktree/Docker Compose naming convention before choosing a project name).

- [ ] **Step 2: Write `internal/binanceclient/client.go`**

```go
// execution/internal/binanceclient/client.go
package binanceclient

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Client is an authenticated Binance Futures client. Every request is
// signed with HMAC-SHA256 per Binance's futures API auth scheme
// (timestamp + all params, signed, appended as &signature=...).
type Client struct {
	http    *http.Client
	limiter *rate.Limiter
	apiKey  string
	secret  string
	baseURL string
}

// New constructs a Client. baseURL is a parameter (not hardcoded here) so
// tests can point it at an httptest server — production callers pass the
// real testnet base URL.
func New(apiKey, secret, baseURL string) *Client {
	return &Client{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Limit(5), 10),
		apiKey:  apiKey,
		secret:  secret,
		baseURL: baseURL,
	}
}

func (c *Client) sign(query string) string {
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(query))
	return hex.EncodeToString(mac.Sum(nil))
}

// signedRequest builds a signed request: params gets `timestamp` added,
// is encoded as the query string, signed, and the signature appended.
// GET/DELETE send the signed query string in the URL; POST sends it as
// the request body (form-encoded) — matching Binance's futures API for
// each of these methods.
func (c *Client) signedRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query := params.Encode()
	query += "&signature=" + c.sign(query)

	var req *http.Request
	var err error
	if method == http.MethodPost {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, strings.NewReader(query))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path+"?"+query, nil)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody := body
		if len(errBody) > 500 {
			errBody = errBody[:500]
		}
		return nil, fmt.Errorf("binance: %s %s: status %d: %s", method, path, resp.StatusCode, errBody)
	}
	return body, nil
}
```

- [ ] **Step 3: Write `internal/binanceclient/client_test.go`**

`sign` is the one piece of real logic in this file worth testing directly — determinism and secret-sensitivity, without needing a pre-computed magic hash value (which would be error-prone to hand-derive and wouldn't test anything `crypto/hmac` doesn't already guarantee).

```go
// execution/internal/binanceclient/client_test.go
package binanceclient

import "testing"

func TestSign_DeterministicAndSecretSensitive(t *testing.T) {
	c1 := &Client{secret: "secret-a"}
	c2 := &Client{secret: "secret-b"}

	sigA1 := c1.sign("symbol=BTCUSDT&timestamp=123")
	sigA2 := c1.sign("symbol=BTCUSDT&timestamp=123")
	sigB := c2.sign("symbol=BTCUSDT&timestamp=123")

	if sigA1 != sigA2 {
		t.Errorf("sign is not deterministic: %q != %q", sigA1, sigA2)
	}
	if sigA1 == sigB {
		t.Error("sign did not change with a different secret")
	}
	if len(sigA1) != 64 {
		t.Errorf("signature length = %d, want 64 (hex-encoded SHA-256)", len(sigA1))
	}
}
```

- [ ] **Step 4: Write `internal/binanceclient/account.go`**

```go
// execution/internal/binanceclient/account.go
package binanceclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Account is the subset of Binance's GET /fapi/v2/account response this
// client needs: available balance and any open positions.
type Account struct {
	AvailableBalance float64
	Positions        []AccountPosition
}

// AccountPosition is one open position, converted from Binance's
// USDT-margined perpetual symbol (e.g. "BTCUSDT") back to the canonical
// asset symbol ("BTC") — same convention market-data's binance collector
// uses for the reverse conversion (see market-data/internal/exchange/binance).
type AccountPosition struct {
	Asset    string
	Quantity float64
}

type accountResponse struct {
	AvailableBalance string `json:"availableBalance"`
	Positions        []struct {
		Symbol      string `json:"symbol"`
		PositionAmt string `json:"positionAmt"`
	} `json:"positions"`
}

// GetAccount reads real balance and open positions from the exchange.
// Positions with a zero quantity are omitted (Binance's account endpoint
// lists every symbol it tracks, most with a zero position).
func (c *Client) GetAccount(ctx context.Context) (Account, error) {
	body, err := c.signedRequest(ctx, http.MethodGet, "/fapi/v2/account", url.Values{})
	if err != nil {
		return Account{}, fmt.Errorf("binance: get account: %w", err)
	}
	var raw accountResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Account{}, fmt.Errorf("binance: get account: unmarshal: %w", err)
	}
	balance, err := strconv.ParseFloat(raw.AvailableBalance, 64)
	if err != nil {
		return Account{}, fmt.Errorf("binance: get account: parse balance: %w", err)
	}
	account := Account{AvailableBalance: balance}
	for _, p := range raw.Positions {
		qty, err := strconv.ParseFloat(p.PositionAmt, 64)
		if err != nil {
			return Account{}, fmt.Errorf("binance: get account: parse position %s: %w", p.Symbol, err)
		}
		if qty == 0 {
			continue
		}
		account.Positions = append(account.Positions, AccountPosition{
			Asset:    strings.TrimSuffix(p.Symbol, "USDT"),
			Quantity: qty,
		})
	}
	return account, nil
}
```

- [ ] **Step 5: Write `internal/binanceclient/account_test.go`**

```go
// execution/internal/binanceclient/account_test.go
package binanceclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAccount_ParsesBalanceAndFiltersZeroPositions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("signature") == "" || r.URL.Query().Get("timestamp") == "" {
			t.Errorf("request missing signature/timestamp: %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("X-MBX-APIKEY"); got != "test-key" {
			t.Errorf("X-MBX-APIKEY = %q, want test-key", got)
		}
		w.Write([]byte(`{
			"availableBalance": "1000.50",
			"positions": [
				{"symbol": "BTCUSDT", "positionAmt": "0.500"},
				{"symbol": "ETHUSDT", "positionAmt": "0.000"}
			]
		}`))
	}))
	defer server.Close()

	client := New("test-key", "test-secret", server.URL)
	account, err := client.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if account.AvailableBalance != 1000.50 {
		t.Errorf("AvailableBalance = %v, want 1000.50", account.AvailableBalance)
	}
	if len(account.Positions) != 1 {
		t.Fatalf("Positions = %+v, want exactly 1 (zero-quantity position filtered out)", account.Positions)
	}
	if account.Positions[0].Asset != "BTC" || account.Positions[0].Quantity != 0.5 {
		t.Errorf("Positions[0] = %+v, want {BTC 0.5}", account.Positions[0])
	}
}
```

- [ ] **Step 6: Run the tests**

Run: `docker compose exec go go test ./internal/binanceclient/... -v`
Expected: PASS for both tests.

- [ ] **Step 7: Commit**

```bash
git add execution/go.mod execution/go.sum execution/docker-compose.yml execution/internal/binanceclient/
git commit -m "feat(execution): module scaffold, Binance auth signing, account fetch"
```

---

### Task 2: `execution` storage (executions audit table)

**Files:**
- Create: `execution/migrations/001_init.sql`
- Create: `execution/internal/storage/db.go`
- Create: `execution/internal/storage/executions.go`
- Create: `execution/internal/storage/executions_test.go`

**Interfaces:**
- Produces: `storage.Store` (`storage.New(ctx, dsn) (*Store, error)`, `(*Store).Close()`), `storage.Execution`, `(*Store).SaveExecution(ctx, Execution) error` — consumed by Task 3's `executor` package.

- [ ] **Step 1: Write `migrations/001_init.sql`**

```sql
-- execution/migrations/001_init.sql
CREATE TABLE IF NOT EXISTS executions (
    id                 TEXT PRIMARY KEY,
    asset              TEXT NOT NULL,
    side               TEXT NOT NULL,
    requested_quantity DOUBLE PRECISION NOT NULL,
    price              DOUBLE PRECISION NOT NULL,
    order_id           TEXT NOT NULL DEFAULT '',
    client_order_id    TEXT NOT NULL,
    status             TEXT NOT NULL,
    filled_quantity    DOUBLE PRECISION NOT NULL DEFAULT 0,
    filled_price       DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL
);
```

Apply it (same shared TimescaleDB instance every module uses):
`docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/001_init.sql`, run from `execution/`.

Then: `docker exec market-data-timescaledb-1 psql -U marketdata -d marketdata -c '\d executions'`

- [ ] **Step 2: Write `internal/storage/db.go`**

```go
// execution/internal/storage/db.go
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

- [ ] **Step 3: Write `internal/storage/executions.go`**

```go
// execution/internal/storage/executions.go
package storage

import (
	"context"
	"time"
)

// Execution is one persisted executions row — the full record of one
// attempt to place and follow an order through to fill or cancellation.
type Execution struct {
	ID                string
	Asset             string
	Side              string
	RequestedQuantity float64
	Price             float64
	OrderID           string
	ClientOrderID     string
	Status            string
	FilledQuantity    float64
	FilledPrice       float64
	CreatedAt         time.Time
}

func (s *Store) SaveExecution(ctx context.Context, e Execution) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO executions
			(id, asset, side, requested_quantity, price, order_id, client_order_id,
			 status, filled_quantity, filled_price, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, e.ID, e.Asset, e.Side, e.RequestedQuantity, e.Price, e.OrderID, e.ClientOrderID,
		e.Status, e.FilledQuantity, e.FilledPrice, e.CreatedAt)
	return err
}

// ExecutionForTest reads back one executions row by ID — used by tests
// to verify SaveExecution persisted what was asked.
func (s *Store) ExecutionForTest(ctx context.Context, id string) (Execution, error) {
	var e Execution
	err := s.pool.QueryRow(ctx, `
		SELECT id, asset, side, requested_quantity, price, order_id, client_order_id,
		       status, filled_quantity, filled_price, created_at
		FROM executions WHERE id = $1
	`, id).Scan(&e.ID, &e.Asset, &e.Side, &e.RequestedQuantity, &e.Price, &e.OrderID, &e.ClientOrderID,
		&e.Status, &e.FilledQuantity, &e.FilledPrice, &e.CreatedAt)
	return e, err
}

// DeleteExecutionForTest removes one executions row — used by tests to
// clean up after themselves.
func (s *Store) DeleteExecutionForTest(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM executions WHERE id = $1`, id)
	return err
}
```

- [ ] **Step 4: Write `internal/storage/executions_test.go`**

```go
// execution/internal/storage/executions_test.go
package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestSaveExecution_RoundTrips(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	id := "test-execution-roundtrip"
	defer store.DeleteExecutionForTest(context.Background(), id)

	want := Execution{
		ID: id, Asset: "BTC", Side: "buy", RequestedQuantity: 1.5, Price: 50000,
		OrderID: "12345", ClientOrderID: id, Status: "filled",
		FilledQuantity: 1.5, FilledPrice: 50001.2, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := store.SaveExecution(context.Background(), want); err != nil {
		t.Fatalf("SaveExecution: %v", err)
	}

	got, err := store.ExecutionForTest(context.Background(), id)
	if err != nil {
		t.Fatalf("ExecutionForTest: %v", err)
	}
	if got != want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `docker compose exec go go test ./internal/storage/... -v`
Expected: PASS if `TEST_DATABASE_URL` is set in the container (it is, per `docker-compose.yml`); skip message otherwise.

- [ ] **Step 6: Commit**

```bash
git add execution/migrations/001_init.sql execution/internal/storage/
git commit -m "feat(execution): executions audit table and storage"
```

---

### Task 3: Binance order operations + `executor` package (public API)

**Files:**
- Create: `execution/internal/binanceclient/orders.go`
- Create: `execution/internal/binanceclient/orders_test.go`
- Create: `execution/executor/client.go`
- Create: `execution/executor/client_test.go`

**Interfaces:**
- Consumes: `binanceclient.Client`/`New`/`GetAccount` (Task 1), `storage.Store`/`New`/`SaveExecution` (Task 2), `risk.Side`/`risk.SideBuy`/`risk.SideSell` (existing, `risk-engine/risk`).
- Produces: `executor.Client` interface (`FetchPortfolio(ctx) (float64, map[string]float64, error)`, `Execute(ctx, asset string, side risk.Side, quantity, price float64, clientOrderID string) (Outcome, error)`), `executor.Outcome`, `executor.NewClient(ctx, dsn string) (*BinanceExecutor, error)`, `(*BinanceExecutor).Close()` — consumed by Tasks 4 and 5 (`strategist`).

- [ ] **Step 1: Write `internal/binanceclient/orders.go`**

```go
// execution/internal/binanceclient/orders.go
package binanceclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Order is the subset of Binance's order response this client needs,
// shared by PlaceLimitOrder, GetOrderStatus, and CancelOrder — all three
// endpoints return the same shape.
type Order struct {
	OrderID       int64
	ClientOrderID string
	Status        string // NEW, PARTIALLY_FILLED, FILLED, CANCELED, EXPIRED, REJECTED
	ExecutedQty   float64
	AvgPrice      float64
}

type orderResponse struct {
	OrderID       int64  `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
	Status        string `json:"status"`
	ExecutedQty   string `json:"executedQty"`
	AvgPrice      string `json:"avgPrice"`
}

func parseOrder(body []byte) (Order, error) {
	var raw orderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Order{}, fmt.Errorf("unmarshal order: %w", err)
	}
	executedQty, err := strconv.ParseFloat(raw.ExecutedQty, 64)
	if err != nil {
		return Order{}, fmt.Errorf("parse executedQty: %w", err)
	}
	avgPrice, err := strconv.ParseFloat(raw.AvgPrice, 64)
	if err != nil {
		return Order{}, fmt.Errorf("parse avgPrice: %w", err)
	}
	return Order{
		OrderID:       raw.OrderID,
		ClientOrderID: raw.ClientOrderID,
		Status:        raw.Status,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
	}, nil
}

// symbolFor converts a canonical asset symbol ("BTC") to Binance's
// USDT-margined perpetual futures symbol ("BTCUSDT") — same convention
// market-data's binance collector uses.
func symbolFor(asset string) string { return asset + "USDT" }

func (c *Client) PlaceLimitOrder(ctx context.Context, asset, side string, quantity, price float64, clientOrderID string) (Order, error) {
	params := url.Values{}
	params.Set("symbol", symbolFor(asset))
	params.Set("side", strings.ToUpper(side))
	params.Set("type", "LIMIT")
	params.Set("timeInForce", "GTC")
	params.Set("quantity", strconv.FormatFloat(quantity, 'f', -1, 64))
	params.Set("price", strconv.FormatFloat(price, 'f', -1, 64))
	params.Set("newClientOrderId", clientOrderID)

	body, err := c.signedRequest(ctx, http.MethodPost, "/fapi/v1/order", params)
	if err != nil {
		return Order{}, fmt.Errorf("binance: place order: %w", err)
	}
	return parseOrder(body)
}

func (c *Client) GetOrderStatus(ctx context.Context, asset, clientOrderID string) (Order, error) {
	params := url.Values{}
	params.Set("symbol", symbolFor(asset))
	params.Set("origClientOrderId", clientOrderID)

	body, err := c.signedRequest(ctx, http.MethodGet, "/fapi/v1/order", params)
	if err != nil {
		return Order{}, fmt.Errorf("binance: get order status: %w", err)
	}
	return parseOrder(body)
}

func (c *Client) CancelOrder(ctx context.Context, asset, clientOrderID string) (Order, error) {
	params := url.Values{}
	params.Set("symbol", symbolFor(asset))
	params.Set("origClientOrderId", clientOrderID)

	body, err := c.signedRequest(ctx, http.MethodDelete, "/fapi/v1/order", params)
	if err != nil {
		return Order{}, fmt.Errorf("binance: cancel order: %w", err)
	}
	return parseOrder(body)
}
```

- [ ] **Step 2: Write `internal/binanceclient/orders_test.go`**

`parseOrder` is the risky parsing logic (numeric strings from Binance's JSON) — worth testing directly, independent of which HTTP verb produced the response.

```go
// execution/internal/binanceclient/orders_test.go
package binanceclient

import "testing"

func TestParseOrder_ParsesNumericStringFields(t *testing.T) {
	order, err := parseOrder([]byte(`{
		"orderId": 12345,
		"clientOrderId": "abc-123",
		"status": "PARTIALLY_FILLED",
		"executedQty": "0.250",
		"avgPrice": "45000.12"
	}`))
	if err != nil {
		t.Fatalf("parseOrder: %v", err)
	}
	want := Order{OrderID: 12345, ClientOrderID: "abc-123", Status: "PARTIALLY_FILLED", ExecutedQty: 0.25, AvgPrice: 45000.12}
	if order != want {
		t.Errorf("order = %+v, want %+v", order, want)
	}
}

func TestSymbolFor(t *testing.T) {
	if got := symbolFor("BTC"); got != "BTCUSDT" {
		t.Errorf("symbolFor(BTC) = %q, want BTCUSDT", got)
	}
}
```

- [ ] **Step 3: Write `executor/client.go`**

```go
// execution/executor/client.go
package executor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"risk-engine/risk"

	"execution/internal/binanceclient"
	"execution/internal/storage"
)

const testnetBaseURL = "https://testnet.binancefuture.com"

// Compile-time assertion that *BinanceExecutor satisfies Client — same
// convention used in market-data/internal/exchange/binance/binance.go.
var _ Client = (*BinanceExecutor)(nil)

// Outcome is the real result of attempting to execute one order on the
// exchange — filled, partially filled (then cancelled at timeout), or
// cancelled with nothing filled. Never an error on its own; a timeout
// without any fill is still a valid Outcome, not a failure.
type Outcome struct {
	OrderID        string
	ClientOrderID  string
	Status         string // "filled", "partial", "cancelled"
	FilledQuantity float64
	FilledPrice    float64
}

// Client is the executor's public interface — FetchPortfolio to read the
// exchange's real balance/positions before sizing a decision, Execute to
// place and follow one order through to fill or cancellation. A fake
// implementing this interface lets strategist's tests exercise the sell
// clamp and execution wiring without a real exchange connection.
type Client interface {
	// FetchPortfolio returns cash and a map of asset symbol to held
	// quantity — the same shape strategist's existing buildPortfolio
	// already expects, so callers price and value positions themselves.
	FetchPortfolio(ctx context.Context) (cash float64, positions map[string]float64, err error)
	Execute(ctx context.Context, asset string, side risk.Side, quantity, price float64, clientOrderID string) (Outcome, error)
}

// binanceOps is the subset of *binanceclient.Client this package calls —
// letting tests substitute a fake instead of hitting the real Binance API.
type binanceOps interface {
	GetAccount(ctx context.Context) (binanceclient.Account, error)
	PlaceLimitOrder(ctx context.Context, asset, side string, quantity, price float64, clientOrderID string) (binanceclient.Order, error)
	GetOrderStatus(ctx context.Context, asset, clientOrderID string) (binanceclient.Order, error)
	CancelOrder(ctx context.Context, asset, clientOrderID string) (binanceclient.Order, error)
}

// executionStore is the subset of *storage.Store this package calls —
// same reason as binanceOps: fakeable in tests.
type executionStore interface {
	SaveExecution(ctx context.Context, e storage.Execution) error
	Close()
}

// BinanceExecutor is the production implementation of Client.
type BinanceExecutor struct {
	binance      binanceOps
	store        executionStore
	pollInterval time.Duration
	fillTimeout  time.Duration
}

// NewClient reads BINANCE_API_KEY/BINANCE_API_SECRET from the
// environment (both required — never a silent no-op) and connects to
// storage using dsn, matching this repo's existing storage.New(ctx, dsn)
// convention (the caller reads DATABASE_URL and passes it in).
func NewClient(ctx context.Context, dsn string) (*BinanceExecutor, error) {
	apiKey := os.Getenv("BINANCE_API_KEY")
	secret := os.Getenv("BINANCE_API_SECRET")
	if apiKey == "" || secret == "" {
		return nil, fmt.Errorf("executor: BINANCE_API_KEY and BINANCE_API_SECRET are required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("executor: connect storage: %w", err)
	}
	return &BinanceExecutor{
		binance:      binanceclient.New(apiKey, secret, testnetBaseURL),
		store:        store,
		pollInterval: 2 * time.Second,
		fillTimeout:  30 * time.Second,
	}, nil
}

func (e *BinanceExecutor) Close() {
	e.store.Close()
}

func (e *BinanceExecutor) FetchPortfolio(ctx context.Context) (float64, map[string]float64, error) {
	account, err := e.binance.GetAccount(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("executor: fetch portfolio: %w", err)
	}
	positions := make(map[string]float64, len(account.Positions))
	for _, p := range account.Positions {
		positions[p.Asset] = p.Quantity
	}
	return account.AvailableBalance, positions, nil
}

// Execute places a limit order and follows it until it fills or
// fillTimeout elapses, cancelling in the latter case. The resulting
// Outcome — filled, partial, or cancelled — is always persisted via
// SaveExecution before returning, using clientOrderID as the row's ID
// (the same value the caller minted as the decision's own ID).
func (e *BinanceExecutor) Execute(ctx context.Context, asset string, side risk.Side, quantity, price float64, clientOrderID string) (Outcome, error) {
	order, err := e.binance.PlaceLimitOrder(ctx, asset, string(side), quantity, price, clientOrderID)
	if err != nil {
		return Outcome{}, fmt.Errorf("executor: %s: place order: %w", asset, err)
	}

	deadline := time.Now().Add(e.fillTimeout)
	for order.Status != "FILLED" && time.Now().Before(deadline) {
		time.Sleep(e.pollInterval)
		order, err = e.binance.GetOrderStatus(ctx, asset, clientOrderID)
		if err != nil {
			return Outcome{}, fmt.Errorf("executor: %s: get order status: %w", asset, err)
		}
	}

	status := "filled"
	if order.Status != "FILLED" {
		cancelled, err := e.binance.CancelOrder(ctx, asset, clientOrderID)
		if err != nil {
			return Outcome{}, fmt.Errorf("executor: %s: cancel order: %w", asset, err)
		}
		order = cancelled
		if order.ExecutedQty > 0 {
			status = "partial"
		} else {
			status = "cancelled"
		}
	}

	outcome := Outcome{
		OrderID:        strconv.FormatInt(order.OrderID, 10),
		ClientOrderID:  order.ClientOrderID,
		Status:         status,
		FilledQuantity: order.ExecutedQty,
		FilledPrice:    order.AvgPrice,
	}
	err = e.store.SaveExecution(ctx, storage.Execution{
		ID: clientOrderID, Asset: asset, Side: string(side),
		RequestedQuantity: quantity, Price: price,
		OrderID: outcome.OrderID, ClientOrderID: clientOrderID,
		Status: status, FilledQuantity: outcome.FilledQuantity, FilledPrice: outcome.FilledPrice,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return outcome, fmt.Errorf("executor: %s: save execution: %w", asset, err)
	}
	return outcome, nil
}
```

- [ ] **Step 4: Write `executor/client_test.go`**

Tests the real logic — status classification (filled / partial /
cancelled) and that a fill on the first poll never calls Cancel — using a
fake `binanceOps` and a fake `executionStore`, with `pollInterval`/
`fillTimeout` shrunk to milliseconds so the timeout-path tests run fast.

```go
// execution/executor/client_test.go
package executor

import (
	"context"
	"testing"
	"time"

	"risk-engine/risk"

	"execution/internal/binanceclient"
	"execution/internal/storage"
)

type fakeBinance struct {
	placeOrder  binanceclient.Order
	statusSeq   []binanceclient.Order
	statusIdx   int
	cancelOrder binanceclient.Order
	cancelCalls int
}

func (f *fakeBinance) GetAccount(context.Context) (binanceclient.Account, error) {
	return binanceclient.Account{}, nil
}

func (f *fakeBinance) PlaceLimitOrder(context.Context, string, string, float64, float64, string) (binanceclient.Order, error) {
	return f.placeOrder, nil
}

func (f *fakeBinance) GetOrderStatus(context.Context, string, string) (binanceclient.Order, error) {
	o := f.statusSeq[f.statusIdx]
	if f.statusIdx < len(f.statusSeq)-1 {
		f.statusIdx++
	}
	return o, nil
}

func (f *fakeBinance) CancelOrder(context.Context, string, string) (binanceclient.Order, error) {
	f.cancelCalls++
	return f.cancelOrder, nil
}

type fakeStore struct {
	saved []storage.Execution
}

func (f *fakeStore) SaveExecution(_ context.Context, e storage.Execution) error {
	f.saved = append(f.saved, e)
	return nil
}

func (f *fakeStore) Close() {}

func TestExecute_FilledOnFirstPollNeverCancels(t *testing.T) {
	binance := &fakeBinance{
		placeOrder: binanceclient.Order{Status: "NEW"},
		statusSeq:  []binanceclient.Order{{OrderID: 1, ClientOrderID: "cid", Status: "FILLED", ExecutedQty: 1.0, AvgPrice: 100}},
	}
	store := &fakeStore{}
	e := &BinanceExecutor{binance: binance, store: store, pollInterval: time.Millisecond, fillTimeout: 20 * time.Millisecond}

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "filled" || outcome.FilledQuantity != 1.0 {
		t.Errorf("outcome = %+v, want filled 1.0", outcome)
	}
	if binance.cancelCalls != 0 {
		t.Errorf("cancelCalls = %d, want 0", binance.cancelCalls)
	}
	if len(store.saved) != 1 || store.saved[0].Status != "filled" {
		t.Errorf("saved = %+v, want one filled execution persisted", store.saved)
	}
}

func TestExecute_TimeoutWithNoFillCancels(t *testing.T) {
	binance := &fakeBinance{
		placeOrder:  binanceclient.Order{Status: "NEW"},
		statusSeq:   []binanceclient.Order{{Status: "NEW"}},
		cancelOrder: binanceclient.Order{OrderID: 2, ClientOrderID: "cid", Status: "CANCELED", ExecutedQty: 0},
	}
	e := &BinanceExecutor{binance: binance, store: &fakeStore{}, pollInterval: time.Millisecond, fillTimeout: 3 * time.Millisecond}

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "cancelled" || outcome.FilledQuantity != 0 {
		t.Errorf("outcome = %+v, want cancelled with 0 filled", outcome)
	}
	if binance.cancelCalls != 1 {
		t.Errorf("cancelCalls = %d, want 1", binance.cancelCalls)
	}
}

func TestExecute_TimeoutWithPartialFillReportsPartial(t *testing.T) {
	binance := &fakeBinance{
		placeOrder:  binanceclient.Order{Status: "NEW"},
		statusSeq:   []binanceclient.Order{{Status: "PARTIALLY_FILLED", ExecutedQty: 0.3}},
		cancelOrder: binanceclient.Order{OrderID: 3, ClientOrderID: "cid", Status: "CANCELED", ExecutedQty: 0.3, AvgPrice: 100},
	}
	e := &BinanceExecutor{binance: binance, store: &fakeStore{}, pollInterval: time.Millisecond, fillTimeout: 3 * time.Millisecond}

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "partial" || outcome.FilledQuantity != 0.3 {
		t.Errorf("outcome = %+v, want partial with 0.3 filled", outcome)
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `docker compose exec go go test ./... -v -count=1`
Expected: PASS for all tests across `internal/binanceclient` and `executor`.

- [ ] **Step 6: Commit**

```bash
git add execution/internal/binanceclient/orders.go execution/internal/binanceclient/orders_test.go execution/executor/
git commit -m "feat(execution): order placement/polling/cancel and the public executor API"
```

---

### Task 4: `strategist` — sell clamp, execution wiring in `Decide`, decision schema

**Files:**
- Modify: `strategist/go.mod`
- Modify: `strategist/docker-compose.yml`
- Create: `strategist/migrations/002_execution_outcome.sql`
- Modify: `strategist/internal/storage/decisions.go`
- Modify: `strategist/internal/strategist/decide.go`
- Modify: `strategist/internal/strategist/decide_test.go`

**Interfaces:**
- Consumes: `executor.Client`/`executor.Outcome` (Task 3), `risk.Side`/`risk.SideBuy`/`risk.SideSell`/`risk.PortfolioState`/`risk.Position` (existing).
- Produces: `strategist.Outcome` gains `Execution *executor.Outcome`, `ExecutionErr error`; `Decide`'s signature gains `execClient executor.Client, decisionID string` — consumed by Task 5 (`runner.go`/`main.go`).

- [ ] **Step 1: Add the `execution` module dependency**

```bash
cd strategist && go mod edit -require=execution@v0.0.0-00010101000000-000000000000 -replace=execution=../execution && go mod tidy
```

Add `../execution:/execution` to `strategist/docker-compose.yml`'s `go` service `volumes:` list, alongside the existing `../risk-engine:/risk-engine`:

```yaml
    volumes:
      - .:/app
      - ../risk-engine:/risk-engine
      - ../execution:/execution
      - go-mod-cache:/go/pkg/mod
```

- [ ] **Step 2: Write `migrations/002_execution_outcome.sql`**

```sql
-- strategist/migrations/002_execution_outcome.sql
ALTER TABLE strategist_decisions
    ADD COLUMN IF NOT EXISTS execution_status TEXT,
    ADD COLUMN IF NOT EXISTS execution_order_id TEXT,
    ADD COLUMN IF NOT EXISTS execution_filled_quantity DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS execution_filled_price DOUBLE PRECISION;
```

Apply it (from `strategist/`):
`docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/002_execution_outcome.sql`

Then: `docker exec market-data-timescaledb-1 psql -U marketdata -d marketdata -c '\d strategist_decisions'`

- [ ] **Step 3: Update `internal/storage/decisions.go`**

Replace the whole file:

```go
// strategist/internal/storage/decisions.go
package storage

import (
	"context"
	"encoding/json"
	"time"
)

// Decision is one persisted strategist_decisions row: the LLM's decision
// (Side/Confidence/SizingPct/Rationale) plus the sized proposal, the
// risk-engine's verdict on it, and — since sub-project 8 — the real
// execution outcome. RiskAllowed is nil when Side is "hold" or
// risk.Evaluate itself failed. The Execution* fields are nil whenever
// Execution wasn't attempted (hold, risk rejection, sell-clamped to
// zero) or the execution call itself failed — see strategist.Decide.
type Decision struct {
	ID                      string
	AnalysisRunID           string
	Asset                   string
	Side                    string
	Confidence              float64
	SizingPct               float64
	Rationale               string
	ProposedQuantity        float64
	ProposedValue           float64
	RiskAllowed             *bool
	RiskReasons             []string
	ExecutionStatus         *string
	ExecutionOrderID        *string
	ExecutionFilledQuantity *float64
	ExecutionFilledPrice    *float64
	CreatedAt               time.Time
}

// SaveDecision marshals RiskReasons to JSON and inserts one
// strategist_decisions row.
func (s *Store) SaveDecision(ctx context.Context, d Decision) error {
	reasons := d.RiskReasons
	if reasons == nil {
		reasons = []string{}
	}
	raw, err := json.Marshal(reasons)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO strategist_decisions
			(id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			 proposed_quantity, proposed_value, risk_allowed, risk_reasons,
			 execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
			 created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`, d.ID, d.AnalysisRunID, d.Asset, d.Side, d.Confidence, d.SizingPct, d.Rationale,
		d.ProposedQuantity, d.ProposedValue, d.RiskAllowed, raw,
		d.ExecutionStatus, d.ExecutionOrderID, d.ExecutionFilledQuantity, d.ExecutionFilledPrice,
		d.CreatedAt)
	return err
}

// DecisionsForTest reads persisted decisions for a run, in insertion
// order — used by tests.
func (s *Store) DecisionsForTest(ctx context.Context, runID string) ([]Decision, error) {
	return s.decisionsForRun(ctx, runID)
}

// DecisionsForRun reads persisted decisions for an analysis run, in
// creation order — used by production callers (e.g. the MCP server's
// run_strategist tool, which reads back what Run just persisted).
func (s *Store) DecisionsForRun(ctx context.Context, analysisRunID string) ([]Decision, error) {
	return s.decisionsForRun(ctx, analysisRunID)
}

func (s *Store) decisionsForRun(ctx context.Context, runID string) ([]Decision, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
		       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
		       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
		       created_at
		FROM strategist_decisions
		WHERE analysis_run_id = $1
		ORDER BY created_at, id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []Decision
	for rows.Next() {
		var d Decision
		var reasonsRaw []byte
		if err := rows.Scan(&d.ID, &d.AnalysisRunID, &d.Asset, &d.Side, &d.Confidence, &d.SizingPct,
			&d.Rationale, &d.ProposedQuantity, &d.ProposedValue, &d.RiskAllowed, &reasonsRaw,
			&d.ExecutionStatus, &d.ExecutionOrderID, &d.ExecutionFilledQuantity, &d.ExecutionFilledPrice,
			&d.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(reasonsRaw, &d.RiskReasons); err != nil {
			return nil, err
		}
		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}

// DeleteDecisionsForRunForTest removes strategist_decisions rows for
// runID — used by tests to clean up after themselves.
func (s *Store) DeleteDecisionsForRunForTest(ctx context.Context, runID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM strategist_decisions WHERE analysis_run_id = $1`, runID)
	return err
}
```

- [ ] **Step 4: Update `internal/strategist/decide.go`**

Replace the whole file:

```go
// strategist/internal/strategist/decide.go
package strategist

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"execution/executor"

	"strategist/internal/llm"
	"strategist/internal/storage"
)

const systemPrompt = `Você é um estrategista de investimentos em criptomoedas. Você recebe indicadores técnicos, de derivativos, de notícias e de contexto de risco sobre um ativo, e decide se deve comprar, vender ou manter a posição atual. Nunca sugira sizing_pct acima de 0.25 (25% do portfólio) numa única operação. Responda sempre usando a ferramenta record_decision.`

// Outcome is what deciding on one asset produces. Risk is nil when
// Decision.Side is "hold", when the sizing clamp reduces Quantity to
// <= 0 (nothing to propose), or when risk.Evaluate itself failed —
// RiskErr is set in that last case so the caller can log it while still
// persisting the LLM's decision. Execution is nil whenever Risk is nil,
// whenever risk.Evaluate rejected the proposal, or when the execution
// call itself failed — ExecutionErr is set in that last case, same
// isolated-failure treatment as RiskErr.
type Outcome struct {
	Decision     llm.Decision
	Quantity     float64
	Value        float64
	Risk         *risk.Decision
	RiskErr      error
	Execution    *executor.Outcome
	ExecutionErr error
}

// Decide asks the LLM for a decision about asset from its three per-asset
// analysis results (technical, derivatives, news) plus the shared
// risk-context result, sizes the proposed operation against
// portfolioValue/price (clamped to the actually-held quantity for a
// sell), validates it via risk.Evaluate, and — if approved — executes it
// for real via execClient using decisionID as the exchange's client
// order ID (so a retry of the same decision never places a duplicate
// order).
//
// Returns an error only when there is no decision at all to persist:
// missing analysis data for asset, or an LLM failure. A risk.Evaluate or
// execution failure is reported through Outcome.RiskErr/ExecutionErr
// instead of the return error, since the LLM's decision is still worth
// keeping in both cases.
func Decide(
	ctx context.Context,
	riskStore *riskstorage.Store,
	llmClient llm.Client,
	execClient executor.Client,
	decisionID string,
	asset string,
	perAsset []storage.AgentResult,
	riskContext storage.AgentResult,
	portfolio risk.PortfolioState,
	portfolioValue, price float64,
) (Outcome, error) {
	userPrompt, err := buildPrompt(asset, perAsset, riskContext)
	if err != nil {
		return Outcome{}, err
	}

	decision, err := llmClient.Decide(ctx, systemPrompt, userPrompt)
	if err != nil {
		return Outcome{}, fmt.Errorf("strategist: %s: decide: %w", asset, err)
	}

	outcome := Outcome{Decision: decision}
	if decision.Side == "hold" {
		return outcome, nil
	}

	outcome.Quantity = decision.SizingPct * portfolioValue / price
	if decision.Side == "sell" {
		outcome.Quantity = math.Min(outcome.Quantity, portfolio.Positions[asset].Quantity)
	}
	if outcome.Quantity <= 0 {
		return outcome, nil
	}
	outcome.Value = outcome.Quantity * price

	proposed := risk.ProposedOperation{
		Asset:    asset,
		Side:     risk.Side(decision.Side),
		Quantity: outcome.Quantity,
		Value:    outcome.Value,
	}
	riskDecision, err := risk.Evaluate(ctx, riskStore, portfolio, proposed, risk.EvalOptions{})
	if err != nil {
		outcome.RiskErr = fmt.Errorf("strategist: %s: risk.Evaluate: %w", asset, err)
		return outcome, nil
	}
	outcome.Risk = &riskDecision

	if !riskDecision.Allowed {
		return outcome, nil
	}

	execOutcome, err := execClient.Execute(ctx, asset, risk.Side(decision.Side), outcome.Quantity, price, decisionID)
	if err != nil {
		outcome.ExecutionErr = fmt.Errorf("strategist: %s: execute: %w", asset, err)
		return outcome, nil
	}
	outcome.Execution = &execOutcome
	return outcome, nil
}

// buildPrompt requires all three per-asset agent types to be present —
// an analysis run that only covers some agents for this asset isn't
// enough context to decide, and the caller should skip the asset instead
// of deciding on partial information.
func buildPrompt(asset string, perAsset []storage.AgentResult, riskContext storage.AgentResult) (string, error) {
	byType := make(map[string]storage.AgentResult, len(perAsset))
	for _, r := range perAsset {
		byType[r.AgentType] = r
	}
	for _, want := range []string{"technical", "derivatives", "news"} {
		if _, ok := byType[want]; !ok {
			return "", fmt.Errorf("strategist: %s: missing %q analysis result for this run", asset, want)
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Ativo: %s\n\n", asset)
	for _, agentType := range []string{"technical", "derivatives", "news"} {
		r := byType[agentType]
		fmt.Fprintf(&sb, "[%s]\nIndicadores: %s\nNarrativa: %s\n\n", agentType, formatIndicators(r.Indicators), r.Narrative)
	}
	fmt.Fprintf(&sb, "[risk_context]\nIndicadores: %s\nNarrativa: %s\n", formatIndicators(riskContext.Indicators), riskContext.Narrative)
	return sb.String(), nil
}

func formatIndicators(indicators map[string]any) string {
	if len(indicators) == 0 {
		return "(nenhum)"
	}
	keys := make([]string, 0, len(indicators))
	for k := range indicators {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, indicators[k]))
	}
	return strings.Join(parts, ", ")
}
```

- [ ] **Step 5: Update `internal/strategist/decide_test.go`**

Replace the whole file:

```go
// strategist/internal/strategist/decide_test.go
package strategist

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"execution/executor"

	"strategist/internal/llm"
	"strategist/internal/storage"
)

func TestBuildPrompt_MissingAgentIsError(t *testing.T) {
	perAsset := []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC", Narrative: "uptrend"},
		{AgentType: "derivatives", Asset: "BTC", Narrative: "normal funding"},
		// "news" missing.
	}
	if _, err := buildPrompt("BTC", perAsset, storage.AgentResult{}); err == nil {
		t.Fatal("expected an error for a missing agent result, got nil")
	}
}

func TestBuildPrompt_IncludesAllThreeAgentsAndRiskContext(t *testing.T) {
	perAsset := []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC", Indicators: map[string]any{"trend": "bullish"}, Narrative: "uptrend narrative"},
		{AgentType: "derivatives", Asset: "BTC", Narrative: "derivatives narrative"},
		{AgentType: "news", Asset: "BTC", Narrative: "news narrative"},
	}
	riskContext := storage.AgentResult{AgentType: "risk_context", Narrative: "risk narrative"}

	prompt, err := buildPrompt("BTC", perAsset, riskContext)
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}
	for _, want := range []string{"uptrend narrative", "derivatives narrative", "news narrative", "risk narrative", "trend=bullish"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

type fakeLLMClient struct {
	decision llm.Decision
	err      error
}

func (f *fakeLLMClient) Decide(context.Context, string, string) (llm.Decision, error) {
	return f.decision, f.err
}

type fakeExecClient struct {
	outcome executor.Outcome
	err     error
	calls   int
}

func (f *fakeExecClient) FetchPortfolio(context.Context) (float64, map[string]float64, error) {
	return 0, nil, nil
}

func (f *fakeExecClient) Execute(context.Context, string, risk.Side, float64, float64, string) (executor.Outcome, error) {
	f.calls++
	return f.outcome, f.err
}

func validPerAsset() []storage.AgentResult {
	return []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC", Narrative: "n1"},
		{AgentType: "derivatives", Asset: "BTC", Narrative: "n2"},
		{AgentType: "news", Asset: "BTC", Narrative: "n3"},
	}
}

func TestDecide_HoldNeverCallsRiskEvaluateOrExecute(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "hold", Rationale: "wait and see"}}

	// riskStore and execClient are nil on purpose: a hold must return
	// before touching either — passing nil turns any accidental call into
	// an immediate nil-pointer panic, which is exactly the assertion we
	// want.
	outcome, err := Decide(context.Background(), nil, client, nil, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Risk != nil || outcome.RiskErr != nil {
		t.Errorf("outcome = %+v, want no risk evaluation for a hold", outcome)
	}
	if outcome.Execution != nil || outcome.ExecutionErr != nil {
		t.Errorf("outcome = %+v, want no execution for a hold", outcome)
	}
	if outcome.Quantity != 0 || outcome.Value != 0 {
		t.Errorf("outcome = %+v, want zero quantity/value for a hold", outcome)
	}
}

func TestDecide_LLMFailureReturnsError(t *testing.T) {
	client := &fakeLLMClient{err: errors.New("rate limited")}

	_, err := Decide(context.Background(), nil, client, nil, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err == nil {
		t.Fatal("expected an error when the LLM call fails, got nil")
	}
}

func TestDecide_MissingAnalysisDataReturnsErrorBeforeCallingLLM(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "hold"}}
	incomplete := []storage.AgentResult{{AgentType: "technical", Asset: "BTC", Narrative: "n1"}}

	if _, err := Decide(context.Background(), nil, client, nil, "decision-1", "BTC", incomplete, storage.AgentResult{}, riskPortfolioState(), 10000, 100); err == nil {
		t.Fatal("expected an error for incomplete analysis data, got nil")
	}
}

func TestDecide_RiskEvaluationFailureIsReportedInOutcomeAndSkipsExecution(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	riskStore, err := riskstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	// Closing the concrete store is the controlled failure path available to
	// this package: risk.Evaluate accepts *storage.Store, not an interface.
	riskStore.Close()

	client := &fakeLLMClient{decision: llm.Decision{Side: "buy", SizingPct: 0.1, Rationale: "persist despite risk failure"}}
	// execClient is nil on purpose: a risk.Evaluate failure must return
	// before ever calling Execute.
	outcome, err := Decide(context.Background(), riskStore, client, nil, "decision-1", "TESTASSETRISKOUTCOME", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Risk != nil || outcome.RiskErr == nil {
		t.Fatalf("outcome = %+v, want Risk=nil and RiskErr set", outcome)
	}
	if outcome.Execution != nil || outcome.ExecutionErr != nil {
		t.Errorf("outcome = %+v, want no execution attempted after a risk.Evaluate failure", outcome)
	}
}

func TestDecide_SellClampsToHeldQuantity(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	riskStore, err := riskstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	// Closed on purpose, same reasoning as the risk-evaluation-failure
	// test above: outcome.Quantity is computed (and clamped) before
	// risk.Evaluate runs, so a controlled risk.Evaluate failure afterward
	// doesn't affect what's being asserted here.
	riskStore.Close()

	// sizing_pct=0.5 against a 10000 portfolio at price=100 would propose
	// selling 50 units — but only 2 are actually held.
	client := &fakeLLMClient{decision: llm.Decision{Side: "sell", SizingPct: 0.5, Rationale: "take profit"}}
	portfolio := riskPortfolioState()
	portfolio.Positions = map[string]risk.Position{"BTC": {Asset: "BTC", Quantity: 2, Value: 200}}

	outcome, err := Decide(context.Background(), riskStore, client, nil, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, portfolio, 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Quantity != 2 {
		t.Errorf("Quantity = %v, want 2 (clamped to the held position)", outcome.Quantity)
	}
}

func TestDecide_SellWithNoPositionClampsToZeroAndSkipsEverything(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "sell", SizingPct: 0.5, Rationale: "take profit"}}
	execClient := &fakeExecClient{}

	// riskStore is nil on purpose: a clamp to zero must skip risk.Evaluate
	// and Execute entirely — nothing to propose, nothing to trade.
	outcome, err := Decide(context.Background(), nil, client, execClient, "decision-1", "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Quantity != 0 {
		t.Errorf("Quantity = %v, want 0 (no position held)", outcome.Quantity)
	}
	if execClient.calls != 0 {
		t.Errorf("execClient.calls = %d, want 0", execClient.calls)
	}
}

func riskPortfolioState() risk.PortfolioState {
	return risk.PortfolioState{}
}
```

- [ ] **Step 6: Run the tests**

Run: `docker compose exec go go build ./... && docker compose exec go go test ./... -v -count=1`
Expected: no build errors, all tests pass (2 integration tests skip if `TEST_DATABASE_URL` is unset, otherwise pass).

- [ ] **Step 7: Commit**

```bash
git add strategist/go.mod strategist/go.sum strategist/docker-compose.yml strategist/migrations/002_execution_outcome.sql strategist/internal/storage/decisions.go strategist/internal/strategist/decide.go strategist/internal/strategist/decide_test.go
git commit -m "feat(strategist): sell-quantity clamp and real execution wiring in Decide"
```

---

### Task 5: `strategist` — remove manual portfolio/timeframe, wire `execution.Client`

**Files:**
- Modify: `strategist/cmd/strategist/main.go`
- Modify: `strategist/runner/runner.go`

**Interfaces:**
- Consumes: `executor.NewClient(ctx, dsn) (*executor.BinanceExecutor, error)`, `executor.Client` (Task 3); `strategist.Decide`'s new signature (Task 4).
- Produces: `runner.Run`'s and `runner.RunWithDSN`'s new signatures (no `timeframe`/`cash`/`positions`) — consumed by Task 6 (`mcp`).

- [ ] **Step 1: Replace `cmd/strategist/main.go`**

```go
// strategist/cmd/strategist/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	riskstorage "risk-engine/storage"

	"execution/executor"

	"strategist/internal/llm"
	"strategist/internal/storage"
	"strategist/runner"
)

func main() {
	runID := flag.String("run-id", "", "analysis_run_id to decide from (required)")
	assetsStr := flag.String("assets", "", "comma-separated asset symbols to decide on (required)")
	dailyLoss := flag.Float64("daily-loss", 0, "portfolio daily loss so far, as a fraction (e.g. 0.02 = 2%)")
	weeklyLoss := flag.Float64("weekly-loss", 0, "portfolio weekly loss so far, as a fraction")
	drawdown := flag.Float64("drawdown", 0, "portfolio drawdown from peak, as a fraction")
	consecutiveLosses := flag.Int("consecutive-losses", 0, "number of consecutive losing trades")
	flag.Parse()

	if err := run(context.Background(), *runID, *assetsStr, *dailyLoss, *weeklyLoss, *drawdown, *consecutiveLosses); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, runID, assetsStr string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) error {
	if runID == "" {
		return fmt.Errorf("-run-id is required")
	}
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()
	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()

	client, err := llm.NewClient()
	if err != nil {
		return err
	}
	execClient, err := executor.NewClient(ctx, dsn)
	if err != nil {
		return err
	}
	defer execClient.Close()

	return runner.Run(ctx, store, riskStore, client, execClient, runID, assets, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
}

func splitNonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
```

`-timeframe`, `-cash`, `-positions` flags and `parsePositions` are gone
entirely — portfolio comes from `execClient.FetchPortfolio`, price
timeframe is always `"1m"` (hardcoded inside `runner.go`, not a flag).

- [ ] **Step 2: Replace `runner/runner.go`**

```go
// strategist/runner/runner.go
package runner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"execution/executor"

	"strategist/internal/llm"
	"strategist/internal/storage"
	"strategist/internal/strategist"
)

const priceTimeframe = "1m"

// Run decides on every requested asset, executes approved decisions for
// real via execClient, and persists the outcome. It returns an error
// only for a whole-run failure (can't read analysis results, can't fetch
// the real portfolio, can't price a held position, zero portfolio
// value); a single asset failing to decide is logged to stderr and
// skipped, not returned as an error. Exported so both cmd/strategist and
// other modules (the MCP server) can call it directly.
func Run(
	ctx context.Context,
	store *storage.Store,
	riskStore *riskstorage.Store,
	client llm.Client,
	execClient executor.Client,
	runID string,
	assets []string,
	dailyLoss, weeklyLoss, drawdown float64,
	consecutiveLosses int,
) error {
	results, err := store.ResultsForRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("read analysis results: %w", err)
	}
	byAsset := make(map[string][]storage.AgentResult)
	var riskContext storage.AgentResult
	for _, r := range results {
		if r.AgentType == "risk_context" {
			riskContext = r
			continue
		}
		byAsset[r.Asset] = append(byAsset[r.Asset], r)
	}

	cash, positions, err := execClient.FetchPortfolio(ctx)
	if err != nil {
		return fmt.Errorf("fetch portfolio: %w", err)
	}
	portfolio, portfolioValue, err := buildPortfolio(ctx, store, positions, cash, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
	if err != nil {
		return fmt.Errorf("build portfolio: %w", err)
	}

	for _, asset := range assets {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, asset, priceTimeframe)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: read price: %v\n", asset, err)
			continue
		}
		if !found {
			fmt.Fprintf(os.Stderr, "%s: no price data, skipping\n", asset)
			continue
		}

		decisionID := uuid.NewString()
		outcome, err := strategist.Decide(ctx, riskStore, client, execClient, decisionID, asset, byAsset[asset], riskContext, portfolio, portfolioValue, price)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", asset, err)
			continue
		}
		if outcome.RiskErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", outcome.RiskErr)
		}
		if outcome.ExecutionErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", outcome.ExecutionErr)
		}
		if err := save(ctx, store, runID, decisionID, asset, outcome); err != nil {
			fmt.Fprintf(os.Stderr, "%s: save decision: %v\n", asset, err)
			continue
		}
		report(asset, outcome)
	}
	return nil
}

// buildPortfolio prices every held position (fatal for the whole run if
// any is missing — an inaccurate portfolio valuation makes every
// risk.Evaluate call downstream meaningless) and totals portfolio value
// (cash + all position values) for sizing. A total <= 0 is also fatal —
// see sub-project 5's final review for why a zero-value portfolio must
// not silently size zero-quantity "approved" trades.
func buildPortfolio(ctx context.Context, store *storage.Store, positions map[string]float64, cash float64, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) (risk.PortfolioState, float64, error) {
	riskPositions := make(map[string]risk.Position, len(positions))
	total := cash
	for symbol, qty := range positions {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, symbol, priceTimeframe)
		if err != nil {
			return risk.PortfolioState{}, 0, fmt.Errorf("price for held position %s: %w", symbol, err)
		}
		if !found {
			return risk.PortfolioState{}, 0, fmt.Errorf("no price data for held position %s on %s", symbol, risk.ReferenceExchange)
		}
		value := qty * price
		riskPositions[symbol] = risk.Position{Asset: symbol, Quantity: qty, Value: value}
		total += value
	}
	portfolio := risk.PortfolioState{
		Positions:         riskPositions,
		Cash:              cash,
		DailyLoss:         dailyLoss,
		WeeklyLoss:        weeklyLoss,
		Drawdown:          drawdown,
		ConsecutiveLosses: consecutiveLosses,
	}
	if total <= 0 {
		return risk.PortfolioState{}, 0, fmt.Errorf("portfolio value is zero — check the exchange account has funds")
	}
	return portfolio, total, nil
}

func save(ctx context.Context, store *storage.Store, runID, decisionID, asset string, outcome strategist.Outcome) error {
	d := storage.Decision{
		ID: decisionID, AnalysisRunID: runID, Asset: asset,
		Side: outcome.Decision.Side, Confidence: outcome.Decision.Confidence,
		SizingPct: outcome.Decision.SizingPct, Rationale: outcome.Decision.Rationale,
		ProposedQuantity: outcome.Quantity, ProposedValue: outcome.Value,
		CreatedAt: time.Now().UTC(),
	}
	if outcome.Risk != nil {
		allowed := outcome.Risk.Allowed
		d.RiskAllowed = &allowed
		d.RiskReasons = outcome.Risk.Reasons
	}
	if outcome.Execution != nil {
		status := outcome.Execution.Status
		orderID := outcome.Execution.OrderID
		filledQty := outcome.Execution.FilledQuantity
		filledPrice := outcome.Execution.FilledPrice
		d.ExecutionStatus = &status
		d.ExecutionOrderID = &orderID
		d.ExecutionFilledQuantity = &filledQty
		d.ExecutionFilledPrice = &filledPrice
	}
	return store.SaveDecision(ctx, d)
}

func report(asset string, outcome strategist.Outcome) {
	if outcome.Decision.Side == "hold" {
		fmt.Fprintf(os.Stderr, "%s: hold — %s\n", asset, outcome.Decision.Rationale)
		return
	}
	status := "risk-engine unavailable"
	if outcome.Risk != nil {
		if outcome.Risk.Allowed {
			status = "approved"
		} else {
			status = fmt.Sprintf("rejected (%s)", strings.Join(outcome.Risk.Reasons, "; "))
		}
	}
	execStatus := "not executed"
	if outcome.Execution != nil {
		execStatus = fmt.Sprintf("%s (%.6f @ %.2f)", outcome.Execution.Status, outcome.Execution.FilledQuantity, outcome.Execution.FilledPrice)
	}
	fmt.Fprintf(os.Stderr, "%s: %s %.6f (%s, %s) — %s\n", asset, outcome.Decision.Side, outcome.Quantity, status, execStatus, outcome.Decision.Rationale)
}

// RunWithDSN connects its own storage and execution client using dsn,
// calls Run, then reads back the decisions it persisted (Run itself only
// returns error) — same cross-module-visibility reason as
// analysis/runner.RunWithDSN (sub-project 6, Task 6): callers outside
// this module can't import strategist/internal/storage,
// strategist/internal/llm, or execution/internal/* directly, so this
// function is the module's public entry point for exactly that kind of
// caller (the MCP server).
func RunWithDSN(ctx context.Context, dsn string, riskStore *riskstorage.Store, analysisRunID string, assets []string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) ([]storage.Decision, error) {
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()
	client, err := llm.NewClient()
	if err != nil {
		return nil, err
	}
	execClient, err := executor.NewClient(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer execClient.Close()
	startedAt := time.Now().UTC()
	if err := Run(ctx, store, riskStore, client, execClient, analysisRunID, assets, dailyLoss, weeklyLoss, drawdown, consecutiveLosses); err != nil {
		return nil, err
	}
	decisions, err := store.DecisionsForRun(ctx, analysisRunID)
	if err != nil {
		return nil, err
	}
	fresh := make([]storage.Decision, 0, len(decisions))
	for _, d := range decisions {
		if !d.CreatedAt.Before(startedAt) {
			fresh = append(fresh, d)
		}
	}
	return fresh, nil
}
```

- [ ] **Step 3: Verify it builds and run the full module test suite**

```bash
docker compose exec go go build ./...
docker compose exec go go test ./... -v -count=1
```

Expected: no build errors, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add strategist/cmd/strategist/main.go strategist/runner/runner.go
git commit -m "feat(strategist): fetch real portfolio, drop manual cash/positions/timeframe flags"
```

---

### Task 6: `mcp` — update `run_strategist` for the new `RunWithDSN` signature

**Files:**
- Modify: `mcp/go.mod`
- Modify: `mcp/docker-compose.yml`
- Modify: `mcp/internal/tools/strategist.go`

**Interfaces:**
- Consumes: `strategist/runner.RunWithDSN`'s new signature (Task 5), `strategist/internal/storage.Decision`'s new `ExecutionStatus`/`ExecutionOrderID`/`ExecutionFilledQuantity`/`ExecutionFilledPrice` fields (Task 4, reached only via the already-legal `RunWithDSN` return value, never by importing `strategist/internal/storage` directly).

- [ ] **Step 1: Declare the new transitive dependency and mount**

```bash
cd mcp && go mod tidy
```

This should add `execution` to `mcp/go.mod`'s indirect-require block (mcp
never imports `execution` directly, only via `strategist/runner` →
`execution/executor`) with the corresponding `go.sum` entries.

Add `../execution:/execution` to `mcp/docker-compose.yml`'s `go` service
`volumes:` list, alongside its existing `../analysis`, `../strategist`,
`../simulation`, `../risk-engine` mounts.

- [ ] **Step 2: Update `internal/tools/strategist.go`**

Replace the whole file:

```go
// mcp/internal/tools/strategist.go
package tools

import (
	"context"
	"fmt"

	riskstorage "risk-engine/storage"

	"strategist/runner"
)

// RunStrategistArgs is the run_strategist tool's input. Since
// sub-project 8, portfolio is always fetched from the real exchange
// account — cash/positions are no longer caller-supplied.
type RunStrategistArgs struct {
	AnalysisRunID     string   `json:"analysis_run_id" jsonschema:"an analysis_run_id already produced by run_analysis"`
	Assets            []string `json:"assets" jsonschema:"asset symbols to decide on, a subset of what was analyzed"`
	DailyLoss         float64  `json:"daily_loss,omitempty" jsonschema:"portfolio daily loss so far, as a fraction, e.g. 0.02 for 2%"`
	WeeklyLoss        float64  `json:"weekly_loss,omitempty" jsonschema:"portfolio weekly loss so far, as a fraction"`
	Drawdown          float64  `json:"drawdown,omitempty" jsonschema:"portfolio drawdown from peak, as a fraction"`
	ConsecutiveLosses int      `json:"consecutive_losses,omitempty" jsonschema:"number of consecutive losing trades"`
}

// DecisionResult is one asset's decision, as returned by run_strategist.
// Since sub-project 8, an approved decision is also executed for real
// against the Binance testnet — the Execution* fields report that
// outcome, nil when execution wasn't attempted or failed.
type DecisionResult struct {
	Asset                   string   `json:"asset"`
	Side                    string   `json:"side"`
	Confidence              float64  `json:"confidence"`
	SizingPct               float64  `json:"sizing_pct"`
	Rationale               string   `json:"rationale"`
	ProposedQuantity        float64  `json:"proposed_quantity"`
	ProposedValue           float64  `json:"proposed_value"`
	RiskAllowed             *bool    `json:"risk_allowed,omitempty"`
	RiskReasons             []string `json:"risk_reasons,omitempty"`
	ExecutionStatus         *string  `json:"execution_status,omitempty"`
	ExecutionOrderID        *string  `json:"execution_order_id,omitempty"`
	ExecutionFilledQuantity *float64 `json:"execution_filled_quantity,omitempty"`
	ExecutionFilledPrice    *float64 `json:"execution_filled_price,omitempty"`
}

// RunStrategistResult is the run_strategist tool's output.
type RunStrategistResult struct {
	Decisions []DecisionResult `json:"decisions"`
}

// RunStrategist runs the strategist pipeline via
// strategist/runner.RunWithDSN, which already reads back the persisted
// decisions internally (see that function's doc comment for why).
func RunStrategist(ctx context.Context, dsn string, riskStore *riskstorage.Store, args RunStrategistArgs) (RunStrategistResult, error) {
	if args.AnalysisRunID == "" {
		return RunStrategistResult{}, fmt.Errorf("analysis_run_id is required")
	}
	if len(args.Assets) == 0 {
		return RunStrategistResult{}, fmt.Errorf("assets is required")
	}

	decisions, err := runner.RunWithDSN(ctx, dsn, riskStore, args.AnalysisRunID, args.Assets, args.DailyLoss, args.WeeklyLoss, args.Drawdown, args.ConsecutiveLosses)
	if err != nil {
		return RunStrategistResult{}, err
	}
	result := RunStrategistResult{Decisions: make([]DecisionResult, len(decisions))}
	for i, d := range decisions {
		result.Decisions[i] = DecisionResult{
			Asset: d.Asset, Side: d.Side, Confidence: d.Confidence, SizingPct: d.SizingPct,
			Rationale: d.Rationale, ProposedQuantity: d.ProposedQuantity, ProposedValue: d.ProposedValue,
			RiskAllowed: d.RiskAllowed, RiskReasons: d.RiskReasons,
			ExecutionStatus: d.ExecutionStatus, ExecutionOrderID: d.ExecutionOrderID,
			ExecutionFilledQuantity: d.ExecutionFilledQuantity, ExecutionFilledPrice: d.ExecutionFilledPrice,
		}
	}
	return result, nil
}
```

- [ ] **Step 3: Verify it builds and run the full module test suite**

```bash
docker compose exec go go build ./...
docker compose exec go go test ./... -v -count=1
```

Expected: no build errors, all tests pass — including
`mcp/cmd/mcp-server`'s protocol-level smoke test, unaffected by this
change (it exercises `get_risk_state`, not `run_strategist`).

- [ ] **Step 4: Commit**

```bash
git add mcp/go.mod mcp/go.sum mcp/docker-compose.yml mcp/internal/tools/strategist.go
git commit -m "fix(mcp): update run_strategist for strategist's simplified RunWithDSN signature"
```

---

## Execution Handoff

After Task 6, before merging: run a final review of the whole branch
against `docs/superpowers/specs/2026-08-19-real-execution-design.md`.
Since nothing in this plan's automated tests calls the real Binance
testnet API (per the spec's reduced-rigor policy), the final review
should flag — as it did for OpenAI in sub-project 7 — that a real
end-to-end run (`run_analysis` → `run_strategist` with real
`BINANCE_API_KEY`/`BINANCE_API_SECRET` pointed at a funded Binance
Futures testnet account) is the only way to validate the account/order
JSON field names this plan's code assumes (see Global Constraints) and
the full place → poll → fill/cancel cycle against the real exchange —
recommend it explicitly in the handoff, but do not block merge on it
being performed automatically, same precedent as sub-project 7.
