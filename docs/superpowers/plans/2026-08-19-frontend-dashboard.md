# Frontend (Dashboard de Acompanhamento) Implementation Plan

> **For agentic workers:** This plan is intended for direct/inline execution (e.g. by Codex), not `superpowers:subagent-driven-development` — follow `superpowers:executing-plans` if that skill is available, otherwise execute the tasks below in order, running each task's tests before moving to the next. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only web dashboard — a new Go module `web-api` exposing a REST API over the platform's existing Postgres tables, and a React+Vite SPA consuming it — showing strategist decisions + real execution outcomes, current risk state, analysis runs, and backtest results.

**Architecture:** `web-api` is a pure read layer: no Go imports of any other module, just raw SQL against tables that already exist (same pattern `strategist/internal/storage.LatestPrice` already uses to read `market-data`'s `candles` table). The frontend is a plain React SPA with tab-based navigation (no router library — 4 flat pages, no deep-linking need), polling each endpoint every 10s. In dev, Vite proxies `/api/*` to `web-api` on `localhost:8080`; in "production" (still local, personal use), `web-api` itself serves the built frontend's static files.

**Tech Stack:** Go 1.22 (matches every sibling module), `github.com/jackc/pgx/v5` (same version already pinned everywhere else, `v5.6.0`), Go 1.22's stdlib `net/http.ServeMux` enhanced routing (`"GET /path/{id}"` patterns + `r.PathValue("id")` — added in Go 1.22, no third-party router needed). React 18 + Vite 5 + TypeScript, no additional frontend dependencies (no router, no chart library — a ~30-line hand-rolled SVG line chart covers the one chart this phase needs).

**Spec:** `docs/superpowers/specs/2026-08-19-frontend-dashboard-design.md`

## Global Constraints

- **`web-api` never imports another module's Go code.** It only depends on `github.com/jackc/pgx/v5` and reads tables owned by `analysis`, `strategist`, `risk-engine`, `execution`, `simulation` directly via SQL — no `replace` directives to any sibling module, no risk of repeating sub-project 7's transitive-dependency surprise.
- **Read-only.** No endpoint in this phase writes to any table. No frontend action triggers `run_analysis`/`run_strategist`/`run_backtest` or modifies `risk_state`.
- **Every list endpoint accepts `?limit=N`**, defaulting to 50, clamped to a maximum of 500 (`internal/api`'s `parseLimit` helper) — never an unbounded query.
- **A missing ID on a detail endpoint (`/api/analysis-runs/{id}`, `/api/backtests/{id}`) returns HTTP 404** via a shared `storage.ErrNotFound` sentinel, never a 500 or an empty 200.
- **List-returning storage functions initialize their slice as `[]T{}`, never `var x []T`** — so "no rows" JSON-encodes as `[]`, never `null`, which the frontend can iterate over unconditionally.
- **No authentication anywhere** — personal, local-only tool, matching the rest of the repo.
- **No third-party frontend dependencies beyond `react`/`react-dom` (runtime) and `vite`/`@vitejs/plugin-react`/`typescript` (dev)** — no router, no state-management library, no chart library, no CSS framework. Use `^` semver ranges in `package.json` (idiomatic for npm, unlike this repo's exact-pin Go convention) and let `npm install` resolve current compatible versions.
- **Frontend runs directly on the host** (`npm install`/`npm run dev`/`npm run build`), no Docker container for it — unlike every Go module, there's no toolchain-version-pinning concern here, and a plain host Node setup is simpler for iterative frontend work. `web-api` stays containerized like every other module.

---

### Task 1: `web-api` module scaffold + storage layer

**Files:**
- Create: `web-api/go.mod`
- Create: `web-api/docker-compose.yml`
- Create: `web-api/internal/storage/db.go`
- Create: `web-api/internal/storage/errors.go`
- Create: `web-api/internal/storage/decisions.go`
- Create: `web-api/internal/storage/decisions_test.go`
- Create: `web-api/internal/storage/riskstate.go`
- Create: `web-api/internal/storage/riskstate_test.go`
- Create: `web-api/internal/storage/analysis.go`
- Create: `web-api/internal/storage/analysis_test.go`
- Create: `web-api/internal/storage/backtests.go`
- Create: `web-api/internal/storage/backtests_test.go`

**Interfaces:**
- Produces: `storage.Store` (`New(ctx, dsn) (*Store, error)`, `Close()`), `storage.ErrNotFound`, `storage.Decision`, `storage.RiskStateResponse`, `storage.AnalysisRun`/`AnalysisResult`/`AnalysisRunDetail`, `storage.BacktestRun`/`BacktestResults`/`BacktestTrade`/`EquityPoint`/`BacktestDetail`, and `(*Store)`'s `RecentDecisions`, `LiveRiskState`, `RecentAnalysisRuns`, `AnalysisRunDetail`, `RecentBacktests`, `BacktestDetail` methods — consumed by Task 2's `internal/api` package.

- [ ] **Step 1: Create the module scaffold**

```bash
mkdir -p web-api/internal/storage
```

`web-api/go.mod`:
```
module web-api

go 1.22

require github.com/jackc/pgx/v5 v5.6.0
```

`web-api/docker-compose.yml`:
```yaml
services:
  go:
    image: golang:1.22
    working_dir: /app
    volumes:
      - .:/app
      - ../frontend/dist:/frontend-dist
      - go-mod-cache:/go/pkg/mod
    environment:
      DATABASE_URL: postgres://marketdata:marketdata@timescaledb:5432/marketdata?sslmode=disable
      TEST_DATABASE_URL: postgres://marketdata:marketdata@timescaledb:5432/marketdata?sslmode=disable
      FRONTEND_DIST_DIR: /frontend-dist
      WEB_API_ADDR: ":8080"
    ports:
      - "8080:8080"
    networks:
      - market-data_default
    command: ["sleep", "infinity"]

networks:
  market-data_default:
    external: true

volumes:
  go-mod-cache:
```

`../frontend/dist` won't exist until Task 5 runs `npm run build` — Docker creates the bind-mount path as an empty directory if it's missing at container-creation time, and later files appearing there (after the build runs) are visible immediately since it's a live bind mount, no container recreation needed.

Bring the container up (pick a project name; if working in a worktree, make it worktree-specific — see this repo's Docker Compose naming convention): `COMPOSE_PROJECT_NAME=<name> docker compose up -d`.

- [ ] **Step 2: Write `internal/storage/db.go`**

```go
// web-api/internal/storage/db.go
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

- [ ] **Step 3: Write `internal/storage/errors.go`**

```go
// web-api/internal/storage/errors.go
package storage

import "errors"

// ErrNotFound is returned by *Detail lookups when the ID doesn't exist —
// internal/api maps it to an HTTP 404.
var ErrNotFound = errors.New("not found")
```

- [ ] **Step 4: Write `internal/storage/decisions.go`**

```go
// web-api/internal/storage/decisions.go
package storage

import (
	"context"
	"encoding/json"
	"time"
)

// Decision is one strategist_decisions row, including the real execution
// outcome fields added in sub-project 8.
type Decision struct {
	ID                      string    `json:"id"`
	AnalysisRunID           string    `json:"analysis_run_id"`
	Asset                   string    `json:"asset"`
	Side                    string    `json:"side"`
	Confidence              float64   `json:"confidence"`
	SizingPct               float64   `json:"sizing_pct"`
	Rationale               string    `json:"rationale"`
	ProposedQuantity        float64   `json:"proposed_quantity"`
	ProposedValue           float64   `json:"proposed_value"`
	RiskAllowed             *bool     `json:"risk_allowed,omitempty"`
	RiskReasons             []string  `json:"risk_reasons,omitempty"`
	ExecutionStatus         *string   `json:"execution_status,omitempty"`
	ExecutionOrderID        *string   `json:"execution_order_id,omitempty"`
	ExecutionFilledQuantity *float64  `json:"execution_filled_quantity,omitempty"`
	ExecutionFilledPrice    *float64  `json:"execution_filled_price,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
}

func (s *Store) RecentDecisions(ctx context.Context, limit int) ([]Decision, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
		       proposed_quantity, proposed_value, risk_allowed, risk_reasons,
		       execution_status, execution_order_id, execution_filled_quantity, execution_filled_price,
		       created_at
		FROM strategist_decisions
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	decisions := []Decision{}
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
```

- [ ] **Step 5: Write `internal/storage/decisions_test.go`**

```go
// web-api/internal/storage/decisions_test.go
package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRecentDecisions_ReturnsNewestFirstUpToLimit(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	runID := "test-web-api-decisions-run"
	older := time.Now().UTC().Add(-time.Hour)
	newer := time.Now().UTC()
	seedDecision(t, store, "test-web-api-decision-older", runID, older)
	seedDecision(t, store, "test-web-api-decision-newer", runID, newer)
	defer deleteDecisionForTest(t, store, "test-web-api-decision-older")
	defer deleteDecisionForTest(t, store, "test-web-api-decision-newer")

	decisions, err := store.RecentDecisions(context.Background(), 1)
	if err != nil {
		t.Fatalf("RecentDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1 (limit enforced)", len(decisions))
	}
	if decisions[0].ID != "test-web-api-decision-newer" {
		t.Errorf("decisions[0].ID = %q, want the newer one", decisions[0].ID)
	}
}

func TestRecentDecisions_NoRowsReturnsEmptySliceNotNil(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	decisions, err := store.RecentDecisions(context.Background(), 1)
	if err != nil {
		t.Fatalf("RecentDecisions: %v", err)
	}
	if decisions == nil {
		t.Error("decisions is nil, want a non-nil (possibly empty) slice so it JSON-encodes as [] not null")
	}
}

func seedDecision(t *testing.T, store *Store, id, runID string, createdAt time.Time) {
	t.Helper()
	_, err := store.pool.Exec(context.Background(), `
		INSERT INTO strategist_decisions
			(id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
			 proposed_quantity, proposed_value, risk_reasons, created_at)
		VALUES ($1, $2, 'BTC', 'buy', 0.8, 0.1, 'test', 1, 100, '[]', $3)
	`, id, runID, createdAt)
	if err != nil {
		t.Fatalf("seedDecision: %v", err)
	}
}

func deleteDecisionForTest(t *testing.T, store *Store, id string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM strategist_decisions WHERE id = $1`, id); err != nil {
		t.Errorf("cleanup decision %s: %v", id, err)
	}
}
```

- [ ] **Step 6: Write `internal/storage/riskstate.go`**

```go
// web-api/internal/storage/riskstate.go
package storage

import (
	"context"
	"time"
)

type RiskState struct {
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	ChangedAt time.Time `json:"changed_at"`
}

type RiskLimits struct {
	MaxPctPerAsset       float64 `json:"max_pct_per_asset"`
	MaxPctCryptoTotal    float64 `json:"max_pct_crypto_total"`
	MaxValuePerTrade     float64 `json:"max_value_per_trade"`
	MaxDailyLoss         float64 `json:"max_daily_loss"`
	MaxWeeklyLoss        float64 `json:"max_weekly_loss"`
	MaxDrawdown          float64 `json:"max_drawdown"`
	MaxConsecutiveLosses int     `json:"max_consecutive_losses"`
	MaxVolatility        float64 `json:"max_volatility"`
	MinLiquidity         float64 `json:"min_liquidity"`
	MaxDataAgeMinutes    int     `json:"max_data_age_minutes"`
}

type RiskStateResponse struct {
	State  RiskState  `json:"state"`
	Limits RiskLimits `json:"limits"`
}

// LiveRiskState reads the live risk_state row (run_id IS NULL — the
// unique index risk_state_live_row guarantees at most one such row)
// together with the single configured risk_limits row.
func (s *Store) LiveRiskState(ctx context.Context) (RiskStateResponse, error) {
	var resp RiskStateResponse
	err := s.pool.QueryRow(ctx, `
		SELECT status, reason, changed_at
		FROM risk_state
		WHERE run_id IS NULL
	`).Scan(&resp.State.Status, &resp.State.Reason, &resp.State.ChangedAt)
	if err != nil {
		return RiskStateResponse{}, err
	}

	err = s.pool.QueryRow(ctx, `
		SELECT max_pct_per_asset, max_pct_crypto_total, max_value_per_trade,
		       max_daily_loss, max_weekly_loss, max_drawdown, max_consecutive_losses,
		       max_volatility, min_liquidity, max_data_age_minutes
		FROM risk_limits
		WHERE id = 1
	`).Scan(&resp.Limits.MaxPctPerAsset, &resp.Limits.MaxPctCryptoTotal, &resp.Limits.MaxValuePerTrade,
		&resp.Limits.MaxDailyLoss, &resp.Limits.MaxWeeklyLoss, &resp.Limits.MaxDrawdown, &resp.Limits.MaxConsecutiveLosses,
		&resp.Limits.MaxVolatility, &resp.Limits.MinLiquidity, &resp.Limits.MaxDataAgeMinutes)
	if err != nil {
		return RiskStateResponse{}, err
	}
	return resp, nil
}
```

- [ ] **Step 7: Write `internal/storage/riskstate_test.go`**

```go
// web-api/internal/storage/riskstate_test.go
package storage

import (
	"context"
	"os"
	"testing"
)

func TestLiveRiskState_ReadsLiveRowAndLimits(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	// risk_state (id=1, run_id NULL) and risk_limits (id=1) are seeded by
	// risk-engine's own migration 001_init.sql and always present in this
	// shared database — this test only verifies the read, not the seed.
	resp, err := store.LiveRiskState(context.Background())
	if err != nil {
		t.Fatalf("LiveRiskState: %v", err)
	}
	if resp.State.Status == "" {
		t.Error("State.Status is empty, want a real status (normal/paused/kill_switch)")
	}
	if resp.Limits.MaxDailyLoss <= 0 {
		t.Errorf("Limits.MaxDailyLoss = %v, want a positive configured limit", resp.Limits.MaxDailyLoss)
	}
}
```

- [ ] **Step 8: Write `internal/storage/analysis.go`**

```go
// web-api/internal/storage/analysis.go
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AnalysisRun struct {
	ID         string     `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Timeframe  string     `json:"timeframe"`
	Status     string     `json:"status"`
	Error      *string    `json:"error,omitempty"`
}

type AnalysisResult struct {
	ID         string         `json:"id"`
	RunID      string         `json:"run_id"`
	AgentType  string         `json:"agent_type"`
	Asset      string         `json:"asset"`
	Indicators map[string]any `json:"indicators"`
	Narrative  string         `json:"narrative"`
	CreatedAt  time.Time      `json:"created_at"`
}

type AnalysisRunDetail struct {
	Run     AnalysisRun      `json:"run"`
	Results []AnalysisResult `json:"results"`
}

func (s *Store) RecentAnalysisRuns(ctx context.Context, limit int) ([]AnalysisRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, started_at, finished_at, timeframe, status, error
		FROM analysis_runs
		ORDER BY started_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []AnalysisRun{}
	for rows.Next() {
		var r AnalysisRun
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Timeframe, &r.Status, &r.Error); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) AnalysisRunDetail(ctx context.Context, id string) (AnalysisRunDetail, error) {
	var run AnalysisRun
	err := s.pool.QueryRow(ctx, `
		SELECT id, started_at, finished_at, timeframe, status, error
		FROM analysis_runs
		WHERE id = $1
	`, id).Scan(&run.ID, &run.StartedAt, &run.FinishedAt, &run.Timeframe, &run.Status, &run.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnalysisRunDetail{}, ErrNotFound
	}
	if err != nil {
		return AnalysisRunDetail{}, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, agent_type, asset, indicators, narrative, created_at
		FROM analysis_results
		WHERE run_id = $1
		ORDER BY created_at
	`, id)
	if err != nil {
		return AnalysisRunDetail{}, err
	}
	defer rows.Close()

	results := []AnalysisResult{}
	for rows.Next() {
		var r AnalysisResult
		var indicatorsRaw []byte
		if err := rows.Scan(&r.ID, &r.RunID, &r.AgentType, &r.Asset, &indicatorsRaw, &r.Narrative, &r.CreatedAt); err != nil {
			return AnalysisRunDetail{}, err
		}
		if err := json.Unmarshal(indicatorsRaw, &r.Indicators); err != nil {
			return AnalysisRunDetail{}, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return AnalysisRunDetail{}, err
	}

	return AnalysisRunDetail{Run: run, Results: results}, nil
}
```

- [ ] **Step 9: Write `internal/storage/analysis_test.go`**

```go
// web-api/internal/storage/analysis_test.go
package storage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestAnalysisRunDetail_UnknownIDReturnsErrNotFound(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	_, err = store.AnalysisRunDetail(context.Background(), "test-web-api-nonexistent-run")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAnalysisRunDetail_ReturnsRunAndResults(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	runID := "test-web-api-analysis-run"
	ctx := context.Background()
	_, err = store.pool.Exec(ctx, `
		INSERT INTO analysis_runs (id, started_at, timeframe, status)
		VALUES ($1, $2, '1h', 'completed')
	`, runID, time.Now().UTC())
	if err != nil {
		t.Fatalf("seed analysis_runs: %v", err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO analysis_results (id, run_id, agent_type, asset, indicators, narrative, created_at)
		VALUES ('test-web-api-analysis-result', $1, 'technical', 'BTC', '{"trend":"bullish"}', 'uptrend', $2)
	`, runID, time.Now().UTC())
	if err != nil {
		t.Fatalf("seed analysis_results: %v", err)
	}
	defer store.pool.Exec(ctx, `DELETE FROM analysis_results WHERE id = 'test-web-api-analysis-result'`)
	defer store.pool.Exec(ctx, `DELETE FROM analysis_runs WHERE id = $1`, runID)

	detail, err := store.AnalysisRunDetail(ctx, runID)
	if err != nil {
		t.Fatalf("AnalysisRunDetail: %v", err)
	}
	if detail.Run.ID != runID {
		t.Errorf("Run.ID = %q, want %q", detail.Run.ID, runID)
	}
	if len(detail.Results) != 1 || detail.Results[0].Narrative != "uptrend" {
		t.Errorf("Results = %+v, want one result with narrative 'uptrend'", detail.Results)
	}
	if detail.Results[0].Indicators["trend"] != "bullish" {
		t.Errorf("Results[0].Indicators[trend] = %v, want bullish", detail.Results[0].Indicators["trend"])
	}
}
```

- [ ] **Step 10: Write `internal/storage/backtests.go`**

```go
// web-api/internal/storage/backtests.go
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type BacktestResults struct {
	TotalReturnPct          float64 `json:"total_return_pct"`
	MaxDrawdownPct          float64 `json:"max_drawdown_pct"`
	SharpeRatio             float64 `json:"sharpe_ratio"`
	SortinoRatio            float64 `json:"sortino_ratio"`
	AnnualizedVolatilityPct float64 `json:"annualized_volatility_pct"`
	WinRatePct              float64 `json:"win_rate_pct"`
	TotalTrades             int     `json:"total_trades"`
	AvgTradePct             float64 `json:"avg_trade_pct"`
}

type BacktestRun struct {
	ID               string           `json:"id"`
	StrategyName     string           `json:"strategy_name"`
	PeriodStart      time.Time        `json:"period_start"`
	PeriodEnd        time.Time        `json:"period_end"`
	Timeframes       []string         `json:"timeframes"`
	DrivingTimeframe string           `json:"driving_timeframe"`
	InitialCash      float64          `json:"initial_cash"`
	FeePct           float64          `json:"fee_pct"`
	Status           string           `json:"status"`
	Error            *string          `json:"error,omitempty"`
	StartedAt        time.Time        `json:"started_at"`
	EndedAt          *time.Time       `json:"ended_at,omitempty"`
	Results          *BacktestResults `json:"results,omitempty"`
}

type BacktestTrade struct {
	Timestamp    time.Time `json:"ts"`
	Asset        string    `json:"asset"`
	Side         string    `json:"side"`
	Quantity     float64   `json:"quantity"`
	Price        float64   `json:"price"`
	Fee          float64   `json:"fee"`
	Allowed      bool      `json:"allowed"`
	RejectReason *string   `json:"reject_reason,omitempty"`
}

type EquityPoint struct {
	Timestamp      time.Time `json:"ts"`
	Cash           float64   `json:"cash"`
	PositionsValue float64   `json:"positions_value"`
	TotalEquity    float64   `json:"total_equity"`
}

type BacktestDetail struct {
	Run         BacktestRun     `json:"run"`
	Trades      []BacktestTrade `json:"trades"`
	EquityCurve []EquityPoint   `json:"equity_curve"`
}

const backtestRunSelect = `
	SELECT r.id, r.strategy_name, r.period_start, r.period_end, r.timeframes,
	       r.driving_timeframe, r.initial_cash, r.fee_pct, r.status, r.error,
	       r.started_at, r.ended_at,
	       res.total_return_pct, res.max_drawdown_pct, res.sharpe_ratio, res.sortino_ratio,
	       res.annualized_volatility_pct, res.win_rate_pct, res.total_trades, res.avg_trade_pct
	FROM backtest_runs r
	LEFT JOIN backtest_results res ON res.run_id = r.id
`

func scanBacktestRun(row pgx.Row) (BacktestRun, error) {
	var r BacktestRun
	var totalReturnPct, maxDrawdownPct, sharpeRatio, sortinoRatio, annualizedVolatilityPct, winRatePct, avgTradePct *float64
	var totalTrades *int
	err := row.Scan(&r.ID, &r.StrategyName, &r.PeriodStart, &r.PeriodEnd, &r.Timeframes,
		&r.DrivingTimeframe, &r.InitialCash, &r.FeePct, &r.Status, &r.Error,
		&r.StartedAt, &r.EndedAt,
		&totalReturnPct, &maxDrawdownPct, &sharpeRatio, &sortinoRatio,
		&annualizedVolatilityPct, &winRatePct, &totalTrades, &avgTradePct)
	if err != nil {
		return BacktestRun{}, err
	}
	if totalReturnPct != nil {
		r.Results = &BacktestResults{
			TotalReturnPct: *totalReturnPct, MaxDrawdownPct: *maxDrawdownPct,
			SharpeRatio: *sharpeRatio, SortinoRatio: *sortinoRatio,
			AnnualizedVolatilityPct: *annualizedVolatilityPct, WinRatePct: *winRatePct,
			TotalTrades: *totalTrades, AvgTradePct: *avgTradePct,
		}
	}
	return r, nil
}

func (s *Store) RecentBacktests(ctx context.Context, limit int) ([]BacktestRun, error) {
	rows, err := s.pool.Query(ctx, backtestRunSelect+`
		ORDER BY r.started_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []BacktestRun{}
	for rows.Next() {
		r, err := scanBacktestRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) BacktestDetail(ctx context.Context, id string) (BacktestDetail, error) {
	run, err := scanBacktestRun(s.pool.QueryRow(ctx, backtestRunSelect+` WHERE r.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return BacktestDetail{}, ErrNotFound
	}
	if err != nil {
		return BacktestDetail{}, err
	}

	tradeRows, err := s.pool.Query(ctx, `
		SELECT ts, asset, side, quantity, price, fee, allowed, reject_reason
		FROM backtest_trades
		WHERE run_id = $1
		ORDER BY ts
	`, id)
	if err != nil {
		return BacktestDetail{}, err
	}
	defer tradeRows.Close()

	trades := []BacktestTrade{}
	for tradeRows.Next() {
		var t BacktestTrade
		if err := tradeRows.Scan(&t.Timestamp, &t.Asset, &t.Side, &t.Quantity, &t.Price, &t.Fee, &t.Allowed, &t.RejectReason); err != nil {
			return BacktestDetail{}, err
		}
		trades = append(trades, t)
	}
	if err := tradeRows.Err(); err != nil {
		return BacktestDetail{}, err
	}

	equityRows, err := s.pool.Query(ctx, `
		SELECT ts, cash, positions_value, total_equity
		FROM backtest_equity_curve
		WHERE run_id = $1
		ORDER BY ts
	`, id)
	if err != nil {
		return BacktestDetail{}, err
	}
	defer equityRows.Close()

	equity := []EquityPoint{}
	for equityRows.Next() {
		var e EquityPoint
		if err := equityRows.Scan(&e.Timestamp, &e.Cash, &e.PositionsValue, &e.TotalEquity); err != nil {
			return BacktestDetail{}, err
		}
		equity = append(equity, e)
	}
	if err := equityRows.Err(); err != nil {
		return BacktestDetail{}, err
	}

	return BacktestDetail{Run: run, Trades: trades, EquityCurve: equity}, nil
}
```

- [ ] **Step 11: Write `internal/storage/backtests_test.go`**

```go
// web-api/internal/storage/backtests_test.go
package storage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestBacktestDetail_UnknownIDReturnsErrNotFound(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	_, err = store.BacktestDetail(context.Background(), "test-web-api-nonexistent-backtest")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestBacktestDetail_ReturnsRunTradesAndEquityCurve(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	runID := "test-web-api-backtest-run"
	started := time.Now().UTC()
	_, err = store.pool.Exec(ctx, `
		INSERT INTO backtest_runs
			(id, strategy_name, period_start, period_end, timeframes, driving_timeframe,
			 initial_cash, fee_pct, status, started_at, ended_at)
		VALUES ($1, 'test-strategy', $2, $2, ARRAY['1h'], '1h', 10000, 0.001, 'completed', $2, $2)
	`, runID, started)
	if err != nil {
		t.Fatalf("seed backtest_runs: %v", err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO backtest_results
			(run_id, total_return_pct, max_drawdown_pct, sharpe_ratio, sortino_ratio,
			 annualized_volatility_pct, win_rate_pct, total_trades, avg_trade_pct)
		VALUES ($1, 12.5, -3.2, 1.4, 1.8, 20.0, 55.0, 10, 1.25)
	`, runID)
	if err != nil {
		t.Fatalf("seed backtest_results: %v", err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO backtest_trades (run_id, ts, asset, side, quantity, price, fee, allowed)
		VALUES ($1, $2, 'BTC', 'buy', 0.1, 50000, 5, true)
	`, runID, started)
	if err != nil {
		t.Fatalf("seed backtest_trades: %v", err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO backtest_equity_curve (run_id, ts, cash, positions_value, total_equity)
		VALUES ($1, $2, 5000, 5000, 10000)
	`, runID, started)
	if err != nil {
		t.Fatalf("seed backtest_equity_curve: %v", err)
	}
	defer store.pool.Exec(ctx, `DELETE FROM backtest_equity_curve WHERE run_id = $1`, runID)
	defer store.pool.Exec(ctx, `DELETE FROM backtest_trades WHERE run_id = $1`, runID)
	defer store.pool.Exec(ctx, `DELETE FROM backtest_results WHERE run_id = $1`, runID)
	defer store.pool.Exec(ctx, `DELETE FROM backtest_runs WHERE id = $1`, runID)

	detail, err := store.BacktestDetail(ctx, runID)
	if err != nil {
		t.Fatalf("BacktestDetail: %v", err)
	}
	if detail.Run.Results == nil || detail.Run.Results.SharpeRatio != 1.4 {
		t.Errorf("Run.Results = %+v, want SharpeRatio 1.4", detail.Run.Results)
	}
	if len(detail.Trades) != 1 || detail.Trades[0].Asset != "BTC" {
		t.Errorf("Trades = %+v, want one BTC trade", detail.Trades)
	}
	if len(detail.EquityCurve) != 1 || detail.EquityCurve[0].TotalEquity != 10000 {
		t.Errorf("EquityCurve = %+v, want one point with TotalEquity 10000", detail.EquityCurve)
	}
}
```

- [ ] **Step 12: Run the tests**

Run: `docker compose exec go go mod tidy && docker compose exec go go build ./... && docker compose exec go go test ./... -v -count=1`
Expected: no build errors, all tests pass (or skip cleanly if `TEST_DATABASE_URL` isn't reachable).

- [ ] **Step 13: Commit**

```bash
git add web-api/go.mod web-api/go.sum web-api/docker-compose.yml web-api/internal/storage/
git commit -m "feat(web-api): module scaffold and read-only storage layer"
```

---

### Task 2: `web-api` HTTP handlers + `main.go`

**Files:**
- Create: `web-api/internal/api/server.go`
- Create: `web-api/internal/api/decisions.go`
- Create: `web-api/internal/api/riskstate.go`
- Create: `web-api/internal/api/analysis.go`
- Create: `web-api/internal/api/backtests.go`
- Create: `web-api/internal/api/server_test.go`
- Create: `web-api/cmd/web-api/main.go`

**Interfaces:**
- Consumes: everything Task 1's `storage` package produces.
- Produces: `api.NewServer(store dataStore, frontendDir string) http.Handler` — consumed by `cmd/web-api/main.go`.

- [ ] **Step 1: Write `internal/api/server.go`**

```go
// web-api/internal/api/server.go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"web-api/internal/storage"
)

const (
	defaultLimit = 50
	maxLimit     = 500
)

// dataStore is the subset of *storage.Store this package calls — lets
// tests substitute a fake instead of a real database.
type dataStore interface {
	RecentDecisions(ctx context.Context, limit int) ([]storage.Decision, error)
	LiveRiskState(ctx context.Context) (storage.RiskStateResponse, error)
	RecentAnalysisRuns(ctx context.Context, limit int) ([]storage.AnalysisRun, error)
	AnalysisRunDetail(ctx context.Context, id string) (storage.AnalysisRunDetail, error)
	RecentBacktests(ctx context.Context, limit int) ([]storage.BacktestRun, error)
	BacktestDetail(ctx context.Context, id string) (storage.BacktestDetail, error)
}

// NewServer builds the full HTTP handler: the /api/* routes, plus (when
// frontendDir is non-empty) a static file server for everything else —
// the built frontend, served by web-api itself in local "production" use.
func NewServer(store dataStore, frontendDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/decisions", handleDecisions(store))
	mux.HandleFunc("GET /api/risk-state", handleRiskState(store))
	mux.HandleFunc("GET /api/analysis-runs", handleAnalysisRuns(store))
	mux.HandleFunc("GET /api/analysis-runs/{id}", handleAnalysisRunDetail(store))
	mux.HandleFunc("GET /api/backtests", handleBacktests(store))
	mux.HandleFunc("GET /api/backtests/{id}", handleBacktestDetail(store))
	if frontendDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(frontendDir)))
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("web-api: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func parseLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}
```

- [ ] **Step 2: Write `internal/api/decisions.go`**

```go
// web-api/internal/api/decisions.go
package api

import (
	"log"
	"net/http"
)

func handleDecisions(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decisions, err := store.RecentDecisions(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentDecisions: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load decisions")
			return
		}
		writeJSON(w, http.StatusOK, decisions)
	}
}
```

- [ ] **Step 3: Write `internal/api/riskstate.go`**

```go
// web-api/internal/api/riskstate.go
package api

import (
	"log"
	"net/http"
)

func handleRiskState(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := store.LiveRiskState(r.Context())
		if err != nil {
			log.Printf("web-api: LiveRiskState: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load risk state")
			return
		}
		writeJSON(w, http.StatusOK, state)
	}
}
```

- [ ] **Step 4: Write `internal/api/analysis.go`**

```go
// web-api/internal/api/analysis.go
package api

import (
	"errors"
	"log"
	"net/http"

	"web-api/internal/storage"
)

func handleAnalysisRuns(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runs, err := store.RecentAnalysisRuns(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentAnalysisRuns: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load analysis runs")
			return
		}
		writeJSON(w, http.StatusOK, runs)
	}
}

func handleAnalysisRunDetail(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		detail, err := store.AnalysisRunDetail(r.Context(), id)
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "analysis run not found")
			return
		}
		if err != nil {
			log.Printf("web-api: AnalysisRunDetail: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load analysis run")
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
```

- [ ] **Step 5: Write `internal/api/backtests.go`**

```go
// web-api/internal/api/backtests.go
package api

import (
	"errors"
	"log"
	"net/http"

	"web-api/internal/storage"
)

func handleBacktests(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runs, err := store.RecentBacktests(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentBacktests: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load backtests")
			return
		}
		writeJSON(w, http.StatusOK, runs)
	}
}

func handleBacktestDetail(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		detail, err := store.BacktestDetail(r.Context(), id)
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "backtest not found")
			return
		}
		if err != nil {
			log.Printf("web-api: BacktestDetail: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load backtest")
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
```

- [ ] **Step 6: Write `internal/api/server_test.go`**

Tests the HTTP layer against a fake `dataStore` (no real database needed) — the routing, status-code mapping (200/404/500), and `?limit=` parsing are the real logic here.

```go
// web-api/internal/api/server_test.go
package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"web-api/internal/storage"
)

type fakeStore struct {
	decisions       []storage.Decision
	riskState       storage.RiskStateResponse
	analysisRuns    []storage.AnalysisRun
	analysisDetail  storage.AnalysisRunDetail
	analysisErr     error
	backtests       []storage.BacktestRun
	backtestDetail  storage.BacktestDetail
	backtestErr     error
	lastLimit       int
}

func (f *fakeStore) RecentDecisions(_ context.Context, limit int) ([]storage.Decision, error) {
	f.lastLimit = limit
	return f.decisions, nil
}
func (f *fakeStore) LiveRiskState(context.Context) (storage.RiskStateResponse, error) {
	return f.riskState, nil
}
func (f *fakeStore) RecentAnalysisRuns(_ context.Context, limit int) ([]storage.AnalysisRun, error) {
	f.lastLimit = limit
	return f.analysisRuns, nil
}
func (f *fakeStore) AnalysisRunDetail(context.Context, string) (storage.AnalysisRunDetail, error) {
	return f.analysisDetail, f.analysisErr
}
func (f *fakeStore) RecentBacktests(_ context.Context, limit int) ([]storage.BacktestRun, error) {
	f.lastLimit = limit
	return f.backtests, nil
}
func (f *fakeStore) BacktestDetail(context.Context, string) (storage.BacktestDetail, error) {
	return f.backtestDetail, f.backtestErr
}

func TestHandleDecisions_ReturnsJSONList(t *testing.T) {
	store := &fakeStore{decisions: []storage.Decision{{ID: "d1", Asset: "BTC"}}}
	server := NewServer(store, "")

	req := httptest.NewRequest(http.MethodGet, "/api/decisions", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"BTC"`) {
		t.Errorf("body = %s, want it to contain the decision", rec.Body.String())
	}
}

func TestHandleDecisions_DefaultLimitIs50(t *testing.T) {
	store := &fakeStore{}
	server := NewServer(store, "")

	req := httptest.NewRequest(http.MethodGet, "/api/decisions", nil)
	server.ServeHTTP(httptest.NewRecorder(), req)

	if store.lastLimit != defaultLimit {
		t.Errorf("lastLimit = %d, want %d", store.lastLimit, defaultLimit)
	}
}

func TestHandleDecisions_LimitIsClampedToMax(t *testing.T) {
	store := &fakeStore{}
	server := NewServer(store, "")

	req := httptest.NewRequest(http.MethodGet, "/api/decisions?limit=999999", nil)
	server.ServeHTTP(httptest.NewRecorder(), req)

	if store.lastLimit != maxLimit {
		t.Errorf("lastLimit = %d, want %d (clamped)", store.lastLimit, maxLimit)
	}
}

func TestHandleAnalysisRunDetail_NotFoundReturns404(t *testing.T) {
	store := &fakeStore{analysisErr: storage.ErrNotFound}
	server := NewServer(store, "")

	req := httptest.NewRequest(http.MethodGet, "/api/analysis-runs/does-not-exist", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleBacktestDetail_NotFoundReturns404(t *testing.T) {
	store := &fakeStore{backtestErr: storage.ErrNotFound}
	server := NewServer(store, "")

	req := httptest.NewRequest(http.MethodGet, "/api/backtests/does-not-exist", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleBacktestDetail_OtherErrorReturns500(t *testing.T) {
	store := &fakeStore{backtestErr: errors.New("db exploded")}
	server := NewServer(store, "")

	req := httptest.NewRequest(http.MethodGet, "/api/backtests/some-id", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "db exploded") {
		t.Error("response body leaks the internal error message")
	}
}
```

- [ ] **Step 7: Write `cmd/web-api/main.go`**

```go
// web-api/cmd/web-api/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"web-api/internal/api"
	"web-api/internal/storage"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := storage.New(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("connect storage: %w", err)
	}
	defer store.Close()

	frontendDir := os.Getenv("FRONTEND_DIST_DIR")
	handler := api.NewServer(store, frontendDir)

	addr := os.Getenv("WEB_API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("web-api listening on %s (frontend dir: %q)", addr, frontendDir)
	return http.ListenAndServe(addr, handler)
}
```

- [ ] **Step 8: Run the tests**

Run: `docker compose exec go go build ./... && docker compose exec go go test ./... -v -count=1`
Expected: no build errors, all tests pass (including the 6 new handler tests, no database needed for those).

- [ ] **Step 9: Commit**

```bash
git add web-api/internal/api/ web-api/cmd/
git commit -m "feat(web-api): HTTP handlers and main entrypoint"
```

---

### Task 3: Frontend scaffold + shared infrastructure

**Files:**
- Create: `frontend/package.json`
- Create: `frontend/tsconfig.json`
- Create: `frontend/tsconfig.node.json`
- Create: `frontend/vite.config.ts`
- Create: `frontend/index.html`
- Create: `frontend/src/main.tsx`
- Create: `frontend/src/index.css`
- Create: `frontend/src/api/client.ts`
- Create: `frontend/src/hooks/usePolling.ts`
- Create: `frontend/src/App.tsx`

**Interfaces:**
- Produces: the `api` client object (`decisions`, `riskState`, `analysisRuns`, `analysisRunDetail`, `backtests`, `backtestDetail`) and every TypeScript type in `api/client.ts`; the `usePolling<T>` hook — consumed by Task 4's page components, which `App.tsx` already imports.

**Note:** `App.tsx` in this task imports `./pages/DecisionsPage`, `./pages/RiskStatePage`, `./pages/AnalysisRunsPage`, `./pages/BacktestsPage` — none of which exist until Task 4. `npm run build`/`tsc` will fail until Task 4 lands; this is expected mid-plan state (the same pattern used in this repo's sub-project 8 plan, Tasks 4→5, where one task's signature change intentionally leaves a caller non-building until the next task supplies it). Only run `npm install` in this task to confirm dependencies resolve — skip the build check until Task 4.

- [ ] **Step 1: Create the scaffold**

```bash
mkdir -p frontend/src/api frontend/src/hooks frontend/src/pages frontend/src/components
```

`frontend/package.json`:
```json
{
  "name": "investment-platform-frontend",
  "private": true,
  "version": "0.0.1",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.3",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.1",
    "typescript": "^5.5.3",
    "vite": "^5.4.0"
  }
}
```

`frontend/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true
  },
  "include": ["src"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
```

`frontend/tsconfig.node.json`:
```json
{
  "compilerOptions": {
    "composite": true,
    "skipLibCheck": true,
    "module": "ESNext",
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true
  },
  "include": ["vite.config.ts"]
}
```

`frontend/vite.config.ts`:
```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
});
```

`frontend/index.html`:
```html
<!doctype html>
<html lang="pt-BR">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Investment Platform Dashboard</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 2: Write `src/api/client.ts`**

```ts
// frontend/src/api/client.ts
export interface Decision {
  id: string;
  analysis_run_id: string;
  asset: string;
  side: string;
  confidence: number;
  sizing_pct: number;
  rationale: string;
  proposed_quantity: number;
  proposed_value: number;
  risk_allowed?: boolean;
  risk_reasons?: string[];
  execution_status?: string;
  execution_order_id?: string;
  execution_filled_quantity?: number;
  execution_filled_price?: number;
  created_at: string;
}

export interface RiskState {
  status: string;
  reason: string;
  changed_at: string;
}

export interface RiskLimits {
  max_pct_per_asset: number;
  max_pct_crypto_total: number;
  max_value_per_trade: number;
  max_daily_loss: number;
  max_weekly_loss: number;
  max_drawdown: number;
  max_consecutive_losses: number;
  max_volatility: number;
  min_liquidity: number;
  max_data_age_minutes: number;
}

export interface RiskStateResponse {
  state: RiskState;
  limits: RiskLimits;
}

export interface AnalysisRun {
  id: string;
  started_at: string;
  finished_at?: string;
  timeframe: string;
  status: string;
  error?: string;
}

export interface AnalysisResult {
  id: string;
  run_id: string;
  agent_type: string;
  asset: string;
  indicators: Record<string, unknown>;
  narrative: string;
  created_at: string;
}

export interface AnalysisRunDetail {
  run: AnalysisRun;
  results: AnalysisResult[];
}

export interface BacktestResults {
  total_return_pct: number;
  max_drawdown_pct: number;
  sharpe_ratio: number;
  sortino_ratio: number;
  annualized_volatility_pct: number;
  win_rate_pct: number;
  total_trades: number;
  avg_trade_pct: number;
}

export interface BacktestRun {
  id: string;
  strategy_name: string;
  period_start: string;
  period_end: string;
  timeframes: string[];
  driving_timeframe: string;
  initial_cash: number;
  fee_pct: number;
  status: string;
  error?: string;
  started_at: string;
  ended_at?: string;
  results?: BacktestResults;
}

export interface BacktestTrade {
  ts: string;
  asset: string;
  side: string;
  quantity: number;
  price: number;
  fee: number;
  allowed: boolean;
  reject_reason?: string;
}

export interface EquityPoint {
  ts: string;
  cash: number;
  positions_value: number;
  total_equity: number;
}

export interface BacktestDetail {
  run: BacktestRun;
  trades: BacktestTrade[];
  equity_curve: EquityPoint[];
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `request failed: ${res.status}`);
  }
  return res.json();
}

export const api = {
  decisions: () => getJSON<Decision[]>('/api/decisions'),
  riskState: () => getJSON<RiskStateResponse>('/api/risk-state'),
  analysisRuns: () => getJSON<AnalysisRun[]>('/api/analysis-runs'),
  analysisRunDetail: (id: string) => getJSON<AnalysisRunDetail>(`/api/analysis-runs/${id}`),
  backtests: () => getJSON<BacktestRun[]>('/api/backtests'),
  backtestDetail: (id: string) => getJSON<BacktestDetail>(`/api/backtests/${id}`),
};
```

- [ ] **Step 3: Write `src/hooks/usePolling.ts`**

```ts
// frontend/src/hooks/usePolling.ts
import { useEffect, useState } from 'react';

const POLL_INTERVAL_MS = 10000;

export function usePolling<T>(fetcher: () => Promise<T>) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const result = await fetcher();
        if (!cancelled) {
          setData(result);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
        }
      }
    }

    load();
    const id = setInterval(load, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
    // fetcher is a stable reference from api/client.ts's `api` object literal
    // (each call re-fetches; re-running this effect on every render would
    // just restart the same interval pointlessly).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return { data, error };
}
```

- [ ] **Step 4: Write `src/index.css`**

```css
/* frontend/src/index.css */
body {
  margin: 0;
  font-family: system-ui, sans-serif;
  background: #0f172a;
  color: #e2e8f0;
}

.app {
  display: flex;
  flex-direction: column;
  height: 100vh;
}

.tabs {
  display: flex;
  gap: 4px;
  padding: 8px;
  background: #1e293b;
}

.tab {
  padding: 8px 16px;
  background: transparent;
  border: none;
  color: #94a3b8;
  cursor: pointer;
  border-radius: 4px;
  font-size: 14px;
}

.tab.active {
  background: #334155;
  color: #e2e8f0;
}

.content {
  flex: 1;
  overflow: auto;
  padding: 16px;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
}

.data-table th,
.data-table td {
  padding: 8px;
  border-bottom: 1px solid #334155;
  text-align: left;
  font-size: 13px;
}

.data-table tbody tr:hover {
  background: #1e293b;
  cursor: pointer;
}

.data-table tr.selected {
  background: #334155;
}

.error {
  color: #f87171;
}

.split-view {
  display: flex;
  gap: 16px;
}

.split-view .data-table {
  flex: 1;
}

.detail-panel {
  flex: 1;
  background: #1e293b;
  padding: 16px;
  border-radius: 4px;
  overflow: auto;
}

.result-card {
  margin-bottom: 16px;
  padding: 8px;
  background: #0f172a;
  border-radius: 4px;
}

.result-card pre {
  white-space: pre-wrap;
  font-size: 12px;
  color: #94a3b8;
}
```

- [ ] **Step 5: Write `src/main.tsx`**

```tsx
// frontend/src/main.tsx
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import './index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
```

- [ ] **Step 6: Write `src/App.tsx`**

```tsx
// frontend/src/App.tsx
import { useState } from 'react';
import DecisionsPage from './pages/DecisionsPage';
import RiskStatePage from './pages/RiskStatePage';
import AnalysisRunsPage from './pages/AnalysisRunsPage';
import BacktestsPage from './pages/BacktestsPage';

type Tab = 'decisions' | 'risk' | 'analysis' | 'backtests';

const TABS: { id: Tab; label: string }[] = [
  { id: 'decisions', label: 'Decisões' },
  { id: 'risk', label: 'Risco' },
  { id: 'analysis', label: 'Análises' },
  { id: 'backtests', label: 'Backtests' },
];

export default function App() {
  const [tab, setTab] = useState<Tab>('decisions');

  return (
    <div className="app">
      <nav className="tabs">
        {TABS.map((t) => (
          <button
            key={t.id}
            className={t.id === tab ? 'tab active' : 'tab'}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </nav>
      <main className="content">
        {tab === 'decisions' && <DecisionsPage />}
        {tab === 'risk' && <RiskStatePage />}
        {tab === 'analysis' && <AnalysisRunsPage />}
        {tab === 'backtests' && <BacktestsPage />}
      </main>
    </div>
  );
}
```

- [ ] **Step 7: Install dependencies**

```bash
cd frontend && npm install
```

Expected: dependencies resolve and install cleanly. Do NOT run `npm run build` yet — `App.tsx`'s page imports don't exist until Task 4, so a build/typecheck failure here is expected, not a bug to fix in this task.

- [ ] **Step 8: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/tsconfig.json frontend/tsconfig.node.json frontend/vite.config.ts frontend/index.html frontend/src/main.tsx frontend/src/index.css frontend/src/api/client.ts frontend/src/hooks/usePolling.ts frontend/src/App.tsx
git commit -m "feat(frontend): Vite+React scaffold, API client, polling hook, app shell"
```

---

### Task 4: Frontend pages

**Files:**
- Create: `frontend/src/pages/DecisionsPage.tsx`
- Create: `frontend/src/pages/RiskStatePage.tsx`
- Create: `frontend/src/pages/AnalysisRunsPage.tsx`
- Create: `frontend/src/pages/BacktestsPage.tsx`
- Create: `frontend/src/components/EquityCurveChart.tsx`

**Interfaces:**
- Consumes: everything Task 3 produces (`api` client, types, `usePolling`).
- Produces: the four page components `App.tsx` already imports — this task makes the frontend build cleanly for the first time in this plan.

- [ ] **Step 1: Write `src/pages/DecisionsPage.tsx`**

```tsx
// frontend/src/pages/DecisionsPage.tsx
import { api, type Decision } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export default function DecisionsPage() {
  const { data, error } = usePolling<Decision[]>(api.decisions);

  if (error) return <p className="error">Erro ao carregar decisões: {error}</p>;
  if (!data) return <p>Carregando...</p>;

  return (
    <table className="data-table">
      <thead>
        <tr>
          <th>Ativo</th>
          <th>Lado</th>
          <th>Confiança</th>
          <th>Risco</th>
          <th>Execução</th>
          <th>Criado em</th>
        </tr>
      </thead>
      <tbody>
        {data.map((d) => (
          <tr key={d.id}>
            <td>{d.asset}</td>
            <td>{d.side}</td>
            <td>{(d.confidence * 100).toFixed(0)}%</td>
            <td>
              {d.risk_allowed === undefined
                ? '—'
                : d.risk_allowed
                  ? 'aprovado'
                  : `rejeitado: ${d.risk_reasons?.join('; ') ?? ''}`}
            </td>
            <td>
              {d.execution_status
                ? `${d.execution_status} (${d.execution_filled_quantity ?? 0} @ ${d.execution_filled_price ?? 0})`
                : '—'}
            </td>
            <td>{new Date(d.created_at).toLocaleString()}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
```

- [ ] **Step 2: Write `src/pages/RiskStatePage.tsx`**

```tsx
// frontend/src/pages/RiskStatePage.tsx
import { api, type RiskStateResponse } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export default function RiskStatePage() {
  const { data, error } = usePolling<RiskStateResponse>(api.riskState);

  if (error) return <p className="error">Erro ao carregar estado de risco: {error}</p>;
  if (!data) return <p>Carregando...</p>;

  return (
    <div className="risk-state">
      <h2>Status: {data.state.status}</h2>
      <p>{data.state.reason}</p>
      <p>Atualizado em: {new Date(data.state.changed_at).toLocaleString()}</p>
      <table className="data-table">
        <thead>
          <tr>
            <th>Limite</th>
            <th>Valor configurado</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>% máx por ativo</td>
            <td>{data.limits.max_pct_per_asset}</td>
          </tr>
          <tr>
            <td>% máx total cripto</td>
            <td>{data.limits.max_pct_crypto_total}</td>
          </tr>
          <tr>
            <td>Valor máx por trade</td>
            <td>{data.limits.max_value_per_trade}</td>
          </tr>
          <tr>
            <td>Perda diária máx</td>
            <td>{data.limits.max_daily_loss}</td>
          </tr>
          <tr>
            <td>Perda semanal máx</td>
            <td>{data.limits.max_weekly_loss}</td>
          </tr>
          <tr>
            <td>Drawdown máx</td>
            <td>{data.limits.max_drawdown}</td>
          </tr>
          <tr>
            <td>Perdas consecutivas máx</td>
            <td>{data.limits.max_consecutive_losses}</td>
          </tr>
          <tr>
            <td>Volatilidade máx</td>
            <td>{data.limits.max_volatility}</td>
          </tr>
          <tr>
            <td>Liquidez mín</td>
            <td>{data.limits.min_liquidity}</td>
          </tr>
          <tr>
            <td>Idade máx do dado (min)</td>
            <td>{data.limits.max_data_age_minutes}</td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}
```

- [ ] **Step 3: Write `src/pages/AnalysisRunsPage.tsx`**

```tsx
// frontend/src/pages/AnalysisRunsPage.tsx
import { useState } from 'react';
import { api, type AnalysisRun, type AnalysisRunDetail } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export default function AnalysisRunsPage() {
  const { data, error } = usePolling<AnalysisRun[]>(api.analysisRuns);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [detail, setDetail] = useState<AnalysisRunDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);

  async function selectRun(id: string) {
    setSelectedID(id);
    setDetail(null);
    setDetailError(null);
    try {
      setDetail(await api.analysisRunDetail(id));
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : String(err));
    }
  }

  if (error) return <p className="error">Erro ao carregar runs: {error}</p>;
  if (!data) return <p>Carregando...</p>;

  return (
    <div className="split-view">
      <table className="data-table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Timeframe</th>
            <th>Status</th>
            <th>Início</th>
          </tr>
        </thead>
        <tbody>
          {data.map((run) => (
            <tr
              key={run.id}
              className={run.id === selectedID ? 'selected' : ''}
              onClick={() => selectRun(run.id)}
            >
              <td>{run.id}</td>
              <td>{run.timeframe}</td>
              <td>{run.status}</td>
              <td>{new Date(run.started_at).toLocaleString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="detail-panel">
        {detailError && <p className="error">{detailError}</p>}
        {detail && (
          <div>
            <h3>Resultados — {detail.run.id}</h3>
            {detail.results.map((r) => (
              <div key={r.id} className="result-card">
                <strong>{r.agent_type}</strong> — {r.asset}
                <p>{r.narrative}</p>
                <pre>{JSON.stringify(r.indicators, null, 2)}</pre>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Write `src/components/EquityCurveChart.tsx`**

A hand-rolled SVG line chart — no charting library dependency for one chart.

```tsx
// frontend/src/components/EquityCurveChart.tsx
import type { EquityPoint } from '../api/client';

const WIDTH = 600;
const HEIGHT = 200;
const PADDING = 20;

export default function EquityCurveChart({ points }: { points: EquityPoint[] }) {
  if (points.length === 0) return <p>Sem dados de equity.</p>;

  const values = points.map((p) => p.total_equity);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1;

  const path = points
    .map((p, i) => {
      const x = PADDING + (i / (points.length - 1 || 1)) * (WIDTH - 2 * PADDING);
      const y = HEIGHT - PADDING - ((p.total_equity - min) / range) * (HEIGHT - 2 * PADDING);
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');

  return (
    <svg width={WIDTH} height={HEIGHT} className="equity-chart">
      <path d={path} fill="none" stroke="#2563eb" strokeWidth={2} />
    </svg>
  );
}
```

- [ ] **Step 5: Write `src/pages/BacktestsPage.tsx`**

```tsx
// frontend/src/pages/BacktestsPage.tsx
import { useState } from 'react';
import { api, type BacktestRun, type BacktestDetail } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import EquityCurveChart from '../components/EquityCurveChart';

export default function BacktestsPage() {
  const { data, error } = usePolling<BacktestRun[]>(api.backtests);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [detail, setDetail] = useState<BacktestDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);

  async function selectRun(id: string) {
    setSelectedID(id);
    setDetail(null);
    setDetailError(null);
    try {
      setDetail(await api.backtestDetail(id));
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : String(err));
    }
  }

  if (error) return <p className="error">Erro ao carregar backtests: {error}</p>;
  if (!data) return <p>Carregando...</p>;

  return (
    <div className="split-view">
      <table className="data-table">
        <thead>
          <tr>
            <th>Estratégia</th>
            <th>Status</th>
            <th>Retorno</th>
            <th>Sharpe</th>
            <th>Início</th>
          </tr>
        </thead>
        <tbody>
          {data.map((run) => (
            <tr
              key={run.id}
              className={run.id === selectedID ? 'selected' : ''}
              onClick={() => selectRun(run.id)}
            >
              <td>{run.strategy_name}</td>
              <td>{run.status}</td>
              <td>{run.results ? `${run.results.total_return_pct.toFixed(2)}%` : '—'}</td>
              <td>{run.results ? run.results.sharpe_ratio.toFixed(2) : '—'}</td>
              <td>{new Date(run.started_at).toLocaleString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="detail-panel">
        {detailError && <p className="error">{detailError}</p>}
        {detail && (
          <div>
            <h3>
              {detail.run.strategy_name} — {detail.run.id}
            </h3>
            <EquityCurveChart points={detail.equity_curve} />
            <table className="data-table">
              <thead>
                <tr>
                  <th>Data</th>
                  <th>Ativo</th>
                  <th>Lado</th>
                  <th>Qtd</th>
                  <th>Preço</th>
                  <th>Permitido</th>
                </tr>
              </thead>
              <tbody>
                {detail.trades.map((t, i) => (
                  <tr key={i}>
                    <td>{new Date(t.ts).toLocaleString()}</td>
                    <td>{t.asset}</td>
                    <td>{t.side}</td>
                    <td>{t.quantity}</td>
                    <td>{t.price}</td>
                    <td>{t.allowed ? 'sim' : `não: ${t.reject_reason ?? ''}`}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Build the frontend**

```bash
cd frontend && npm run build
```

Expected: `tsc -b` and `vite build` both succeed, producing `frontend/dist/`.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/ frontend/src/components/
git commit -m "feat(frontend): decisions, risk state, analysis, and backtest pages"
```

---

### Task 5: End-to-end verification

**Files:** none created — this task verifies Tasks 1-4 work together, per the spec's completion criteria.

- [ ] **Step 1: Bring up `web-api` and confirm the API responds**

```bash
cd web-api && docker compose up -d
docker compose exec go go run ./cmd/web-api &
curl -s http://localhost:8080/api/risk-state
curl -s http://localhost:8080/api/decisions?limit=5
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/analysis-runs/does-not-exist
```

Expected: the first two return real JSON from the shared database (even if empty lists/default risk state — the tables already exist from earlier sub-projects' migrations); the third prints `404`.

- [ ] **Step 2: Confirm the frontend dev proxy works**

With `web-api` still running from Step 1:

```bash
cd frontend && npm run dev
```

Open the printed local URL (typically `http://localhost:5173`) in a browser. Expected: all four tabs load without a CORS error in the browser console (the Vite proxy forwards `/api/*` to `localhost:8080`), and the Decisões/Risco/Análises/Backtests tabs show real data (or empty states) matching what Step 1's `curl` calls returned.

- [ ] **Step 3: Confirm `web-api` serves the built frontend directly**

Stop the `npm run dev` process from Step 2. Then:

```bash
cd frontend && npm run build
```

Restart `web-api` so it picks up the now-populated bind-mounted `dist/` directory (if it was already running, `docker compose restart` inside `web-api/` is enough — the bind mount itself doesn't need remounting, only the Go process needs to still be serving with `FRONTEND_DIST_DIR` set, which `docker-compose.yml` already does).

Open `http://localhost:8080` directly (no separate frontend dev server running). Expected: the same dashboard loads, served entirely by `web-api` — confirming the "production" local-serving story from the spec's completion criteria works end to end.

- [ ] **Step 4: Stop the ad-hoc `go run` process from Step 1**

The `go run` in Step 1 was for a quick manual check; a real "production" run should go through `docker compose exec go go run ./cmd/web-api` (or a built binary) as a supervised process, not a backgrounded shell job. Kill it now that verification is done.

- [ ] **Step 5: Final commit (if any stray files need adding)**

```bash
git status
```

If everything from Tasks 1-4 is already committed and `git status` is clean, there's nothing to commit here — this task is verification-only.
