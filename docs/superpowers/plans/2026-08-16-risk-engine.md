# Risk Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `risk-engine` Go module — a library that evaluates a proposed crypto trade against configurable risk limits (concentration, losses, asset/data quality) and returns an allow/reject decision, using portfolio state supplied by the caller and market data read from the existing shared TimescaleDB.

**Architecture:** A standalone Go module (sibling to `market-data/`) with two packages: `internal/storage` (limits/state/decisions persistence + read-only market-data queries against the shared DB) and `internal/risk` (pure rule functions plus an `Evaluate` orchestrator). No new database instance — connects to the TimescaleDB already running for the market-data-foundation sub-project via a shared Docker network.

**Tech Stack:** Go 1.22, `github.com/jackc/pgx/v5` (pinned to v5.6.0 — the only pgx/v5 release compatible with Go 1.22, already discovered and verified during the market-data-foundation sub-project), stdlib `testing`, no ORM, no HTTP framework (no API in this phase).

**Spec:** `docs/superpowers/specs/2026-08-16-risk-engine-design.md`

## Global Constraints

- Scope is crypto only, consistent with the market-data-foundation sub-project.
- `risk-engine` is a separate Go module (own `go.mod`) — it does not import `market-data`'s `internal/` packages (not importable across modules) and does not duplicate market-data's write-side collector logic, only reads the shared `candles` table it does not own.
- Portfolio state (positions, cash, loss metrics) is supplied by the caller on every call — this module never persists or tracks portfolio state.
- Risk limits are stored in a database table (`risk_limits`), editable at runtime, not in a config file.
- Protective mechanisms (pause, kill switch) only change persisted operational state (`risk_state`) and are logged — they never cancel orders or close positions; that is a future sub-project's job.
- No API layer (HTTP or MCP) in this phase — consumed as a Go library.
- Fail-safe by default: any error or missing data needed for a rule results in rejection, never silent approval.
- `pgx/v5` must be pinned to `v5.6.0` specifically (`go get github.com/jackc/pgx/v5/pgxpool@v5.6.0`) — later v5 releases require Go >=1.25, which this project's toolchain does not have.
- Quality-of-data rules (volatility, liquidity, freshness) read market data from a single reference exchange, `binance`, defined as `risk.ReferenceExchange` — not aggregated across exchanges, a deliberate simplification for this phase.

---

## File Structure

```text
investment-platform/
  risk-engine/
    go.mod
    go.sum
    docker-compose.yml
    migrations/
      001_init.sql
    internal/
      risk/
        types.go
        concentration.go
        concentration_test.go
        losses.go
        losses_test.go
        quality.go
        quality_test.go
        evaluate.go
        evaluate_test.go
      storage/
        db.go
        limits.go
        limits_test.go
        state.go
        state_test.go
        decisions.go
        decisions_test.go
        marketdata.go
        marketdata_test.go
```

---

### Task 1: Scaffold — go.mod, shared-network docker-compose, schema migration

**Files:**
- Create: `risk-engine/go.mod`
- Create: `risk-engine/docker-compose.yml`
- Create: `risk-engine/migrations/001_init.sql`

**Interfaces:**
- Produces: a `go` dev-shell container joined to the market-data-foundation sub-project's existing Docker network (`market-data_default`), able to reach its `timescaledb` service by that DNS name; three new tables (`risk_limits`, `risk_state`, `risk_decisions`) applied to that same running database.

This task's deliverable is verified by inspection and direct SQL queries, not `go test` — there's no Go code yet.

- [ ] **Step 1: Create `go.mod`**

```go
module risk-engine

go 1.22
```

- [ ] **Step 2: Write the schema migration**

```sql
-- risk-engine/migrations/001_init.sql
CREATE TABLE IF NOT EXISTS risk_limits (
    id                     INT PRIMARY KEY DEFAULT 1,
    max_pct_per_asset      DOUBLE PRECISION NOT NULL,
    max_pct_crypto_total   DOUBLE PRECISION NOT NULL,
    max_value_per_trade    DOUBLE PRECISION NOT NULL,
    max_daily_loss         DOUBLE PRECISION NOT NULL,
    max_weekly_loss        DOUBLE PRECISION NOT NULL,
    max_drawdown           DOUBLE PRECISION NOT NULL,
    max_consecutive_losses INT NOT NULL,
    max_volatility         DOUBLE PRECISION NOT NULL,
    min_liquidity          DOUBLE PRECISION NOT NULL,
    max_data_age_minutes   INT NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT risk_limits_single_row CHECK (id = 1)
);

CREATE TABLE IF NOT EXISTS risk_state (
    id         INT PRIMARY KEY DEFAULT 1,
    status     TEXT NOT NULL DEFAULT 'normal',
    reason     TEXT NOT NULL DEFAULT 'initial state',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT risk_state_single_row CHECK (id = 1),
    CONSTRAINT risk_state_valid_status CHECK (status IN ('normal', 'paused', 'kill_switch'))
);

CREATE TABLE IF NOT EXISTS risk_decisions (
    id            BIGSERIAL PRIMARY KEY,
    ts            TIMESTAMPTZ NOT NULL DEFAULT now(),
    asset         TEXT NOT NULL,
    side          TEXT NOT NULL,
    quantity      DOUBLE PRECISION NOT NULL,
    value         DOUBLE PRECISION NOT NULL,
    allowed       BOOLEAN NOT NULL,
    reasons       JSONB NOT NULL,
    rules_checked JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS risk_decisions_ts ON risk_decisions (ts DESC);

INSERT INTO risk_limits (
    id, max_pct_per_asset, max_pct_crypto_total, max_value_per_trade,
    max_daily_loss, max_weekly_loss, max_drawdown, max_consecutive_losses,
    max_volatility, min_liquidity, max_data_age_minutes
) VALUES (
    1, 0.20, 1.00, 1000,
    0.05, 0.10, 0.20, 5,
    0.15, 100000, 30
) ON CONFLICT (id) DO NOTHING;

INSERT INTO risk_state (id, status, reason) VALUES (1, 'normal', 'initial state')
ON CONFLICT (id) DO NOTHING;
```

Default limits above are conservative personal-scale starting points (20% max per asset, $1000 max per trade, 5%/10% daily/weekly loss, 20% max drawdown, 5 consecutive losses, 15% volatility, $100k min liquidity, 30 min max data age) — adjustable later via `risk_limits` without a migration.

- [ ] **Step 3: Write `docker-compose.yml`**

```yaml
services:
  go:
    image: golang:1.22
    working_dir: /app
    volumes:
      - .:/app
      - go-mod-cache:/go/pkg/mod
    environment:
      DATABASE_URL: postgres://marketdata:marketdata@timescaledb:5432/marketdata?sslmode=disable
      TEST_DATABASE_URL: postgres://marketdata:marketdata@timescaledb:5432/marketdata?sslmode=disable
    networks:
      - market-data_default
    command: ["sleep", "infinity"]

networks:
  market-data_default:
    external: true

volumes:
  go-mod-cache:
```

This joins the market-data-foundation sub-project's existing network rather than starting a second TimescaleDB — `timescaledb` resolves by that DNS name because it's the market-data compose project's service name.

- [ ] **Step 4: Bring up the dev container and confirm it joins the shared network**

Run (from `risk-engine/`): `docker compose up -d go`
Then: `docker compose exec go go version`
Expected: prints `go version go1.22...`. If this step fails with a network-not-found error, the market-data-foundation stack isn't running — start it first with `docker compose up -d timescaledb` from the `market-data/` directory.

- [ ] **Step 5: Apply the migration to the already-running shared database**

The market-data TimescaleDB container was initialized before this migration existed, so `docker-entrypoint-initdb.d` auto-run doesn't apply here — apply it directly:

Run: `docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/001_init.sql`
Expected: `CREATE TABLE` / `CREATE INDEX` / `INSERT 0 1` lines, no errors. (If the container name differs on this machine, find it with `docker ps --filter name=timescaledb`.)

- [ ] **Step 6: Verify the tables and seed rows**

Run: `docker exec market-data-timescaledb-1 psql -U marketdata -d marketdata -c "SELECT status, reason FROM risk_state; SELECT max_pct_per_asset, max_value_per_trade FROM risk_limits;"`
Expected: one row each — `status='normal'`, `reason='initial state'`; `max_pct_per_asset=0.2`, `max_value_per_trade=1000`.

- [ ] **Step 7: Commit**

```bash
git add risk-engine/go.mod risk-engine/docker-compose.yml risk-engine/migrations/001_init.sql
git commit -m "chore: scaffold risk-engine module against shared TimescaleDB"
```

From here on, every `go` command in this plan runs as `docker compose exec go <command>` from the `risk-engine/` directory, unless noted otherwise.

---

### Task 2: Shared risk types + concentration rules

**Files:**
- Create: `risk-engine/internal/risk/types.go`
- Create: `risk-engine/internal/risk/concentration.go`
- Test: `risk-engine/internal/risk/concentration_test.go`

**Interfaces:**
- Produces: `risk.Position{Asset, Quantity, Value}`, `risk.PortfolioState{Positions map[string]Position, Cash, DailyLoss, WeeklyLoss, Drawdown float64, ConsecutiveLosses int}`, `risk.Side` (`SideBuy`, `SideSell`), `risk.ProposedOperation{Asset string, Side Side, Quantity, Value float64}`, `risk.RuleResult{Rule string, Passed bool, Measured, Limit float64, Detail string}`, `risk.Decision{Allowed bool, Reasons []string, RulesChecked []RuleResult}`. Every later task in this module imports these.
- Produces: `checkAssetConcentration(portfolio PortfolioState, proposed ProposedOperation, maxPct float64) RuleResult`, `checkCryptoTotalConcentration(portfolio PortfolioState, proposed ProposedOperation, maxPct float64) RuleResult`, `checkMaxTradeValue(proposed ProposedOperation, maxValue float64) RuleResult`.

These are pure functions — no database, no I/O — so tests run with no containers needed, though `docker compose exec go go test` still works fine if containers are up.

- [ ] **Step 1: Write the failing test**

```go
// risk-engine/internal/risk/concentration_test.go
package risk

import "testing"

func TestCheckAssetConcentration_RejectsOverLimit(t *testing.T) {
	portfolio := PortfolioState{
		Cash: 5000,
		Positions: map[string]Position{
			"BTC": {Asset: "BTC", Quantity: 0.1, Value: 4000},
		},
	}
	proposed := ProposedOperation{Asset: "BTC", Side: SideBuy, Quantity: 0.05, Value: 2000}

	result := checkAssetConcentration(portfolio, proposed, 0.5)

	if result.Passed {
		t.Fatalf("expected rejection: BTC would be (4000+2000)/(5000+4000) = %.4f, limit 0.5", result.Measured)
	}
	if result.Rule != "asset_concentration" {
		t.Errorf("Rule = %q", result.Rule)
	}
}

func TestCheckAssetConcentration_AllowsUnderLimit(t *testing.T) {
	portfolio := PortfolioState{
		Cash: 8000,
		Positions: map[string]Position{
			"BTC": {Asset: "BTC", Quantity: 0.05, Value: 2000},
		},
	}
	proposed := ProposedOperation{Asset: "BTC", Side: SideBuy, Quantity: 0.01, Value: 500}

	result := checkAssetConcentration(portfolio, proposed, 0.5)

	if !result.Passed {
		t.Fatalf("expected approval: BTC would be (2000+500)/(8000+2000) = %.4f, limit 0.5", result.Measured)
	}
}

func TestCheckCryptoTotalConcentration_RejectsOverLimit(t *testing.T) {
	portfolio := PortfolioState{
		Cash: 1000,
		Positions: map[string]Position{
			"BTC": {Asset: "BTC", Value: 8000},
			"ETH": {Asset: "ETH", Value: 500},
		},
	}
	proposed := ProposedOperation{Asset: "ETH", Side: SideBuy, Quantity: 1, Value: 800}

	result := checkCryptoTotalConcentration(portfolio, proposed, 0.9)

	if result.Passed {
		t.Fatalf("expected rejection: crypto would be (8000+500+800)/(1000+8000+500) = %.4f, limit 0.9", result.Measured)
	}
}

func TestCheckMaxTradeValue(t *testing.T) {
	tooLarge := ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 1500}
	if r := checkMaxTradeValue(tooLarge, 1000); r.Passed {
		t.Fatalf("expected rejection for value 1500 > limit 1000")
	}

	fine := ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 500}
	if r := checkMaxTradeValue(fine, 1000); !r.Passed {
		t.Fatalf("expected approval for value 500 <= limit 1000")
	}
}
```

- [ ] **Step 2: Run it to confirm it fails to compile**

Run: `docker compose exec go go test ./internal/risk/... -v`
Expected: FAIL — package doesn't build (`Position`, `PortfolioState`, etc. undefined).

- [ ] **Step 3: Implement `types.go`**

```go
// risk-engine/internal/risk/types.go
package risk

// Position is one held asset's current quantity and value, as reported by
// the caller — this module never persists portfolio state itself.
type Position struct {
	Asset    string
	Quantity float64
	Value    float64
}

// PortfolioState is supplied by the caller on every Evaluate call — the
// risk engine has no memory of it between calls.
type PortfolioState struct {
	Positions         map[string]Position
	Cash              float64
	DailyLoss         float64
	WeeklyLoss        float64
	Drawdown          float64
	ConsecutiveLosses int
}

type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

type ProposedOperation struct {
	Asset    string
	Side     Side
	Quantity float64
	Value    float64
}

// RuleResult is one limit's evaluation, kept regardless of pass/fail so
// every decision is fully auditable.
type RuleResult struct {
	Rule     string
	Passed   bool
	Measured float64
	Limit    float64
	Detail   string
}

type Decision struct {
	Allowed      bool
	Reasons      []string
	RulesChecked []RuleResult
}
```

- [ ] **Step 4: Implement `concentration.go`**

```go
// risk-engine/internal/risk/concentration.go
package risk

import "fmt"

// checkAssetConcentration rejects an operation that would push a single
// asset's share of total portfolio value (cash + all positions) above
// maxPct. Buying/selling is assumed to move value 1:1 between cash and the
// position (no slippage/fee modeling in this phase), so total portfolio
// value is unchanged by the operation itself.
func checkAssetConcentration(portfolio PortfolioState, proposed ProposedOperation, maxPct float64) RuleResult {
	total := portfolio.Cash
	for _, p := range portfolio.Positions {
		total += p.Value
	}
	if total <= 0 {
		return RuleResult{Rule: "asset_concentration", Passed: true, Detail: "no portfolio value to evaluate"}
	}

	assetAfter := portfolio.Positions[proposed.Asset].Value
	if proposed.Side == SideBuy {
		assetAfter += proposed.Value
	} else {
		assetAfter -= proposed.Value
		if assetAfter < 0 {
			assetAfter = 0
		}
	}

	pct := assetAfter / total
	return RuleResult{
		Rule: "asset_concentration", Passed: pct <= maxPct,
		Measured: pct, Limit: maxPct,
		Detail: fmt.Sprintf("%s would be %.1f%% of portfolio after this operation", proposed.Asset, pct*100),
	}
}

// checkCryptoTotalConcentration rejects an operation that would push total
// crypto exposure (all positions — everything is crypto in this phase)
// above maxPct of total portfolio value.
func checkCryptoTotalConcentration(portfolio PortfolioState, proposed ProposedOperation, maxPct float64) RuleResult {
	total := portfolio.Cash
	var crypto float64
	for _, p := range portfolio.Positions {
		total += p.Value
		crypto += p.Value
	}
	if total <= 0 {
		return RuleResult{Rule: "crypto_total_concentration", Passed: true, Detail: "no portfolio value to evaluate"}
	}

	if proposed.Side == SideBuy {
		crypto += proposed.Value
	} else {
		crypto -= proposed.Value
		if crypto < 0 {
			crypto = 0
		}
	}

	pct := crypto / total
	return RuleResult{
		Rule: "crypto_total_concentration", Passed: pct <= maxPct,
		Measured: pct, Limit: maxPct,
		Detail: fmt.Sprintf("total crypto exposure would be %.1f%% of portfolio after this operation", pct*100),
	}
}

// checkMaxTradeValue rejects a single operation larger than maxValue.
func checkMaxTradeValue(proposed ProposedOperation, maxValue float64) RuleResult {
	return RuleResult{
		Rule: "max_trade_value", Passed: proposed.Value <= maxValue,
		Measured: proposed.Value, Limit: maxValue,
		Detail: fmt.Sprintf("operation value %.2f", proposed.Value),
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `docker compose exec go go test ./internal/risk/... -v`
Expected: PASS (all four tests).

- [ ] **Step 6: Commit**

```bash
git add risk-engine/internal/risk/types.go risk-engine/internal/risk/concentration.go risk-engine/internal/risk/concentration_test.go
git commit -m "feat: add risk types and concentration rules"
```

---

### Task 3: Loss rules

**Files:**
- Create: `risk-engine/internal/risk/losses.go`
- Test: `risk-engine/internal/risk/losses_test.go`

**Interfaces:**
- Consumes: `PortfolioState` (Task 2).
- Produces: `checkDailyLoss(portfolio PortfolioState, maxDailyLoss float64) RuleResult`, `checkWeeklyLoss(portfolio PortfolioState, maxWeeklyLoss float64) RuleResult`, `checkDrawdown(portfolio PortfolioState, maxDrawdown float64) RuleResult`, `checkConsecutiveLosses(portfolio PortfolioState, maxConsecutiveLosses int) RuleResult`. Task 9 (`Evaluate`) calls all four and, if any fails, transitions operational state to `paused`.

Pure functions again — no database needed.

- [ ] **Step 1: Write the failing test**

```go
// risk-engine/internal/risk/losses_test.go
package risk

import "testing"

func TestCheckDailyLoss(t *testing.T) {
	over := PortfolioState{DailyLoss: 0.08}
	if r := checkDailyLoss(over, 0.05); r.Passed {
		t.Fatalf("expected rejection: daily loss 0.08 > limit 0.05")
	}

	under := PortfolioState{DailyLoss: 0.02}
	if r := checkDailyLoss(under, 0.05); !r.Passed {
		t.Fatalf("expected approval: daily loss 0.02 <= limit 0.05")
	}
}

func TestCheckWeeklyLoss(t *testing.T) {
	over := PortfolioState{WeeklyLoss: 0.15}
	if r := checkWeeklyLoss(over, 0.10); r.Passed {
		t.Fatalf("expected rejection: weekly loss 0.15 > limit 0.10")
	}
}

func TestCheckDrawdown(t *testing.T) {
	over := PortfolioState{Drawdown: 0.25}
	if r := checkDrawdown(over, 0.20); r.Passed {
		t.Fatalf("expected rejection: drawdown 0.25 > limit 0.20")
	}
}

func TestCheckConsecutiveLosses(t *testing.T) {
	over := PortfolioState{ConsecutiveLosses: 6}
	if r := checkConsecutiveLosses(over, 5); r.Passed {
		t.Fatalf("expected rejection: 6 consecutive losses > limit 5")
	}

	under := PortfolioState{ConsecutiveLosses: 3}
	if r := checkConsecutiveLosses(under, 5); !r.Passed {
		t.Fatalf("expected approval: 3 consecutive losses <= limit 5")
	}
}
```

- [ ] **Step 2: Run to confirm it fails to compile**

Run: `docker compose exec go go test ./internal/risk/... -run TestCheckDailyLoss -v`
Expected: FAIL — `undefined: checkDailyLoss`.

- [ ] **Step 3: Implement `losses.go`**

```go
// risk-engine/internal/risk/losses.go
package risk

import "fmt"

func checkDailyLoss(portfolio PortfolioState, maxDailyLoss float64) RuleResult {
	return RuleResult{
		Rule: "daily_loss", Passed: portfolio.DailyLoss <= maxDailyLoss,
		Measured: portfolio.DailyLoss, Limit: maxDailyLoss,
		Detail: fmt.Sprintf("daily loss so far: %.4f", portfolio.DailyLoss),
	}
}

func checkWeeklyLoss(portfolio PortfolioState, maxWeeklyLoss float64) RuleResult {
	return RuleResult{
		Rule: "weekly_loss", Passed: portfolio.WeeklyLoss <= maxWeeklyLoss,
		Measured: portfolio.WeeklyLoss, Limit: maxWeeklyLoss,
		Detail: fmt.Sprintf("weekly loss so far: %.4f", portfolio.WeeklyLoss),
	}
}

func checkDrawdown(portfolio PortfolioState, maxDrawdown float64) RuleResult {
	return RuleResult{
		Rule: "drawdown", Passed: portfolio.Drawdown <= maxDrawdown,
		Measured: portfolio.Drawdown, Limit: maxDrawdown,
		Detail: fmt.Sprintf("current drawdown: %.4f", portfolio.Drawdown),
	}
}

func checkConsecutiveLosses(portfolio PortfolioState, maxConsecutiveLosses int) RuleResult {
	return RuleResult{
		Rule: "consecutive_losses", Passed: portfolio.ConsecutiveLosses <= maxConsecutiveLosses,
		Measured: float64(portfolio.ConsecutiveLosses), Limit: float64(maxConsecutiveLosses),
		Detail: fmt.Sprintf("%d consecutive losses", portfolio.ConsecutiveLosses),
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `docker compose exec go go test ./internal/risk/... -v`
Expected: PASS (all tests in the package, including Task 2's).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/internal/risk/losses.go risk-engine/internal/risk/losses_test.go
git commit -m "feat: add loss limit rules"
```

---

### Task 4: Storage — connection pool + risk limits

**Files:**
- Create: `risk-engine/internal/storage/db.go`
- Create: `risk-engine/internal/storage/limits.go`
- Test: `risk-engine/internal/storage/limits_test.go`

**Interfaces:**
- Produces: `storage.New(ctx, dsn) (*Store, error)`, `(*Store) Close()`, `storage.querier` interface (satisfied by both `*pgxpool.Pool` and `pgx.Tx`), `storage.Limits{MaxPctPerAsset, MaxPctCryptoTotal, MaxValuePerTrade, MaxDailyLoss, MaxWeeklyLoss, MaxDrawdown float64, MaxConsecutiveLosses int, MaxVolatility, MinLiquidity float64, MaxDataAgeMinutes int}`, `(*Store) GetLimits(ctx) (Limits, error)`, `(*Store) SetLimits(ctx, Limits) error`. Tasks 5, 6, 7, 9 depend on `Store` and `querier`.

Tests need the real shared database from Task 1 and read `TEST_DATABASE_URL` (already set on the `go` compose service), skipping if unset.

- [ ] **Step 1: Write the failing test**

```go
// risk-engine/internal/storage/limits_test.go
package storage

import (
	"context"
	"os"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping storage tests")
	}
	s, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestGetAndSetLimits(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	original, err := s.GetLimits(ctx)
	if err != nil {
		t.Fatalf("GetLimits: %v", err)
	}
	if original.MaxPctPerAsset <= 0 {
		t.Fatalf("expected a seeded MaxPctPerAsset > 0, got %v", original.MaxPctPerAsset)
	}

	updated := original
	updated.MaxValuePerTrade = 12345
	if err := s.SetLimits(ctx, updated); err != nil {
		t.Fatalf("SetLimits: %v", err)
	}

	got, err := s.GetLimits(ctx)
	if err != nil {
		t.Fatalf("GetLimits after update: %v", err)
	}
	if got.MaxValuePerTrade != 12345 {
		t.Errorf("MaxValuePerTrade = %v, want 12345", got.MaxValuePerTrade)
	}

	// restore original so the seeded fixture isn't permanently mutated for other tests/runs
	if err := s.SetLimits(ctx, original); err != nil {
		t.Fatalf("SetLimits (restore): %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm it fails to compile**

Run: `docker compose exec go go test ./internal/storage/... -v`
Expected: FAIL — package doesn't build (`New`, `Store`, etc. undefined).

- [ ] **Step 3: Add the pgx dependency and implement `db.go`**

Run: `docker compose exec go go get github.com/jackc/pgx/v5/pgxpool@v5.6.0`

```go
// risk-engine/internal/storage/db.go
package storage

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func (s *Store) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return s.pool.Begin(ctx)
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx, so functions in
// this package can run either standalone (against the pool) or inside a
// caller-managed transaction (Evaluate, Task 9, uses this to make a pause
// transition and its decision record commit atomically).
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

- [ ] **Step 4: Implement `limits.go`**

```go
// risk-engine/internal/storage/limits.go
package storage

import "context"

type Limits struct {
	MaxPctPerAsset       float64
	MaxPctCryptoTotal    float64
	MaxValuePerTrade     float64
	MaxDailyLoss         float64
	MaxWeeklyLoss        float64
	MaxDrawdown          float64
	MaxConsecutiveLosses int
	MaxVolatility        float64
	MinLiquidity         float64
	MaxDataAgeMinutes    int
}

func (s *Store) GetLimits(ctx context.Context) (Limits, error) {
	var l Limits
	err := s.pool.QueryRow(ctx, `
		SELECT max_pct_per_asset, max_pct_crypto_total, max_value_per_trade,
		       max_daily_loss, max_weekly_loss, max_drawdown, max_consecutive_losses,
		       max_volatility, min_liquidity, max_data_age_minutes
		FROM risk_limits WHERE id = 1
	`).Scan(&l.MaxPctPerAsset, &l.MaxPctCryptoTotal, &l.MaxValuePerTrade,
		&l.MaxDailyLoss, &l.MaxWeeklyLoss, &l.MaxDrawdown, &l.MaxConsecutiveLosses,
		&l.MaxVolatility, &l.MinLiquidity, &l.MaxDataAgeMinutes)
	return l, err
}

func (s *Store) SetLimits(ctx context.Context, l Limits) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE risk_limits SET
			max_pct_per_asset = $1, max_pct_crypto_total = $2, max_value_per_trade = $3,
			max_daily_loss = $4, max_weekly_loss = $5, max_drawdown = $6, max_consecutive_losses = $7,
			max_volatility = $8, min_liquidity = $9, max_data_age_minutes = $10, updated_at = now()
		WHERE id = 1
	`, l.MaxPctPerAsset, l.MaxPctCryptoTotal, l.MaxValuePerTrade,
		l.MaxDailyLoss, l.MaxWeeklyLoss, l.MaxDrawdown, l.MaxConsecutiveLosses,
		l.MaxVolatility, l.MinLiquidity, l.MaxDataAgeMinutes)
	return err
}
```

- [ ] **Step 5: Run the tests against the real database**

Run: `docker compose exec go go test ./internal/storage/... -v`
Expected: PASS. If it reports `TEST_DATABASE_URL not set`, confirm Task 1's `go` service is the one running the tests.

- [ ] **Step 6: Commit**

```bash
git add risk-engine/go.mod risk-engine/go.sum risk-engine/internal/storage/db.go risk-engine/internal/storage/limits.go risk-engine/internal/storage/limits_test.go
git commit -m "feat: add storage layer for risk limits"
```

---

### Task 5: Storage — operational state

**Files:**
- Create: `risk-engine/internal/storage/state.go`
- Test: `risk-engine/internal/storage/state_test.go`

**Interfaces:**
- Consumes: `Store`, `querier` (Task 4).
- Produces: `storage.StatusNormal/StatusPaused/StatusKillSwitch` (string constants), `storage.State{Status, Reason string, ChangedAt time.Time}`, `(*Store) GetState(ctx) (State, error)`, `storage.SetState(ctx, db querier, status, reason string) error` (package-level, transaction-capable), `(*Store) SetState(ctx, status, reason string) error` (convenience wrapper over the pool), `(*Store) Reset(ctx, reason string) error`. Task 9 (`Evaluate`) calls the package-level `SetState` inside a transaction; Task 10 exposes `Reset` as the manual unpause entry point.

- [ ] **Step 1: Write the failing test**

```go
// risk-engine/internal/storage/state_test.go
package storage

import (
	"context"
	"testing"
)

func TestGetState_SeededAsNormal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Ensure a clean starting point regardless of what earlier test runs left behind.
	if err := s.SetState(ctx, StatusNormal, "test setup"); err != nil {
		t.Fatalf("SetState (setup): %v", err)
	}

	st, err := s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != StatusNormal {
		t.Errorf("Status = %q, want %q", st.Status, StatusNormal)
	}
}

func TestSetState_TransitionsAndPersists(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.SetState(ctx, StatusPaused, "test: daily_loss breached"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	st, err := s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != StatusPaused {
		t.Errorf("Status = %q, want %q", st.Status, StatusPaused)
	}
	if st.Reason != "test: daily_loss breached" {
		t.Errorf("Reason = %q", st.Reason)
	}

	if err := s.Reset(ctx, "test cleanup"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	st, err = s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState after reset: %v", err)
	}
	if st.Status != StatusNormal {
		t.Errorf("Status after Reset = %q, want %q", st.Status, StatusNormal)
	}
}
```

- [ ] **Step 2: Run to confirm it fails to compile**

Run: `docker compose exec go go test ./internal/storage/... -run TestGetState -v`
Expected: FAIL — `undefined: StatusNormal`.

- [ ] **Step 3: Implement `state.go`**

```go
// risk-engine/internal/storage/state.go
package storage

import (
	"context"
	"time"
)

const (
	StatusNormal     = "normal"
	StatusPaused     = "paused"
	StatusKillSwitch = "kill_switch"
)

type State struct {
	Status    string
	Reason    string
	ChangedAt time.Time
}

func (s *Store) GetState(ctx context.Context) (State, error) {
	var st State
	err := s.pool.QueryRow(ctx, `SELECT status, reason, changed_at FROM risk_state WHERE id = 1`).
		Scan(&st.Status, &st.Reason, &st.ChangedAt)
	return st, err
}

// SetState updates the operational state using db, which may be the
// Store's pool (standalone call) or a transaction, so the state change and
// a related risk_decisions row can commit or roll back together.
func SetState(ctx context.Context, db querier, status, reason string) error {
	_, err := db.Exec(ctx, `UPDATE risk_state SET status = $1, reason = $2, changed_at = now() WHERE id = 1`, status, reason)
	return err
}

func (s *Store) SetState(ctx context.Context, status, reason string) error {
	return SetState(ctx, s.pool, status, reason)
}

// Reset manually clears a paused/kill_switch state back to normal. The risk
// engine never does this on its own — it is a deliberate operator action.
func (s *Store) Reset(ctx context.Context, reason string) error {
	return s.SetState(ctx, StatusNormal, reason)
}
```

- [ ] **Step 4: Run the tests**

Run: `docker compose exec go go test ./internal/storage/... -v`
Expected: PASS (all tests in the package).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/internal/storage/state.go risk-engine/internal/storage/state_test.go
git commit -m "feat: add operational state storage (normal/paused/kill_switch)"
```

---

### Task 6: Storage — decision log

**Files:**
- Create: `risk-engine/internal/storage/decisions.go`
- Test: `risk-engine/internal/storage/decisions_test.go`

**Interfaces:**
- Consumes: `Store`, `querier` (Task 4).
- Produces: `storage.RuleResultRecord{Rule string, Passed bool, Measured, Limit float64, Detail string}` (JSON-tagged), `storage.DecisionRecord{Asset, Side string, Quantity, Value float64, Allowed bool, Reasons []string, RulesChecked []RuleResultRecord}`, `storage.RecordDecision(ctx, db querier, d DecisionRecord) error` (package-level, transaction-capable), `(*Store) RecordDecision(ctx, d DecisionRecord) error`. Task 9 (`Evaluate`) calls both the transactional and standalone forms.

- [ ] **Step 1: Write the failing test**

```go
// risk-engine/internal/storage/decisions_test.go
package storage

import (
	"context"
	"testing"
)

func TestRecordDecision(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	d := DecisionRecord{
		Asset: "BTC", Side: "buy", Quantity: 0.01, Value: 500,
		Allowed: false,
		Reasons: []string{"daily_loss: daily loss so far: 0.0800"},
		RulesChecked: []RuleResultRecord{
			{Rule: "daily_loss", Passed: false, Measured: 0.08, Limit: 0.05, Detail: "daily loss so far: 0.0800"},
			{Rule: "asset_concentration", Passed: true, Measured: 0.10, Limit: 0.20, Detail: "BTC would be 10.0% of portfolio"},
		},
	}

	if err := s.RecordDecision(ctx, d); err != nil {
		t.Fatalf("RecordDecision: %v", err)
	}

	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM risk_decisions WHERE asset = 'BTC' AND allowed = false`).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one matching row in risk_decisions")
	}
}
```

- [ ] **Step 2: Run to confirm it fails to compile**

Run: `docker compose exec go go test ./internal/storage/... -run TestRecordDecision -v`
Expected: FAIL — `undefined: DecisionRecord`.

- [ ] **Step 3: Implement `decisions.go`**

```go
// risk-engine/internal/storage/decisions.go
package storage

import (
	"context"
	"encoding/json"
	"time"
)

type RuleResultRecord struct {
	Rule     string  `json:"rule"`
	Passed   bool    `json:"passed"`
	Measured float64 `json:"measured"`
	Limit    float64 `json:"limit"`
	Detail   string  `json:"detail"`
}

type DecisionRecord struct {
	Asset        string
	Side         string
	Quantity     float64
	Value        float64
	Allowed      bool
	Reasons      []string
	RulesChecked []RuleResultRecord
}

func RecordDecision(ctx context.Context, db querier, d DecisionRecord) error {
	reasonsJSON, err := json.Marshal(d.Reasons)
	if err != nil {
		return err
	}
	rulesJSON, err := json.Marshal(d.RulesChecked)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO risk_decisions (ts, asset, side, quantity, value, allowed, reasons, rules_checked)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, time.Now().UTC(), d.Asset, d.Side, d.Quantity, d.Value, d.Allowed, reasonsJSON, rulesJSON)
	return err
}

func (s *Store) RecordDecision(ctx context.Context, d DecisionRecord) error {
	return RecordDecision(ctx, s.pool, d)
}
```

- [ ] **Step 4: Run the tests**

Run: `docker compose exec go go test ./internal/storage/... -v`
Expected: PASS (all tests in the package).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/internal/storage/decisions.go risk-engine/internal/storage/decisions_test.go
git commit -m "feat: add decision audit log storage"
```

---

### Task 7: Storage — read-only market data queries

**Files:**
- Create: `risk-engine/internal/storage/marketdata.go`
- Test: `risk-engine/internal/storage/marketdata_test.go`

**Interfaces:**
- Consumes: `Store` (Task 4); reads the `candles` table owned by the market-data-foundation sub-project (schema: `exchange, symbol, timeframe, ts, open, high, low, close, volume` — see `market-data/migrations/001_init.sql`).
- Produces: `storage.Candle{Time time.Time, Open, High, Low, Close, Volume float64}`, `(*Store) LatestCandle(ctx, exchange, symbol string) (Candle, bool, error)`, `(*Store) RecentCandles(ctx, exchange, symbol string, n int) ([]Candle, error)` (oldest first). Task 8 (`quality.go`) depends on both.

This module never writes to `candles` — these are read-only queries. Tests insert their own fixture rows under a synthetic `exchange`/`symbol` pair so they don't depend on or disturb real collected data, and clean up afterward.

- [ ] **Step 1: Write the failing test**

```go
// risk-engine/internal/storage/marketdata_test.go
package storage

import (
	"context"
	"testing"
	"time"
)

func seedCandles(t *testing.T, s *Store, exchange, symbol string, candles []Candle) {
	t.Helper()
	ctx := context.Background()
	for _, c := range candles {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
			VALUES ($1, $2, '1m', $3, $4, $5, $6, $7, $8)
			ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
			SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
			    close = EXCLUDED.close, volume = EXCLUDED.volume
		`, exchange, symbol, c.Time, c.Open, c.High, c.Low, c.Close, c.Volume)
		if err != nil {
			t.Fatalf("seedCandles insert: %v", err)
		}
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM candles WHERE exchange = $1 AND symbol = $2`, exchange, symbol)
	})
}

func TestLatestCandle(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	seedCandles(t, s, "test-exchange", "TESTCOIN", []Candle{
		{Time: now.Add(-2 * time.Minute), Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 10},
		{Time: now.Add(-1 * time.Minute), Open: 100.5, High: 102, Low: 100, Close: 101, Volume: 12},
	})

	c, found, err := s.LatestCandle(context.Background(), "test-exchange", "TESTCOIN")
	if err != nil {
		t.Fatalf("LatestCandle: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if c.Close != 101 {
		t.Errorf("Close = %v, want 101 (the most recent candle)", c.Close)
	}
}

func TestLatestCandle_NotFound(t *testing.T) {
	s := testStore(t)
	_, found, err := s.LatestCandle(context.Background(), "test-exchange", "NOSUCHASSET")
	if err != nil {
		t.Fatalf("LatestCandle: %v", err)
	}
	if found {
		t.Fatal("expected found=false for an asset with no candles")
	}
}

func TestRecentCandles_OldestFirst(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	seedCandles(t, s, "test-exchange", "TESTCOIN2", []Candle{
		{Time: now.Add(-3 * time.Minute), Close: 100, Volume: 1},
		{Time: now.Add(-2 * time.Minute), Close: 101, Volume: 2},
		{Time: now.Add(-1 * time.Minute), Close: 102, Volume: 3},
	})

	candles, err := s.RecentCandles(context.Background(), "test-exchange", "TESTCOIN2", 10)
	if err != nil {
		t.Fatalf("RecentCandles: %v", err)
	}
	if len(candles) != 3 {
		t.Fatalf("len(candles) = %d, want 3", len(candles))
	}
	if candles[0].Close != 100 || candles[2].Close != 102 {
		t.Errorf("candles not oldest-first: %+v", candles)
	}
}
```

- [ ] **Step 2: Run to confirm it fails to compile**

Run: `docker compose exec go go test ./internal/storage/... -run TestLatestCandle -v`
Expected: FAIL — `undefined: Candle`.

- [ ] **Step 3: Implement `marketdata.go`**

```go
// risk-engine/internal/storage/marketdata.go
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// LatestCandle reads the most recent 1m candle for exchange/symbol from the
// candles table owned by the market-data-foundation sub-project — this
// module only ever reads it, never writes.
func (s *Store) LatestCandle(ctx context.Context, exchange, symbol string) (Candle, bool, error) {
	var c Candle
	err := s.pool.QueryRow(ctx, `
		SELECT ts, open, high, low, close, volume FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
		ORDER BY ts DESC LIMIT 1
	`, exchange, symbol).Scan(&c.Time, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Candle{}, false, nil
		}
		return Candle{}, false, err
	}
	return c, true, nil
}

// RecentCandles returns the last n 1m candles for exchange/symbol, oldest
// first, used to compute recent volatility and liquidity.
func (s *Store) RecentCandles(ctx context.Context, exchange, symbol string, n int) ([]Candle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, open, high, low, close, volume FROM (
			SELECT ts, open, high, low, close, volume FROM candles
			WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
			ORDER BY ts DESC LIMIT $3
		) sub ORDER BY ts ASC
	`, exchange, symbol, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candles []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.Time, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume); err != nil {
			return nil, err
		}
		candles = append(candles, c)
	}
	return candles, rows.Err()
}
```

- [ ] **Step 4: Run the tests**

Run: `docker compose exec go go test ./internal/storage/... -v`
Expected: PASS (all tests in the package).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/internal/storage/marketdata.go risk-engine/internal/storage/marketdata_test.go
git commit -m "feat: add read-only market data queries"
```

---

### Task 8: Asset/data quality rules

**Files:**
- Create: `risk-engine/internal/risk/quality.go`
- Test: `risk-engine/internal/risk/quality_test.go`

**Interfaces:**
- Consumes: `storage.Candle`, `storage.Store` (via a local `marketDataReader` interface — Tasks 4/7).
- Produces: `risk.ReferenceExchange` (constant, `"binance"`), `checkDataFreshness(ctx, md marketDataReader, asset string, maxAgeMinutes int) RuleResult`, `checkVolatility(ctx, md marketDataReader, asset string, maxVolatility float64) RuleResult`, `checkLiquidity(ctx, md marketDataReader, asset string, minLiquidity float64) RuleResult`. Task 9 (`Evaluate`) calls all three with a `*storage.Store`.

This module imports `risk-engine/internal/storage` (same Go module, so `internal/` is importable across packages within it — unlike across separate modules).

- [ ] **Step 1: Write the failing test**

```go
// risk-engine/internal/risk/quality_test.go
package risk

import (
	"context"
	"testing"
	"time"

	"risk-engine/internal/storage"
)

// fakeMarketData lets quality rule tests run without a database.
type fakeMarketData struct {
	latest       storage.Candle
	latestFound  bool
	latestErr    error
	recent       []storage.Candle
	recentErr    error
}

func (f *fakeMarketData) LatestCandle(ctx context.Context, exchange, symbol string) (storage.Candle, bool, error) {
	return f.latest, f.latestFound, f.latestErr
}
func (f *fakeMarketData) RecentCandles(ctx context.Context, exchange, symbol string, n int) ([]storage.Candle, error) {
	return f.recent, f.recentErr
}

func TestCheckDataFreshness_RejectsStaleData(t *testing.T) {
	md := &fakeMarketData{
		latest:      storage.Candle{Time: time.Now().UTC().Add(-45 * time.Minute)},
		latestFound: true,
	}
	result := checkDataFreshness(context.Background(), md, "BTC", 30)
	if result.Passed {
		t.Fatal("expected rejection: candle is 45 minutes old, limit 30")
	}
}

func TestCheckDataFreshness_RejectsMissingData(t *testing.T) {
	md := &fakeMarketData{latestFound: false}
	result := checkDataFreshness(context.Background(), md, "BTC", 30)
	if result.Passed {
		t.Fatal("expected rejection when no candle data exists (fail-safe)")
	}
}

func TestCheckDataFreshness_AllowsFreshData(t *testing.T) {
	md := &fakeMarketData{
		latest:      storage.Candle{Time: time.Now().UTC().Add(-5 * time.Minute)},
		latestFound: true,
	}
	result := checkDataFreshness(context.Background(), md, "BTC", 30)
	if !result.Passed {
		t.Fatal("expected approval: candle is 5 minutes old, limit 30")
	}
}

func TestCheckVolatility_RejectsHighVolatility(t *testing.T) {
	// Alternating +10%/-9% moves produce high volatility.
	base := time.Now().UTC().Add(-10 * time.Minute)
	md := &fakeMarketData{recent: []storage.Candle{
		{Time: base, Close: 100},
		{Time: base.Add(time.Minute), Close: 110},
		{Time: base.Add(2 * time.Minute), Close: 100},
		{Time: base.Add(3 * time.Minute), Close: 110},
		{Time: base.Add(4 * time.Minute), Close: 100},
	}}
	result := checkVolatility(context.Background(), md, "BTC", 0.02)
	if result.Passed {
		t.Fatalf("expected rejection: measured volatility %.4f should exceed limit 0.02", result.Measured)
	}
}

func TestCheckVolatility_RejectsInsufficientData(t *testing.T) {
	md := &fakeMarketData{recent: []storage.Candle{{Close: 100}}}
	result := checkVolatility(context.Background(), md, "BTC", 0.5)
	if result.Passed {
		t.Fatal("expected rejection with fewer than 2 candles (fail-safe)")
	}
}

func TestCheckLiquidity_RejectsLowVolume(t *testing.T) {
	base := time.Now().UTC().Add(-2 * time.Minute)
	md := &fakeMarketData{recent: []storage.Candle{
		{Time: base, Close: 100, Volume: 1},
		{Time: base.Add(time.Minute), Close: 100, Volume: 1},
	}}
	result := checkLiquidity(context.Background(), md, "BTC", 1000000)
	if result.Passed {
		t.Fatalf("expected rejection: measured liquidity %.2f should be under limit 1000000", result.Measured)
	}
}

func TestCheckLiquidity_AllowsHighVolume(t *testing.T) {
	base := time.Now().UTC().Add(-2 * time.Minute)
	md := &fakeMarketData{recent: []storage.Candle{
		{Time: base, Close: 100, Volume: 10000},
		{Time: base.Add(time.Minute), Close: 100, Volume: 10000},
	}}
	result := checkLiquidity(context.Background(), md, "BTC", 100000)
	if !result.Passed {
		t.Fatalf("expected approval: measured liquidity %.2f should meet limit 100000", result.Measured)
	}
}
```

- [ ] **Step 2: Run to confirm it fails to compile**

Run: `docker compose exec go go test ./internal/risk/... -run TestCheckDataFreshness -v`
Expected: FAIL — `undefined: checkDataFreshness`.

- [ ] **Step 3: Implement `quality.go`**

```go
// risk-engine/internal/risk/quality.go
package risk

import (
	"context"
	"fmt"
	"math"
	"time"

	"risk-engine/internal/storage"
)

// ReferenceExchange is which exchange's market data quality rules are
// checked against. All three exchanges are collected by the
// market-data-foundation sub-project, but for a single personal risk
// check, one consistent reference source is simpler and sufficient for
// this phase.
const ReferenceExchange = "binance"

// marketDataReader is the slice of *storage.Store this file depends on, so
// quality rules are unit-testable with a fake instead of a real database.
type marketDataReader interface {
	LatestCandle(ctx context.Context, exchange, symbol string) (storage.Candle, bool, error)
	RecentCandles(ctx context.Context, exchange, symbol string, n int) ([]storage.Candle, error)
}

func checkDataFreshness(ctx context.Context, md marketDataReader, asset string, maxAgeMinutes int) RuleResult {
	candle, found, err := md.LatestCandle(ctx, ReferenceExchange, asset)
	if err != nil || !found {
		return RuleResult{Rule: "data_freshness", Passed: false, Detail: "no market data available"}
	}
	age := time.Since(candle.Time).Minutes()
	return RuleResult{
		Rule: "data_freshness", Passed: age <= float64(maxAgeMinutes),
		Measured: age, Limit: float64(maxAgeMinutes),
		Detail: fmt.Sprintf("latest candle is %.1f minutes old", age),
	}
}

func checkVolatility(ctx context.Context, md marketDataReader, asset string, maxVolatility float64) RuleResult {
	candles, err := md.RecentCandles(ctx, ReferenceExchange, asset, 60)
	if err != nil || len(candles) < 2 {
		return RuleResult{Rule: "volatility", Passed: false, Detail: "insufficient market data"}
	}
	vol := stddevReturns(candles)
	return RuleResult{
		Rule: "volatility", Passed: vol <= maxVolatility,
		Measured: vol, Limit: maxVolatility,
		Detail: fmt.Sprintf("volatility over last %d candles: %.4f", len(candles), vol),
	}
}

func stddevReturns(candles []storage.Candle) float64 {
	returns := make([]float64, 0, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		if candles[i-1].Close == 0 {
			continue
		}
		returns = append(returns, (candles[i].Close-candles[i-1].Close)/candles[i-1].Close)
	}
	if len(returns) == 0 {
		return 0
	}
	var mean float64
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	var variance float64
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
	}
	variance /= float64(len(returns))
	return math.Sqrt(variance)
}

func checkLiquidity(ctx context.Context, md marketDataReader, asset string, minLiquidity float64) RuleResult {
	candles, err := md.RecentCandles(ctx, ReferenceExchange, asset, 60)
	if err != nil || len(candles) == 0 {
		return RuleResult{Rule: "liquidity", Passed: false, Detail: "insufficient market data"}
	}
	var quoteVolume float64
	for _, c := range candles {
		quoteVolume += c.Volume * c.Close
	}
	return RuleResult{
		Rule: "liquidity", Passed: quoteVolume >= minLiquidity,
		Measured: quoteVolume, Limit: minLiquidity,
		Detail: fmt.Sprintf("quote volume over last %d candles: %.2f", len(candles), quoteVolume),
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `docker compose exec go go test ./internal/risk/... -v`
Expected: PASS (all tests in the package, including Tasks 2/3's).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/internal/risk/quality.go risk-engine/internal/risk/quality_test.go
git commit -m "feat: add asset/data quality rules"
```

---

### Task 9: Evaluate orchestrator

**Files:**
- Create: `risk-engine/internal/risk/evaluate.go`
- Test: `risk-engine/internal/risk/evaluate_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2, 3, 4, 5, 6, 7, 8 — `*storage.Store`, all `checkX` functions, `storage.SetState`/`RecordDecision` (transactional forms), `storage.StatusNormal/Paused`.
- Produces: `Evaluate(ctx context.Context, store *storage.Store, portfolio PortfolioState, proposed ProposedOperation) (Decision, error)` — the module's main entry point. Task 10's end-to-end test calls this directly.

This is the task where correctness matters most — it wires the ordering the spec requires (operational state → concentration → losses with auto-pause → quality → audit log) and needs a real database for the transactional pause path.

- [ ] **Step 1: Write the failing test**

```go
// risk-engine/internal/risk/evaluate_test.go
package risk

import (
	"context"
	"os"
	"testing"

	"risk-engine/internal/storage"
)

func testEvaluateStore(t *testing.T) *storage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping evaluate integration tests")
	}
	s, err := storage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.SetState(context.Background(), storage.StatusNormal, "test setup"); err != nil {
		t.Fatalf("reset state: %v", err)
	}
	return s
}

func TestEvaluate_RejectsWhenAlreadyPaused(t *testing.T) {
	s := testEvaluateStore(t)
	if err := s.SetState(context.Background(), storage.StatusPaused, "pre-existing pause for test"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	decision, err := Evaluate(context.Background(), s,
		PortfolioState{Cash: 10000},
		ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 100},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected rejection: system is paused")
	}
}

func TestEvaluate_AutoPausesOnLossBreach(t *testing.T) {
	s := testEvaluateStore(t)

	portfolio := PortfolioState{Cash: 10000, DailyLoss: 0.99} // certain to breach the seeded 0.05 limit
	_, err := Evaluate(context.Background(), s, portfolio,
		ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 100},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	st, err := s.GetState(context.Background())
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != storage.StatusPaused {
		t.Fatalf("Status = %q, want %q after a loss-limit breach", st.Status, storage.StatusPaused)
	}
}

func TestEvaluate_RejectsOnMissingMarketData(t *testing.T) {
	s := testEvaluateStore(t)

	decision, err := Evaluate(context.Background(), s,
		PortfolioState{Cash: 10000},
		ProposedOperation{Asset: "NOSUCHASSET_" + t.Name(), Side: SideBuy, Value: 50},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected rejection: no market data exists for this asset (fail-safe)")
	}
}
```

The three tests above are sufficient to prove the state-check short-circuit, the auto-pause transaction, and the fail-safe path without needing any seeded candle fixtures — `TestEvaluate_RejectsOnMissingMarketData` already proves the quality-rule fail-safe path against the real database (no candles exist for that asset). A fourth, fully-passing scenario with seeded fresh/liquid candles is added in Task 10's end-to-end test, where it belongs alongside the full realistic flow.

- [ ] **Step 2: Run to confirm it fails to compile**

Run: `docker compose exec go go test ./internal/risk/... -run TestEvaluate -v`
Expected: FAIL — `undefined: Evaluate`.

- [ ] **Step 3: Implement `evaluate.go`**

```go
// risk-engine/internal/risk/evaluate.go
package risk

import (
	"context"
	"fmt"

	"risk-engine/internal/storage"
)

// Evaluate is the risk engine's entry point: given the caller-supplied
// portfolio state and a proposed operation, it returns an allow/reject
// decision and records it, transitioning operational state to paused if a
// loss limit is breached.
func Evaluate(ctx context.Context, store *storage.Store, portfolio PortfolioState, proposed ProposedOperation) (Decision, error) {
	state, err := store.GetState(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("risk: get state: %w", err)
	}
	if state.Status != storage.StatusNormal {
		d := Decision{Allowed: false, Reasons: []string{fmt.Sprintf("system is %s: %s", state.Status, state.Reason)}}
		if err := store.RecordDecision(ctx, toRecord(proposed, d)); err != nil {
			return Decision{}, fmt.Errorf("risk: record decision: %w", err)
		}
		return d, nil
	}

	limits, err := store.GetLimits(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("risk: get limits: %w", err)
	}

	var results []RuleResult
	results = append(results,
		checkAssetConcentration(portfolio, proposed, limits.MaxPctPerAsset),
		checkCryptoTotalConcentration(portfolio, proposed, limits.MaxPctCryptoTotal),
		checkMaxTradeValue(proposed, limits.MaxValuePerTrade),
	)

	lossResults := []RuleResult{
		checkDailyLoss(portfolio, limits.MaxDailyLoss),
		checkWeeklyLoss(portfolio, limits.MaxWeeklyLoss),
		checkDrawdown(portfolio, limits.MaxDrawdown),
		checkConsecutiveLosses(portfolio, limits.MaxConsecutiveLosses),
	}
	results = append(results, lossResults...)

	lossViolated := ""
	for _, r := range lossResults {
		if !r.Passed {
			lossViolated = r.Rule
			break
		}
	}

	results = append(results,
		checkDataFreshness(ctx, store, proposed.Asset, limits.MaxDataAgeMinutes),
		checkVolatility(ctx, store, proposed.Asset, limits.MaxVolatility),
		checkLiquidity(ctx, store, proposed.Asset, limits.MinLiquidity),
	)

	d := Decision{Allowed: true, RulesChecked: results}
	for _, r := range results {
		if !r.Passed {
			d.Allowed = false
			d.Reasons = append(d.Reasons, fmt.Sprintf("%s: %s", r.Rule, r.Detail))
		}
	}

	if lossViolated != "" {
		tx, err := store.BeginTx(ctx)
		if err != nil {
			return Decision{}, fmt.Errorf("risk: begin tx: %w", err)
		}
		defer tx.Rollback(ctx)

		reason := fmt.Sprintf("auto-paused: %s limit breached", lossViolated)
		if err := storage.SetState(ctx, tx, storage.StatusPaused, reason); err != nil {
			return Decision{}, fmt.Errorf("risk: set state: %w", err)
		}
		if err := storage.RecordDecision(ctx, tx, toRecord(proposed, d)); err != nil {
			return Decision{}, fmt.Errorf("risk: record decision: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Decision{}, fmt.Errorf("risk: commit: %w", err)
		}
		return d, nil
	}

	if err := store.RecordDecision(ctx, toRecord(proposed, d)); err != nil {
		return Decision{}, fmt.Errorf("risk: record decision: %w", err)
	}
	return d, nil
}

func toRecord(proposed ProposedOperation, d Decision) storage.DecisionRecord {
	rules := make([]storage.RuleResultRecord, len(d.RulesChecked))
	for i, r := range d.RulesChecked {
		rules[i] = storage.RuleResultRecord{Rule: r.Rule, Passed: r.Passed, Measured: r.Measured, Limit: r.Limit, Detail: r.Detail}
	}
	return storage.DecisionRecord{
		Asset: proposed.Asset, Side: string(proposed.Side), Quantity: proposed.Quantity, Value: proposed.Value,
		Allowed: d.Allowed, Reasons: d.Reasons, RulesChecked: rules,
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `docker compose exec go go test ./internal/risk/... -v`
Expected: PASS (all three `TestEvaluate_*` tests, plus everything from Tasks 2/3/8).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/internal/risk/evaluate.go risk-engine/internal/risk/evaluate_test.go
git commit -m "feat: add Evaluate orchestrator with auto-pause on loss breach"
```

---

### Task 10: End-to-end scenario test + spec completion check

**Files:**
- Test: `risk-engine/internal/risk/evaluate_test.go` (append)

**Interfaces:**
- Consumes: `Evaluate` (Task 9), `(*storage.Store) RecentCandles`/direct pool access for seeding (Task 7).
- Produces: nothing new — this task is purely a realistic end-to-end test proving the spec's completion criteria, and is the plan's final task.

- [ ] **Step 1: Add test-support helpers to `storage`, so the end-to-end test in Step 2 has something to seed fixture data with**

```go
// risk-engine/internal/storage/testsupport.go
package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestOnlyInsertCandle and TestOnlyDeleteCandles exist so tests in other
// packages of this module (internal/risk) can seed and clean up fixture
// candle rows without duplicating raw SQL or exporting the full query
// surface. Not used by production code.
func TestOnlyInsertCandle(ctx context.Context, s *Store, exchange, symbol string, ts time.Time, open, high, low, close, volume float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
		VALUES ($1, $2, '1m', $3, $4, $5, $6, $7, $8)
		ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
		SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
		    close = EXCLUDED.close, volume = EXCLUDED.volume
	`, exchange, symbol, ts, open, high, low, close, volume)
	return err
}

func TestOnlyDeleteCandles(ctx context.Context, s *Store, exchange, symbol string) {
	s.pool.Exec(ctx, `DELETE FROM candles WHERE exchange = $1 AND symbol = $2`, exchange, symbol)
}

// QueryRowTestHelper exposes the pool's QueryRow for test-only ad-hoc
// assertions in other packages, avoiding a bespoke method for every
// one-off query a test needs.
func (s *Store) QueryRowTestHelper(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.pool.QueryRow(ctx, sql, args...)
}
```

- [ ] **Step 2: Write the end-to-end test**

Append to `risk-engine/internal/risk/evaluate_test.go`:

```go
func TestEvaluate_ApprovesHealthyOperationWithGoodMarketData(t *testing.T) {
	s := testEvaluateStore(t)
	ctx := context.Background()
	asset := "E2ECOIN"

	// Seed 10 fresh, low-volatility, high-liquidity 1m candles so every
	// quality rule passes. This test seeds its own fixture data under a
	// dedicated symbol; it doesn't depend on or disturb real collected data.
	now := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 10; i++ {
		ts := now.Add(time.Duration(i-9) * time.Minute)
		price := 100 + float64(i)*0.01
		if err := storage.TestOnlyInsertCandle(ctx, s, ReferenceExchange, asset, ts, price, price, price, price, 50000); err != nil {
			t.Fatalf("seed candle %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		storage.TestOnlyDeleteCandles(context.Background(), s, ReferenceExchange, asset)
	})

	portfolio := PortfolioState{
		Cash: 8000,
		Positions: map[string]Position{
			asset: {Asset: asset, Quantity: 1, Value: 1000},
		},
		DailyLoss:         0.01,
		WeeklyLoss:        0.02,
		Drawdown:          0.03,
		ConsecutiveLosses: 1,
	}
	proposed := ProposedOperation{Asset: asset, Side: SideBuy, Quantity: 0.5, Value: 500}

	decision, err := Evaluate(ctx, s, portfolio, proposed)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected approval, got rejection with reasons: %v", decision.Reasons)
	}
	if len(decision.RulesChecked) != 10 {
		t.Errorf("RulesChecked len = %d, want 10 (3 concentration + 4 loss + 3 quality)", len(decision.RulesChecked))
	}

	// Confirm the approval was actually logged for audit.
	var count int
	err = s.QueryRowTestHelper(ctx, `SELECT count(*) FROM risk_decisions WHERE asset = $1 AND allowed = true`, asset).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count == 0 {
		t.Fatal("expected the approved decision to be recorded in risk_decisions")
	}

	// State should remain normal after a clean approval.
	st, err := s.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st.Status != storage.StatusNormal {
		t.Errorf("Status = %q, want %q after a clean approval", st.Status, storage.StatusNormal)
	}
}
```

- [ ] **Step 3: Run the full test suite**

Run: `docker compose exec go go test -count=1 ./... -v`
Expected: PASS — every test in both `internal/risk` and `internal/storage`, including the new end-to-end scenario.

- [ ] **Step 4: Confirm the spec's completion criteria directly**

Run: `docker compose exec go go vet ./...` — expect no output.
Run: `docker compose exec go gofmt -l .` — expect no output.
Then manually walk the design spec's "Critério de conclusão desta fase" section against what Task 9/10 built:
- `Evaluate` applies all concentration/loss/quality rules — confirmed by Tasks 2/3/8's unit tests plus this task's end-to-end test.
- A loss-rule violation transitions `risk_state` to `paused` atomically — confirmed by `TestEvaluate_AutoPausesOnLossBreach` (Task 9).
- Every `Evaluate` call is recorded in `risk_decisions` — confirmed by this task's audit-log assertion and Task 9's tests.
- Missing market data results in rejection, not approval — confirmed by `TestEvaluate_RejectsOnMissingMarketData` (Task 9).
- Runs against the real shared TimescaleDB without a second database instance — confirmed by every integration test in this plan using `TEST_DATABASE_URL` pointed at the market-data-foundation stack's `timescaledb` service.

- [ ] **Step 5: Commit**

```bash
git add risk-engine/internal/storage/testsupport.go risk-engine/internal/risk/evaluate_test.go
git commit -m "test: add end-to-end approval scenario, confirm spec completion criteria"
```
