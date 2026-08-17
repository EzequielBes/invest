# Ambiente de Simulação / Backtest Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `simulation` Go module — a CLI-driven backtest engine that replays a `Strategy` against real historical candles, simulates order execution and a portfolio, calls the real `risk-engine` before every simulated operation, and persists trades/equity/metrics — plus the backward-compatible `risk-engine` extensions (`AsOf`, `RunID`) it needs to do that safely.

**Architecture:** Two-part change. (1) `risk-engine`'s `risk`/`storage` packages gain two optional parameters — `AsOf *time.Time` (so quality checks never see future candles) and `RunID *string` (so a backtest run's `risk_state`/`risk_decisions` never collide with live operation or other runs) — both `nil` by default, preserving today's live behavior exactly. (2) A new `simulation` module (own `go.mod`, `replace`-linked to `../risk-engine`) reads `market-data`'s `candles` table, drives a per-candle simulation loop (mark-to-market → apply pending fill → `Strategy.Decide` → `risk.Evaluate` → enqueue fill), and persists to four new tables of its own.

**Tech Stack:** Go 1.22, `github.com/jackc/pgx/v5` (TimescaleDB), `github.com/google/uuid` (run IDs, `simulation` only), Docker Compose dev shell (mirrors `risk-engine`'s pattern), CLI via `flag`.

**Spec:** `docs/superpowers/specs/2026-08-16-simulation-backtest-design.md`

## Global Constraints

- Go 1.22, matching `risk-engine` and `market-data`.
- `simulation/go.mod` depends on `risk-engine` via a local `replace risk-engine => ../risk-engine` (not published to any registry) — per spec.
- **IDs are TEXT, not native Postgres `UUID`.** The spec mentions `UUID` columns with `gen_random_uuid()`; this plan generates IDs in Go via `github.com/google/uuid` (`uuid.NewString()`) and stores them as `TEXT` everywhere (`backtest_runs.id`, `risk_state.run_id`, `risk_decisions.run_id`, and every `*.run_id` foreign key in `simulation`'s tables). This sidesteps pgx v5's UUID codec entirely and avoids requiring the `pgcrypto` extension on the shared TimescaleDB. Uniqueness/indexing behavior is identical to native `UUID`. `google/uuid` is a dependency of `simulation` only — `risk-engine` never generates IDs, it only stores and filters on the opaque `*string` it's given.
- **Direct signature changes, no wrapper duplication.** Every `risk-engine` function this plan touches (`GetState`, `SetState`, `SetStateIfNormal`, `Reset`, `LatestCandle`, `RecentCandles`, `RecordDecision`, `checkDataFreshness`, `checkVolatility`, `checkLiquidity`, `Evaluate`) gets its signature changed directly. There are no external consumers of `risk-engine` yet besides its own tests, so there's nothing to preserve a parallel old signature for.
- **Nil-safe filtering via `IS NOT DISTINCT FROM`.** Every SQL query that filters by an optional `run_id`/`asOf` uses Postgres's `IS NOT DISTINCT FROM` (NULL-safe equality) or a `$N::type IS NULL OR ...` guard in a single query, instead of branching Go code into two separate queries. `nil` behaves exactly as it did before this plan (the live `run_id IS NULL` row, no time cutoff).
- **Risk-free rate = 0** for Sharpe/Sortino (spec, explicit).
- **Downside deviation** = RMS of *negative* returns computed over **all** returns, not just the losing subset (spec, explicit — this is what makes Sortino's denominator correct).
- **Fill price = next driving-timeframe candle's OPEN**, never the candle that produced the signal (spec — avoids lookahead bias). Fee is `fee_pct * (quantity * fill_price)`.
- **`MarketView` always reads `risk.ReferenceExchange`** (`"binance"`, already defined in `risk-engine/risk/quality.go`). The spec's own `MarketView.Candles(ctx, tf, asset string, n int)` signature has no exchange parameter, so this plan reuses the same single reference exchange `risk-engine`'s quality checks already use, rather than inventing a second convention.
- **Go commands run via each module's own Docker dev shell**: `docker compose exec go <command>`, run from inside that module's directory (`risk-engine/` or `simulation/`), mirroring `docs/superpowers/plans/2026-08-16-risk-engine.md`'s convention.
- **Migrations applied via**: `docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/NNN_name.sql`, run from inside the module's directory — same TimescaleDB instance both modules share.
- **Regression requirement**: `risk-engine`'s existing 33 tests must keep passing, unmodified in behavior, once every call site adds a trailing `nil`/`EvalOptions{}` argument — the extension is additive only.
- Full test suite check for both modules uses `go test -p 1 -count=1 ./... -v`, `go vet ./...`, `gofmt -l .` (the `-p 1` matters for `risk-engine` per its existing shared-row test-isolation note; harmless for `simulation`, which has no comparable singleton row).

---

## Part 1 — `risk-engine` extension

### Task 1: Migration 002 — run-scoped `risk_state` / `risk_decisions`

**Files:**
- Create: `risk-engine/migrations/002_run_scoped_state.sql`

**Interfaces:**
- Produces: `risk_state.run_id TEXT NULL`, `risk_decisions.run_id TEXT NULL`; `risk_state.id` now defaults from a sequence instead of a hardcoded `1`; the `risk_state_single_row` CHECK is gone, replaced by two partial unique indexes (one live row, one row per non-null `run_id`).

- [ ] **Step 1: Write the migration**

```sql
-- risk-engine/migrations/002_run_scoped_state.sql
-- Drops the singleton constraint on risk_state so a backtest run can get
-- its own row (run_id set), while the live row (run_id IS NULL, id=1)
-- keeps behaving exactly as before this migration.
ALTER TABLE risk_state DROP CONSTRAINT IF EXISTS risk_state_single_row;

CREATE SEQUENCE IF NOT EXISTS risk_state_id_seq START WITH 2;
ALTER TABLE risk_state ALTER COLUMN id SET DEFAULT nextval('risk_state_id_seq');
ALTER SEQUENCE risk_state_id_seq OWNED BY risk_state.id;

ALTER TABLE risk_state ADD COLUMN IF NOT EXISTS run_id TEXT NULL;
ALTER TABLE risk_decisions ADD COLUMN IF NOT EXISTS run_id TEXT NULL;

-- At most one live row (run_id IS NULL) and at most one row per run_id.
CREATE UNIQUE INDEX IF NOT EXISTS risk_state_live_row ON risk_state ((1)) WHERE run_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS risk_state_run_row ON risk_state (run_id) WHERE run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS risk_decisions_run_id ON risk_decisions (run_id) WHERE run_id IS NOT NULL;
```

- [ ] **Step 2: Apply the migration**

Run (from `risk-engine/`): `docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/002_run_scoped_state.sql`
Expected: no errors; `ALTER TABLE`/`CREATE INDEX` statements print `ALTER TABLE`/`CREATE INDEX`/`CREATE SEQUENCE`.

- [ ] **Step 3: Verify the existing live row is untouched**

Run: `docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata -c "SELECT id, run_id, status FROM risk_state;"`
Expected: exactly one row, `id=1`, `run_id` is NULL, `status='normal'` (or whatever it was left at).

- [ ] **Step 4: Commit**

```bash
git add risk-engine/migrations/002_run_scoped_state.sql
git commit -m "feat(risk-engine): add run-scoped columns to risk_state and risk_decisions"
```

---

### Task 2: `storage.State` gains `runID *string`

**Files:**
- Modify: `risk-engine/storage/state.go`
- Test: `risk-engine/storage/state_test.go`

**Interfaces:**
- Consumes: Task 1's `run_id` columns.
- Produces: `(s *Store) GetState(ctx context.Context, runID *string) (State, error)`, `(s *Store) SetState(ctx context.Context, runID *string, status, reason string) error`, `SetStateIfNormal(ctx context.Context, db querier, runID *string, status, reason string) (bool, error)`, `(s *Store) Reset(ctx context.Context, runID *string, reason string) error`, `(s *Store) InitRunState(ctx context.Context, runID string) error`.

- [ ] **Step 1: Write the failing tests**

Append to `risk-engine/storage/state_test.go`:

```go
func TestInitRunState_CreatesNormalRowIsolatedFromLive(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	if err := s.InitRunState(ctx, runID); err != nil {
		t.Fatalf("InitRunState: %v", err)
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM risk_state WHERE run_id = $1`, runID)
	})

	st, err := s.GetState(ctx, &runID)
	if err != nil {
		t.Fatalf("GetState(runID): %v", err)
	}
	if st.Status != StatusNormal {
		t.Errorf("Status = %q, want %q", st.Status, StatusNormal)
	}

	live, err := s.GetState(ctx, nil)
	if err != nil {
		t.Fatalf("GetState(nil): %v", err)
	}
	if live.Status == "" {
		t.Fatal("expected the live row (run_id IS NULL) to still be readable")
	}
}

func TestSetState_RunScoped_DoesNotAffectLiveOrOtherRuns(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runA := "test-run-a-" + t.Name()
	runB := "test-run-b-" + t.Name()

	if err := s.InitRunState(ctx, runA); err != nil {
		t.Fatalf("InitRunState(A): %v", err)
	}
	if err := s.InitRunState(ctx, runB); err != nil {
		t.Fatalf("InitRunState(B): %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		s.pool.Exec(ctx, `DELETE FROM risk_state WHERE run_id IN ($1, $2)`, runA, runB)
	})

	if err := s.SetState(ctx, &runA, StatusPaused, "test: run A paused"); err != nil {
		t.Fatalf("SetState(A): %v", err)
	}

	stA, err := s.GetState(ctx, &runA)
	if err != nil {
		t.Fatalf("GetState(A): %v", err)
	}
	if stA.Status != StatusPaused {
		t.Errorf("run A Status = %q, want %q", stA.Status, StatusPaused)
	}

	stB, err := s.GetState(ctx, &runB)
	if err != nil {
		t.Fatalf("GetState(B): %v", err)
	}
	if stB.Status != StatusNormal {
		t.Errorf("run B Status = %q, want %q (must not be affected by run A)", stB.Status, StatusNormal)
	}

	live, err := s.GetState(ctx, nil)
	if err != nil {
		t.Fatalf("GetState(nil): %v", err)
	}
	if live.Status != StatusNormal {
		t.Errorf("live Status = %q, want %q (must not be affected by either run)", live.Status, StatusNormal)
	}
}
```

Also update the two existing tests' calls to pass `nil` as the new leading `runID` argument: `s.SetState(ctx, nil, StatusNormal, "test setup")`, `s.GetState(ctx, nil)`, `s.SetState(context.Background(), nil, StatusNormal, "test cleanup")`, `s.SetState(ctx, nil, StatusPaused, "test: daily_loss breached")`, `s.Reset(context.Background(), nil, "test cleanup")` — everywhere `state_test.go` currently calls these four methods.

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker compose exec go go test ./storage/... -run TestInitRunState -v`
Expected: FAIL — `InitRunState` undefined, and the pre-existing calls now fail to compile with the old (pre-`runID`) arity.

- [ ] **Step 3: Rewrite `state.go`**

```go
// risk-engine/storage/state.go
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

// GetState reads operational state for runID. nil means the live row
// (run_id IS NULL) — exactly today's behavior. A non-nil runID reads that
// backtest run's isolated row instead.
func (s *Store) GetState(ctx context.Context, runID *string) (State, error) {
	var st State
	err := s.pool.QueryRow(ctx, `
		SELECT status, reason, changed_at FROM risk_state WHERE run_id IS NOT DISTINCT FROM $1
	`, runID).Scan(&st.Status, &st.Reason, &st.ChangedAt)
	return st, err
}

// InitRunState creates a fresh 'normal' risk_state row for a backtest run,
// idempotently (a re-run with the same runID is a no-op). Every backtest
// run must call this once before its first risk.Evaluate call, since
// unlike the live row there is no pre-seeded row for a new run_id.
func (s *Store) InitRunState(ctx context.Context, runID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO risk_state (run_id, status, reason) VALUES ($1, $2, $3)
		ON CONFLICT (run_id) WHERE run_id IS NOT NULL DO NOTHING
	`, runID, StatusNormal, "run started")
	return err
}

// setState updates the operational state using db, which may be the
// Store's pool (standalone call) or a transaction, so the state change and
// a related risk_decisions row can commit or roll back together.
//
// This is deliberately package-private to prevent unguarded external use:
// an unconditional overwrite of risk_state is exactly the hazard
// SetStateIfNormal exists to guard against. SetStateIfNormal is the safe
// path for conditional writes; (*Store).SetState and (*Store).Reset are the
// intentional operator-facing manual-override paths.
func setState(ctx context.Context, db querier, runID *string, status, reason string) error {
	_, err := db.Exec(ctx, `
		UPDATE risk_state SET status = $1, reason = $2, changed_at = now()
		WHERE run_id IS NOT DISTINCT FROM $3
	`, status, reason, runID)
	return err
}

func (s *Store) SetState(ctx context.Context, runID *string, status, reason string) error {
	return setState(ctx, s.pool, runID, status, reason)
}

// SetStateIfNormal transitions status only if the current status is still
// StatusNormal, returning whether the transition was applied. This guards
// against a race where an operator (or, for a run, an earlier concurrent
// evaluation) changes state between Evaluate's initial GetState and this
// write — the engine must never silently downgrade a protective state it
// didn't set.
func SetStateIfNormal(ctx context.Context, db querier, runID *string, status, reason string) (bool, error) {
	tag, err := db.Exec(ctx, `
		UPDATE risk_state SET status = $1, reason = $2, changed_at = now()
		WHERE run_id IS NOT DISTINCT FROM $3 AND status = $4
	`, status, reason, runID, StatusNormal)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// Reset manually clears a paused/kill_switch state back to normal. The risk
// engine never does this on its own — it is a deliberate operator action.
func (s *Store) Reset(ctx context.Context, runID *string, reason string) error {
	return s.SetState(ctx, runID, StatusNormal, reason)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./storage/... -run 'TestInitRunState|TestSetState_RunScoped|TestGetState_SeededAsNormal|TestSetState_TransitionsAndPersists' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add risk-engine/storage/state.go risk-engine/storage/state_test.go
git commit -m "feat(risk-engine): scope risk_state reads/writes by optional runID"
```

---

### Task 3: `storage.RecordDecision` gains `runID *string`

**Files:**
- Modify: `risk-engine/storage/decisions.go`
- Test: `risk-engine/storage/decisions_test.go`

**Interfaces:**
- Consumes: Task 1's `risk_decisions.run_id` column.
- Produces: `RecordDecision(ctx context.Context, db querier, runID *string, d DecisionRecord) error`, `(s *Store) RecordDecision(ctx context.Context, runID *string, d DecisionRecord) error`.

- [ ] **Step 1: Write the failing test**

Append to `risk-engine/storage/decisions_test.go`:

```go
func TestRecordDecision_RunScoped(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	d := DecisionRecord{
		Asset: "ETH", Side: "buy", Quantity: 1, Value: 100,
		Allowed: true, Reasons: []string{}, RulesChecked: []RuleResultRecord{},
	}
	if err := s.RecordDecision(ctx, &runID, d); err != nil {
		t.Fatalf("RecordDecision: %v", err)
	}

	var gotRunID string
	err := s.pool.QueryRow(ctx, `
		SELECT run_id FROM risk_decisions WHERE asset = 'ETH' AND run_id = $1 ORDER BY id DESC LIMIT 1
	`, runID).Scan(&gotRunID)
	if err != nil {
		t.Fatalf("query inserted row: %v", err)
	}
	if gotRunID != runID {
		t.Errorf("run_id = %q, want %q", gotRunID, runID)
	}
}
```

Also update the existing `TestRecordDecision` call to `s.RecordDecision(ctx, nil, d)`.

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec go go test ./storage/... -run TestRecordDecision -v`
Expected: FAIL (compile error — arity mismatch).

- [ ] **Step 3: Update `decisions.go`**

```go
// risk-engine/storage/decisions.go
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

// RecordDecision writes an audit row for one Evaluate call. runID is nil
// for a live operation, or the backtest run this decision belongs to.
func RecordDecision(ctx context.Context, db querier, runID *string, d DecisionRecord) error {
	reasonsJSON, err := json.Marshal(d.Reasons)
	if err != nil {
		return err
	}
	rulesJSON, err := json.Marshal(d.RulesChecked)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO risk_decisions (ts, asset, side, quantity, value, allowed, reasons, rules_checked, run_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, time.Now().UTC(), d.Asset, d.Side, d.Quantity, d.Value, d.Allowed, reasonsJSON, rulesJSON, runID)
	return err
}

func (s *Store) RecordDecision(ctx context.Context, runID *string, d DecisionRecord) error {
	return RecordDecision(ctx, s.pool, runID, d)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./storage/... -run TestRecordDecision -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add risk-engine/storage/decisions.go risk-engine/storage/decisions_test.go
git commit -m "feat(risk-engine): scope risk_decisions writes by optional runID"
```

---

### Task 4: `storage.LatestCandle` / `RecentCandles` gain `asOf *time.Time`

**Files:**
- Modify: `risk-engine/storage/marketdata.go`
- Test: `risk-engine/storage/marketdata_test.go`

**Interfaces:**
- Produces: `(s *Store) LatestCandle(ctx context.Context, exchange, symbol string, asOf *time.Time) (Candle, bool, error)`, `(s *Store) RecentCandles(ctx context.Context, exchange, symbol string, n int, asOf *time.Time) ([]Candle, error)`.

- [ ] **Step 1: Write the failing test**

Append to `risk-engine/storage/marketdata_test.go`:

```go
func TestLatestCandle_AsOf_IgnoresFutureCandles(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	seedCandles(t, s, "test-exchange", "TESTCOIN3", []Candle{
		{Time: now.Add(-3 * time.Minute), Close: 100, Volume: 1},
		{Time: now.Add(-2 * time.Minute), Close: 101, Volume: 1},
		{Time: now, Close: 999, Volume: 1}, // "future" relative to asOf below
	})

	asOf := now.Add(-1 * time.Minute)
	c, found, err := s.LatestCandle(context.Background(), "test-exchange", "TESTCOIN3", &asOf)
	if err != nil {
		t.Fatalf("LatestCandle: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if c.Close != 101 {
		t.Errorf("Close = %v, want 101 (the -2min candle; the -3min-cutoff excludes the -1min-cutoff's own not-yet-closed candle and the future one)", c.Close)
	}
}

func TestRecentCandles_AsOf_IgnoresFutureCandles(t *testing.T) {
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	seedCandles(t, s, "test-exchange", "TESTCOIN4", []Candle{
		{Time: now.Add(-3 * time.Minute), Close: 100, Volume: 1},
		{Time: now.Add(-2 * time.Minute), Close: 101, Volume: 1},
		{Time: now.Add(-1 * time.Minute), Close: 102, Volume: 1},
		{Time: now, Close: 999, Volume: 1}, // must never be visible
	})

	asOf := now.Add(-1 * time.Minute)
	candles, err := s.RecentCandles(context.Background(), "test-exchange", "TESTCOIN4", 10, &asOf)
	if err != nil {
		t.Fatalf("RecentCandles: %v", err)
	}
	for _, c := range candles {
		if c.Close == 999 {
			t.Fatalf("RecentCandles returned a candle at or after asOf's cutoff: %+v", candles)
		}
	}
	if len(candles) != 2 {
		t.Fatalf("len(candles) = %d, want 2 (closes 100, 101 — the -1min candle's own close time equals asOf, so it's excluded too)", len(candles))
	}
}
```

Also update the three pre-existing test calls to `LatestCandle`/`RecentCandles` to pass a trailing `nil`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker compose exec go go test ./storage/... -run 'TestLatestCandle_AsOf|TestRecentCandles_AsOf' -v`
Expected: FAIL (compile error — arity mismatch).

- [ ] **Step 3: Update `marketdata.go`**

```go
// risk-engine/storage/marketdata.go
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
// module only ever reads it, never writes. asOf, if non-nil, excludes any
// candle not yet closed at that instant (ts <= asOf - 1 minute) — used by a
// backtest to prevent seeing data from its own simulated future. nil means
// no cutoff (today's live behavior).
func (s *Store) LatestCandle(ctx context.Context, exchange, symbol string, asOf *time.Time) (Candle, bool, error) {
	var c Candle
	err := s.pool.QueryRow(ctx, `
		SELECT ts, open, high, low, close, volume FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
		  AND ($3::timestamptz IS NULL OR ts <= $3::timestamptz - interval '1 minute')
		ORDER BY ts DESC LIMIT 1
	`, exchange, symbol, asOf).Scan(&c.Time, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Candle{}, false, nil
		}
		return Candle{}, false, err
	}
	return c, true, nil
}

// RecentCandles returns the last n 1m candles for exchange/symbol, oldest
// first, used to compute recent volatility and liquidity. See LatestCandle
// for asOf's semantics.
func (s *Store) RecentCandles(ctx context.Context, exchange, symbol string, n int, asOf *time.Time) ([]Candle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, open, high, low, close, volume FROM (
			SELECT ts, open, high, low, close, volume FROM candles
			WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
			  AND ($4::timestamptz IS NULL OR ts <= $4::timestamptz - interval '1 minute')
			ORDER BY ts DESC LIMIT $3
		) sub ORDER BY ts ASC
	`, exchange, symbol, n, asOf)
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./storage/... -v`
Expected: PASS (full `storage` package).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/storage/marketdata.go risk-engine/storage/marketdata_test.go
git commit -m "feat(risk-engine): add optional asOf cutoff to candle reads"
```

---

### Task 5: `risk` quality checks thread `asOf`

**Files:**
- Modify: `risk-engine/risk/quality.go`
- Test: `risk-engine/risk/quality_test.go`

**Interfaces:**
- Consumes: Task 4's `LatestCandle`/`RecentCandles` signatures.
- Produces: `checkDataFreshness(ctx, md marketDataReader, asset string, maxAgeMinutes int, asOf *time.Time) RuleResult`, `checkVolatility(ctx, md, asset string, maxVolatility float64, asOf *time.Time) RuleResult`, `checkLiquidity(ctx, md, asset string, minLiquidity float64, asOf *time.Time) RuleResult`.

- [ ] **Step 1: Write the failing test**

Append to `risk-engine/risk/quality_test.go`:

```go
func TestCheckDataFreshness_AsOf_MeasuresAgeRelativeToAsOf(t *testing.T) {
	// The candle is "ancient" relative to real wall-clock now, but fresh
	// relative to a simulated asOf a year in the past — freshness must be
	// judged against asOf, not time.Now(), or every backtest over
	// historical data would reject on data_freshness immediately.
	candleTime := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	asOf := candleTime.Add(5 * time.Minute)
	md := &fakeMarketData{
		latest:      storage.Candle{Time: candleTime},
		latestFound: true,
	}
	result := checkDataFreshness(context.Background(), md, "BTC", 30, &asOf)
	if !result.Passed {
		t.Fatalf("expected approval: candle is 5 minutes old relative to asOf, limit 30, got Detail=%q", result.Detail)
	}
}
```

Also update `fakeMarketData`'s two methods to accept and ignore the new trailing `asOf *time.Time` parameter, and add a trailing `nil` to every existing `checkDataFreshness`/`checkVolatility`/`checkLiquidity` call in this file.

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec go go test ./risk/... -run TestCheckDataFreshness_AsOf -v`
Expected: FAIL (compile error — arity mismatch, and/or the un-fixed old test would fail the assertion since `time.Since` would report ~6 years old).

- [ ] **Step 3: Update `quality.go`**

```go
// risk-engine/risk/quality.go
package risk

import (
	"context"
	"fmt"
	"math"
	"time"

	"risk-engine/storage"
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
	LatestCandle(ctx context.Context, exchange, symbol string, asOf *time.Time) (storage.Candle, bool, error)
	RecentCandles(ctx context.Context, exchange, symbol string, n int, asOf *time.Time) ([]storage.Candle, error)
}

func checkDataFreshness(ctx context.Context, md marketDataReader, asset string, maxAgeMinutes int, asOf *time.Time) RuleResult {
	candle, found, err := md.LatestCandle(ctx, ReferenceExchange, asset, asOf)
	if err != nil {
		return RuleResult{Rule: "data_freshness", Passed: false, Limit: float64(maxAgeMinutes), Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if !found {
		return RuleResult{Rule: "data_freshness", Passed: false, Limit: float64(maxAgeMinutes), Detail: "no market data available"}
	}
	reference := time.Now()
	if asOf != nil {
		reference = *asOf
	}
	age := reference.Sub(candle.Time).Minutes()
	return RuleResult{
		Rule: "data_freshness", Passed: age <= float64(maxAgeMinutes),
		Measured: age, Limit: float64(maxAgeMinutes),
		Detail: fmt.Sprintf("latest candle is %.1f minutes old", age),
	}
}

func checkVolatility(ctx context.Context, md marketDataReader, asset string, maxVolatility float64, asOf *time.Time) RuleResult {
	candles, err := md.RecentCandles(ctx, ReferenceExchange, asset, 60, asOf)
	if err != nil {
		return RuleResult{Rule: "volatility", Passed: false, Limit: maxVolatility, Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if len(candles) < 2 {
		return RuleResult{Rule: "volatility", Passed: false, Limit: maxVolatility, Detail: "insufficient market data"}
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

func checkLiquidity(ctx context.Context, md marketDataReader, asset string, minLiquidity float64, asOf *time.Time) RuleResult {
	candles, err := md.RecentCandles(ctx, ReferenceExchange, asset, 60, asOf)
	if err != nil {
		return RuleResult{Rule: "liquidity", Passed: false, Limit: minLiquidity, Detail: fmt.Sprintf("market data lookup failed: %v", err)}
	}
	if len(candles) == 0 {
		return RuleResult{Rule: "liquidity", Passed: false, Limit: minLiquidity, Detail: "insufficient market data"}
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./risk/... -run TestCheck -v`
Expected: PASS (all quality tests).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/risk/quality.go risk-engine/risk/quality_test.go
git commit -m "feat(risk-engine): quality checks measure freshness/recency relative to optional asOf"
```

---

### Task 6: `risk.Evaluate` gains `EvalOptions{AsOf, RunID}`

**Files:**
- Modify: `risk-engine/risk/evaluate.go`
- Test: `risk-engine/risk/evaluate_test.go`

**Interfaces:**
- Consumes: Tasks 2, 3, 5's signatures.
- Produces: `type EvalOptions struct { AsOf *time.Time; RunID *string }`, `Evaluate(ctx context.Context, store *storage.Store, portfolio PortfolioState, proposed ProposedOperation, opts EvalOptions) (Decision, error)`.

- [ ] **Step 1: Write the failing test**

Append to `risk-engine/risk/evaluate_test.go`:

```go
func TestEvaluate_RunScoped_PauseIsolatedFromLiveAndOtherRuns(t *testing.T) {
	s := testEvaluateStore(t)
	seeder := testEvaluateSeeder(t)
	ctx := context.Background()
	asset := "E2ECOIN_RUNSCOPE"
	runA := "test-run-a-" + t.Name()
	runB := "test-run-b-" + t.Name()

	seedFreshCandles(t, ctx, seeder, asset)
	if err := s.InitRunState(ctx, runA); err != nil {
		t.Fatalf("InitRunState(A): %v", err)
	}
	if err := s.InitRunState(ctx, runB); err != nil {
		t.Fatalf("InitRunState(B): %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		s.pool.Exec(ctx, `DELETE FROM risk_state WHERE run_id IN ($1, $2)`, runA, runB)
	})

	// Breach the loss limit inside run A only.
	portfolio := PortfolioState{Cash: 10000, DailyLoss: 0.99}
	_, err := Evaluate(ctx, s, portfolio,
		ProposedOperation{Asset: asset, Side: SideBuy, Value: 100},
		EvalOptions{RunID: &runA},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	stA, err := s.GetState(ctx, &runA)
	if err != nil {
		t.Fatalf("GetState(A): %v", err)
	}
	if stA.Status != storage.StatusPaused {
		t.Fatalf("run A Status = %q, want %q", stA.Status, storage.StatusPaused)
	}

	stB, err := s.GetState(ctx, &runB)
	if err != nil {
		t.Fatalf("GetState(B): %v", err)
	}
	if stB.Status != storage.StatusNormal {
		t.Errorf("run B Status = %q, want %q — a breach in run A must not affect run B", stB.Status, storage.StatusNormal)
	}

	live, err := s.GetState(ctx, nil)
	if err != nil {
		t.Fatalf("GetState(nil): %v", err)
	}
	if live.Status != storage.StatusNormal {
		t.Errorf("live Status = %q, want %q — a breach in a backtest run must never touch live state", live.Status, storage.StatusNormal)
	}
}

func TestEvaluate_AsOf_QualityChecksIgnoreFutureCandles(t *testing.T) {
	s := testEvaluateStore(t)
	seeder := testEvaluateSeeder(t)
	ctx := context.Background()
	asset := "E2ECOIN_ASOF"

	// Seed only a stale-relative-to-asOf candle: 45 minutes before asOf,
	// exceeding the seeded max_data_age_minutes (30).
	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Minute)
	if err := seeder.InsertCandle(ctx, ReferenceExchange, asset, past, 100, 100, 100, 100, 50000); err != nil {
		t.Fatalf("seed candle: %v", err)
	}
	t.Cleanup(func() { seeder.DeleteCandles(context.Background(), ReferenceExchange, asset) })

	asOf := past.Add(45 * time.Minute)
	decision, err := Evaluate(ctx, s, PortfolioState{Cash: 10000},
		ProposedOperation{Asset: asset, Side: SideBuy, Value: 100},
		EvalOptions{AsOf: &asOf},
	)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected rejection: only candle available is 45 minutes stale relative to asOf, limit is 30")
	}
}
```

Also add a trailing `EvalOptions{}` argument to every existing `Evaluate(...)` call in this file (the five pre-existing tests).

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker compose exec go go test ./risk/... -run 'TestEvaluate_RunScoped|TestEvaluate_AsOf' -v`
Expected: FAIL (compile error — arity mismatch).

- [ ] **Step 3: Update `evaluate.go`**

```go
// risk-engine/risk/evaluate.go
package risk

import (
	"context"
	"fmt"
	"time"

	"risk-engine/storage"
)

// EvalOptions carries the optional, backward-compatible parameters a
// simulated (backtest) caller needs and a live caller doesn't: AsOf caps
// which candles quality checks may see (nil = no cutoff, i.e. now);
// RunID isolates risk_state/risk_decisions to one backtest run (nil = the
// live row, exactly today's behavior).
type EvalOptions struct {
	AsOf  *time.Time
	RunID *string
}

// Evaluate is the risk engine's entry point: given the caller-supplied
// portfolio state and a proposed operation, it returns an allow/reject
// decision and records it, transitioning operational state to paused if a
// loss limit is breached.
func Evaluate(ctx context.Context, store *storage.Store, portfolio PortfolioState, proposed ProposedOperation, opts EvalOptions) (Decision, error) {
	state, err := store.GetState(ctx, opts.RunID)
	if err != nil {
		return Decision{}, fmt.Errorf("risk: get state: %w", err)
	}
	if state.Status != storage.StatusNormal {
		d := Decision{Allowed: false, Reasons: []string{fmt.Sprintf("system is %s: %s", state.Status, state.Reason)}}
		if err := store.RecordDecision(ctx, opts.RunID, toRecord(proposed, d)); err != nil {
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
		checkDataFreshness(ctx, store, proposed.Asset, limits.MaxDataAgeMinutes, opts.AsOf),
		checkVolatility(ctx, store, proposed.Asset, limits.MaxVolatility, opts.AsOf),
		checkLiquidity(ctx, store, proposed.Asset, limits.MinLiquidity, opts.AsOf),
	)

	d := Decision{Allowed: true, Reasons: []string{}, RulesChecked: results}
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
		defer tx.Rollback(context.WithoutCancel(ctx))

		reason := fmt.Sprintf("auto-paused: %s limit breached", lossViolated)
		if _, err := storage.SetStateIfNormal(ctx, tx, opts.RunID, storage.StatusPaused, reason); err != nil {
			return Decision{}, fmt.Errorf("risk: set state: %w", err)
		}
		// If SetStateIfNormal didn't apply, state already changed (e.g. to
		// kill_switch) since our initial read — don't downgrade it. The
		// operation is still rejected for the loss breach either way; just
		// record the decision without touching state further.
		if err := storage.RecordDecision(ctx, tx, opts.RunID, toRecord(proposed, d)); err != nil {
			return Decision{}, fmt.Errorf("risk: record decision: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Decision{}, fmt.Errorf("risk: commit: %w", err)
		}
		return d, nil
	}

	if err := store.RecordDecision(ctx, opts.RunID, toRecord(proposed, d)); err != nil {
		return Decision{}, fmt.Errorf("risk: record decision: %w", err)
	}
	return d, nil
}

func toRecord(proposed ProposedOperation, d Decision) storage.DecisionRecord {
	rules := make([]storage.RuleResultRecord, len(d.RulesChecked))
	for i, r := range d.RulesChecked {
		rules[i] = storage.RuleResultRecord{Rule: r.Rule, Passed: r.Passed, Measured: r.Measured, Limit: r.Limit, Detail: r.Detail}
	}
	reasons := d.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return storage.DecisionRecord{
		Asset: proposed.Asset, Side: string(proposed.Side), Quantity: proposed.Quantity, Value: proposed.Value,
		Allowed: d.Allowed, Reasons: reasons, RulesChecked: rules,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./risk/... -v`
Expected: PASS (full `risk` package).

- [ ] **Step 5: Commit**

```bash
git add risk-engine/risk/evaluate.go risk-engine/risk/evaluate_test.go
git commit -m "feat(risk-engine): Evaluate accepts EvalOptions{AsOf, RunID}, both optional"
```

---

### Task 7: Full `risk-engine` regression check

**Files:** none (verification only).

- [ ] **Step 1: Run the full suite with the isolation flag**

Run (from `risk-engine/`): `docker compose exec go go test -p 1 -count=1 ./... -v`
Expected: PASS, all 33+ pre-existing tests plus this plan's new ones, none skipped for a reason other than `TEST_DATABASE_URL` unset.

- [ ] **Step 2: Vet and format**

Run: `docker compose exec go go vet ./...` — expected: no output.
Run: `docker compose exec go gofmt -l .` — expected: no output (nothing unformatted).

- [ ] **Step 3: Commit (only if the above steps required fixes)**

```bash
git add risk-engine/
git commit -m "fix(risk-engine): address regression check findings"
```

If nothing needed fixing, skip this step — there's nothing to commit.

---

## Part 2 — `simulation` module

### Task 8: Scaffold the module

**Files:**
- Create: `simulation/go.mod`
- Create: `simulation/docker-compose.yml`
- Create: `simulation/migrations/001_init.sql`

**Interfaces:**
- Produces: `backtest_runs`, `backtest_trades`, `backtest_equity_curve`, `backtest_results` tables.

- [ ] **Step 1: Write `go.mod`**

```go
// simulation/go.mod
module simulation

go 1.22

require (
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
	risk-engine v0.0.0
)

replace risk-engine => ../risk-engine
```

- [ ] **Step 2: Write `docker-compose.yml`**

```yaml
# simulation/docker-compose.yml
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
    networks:
      - market-data_default
    command: ["sleep", "infinity"]

networks:
  market-data_default:
    external: true

volumes:
  go-mod-cache:
```

Note: the `replace risk-engine => ../risk-engine` in `go.mod` resolves relative to `/app` inside the container, so `../risk-engine` must resolve to `/risk-engine` — mount `../risk-engine` at `/risk-engine` (not at `/risk-engine` under a different parent) so the relative path `../risk-engine` from `/app` lands there.

- [ ] **Step 3: Write the migration**

```sql
-- simulation/migrations/001_init.sql
CREATE TABLE IF NOT EXISTS backtest_runs (
    id                TEXT PRIMARY KEY,
    strategy_name     TEXT NOT NULL,
    period_start      TIMESTAMPTZ NOT NULL,
    period_end        TIMESTAMPTZ NOT NULL,
    timeframes        TEXT[] NOT NULL,
    driving_timeframe TEXT NOT NULL,
    initial_cash      DOUBLE PRECISION NOT NULL,
    fee_pct           DOUBLE PRECISION NOT NULL,
    status            TEXT NOT NULL DEFAULT 'running',
    error             TEXT,
    started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at          TIMESTAMPTZ,
    CONSTRAINT backtest_runs_valid_status CHECK (status IN ('running', 'completed', 'failed'))
);

CREATE TABLE IF NOT EXISTS backtest_trades (
    id            BIGSERIAL PRIMARY KEY,
    run_id        TEXT NOT NULL REFERENCES backtest_runs(id),
    ts            TIMESTAMPTZ NOT NULL,
    asset         TEXT NOT NULL,
    side          TEXT NOT NULL,
    quantity      DOUBLE PRECISION NOT NULL,
    price         DOUBLE PRECISION NOT NULL,
    fee           DOUBLE PRECISION NOT NULL,
    allowed       BOOLEAN NOT NULL,
    reject_reason TEXT
);
CREATE INDEX IF NOT EXISTS backtest_trades_run_id ON backtest_trades (run_id, ts);

CREATE TABLE IF NOT EXISTS backtest_equity_curve (
    id              BIGSERIAL PRIMARY KEY,
    run_id          TEXT NOT NULL REFERENCES backtest_runs(id),
    ts              TIMESTAMPTZ NOT NULL,
    cash            DOUBLE PRECISION NOT NULL,
    positions_value DOUBLE PRECISION NOT NULL,
    total_equity    DOUBLE PRECISION NOT NULL
);
CREATE INDEX IF NOT EXISTS backtest_equity_curve_run_id ON backtest_equity_curve (run_id, ts);

CREATE TABLE IF NOT EXISTS backtest_results (
    run_id                    TEXT PRIMARY KEY REFERENCES backtest_runs(id),
    total_return_pct          DOUBLE PRECISION NOT NULL,
    max_drawdown_pct          DOUBLE PRECISION NOT NULL,
    sharpe_ratio              DOUBLE PRECISION NOT NULL,
    sortino_ratio             DOUBLE PRECISION NOT NULL,
    annualized_volatility_pct DOUBLE PRECISION NOT NULL,
    win_rate_pct              DOUBLE PRECISION NOT NULL,
    total_trades              INT NOT NULL,
    avg_trade_pct             DOUBLE PRECISION NOT NULL
);
```

- [ ] **Step 4: Bring up the dev shell and tidy modules**

Run (from `simulation/`): `docker compose up -d`
Run: `docker compose exec go go mod tidy`
Expected: `go.mod`/`go.sum` are written/updated with the full indirect-dependency set; no errors resolving the local `replace`.

- [ ] **Step 5: Apply the migration**

Run: `docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/001_init.sql`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add simulation/go.mod simulation/go.sum simulation/docker-compose.yml simulation/migrations/001_init.sql
git commit -m "feat(simulation): scaffold module, schema, and dev environment"
```

---

### Task 9: `internal/storage` — candle reads

**Files:**
- Create: `simulation/internal/storage/db.go`
- Create: `simulation/internal/storage/candles.go`
- Test: `simulation/internal/storage/candles_test.go`

**Interfaces:**
- Produces: `type Store struct{...}`, `New(ctx, dsn) (*Store, error)`, `(s *Store) Close()`, `type Candle struct{Time,Open,High,Low,Close,Volume}`, `TimeframeDuration(tf string) (time.Duration, error)`, `(s *Store) RecentCandles(ctx, exchange, symbol, timeframe string, n int, asOf time.Time) ([]Candle, error)`.

- [ ] **Step 1: Write `db.go`**

```go
// simulation/internal/storage/db.go
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

- [ ] **Step 2: Write the failing test for `TimeframeDuration`**

```go
// simulation/internal/storage/candles_test.go
package storage

import (
	"context"
	"os"
	"testing"
	"time"
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

func TestTimeframeDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"1m": time.Minute, "5m": 5 * time.Minute, "15m": 15 * time.Minute,
		"1h": time.Hour, "4h": 4 * time.Hour, "1d": 24 * time.Hour,
	}
	for tf, want := range cases {
		got, err := TimeframeDuration(tf)
		if err != nil {
			t.Errorf("TimeframeDuration(%q): %v", tf, err)
		}
		if got != want {
			t.Errorf("TimeframeDuration(%q) = %v, want %v", tf, got, want)
		}
	}
}

func TestTimeframeDuration_RejectsUnknown(t *testing.T) {
	if _, err := TimeframeDuration("3m"); err == nil {
		t.Fatal("expected an error for an uncollected timeframe")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `docker compose exec go go test ./internal/storage/... -run TestTimeframeDuration -v`
Expected: FAIL — `TimeframeDuration` undefined.

- [ ] **Step 4: Write `candles.go`**

```go
// simulation/internal/storage/candles.go
package storage

import (
	"context"
	"fmt"
	"time"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// TimeframeDuration returns the fixed wall-clock duration of one candle in
// tf, for the timeframes market-data (sub-project 1) collects.
func TimeframeDuration(tf string) (time.Duration, error) {
	switch tf {
	case "1m":
		return time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("storage: unknown timeframe %q", tf)
	}
}

// RecentCandles returns the last n candles for exchange/symbol/timeframe
// whose close time (open + duration) is <= asOf, oldest first — this is
// how the simulation guarantees it never sees a candle that hasn't closed
// yet at the current simulated instant.
func (s *Store) RecentCandles(ctx context.Context, exchange, symbol, timeframe string, n int, asOf time.Time) ([]Candle, error) {
	dur, err := TimeframeDuration(timeframe)
	if err != nil {
		return nil, err
	}
	cutoff := asOf.Add(-dur)
	rows, err := s.pool.Query(ctx, `
		SELECT ts, open, high, low, close, volume FROM (
			SELECT ts, open, high, low, close, volume FROM candles
			WHERE exchange = $1 AND symbol = $2 AND timeframe = $3 AND ts <= $4
			ORDER BY ts DESC LIMIT $5
		) sub ORDER BY ts ASC
	`, exchange, symbol, timeframe, cutoff, n)
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

- [ ] **Step 5: Add the non-lookahead integration test**

Append to `candles_test.go`:

```go
func seedCandles(t *testing.T, s *Store, exchange, symbol, timeframe string, candles []Candle) {
	t.Helper()
	ctx := context.Background()
	for _, c := range candles {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
			SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
			    close = EXCLUDED.close, volume = EXCLUDED.volume
		`, exchange, symbol, timeframe, c.Time, c.Open, c.High, c.Low, c.Close, c.Volume)
		if err != nil {
			t.Fatalf("seedCandles insert: %v", err)
		}
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), `DELETE FROM candles WHERE exchange = $1 AND symbol = $2 AND timeframe = $3`, exchange, symbol, timeframe)
	})
}

func TestRecentCandles_ExcludesNotYetClosedAndFutureCandles(t *testing.T) {
	s := testStore(t)
	base := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	seedCandles(t, s, "test-exchange", "SIMCOIN", "1h", []Candle{
		{Time: base, Close: 100},
		{Time: base.Add(time.Hour), Close: 101},
		{Time: base.Add(2 * time.Hour), Close: 102}, // not yet closed at asOf below
		{Time: base.Add(3 * time.Hour), Close: 999}, // future
	})

	asOf := base.Add(2 * time.Hour) // the [base+1h, base+2h) candle just closed
	candles, err := s.RecentCandles(context.Background(), "test-exchange", "SIMCOIN", "1h", 10, asOf)
	if err != nil {
		t.Fatalf("RecentCandles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len(candles) = %d, want 2, got %+v", len(candles), candles)
	}
	if candles[len(candles)-1].Close != 101 {
		t.Errorf("most recent visible close = %v, want 101 (the 102 and 999 candles haven't closed yet at asOf)", candles[len(candles)-1].Close)
	}
}
```

- [ ] **Step 6: Run all tests to verify they pass**

Run: `docker compose exec go go test ./internal/storage/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add simulation/internal/storage/db.go simulation/internal/storage/candles.go simulation/internal/storage/candles_test.go
git commit -m "feat(simulation): read candles from market-data with an asOf cutoff"
```

---

### Task 10: `internal/metrics`

**Files:**
- Create: `simulation/internal/metrics/metrics.go`
- Test: `simulation/internal/metrics/metrics_test.go`

**Interfaces:**
- Produces: `type Results struct{TotalReturnPct, MaxDrawdownPct, SharpeRatio, SortinoRatio, AnnualizedVolatilityPct, WinRatePct float64; TotalTrades int; AvgTradePct float64}`, `Compute(equity []float64, tradeReturnsPct []float64, periodsPerYear float64) Results`.

- [ ] **Step 1: Write the failing tests**

```go
// simulation/internal/metrics/metrics_test.go
package metrics

import "testing"

func approxEqual(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestCompute_TotalReturnAndDrawdown(t *testing.T) {
	// Equity rises to a peak of 11000, drops to 9900 (drawdown from peak),
	// ends at 10500.
	equity := []float64{10000, 11000, 9900, 10500}
	r := Compute(equity, nil, 365)

	wantReturn := (10500.0 - 10000.0) / 10000.0 * 100
	if !approxEqual(r.TotalReturnPct, wantReturn, 1e-9) {
		t.Errorf("TotalReturnPct = %v, want %v", r.TotalReturnPct, wantReturn)
	}

	wantDD := (11000.0 - 9900.0) / 11000.0 * 100
	if !approxEqual(r.MaxDrawdownPct, wantDD, 1e-9) {
		t.Errorf("MaxDrawdownPct = %v, want %v", r.MaxDrawdownPct, wantDD)
	}
}

func TestCompute_WinRateAndAvgTrade(t *testing.T) {
	trades := []float64{5, -2, 3, -1} // 2 of 4 positive
	r := Compute([]float64{10000, 10000}, trades, 365)

	if r.TotalTrades != 4 {
		t.Errorf("TotalTrades = %d, want 4", r.TotalTrades)
	}
	if !approxEqual(r.WinRatePct, 50, 1e-9) {
		t.Errorf("WinRatePct = %v, want 50", r.WinRatePct)
	}
	wantAvg := (5.0 - 2 + 3 - 1) / 4
	if !approxEqual(r.AvgTradePct, wantAvg, 1e-9) {
		t.Errorf("AvgTradePct = %v, want %v", r.AvgTradePct, wantAvg)
	}
}

func TestCompute_SharpeAndSortino_KnownSeries(t *testing.T) {
	// Equity returns: +1%, -1%, +1%, -1% — mean return 0, so Sharpe and
	// Sortino are both exactly 0 regardless of volatility.
	equity := []float64{100, 101, 99.99, 100.99, 99.98}
	r := Compute(equity, nil, 365)
	if !approxEqual(r.SharpeRatio, 0, 1e-6) {
		t.Errorf("SharpeRatio = %v, want ~0 (mean return is ~0)", r.SharpeRatio)
	}
	if !approxEqual(r.SortinoRatio, 0, 1e-6) {
		t.Errorf("SortinoRatio = %v, want ~0 (mean return is ~0)", r.SortinoRatio)
	}
}

func TestCompute_Sortino_OnlyPenalizesDownside(t *testing.T) {
	// Series A: alternating +2%/-2% (symmetric). Series B: same magnitude
	// of downside moves but the upside moves are much larger — Sortino
	// should be higher for B than A even though both have the same
	// downside deviation, because Sortino's numerator (mean return) is
	// larger while the denominator (downside deviation) is identical.
	equityA := []float64{100, 102, 99.96, 101.96, 99.92}
	equityB := []float64{100, 110, 107.8, 118.58, 116.21}
	a := Compute(equityA, nil, 365)
	b := Compute(equityB, nil, 365)
	if !(b.SortinoRatio > a.SortinoRatio) {
		t.Errorf("expected series B's Sortino (%v) > series A's (%v) — same downside shape, larger mean return", b.SortinoRatio, a.SortinoRatio)
	}
}

func TestCompute_EmptyInputs_NoDivideByZero(t *testing.T) {
	r := Compute(nil, nil, 365)
	if r.TotalReturnPct != 0 || r.SharpeRatio != 0 || r.SortinoRatio != 0 || r.WinRatePct != 0 {
		t.Errorf("expected all-zero Results for empty input, got %+v", r)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker compose exec go go test ./internal/metrics/... -v`
Expected: FAIL — `Compute` undefined.

- [ ] **Step 3: Write `metrics.go`**

```go
// simulation/internal/metrics/metrics.go
package metrics

import "math"

type Results struct {
	TotalReturnPct          float64
	MaxDrawdownPct          float64
	SharpeRatio             float64
	SortinoRatio            float64
	AnnualizedVolatilityPct float64
	WinRatePct              float64
	TotalTrades             int
	AvgTradePct             float64
}

// Compute derives final backtest metrics. equity is the total-equity value
// recorded at each driving-timeframe candle, chronological order.
// tradeReturnsPct is the realized P&L percentage of each closed, allowed
// trade. periodsPerYear is how many driving-timeframe candles occur in one
// year (e.g. 365*24 for 1h candles) — the caller computes this from the
// driving timeframe's duration. Risk-free rate is assumed zero.
func Compute(equity []float64, tradeReturnsPct []float64, periodsPerYear float64) Results {
	var r Results
	r.TotalTrades = len(tradeReturnsPct)

	if len(equity) >= 2 && equity[0] != 0 {
		r.TotalReturnPct = (equity[len(equity)-1] - equity[0]) / equity[0] * 100
	}
	r.MaxDrawdownPct = maxDrawdownPct(equity) * 100

	if r.TotalTrades > 0 {
		var wins int
		var sum float64
		for _, p := range tradeReturnsPct {
			if p > 0 {
				wins++
			}
			sum += p
		}
		r.WinRatePct = float64(wins) / float64(r.TotalTrades) * 100
		r.AvgTradePct = sum / float64(r.TotalTrades)
	}

	returns := periodReturns(equity)
	mean, stddev := meanStddev(returns)
	annualizer := math.Sqrt(periodsPerYear)
	r.AnnualizedVolatilityPct = stddev * annualizer * 100
	if stddev > 0 {
		r.SharpeRatio = mean / stddev * annualizer
	}
	if downside := downsideDeviation(returns); downside > 0 {
		r.SortinoRatio = mean / downside * annualizer
	}
	return r
}

func periodReturns(equity []float64) []float64 {
	if len(equity) < 2 {
		return nil
	}
	returns := make([]float64, 0, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1] == 0 {
			continue
		}
		returns = append(returns, (equity[i]-equity[i-1])/equity[i-1])
	}
	return returns
}

func meanStddev(xs []float64) (mean, stddev float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))

	var variance float64
	for _, x := range xs {
		variance += (x - mean) * (x - mean)
	}
	variance /= float64(len(xs))
	return mean, math.Sqrt(variance)
}

// downsideDeviation is the RMS of negative returns computed over ALL of
// xs, not just the losing subset — the spec's precise Sortino definition.
func downsideDeviation(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sumSq float64
	for _, x := range xs {
		if x < 0 {
			sumSq += x * x
		}
	}
	return math.Sqrt(sumSq / float64(len(xs)))
}

func maxDrawdownPct(equity []float64) float64 {
	if len(equity) == 0 {
		return 0
	}
	peak := equity[0]
	var maxDD float64
	for _, e := range equity {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			if dd := (peak - e) / peak; dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./internal/metrics/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add simulation/internal/metrics/metrics.go simulation/internal/metrics/metrics_test.go
git commit -m "feat(simulation): compute final backtest metrics (return, drawdown, Sharpe, Sortino, win rate)"
```

---

### Task 11: `internal/storage` — run/trade/equity/results persistence

**Files:**
- Create: `simulation/internal/storage/runs.go`
- Test: `simulation/internal/storage/runs_test.go`

**Interfaces:**
- Consumes: Task 9's `Store`, Task 10's `metrics.Results`.
- Produces: `type Run struct{...}`, `(s *Store) CreateRun(ctx, r Run) error`, `(s *Store) FinishRun(ctx, runID string, runErr error) error`, `type Trade struct{...}`, `(s *Store) RecordTrade(ctx, t Trade) error`, `type EquityPoint struct{...}`, `(s *Store) RecordEquityPoint(ctx, e EquityPoint) error`, `(s *Store) SaveResults(ctx, runID string, m metrics.Results) error`.

- [ ] **Step 1: Write the failing test**

```go
// simulation/internal/storage/runs_test.go
package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"simulation/internal/metrics"
)

func TestCreateRun_And_FinishRun_Completed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	r := Run{
		ID: runID, StrategyName: "fixed-replay",
		PeriodStart: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC),
		Timeframes:  []string{"1h"}, DrivingTimeframe: "1h",
		InitialCash: 10000, FeePct: 0.001,
	}
	if err := s.CreateRun(ctx, r); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		s.pool.Exec(ctx, `DELETE FROM backtest_results WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_equity_curve WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_trades WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_runs WHERE id = $1`, runID)
	})

	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM backtest_runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "running" {
		t.Errorf("status = %q, want %q", status, "running")
	}

	if err := s.FinishRun(ctx, runID, nil); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT status FROM backtest_runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "completed" {
		t.Errorf("status = %q, want %q", status, "completed")
	}
}

func TestFinishRun_Failed_RecordsError(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	if err := s.CreateRun(ctx, Run{ID: runID, StrategyName: "x", PeriodStart: time.Now(), PeriodEnd: time.Now(), Timeframes: []string{"1h"}, DrivingTimeframe: "1h", InitialCash: 1, FeePct: 0}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	t.Cleanup(func() { s.pool.Exec(context.Background(), `DELETE FROM backtest_runs WHERE id = $1`, runID) })

	if err := s.FinishRun(ctx, runID, errors.New("database connection lost")); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	var status, errMsg string
	if err := s.pool.QueryRow(ctx, `SELECT status, error FROM backtest_runs WHERE id = $1`, runID).Scan(&status, &errMsg); err != nil {
		t.Fatalf("query: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want %q", status, "failed")
	}
	if errMsg != "database connection lost" {
		t.Errorf("error = %q, want %q", errMsg, "database connection lost")
	}
}

func TestRecordTrade_And_RecordEquityPoint_And_SaveResults(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	runID := "test-run-" + t.Name()

	if err := s.CreateRun(ctx, Run{ID: runID, StrategyName: "x", PeriodStart: time.Now(), PeriodEnd: time.Now(), Timeframes: []string{"1h"}, DrivingTimeframe: "1h", InitialCash: 10000, FeePct: 0.001}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		s.pool.Exec(ctx, `DELETE FROM backtest_results WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_equity_curve WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_trades WHERE run_id = $1`, runID)
		s.pool.Exec(ctx, `DELETE FROM backtest_runs WHERE id = $1`, runID)
	})

	if err := s.RecordTrade(ctx, Trade{RunID: runID, Time: time.Now(), Asset: "BTC", Side: "buy", Quantity: 1, Price: 100, Fee: 0.1, Allowed: true}); err != nil {
		t.Fatalf("RecordTrade: %v", err)
	}
	reason := "daily_loss breached"
	if err := s.RecordTrade(ctx, Trade{RunID: runID, Time: time.Now(), Asset: "BTC", Side: "sell", Quantity: 1, Price: 0, Fee: 0, Allowed: false, RejectReason: &reason}); err != nil {
		t.Fatalf("RecordTrade (rejected): %v", err)
	}

	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backtest_trades WHERE run_id = $1`, runID).Scan(&count); err != nil {
		t.Fatalf("count trades: %v", err)
	}
	if count != 2 {
		t.Errorf("trade count = %d, want 2", count)
	}

	if err := s.RecordEquityPoint(ctx, EquityPoint{RunID: runID, Time: time.Now(), Cash: 9900, PositionsValue: 100, TotalEquity: 10000}); err != nil {
		t.Fatalf("RecordEquityPoint: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backtest_equity_curve WHERE run_id = $1`, runID).Scan(&count); err != nil {
		t.Fatalf("count equity points: %v", err)
	}
	if count != 1 {
		t.Errorf("equity point count = %d, want 1", count)
	}

	results := metrics.Results{
		TotalReturnPct: 5, MaxDrawdownPct: 2, SharpeRatio: 1.1, SortinoRatio: 1.5,
		AnnualizedVolatilityPct: 20, WinRatePct: 60, TotalTrades: 2, AvgTradePct: 1.2,
	}
	if err := s.SaveResults(ctx, runID, results); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}
	var gotSharpe float64
	if err := s.pool.QueryRow(ctx, `SELECT sharpe_ratio FROM backtest_results WHERE run_id = $1`, runID).Scan(&gotSharpe); err != nil {
		t.Fatalf("query results: %v", err)
	}
	if gotSharpe != 1.1 {
		t.Errorf("sharpe_ratio = %v, want 1.1", gotSharpe)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker compose exec go go test ./internal/storage/... -run 'TestCreateRun|TestFinishRun|TestRecordTrade' -v`
Expected: FAIL — `Run`/`CreateRun`/etc. undefined.

- [ ] **Step 3: Write `runs.go`**

```go
// simulation/internal/storage/runs.go
package storage

import (
	"context"
	"time"

	"simulation/internal/metrics"
)

type Run struct {
	ID               string
	StrategyName     string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	Timeframes       []string
	DrivingTimeframe string
	InitialCash      float64
	FeePct           float64
}

func (s *Store) CreateRun(ctx context.Context, r Run) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_runs (id, strategy_name, period_start, period_end, timeframes, driving_timeframe, initial_cash, fee_pct, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'running')
	`, r.ID, r.StrategyName, r.PeriodStart, r.PeriodEnd, r.Timeframes, r.DrivingTimeframe, r.InitialCash, r.FeePct)
	return err
}

// FinishRun marks runID 'completed' (runErr nil) or 'failed' with runErr's
// message recorded, and stamps ended_at either way — a backtest never ends
// with a silent partial 'running' row.
func (s *Store) FinishRun(ctx context.Context, runID string, runErr error) error {
	status := "completed"
	var errMsg *string
	if runErr != nil {
		status = "failed"
		msg := runErr.Error()
		errMsg = &msg
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE backtest_runs SET status = $1, error = $2, ended_at = now() WHERE id = $3
	`, status, errMsg, runID)
	return err
}

type Trade struct {
	RunID        string
	Time         time.Time
	Asset        string
	Side         string
	Quantity     float64
	Price        float64
	Fee          float64
	Allowed      bool
	RejectReason *string
}

func (s *Store) RecordTrade(ctx context.Context, t Trade) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_trades (run_id, ts, asset, side, quantity, price, fee, allowed, reject_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, t.RunID, t.Time, t.Asset, t.Side, t.Quantity, t.Price, t.Fee, t.Allowed, t.RejectReason)
	return err
}

type EquityPoint struct {
	RunID          string
	Time           time.Time
	Cash           float64
	PositionsValue float64
	TotalEquity    float64
}

func (s *Store) RecordEquityPoint(ctx context.Context, e EquityPoint) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_equity_curve (run_id, ts, cash, positions_value, total_equity)
		VALUES ($1, $2, $3, $4, $5)
	`, e.RunID, e.Time, e.Cash, e.PositionsValue, e.TotalEquity)
	return err
}

func (s *Store) SaveResults(ctx context.Context, runID string, m metrics.Results) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_results (run_id, total_return_pct, max_drawdown_pct, sharpe_ratio, sortino_ratio, annualized_volatility_pct, win_rate_pct, total_trades, avg_trade_pct)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, runID, m.TotalReturnPct, m.MaxDrawdownPct, m.SharpeRatio, m.SortinoRatio, m.AnnualizedVolatilityPct, m.WinRatePct, m.TotalTrades, m.AvgTradePct)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./internal/storage/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add simulation/internal/storage/runs.go simulation/internal/storage/runs_test.go
git commit -m "feat(simulation): persist backtest runs, trades, equity curve, and results"
```

---

### Task 12: `internal/portfolio`

**Files:**
- Create: `simulation/internal/portfolio/portfolio.go`
- Test: `simulation/internal/portfolio/portfolio_test.go`

**Interfaces:**
- Consumes: `risk.Position`, `risk.Side`, `risk.SideBuy`, `risk.SideSell` from `risk-engine/risk`.
- Produces: `type Snapshot struct{Positions map[string]risk.Position; Cash, DailyLoss, WeeklyLoss, Drawdown float64; ConsecutiveLosses int}`, `type Fill struct{Time time.Time; Asset string; Side risk.Side; Quantity, Price, Fee float64}`, `type Tracker struct{...}`, `NewTracker(initialCash float64) *Tracker`, `(t *Tracker) ApplyFill(f Fill) float64`, `(t *Tracker) MarkToMarket(now time.Time, closes map[string]float64) (cash, positionsValue, totalEquity float64)`, `(t *Tracker) Snapshot(now time.Time) Snapshot`, `(t *Tracker) EquityCurve() []float64`.

- [ ] **Step 1: Write the failing tests**

```go
// simulation/internal/portfolio/portfolio_test.go
package portfolio

import (
	"testing"
	"time"

	"risk-engine/risk"
)

func TestApplyFill_Buy_UpdatesCashAndWeightedAvgCost(t *testing.T) {
	tr := NewTracker(10000)
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 1, Price: 100, Fee: 1})
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 1, Price: 200, Fee: 1})

	tr.MarkToMarket(time.Now(), map[string]float64{"BTC": 200})
	snap := tr.Snapshot(time.Now())

	wantCash := 10000.0 - 100 - 1 - 200 - 1
	if snap.Cash != wantCash {
		t.Errorf("Cash = %v, want %v", snap.Cash, wantCash)
	}
	pos, ok := snap.Positions["BTC"]
	if !ok {
		t.Fatal("expected a BTC position")
	}
	if pos.Quantity != 2 {
		t.Errorf("Quantity = %v, want 2", pos.Quantity)
	}
	wantValue := 2 * 200.0 // marked at last close
	if pos.Value != wantValue {
		t.Errorf("Value = %v, want %v", pos.Value, wantValue)
	}
}

func TestApplyFill_Sell_RealizesPnLAndTracksConsecutiveLosses(t *testing.T) {
	tr := NewTracker(10000)
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 1, Price: 100, Fee: 0})

	// Sell at a loss: realized = (90-100)*1 - 1 = -11
	realized := tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideSell, Quantity: 1, Price: 90, Fee: 1})
	if realized != -11 {
		t.Errorf("realized = %v, want -11", realized)
	}
	tr.MarkToMarket(time.Now(), map[string]float64{"BTC": 90})
	snap := tr.Snapshot(time.Now())
	if snap.ConsecutiveLosses != 1 {
		t.Errorf("ConsecutiveLosses = %d, want 1", snap.ConsecutiveLosses)
	}

	// Buy and sell again, this time at a profit — resets the streak.
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 1, Price: 100, Fee: 0})
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideSell, Quantity: 1, Price: 110, Fee: 0})
	tr.MarkToMarket(time.Now(), map[string]float64{"BTC": 110})
	snap = tr.Snapshot(time.Now())
	if snap.ConsecutiveLosses != 0 {
		t.Errorf("ConsecutiveLosses = %d, want 0 after a winning trade", snap.ConsecutiveLosses)
	}
}

func TestSnapshot_Drawdown_MeasuredFromPeak(t *testing.T) {
	tr := NewTracker(10000)
	now := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	tr.MarkToMarket(now, nil) // 10000, new peak
	now = now.Add(time.Hour)
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 10, Price: 100, Fee: 0}) // cash 9000, 10 BTC
	tr.MarkToMarket(now, map[string]float64{"BTC": 150})                                   // equity = 9000 + 1500 = 10500, new peak
	now = now.Add(time.Hour)
	tr.MarkToMarket(now, map[string]float64{"BTC": 100}) // equity = 9000 + 1000 = 10000

	snap := tr.Snapshot(now)
	wantDD := (10500.0 - 10000.0) / 10500.0
	if snap.Drawdown != wantDD {
		t.Errorf("Drawdown = %v, want %v", snap.Drawdown, wantDD)
	}
}

func TestSnapshot_DailyLoss_MeasuredSinceStartOfUTCDay(t *testing.T) {
	tr := NewTracker(10000)
	dayStart := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)

	tr.MarkToMarket(dayStart.Add(-time.Hour), nil)          // yesterday, equity 10000 — must not count
	tr.MarkToMarket(dayStart.Add(1*time.Hour), nil)         // today's first point, still 10000
	tr.ApplyFill(Fill{Asset: "BTC", Side: risk.SideBuy, Quantity: 10, Price: 100, Fee: 0})
	now := dayStart.Add(2 * time.Hour)
	tr.MarkToMarket(now, map[string]float64{"BTC": 90}) // equity = 9000 + 900 = 9900

	snap := tr.Snapshot(now)
	wantDailyLoss := (10000.0 - 9900.0) / 10000.0
	if snap.DailyLoss != wantDailyLoss {
		t.Errorf("DailyLoss = %v, want %v", snap.DailyLoss, wantDailyLoss)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker compose exec go go test ./internal/portfolio/... -v`
Expected: FAIL — package undefined.

- [ ] **Step 3: Write `portfolio.go`**

```go
// simulation/internal/portfolio/portfolio.go
package portfolio

import (
	"time"

	"risk-engine/risk"
)

// Snapshot is the simulated portfolio's state at one instant, in the exact
// shape risk.PortfolioState expects — engine converts between the two with
// a direct type conversion (identical underlying type), no wrapper needed.
type Snapshot struct {
	Positions         map[string]risk.Position
	Cash              float64
	DailyLoss         float64
	WeeklyLoss        float64
	Drawdown          float64
	ConsecutiveLosses int
}

// Fill is one executed trade applied to the tracker. Price is the actual
// fill price (next driving-timeframe candle's open); Fee is charged in
// cash separately from Price*Quantity.
type Fill struct {
	Time     time.Time
	Asset    string
	Side     risk.Side
	Quantity float64
	Price    float64
	Fee      float64
}

type equityPoint struct {
	time  time.Time
	total float64
}

// Tracker holds simulated portfolio state across one backtest run: cash,
// per-asset quantity and weighted-average cost basis, and the equity
// history needed to compute DailyLoss/WeeklyLoss/Drawdown at each step.
type Tracker struct {
	cash              float64
	quantity          map[string]float64
	avgEntry          map[string]float64
	lastClose         map[string]float64
	peakEquity        float64
	consecutiveLosses int
	equityHistory     []equityPoint
}

func NewTracker(initialCash float64) *Tracker {
	return &Tracker{
		cash:       initialCash,
		quantity:   map[string]float64{},
		avgEntry:   map[string]float64{},
		lastClose:  map[string]float64{},
		peakEquity: initialCash,
	}
}

// ApplyFill executes a fill: debits/credits cash, updates quantity and
// weighted-average cost basis on a buy, and on a sell computes realized
// P&L as (price-avgEntry)*qty-fee, feeding ConsecutiveLosses. Returns the
// realized P&L in currency units (always 0 for a buy).
func (t *Tracker) ApplyFill(f Fill) float64 {
	switch f.Side {
	case risk.SideBuy:
		cost := f.Quantity * f.Price
		newQty := t.quantity[f.Asset] + f.Quantity
		if newQty > 0 {
			t.avgEntry[f.Asset] = (t.avgEntry[f.Asset]*t.quantity[f.Asset] + cost) / newQty
		}
		t.quantity[f.Asset] = newQty
		t.cash -= cost + f.Fee
		return 0
	case risk.SideSell:
		realized := (f.Price-t.avgEntry[f.Asset])*f.Quantity - f.Fee
		t.quantity[f.Asset] -= f.Quantity
		t.cash += f.Quantity*f.Price - f.Fee
		if realized < 0 {
			t.consecutiveLosses++
		} else {
			t.consecutiveLosses = 0
		}
		return realized
	}
	return 0
}

// MarkToMarket values the whole portfolio at now using closes (per-asset
// last-known close price; an asset absent from closes keeps its previous
// price), updates the peak-equity high-water mark, and records the point
// for later DailyLoss/WeeklyLoss/EquityCurve queries.
func (t *Tracker) MarkToMarket(now time.Time, closes map[string]float64) (cash, positionsValue, totalEquity float64) {
	for asset, price := range closes {
		t.lastClose[asset] = price
	}
	for asset, qty := range t.quantity {
		positionsValue += qty * t.lastClose[asset]
	}
	cash = t.cash
	totalEquity = cash + positionsValue
	if totalEquity > t.peakEquity {
		t.peakEquity = totalEquity
	}
	t.equityHistory = append(t.equityHistory, equityPoint{time: now, total: totalEquity})
	return cash, positionsValue, totalEquity
}

// Snapshot reports the portfolio's current state for the risk-engine and
// the Strategy. Call MarkToMarket for now before Snapshot in the same
// loop step, so the equity figures Snapshot derives are current.
func (t *Tracker) Snapshot(now time.Time) Snapshot {
	positions := make(map[string]risk.Position, len(t.quantity))
	for asset, qty := range t.quantity {
		if qty == 0 {
			continue
		}
		positions[asset] = risk.Position{Asset: asset, Quantity: qty, Value: qty * t.lastClose[asset]}
	}
	var current float64
	if n := len(t.equityHistory); n > 0 {
		current = t.equityHistory[n-1].total
	}
	return Snapshot{
		Positions:         positions,
		Cash:              t.cash,
		DailyLoss:         t.periodLoss(startOfUTCDay(now)),
		WeeklyLoss:        t.periodLoss(startOfUTCWeek(now)),
		Drawdown:          t.drawdown(current),
		ConsecutiveLosses: t.consecutiveLosses,
	}
}

// EquityCurve returns the recorded total-equity values in chronological
// order, for metrics.Compute at the end of a run.
func (t *Tracker) EquityCurve() []float64 {
	vals := make([]float64, len(t.equityHistory))
	for i, p := range t.equityHistory {
		vals[i] = p.total
	}
	return vals
}

// periodLoss is the fractional drop in total equity from the first
// recorded point at or after periodStart to the most recent point — never
// negative (a gain reports 0 loss).
func (t *Tracker) periodLoss(periodStart time.Time) float64 {
	if len(t.equityHistory) == 0 {
		return 0
	}
	current := t.equityHistory[len(t.equityHistory)-1].total
	baseline := current
	for _, p := range t.equityHistory {
		if !p.time.Before(periodStart) {
			baseline = p.total
			break
		}
	}
	if baseline <= 0 {
		return 0
	}
	if loss := (baseline - current) / baseline; loss > 0 {
		return loss
	}
	return 0
}

func (t *Tracker) drawdown(current float64) float64 {
	if t.peakEquity <= 0 {
		return 0
	}
	if dd := (t.peakEquity - current) / t.peakEquity; dd > 0 {
		return dd
	}
	return 0
}

func startOfUTCDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func startOfUTCWeek(t time.Time) time.Time {
	day := startOfUTCDay(t)
	offset := (int(day.Weekday()) + 6) % 7 // Monday = 0
	return day.AddDate(0, 0, -offset)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./internal/portfolio/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add simulation/internal/portfolio/portfolio.go simulation/internal/portfolio/portfolio_test.go
git commit -m "feat(simulation): track simulated portfolio state (cost basis, drawdown, daily/weekly loss)"
```

---

### Task 13: `internal/strategy` — interfaces + `FixedOperationsStrategy`

**Files:**
- Create: `simulation/internal/strategy/strategy.go`
- Create: `simulation/internal/strategy/fixed.go`
- Test: `simulation/internal/strategy/fixed_test.go`

**Interfaces:**
- Consumes: `simulation/internal/storage.Candle`, `simulation/internal/portfolio.Snapshot`, `risk-engine/risk.{ProposedOperation,Side,SideBuy,SideSell}`.
- Produces: `type MarketView interface{Candles(...)([]storage.Candle,error); Now() time.Time}`, `type Strategy interface{Decide(...)([]risk.ProposedOperation,error)}`, `type FixedOp struct{Time,Asset,Side,Quantity}`, `NewFixedOperationsStrategy(ops []FixedOp, drivingTimeframe string) (*FixedOperationsStrategy, error)`, `(s *FixedOperationsStrategy) Decide(...)`.

- [ ] **Step 1: Write `strategy.go`**

```go
// simulation/internal/strategy/strategy.go
package strategy

import (
	"context"
	"time"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

// MarketView gives a Strategy read access to candles closed at or before
// the current simulated instant, across whichever timeframes it asks for —
// never data from the future relative to Now().
type MarketView interface {
	Candles(ctx context.Context, timeframe, asset string, n int) ([]storage.Candle, error)
	Now() time.Time
}

// Strategy decides what to do at each driving-timeframe candle. Value on
// each returned risk.ProposedOperation is Quantity times the current
// driving-timeframe close (the price known at decision time) — the actual
// fill price (next candle's open) is resolved later by the engine.
type Strategy interface {
	Decide(ctx context.Context, view MarketView, snap portfolio.Snapshot) ([]risk.ProposedOperation, error)
}
```

- [ ] **Step 2: Write the failing test**

```go
// simulation/internal/strategy/fixed_test.go
package strategy

import (
	"context"
	"testing"
	"time"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

type fakeView struct {
	now    time.Time
	closes map[string]float64
}

func (v *fakeView) Now() time.Time { return v.now }
func (v *fakeView) Candles(ctx context.Context, timeframe, asset string, n int) ([]storage.Candle, error) {
	price, ok := v.closes[asset]
	if !ok {
		return nil, nil
	}
	return []storage.Candle{{Time: v.now, Close: price}}, nil
}

func TestFixedOperationsStrategy_EmitsEachOpExactlyOnceInItsWindow(t *testing.T) {
	base := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := NewFixedOperationsStrategy([]FixedOp{
		{Time: base.Add(30 * time.Minute), Asset: "BTC", Side: risk.SideBuy, Quantity: 1},
		{Time: base.Add(90 * time.Minute), Asset: "ETH", Side: risk.SideBuy, Quantity: 2},
	}, "1h")
	if err != nil {
		t.Fatalf("NewFixedOperationsStrategy: %v", err)
	}

	view := &fakeView{now: base.Add(time.Hour), closes: map[string]float64{"BTC": 100, "ETH": 200}}
	ops, err := s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide (step 1): %v", err)
	}
	if len(ops) != 1 || ops[0].Asset != "BTC" {
		t.Fatalf("step 1 ops = %+v, want exactly the BTC op (its window is [0h,1h))", ops)
	}
	if ops[0].Value != 100 {
		t.Errorf("Value = %v, want 100 (1 * close 100)", ops[0].Value)
	}

	// Re-calling Decide for the same window must not re-emit the BTC op.
	ops, err = s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide (re-call): %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("re-call ops = %+v, want none (already emitted)", ops)
	}

	view.now = base.Add(2 * time.Hour)
	ops, err = s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide (step 2): %v", err)
	}
	if len(ops) != 1 || ops[0].Asset != "ETH" {
		t.Fatalf("step 2 ops = %+v, want exactly the ETH op (its window is [1h,2h))", ops)
	}
}

func TestNewFixedOperationsStrategy_RejectsUnknownTimeframe(t *testing.T) {
	if _, err := NewFixedOperationsStrategy(nil, "3m"); err == nil {
		t.Fatal("expected an error for an uncollected timeframe")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `docker compose exec go go test ./internal/strategy/... -v`
Expected: FAIL — `FixedOp`/`NewFixedOperationsStrategy` undefined.

- [ ] **Step 4: Write `fixed.go`**

```go
// simulation/internal/strategy/fixed.go
package strategy

import (
	"context"
	"time"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

// FixedOp is one pre-scripted operation FixedOperationsStrategy replays.
type FixedOp struct {
	Time     time.Time
	Asset    string
	Side     risk.Side
	Quantity float64
}

// FixedOperationsStrategy replays a pre-defined list of operations,
// emitting each one on the first Decide call whose driving-timeframe
// candle window [Now()-duration, Now()) contains its Time. Ops need not be
// sorted; already-emitted ops are never re-emitted. Covers the replay case
// and serves as the fixture for this module's own integration tests.
type FixedOperationsStrategy struct {
	Ops             []FixedOp
	drivingDuration time.Duration
	drivingTF       string
	emitted         map[int]bool
}

func NewFixedOperationsStrategy(ops []FixedOp, drivingTimeframe string) (*FixedOperationsStrategy, error) {
	dur, err := storage.TimeframeDuration(drivingTimeframe)
	if err != nil {
		return nil, err
	}
	return &FixedOperationsStrategy{Ops: ops, drivingDuration: dur, drivingTF: drivingTimeframe, emitted: map[int]bool{}}, nil
}

func (s *FixedOperationsStrategy) Decide(ctx context.Context, view MarketView, snap portfolio.Snapshot) ([]risk.ProposedOperation, error) {
	now := view.Now()
	windowStart := now.Add(-s.drivingDuration)
	var out []risk.ProposedOperation
	for i, op := range s.Ops {
		if s.emitted[i] || op.Time.Before(windowStart) || !op.Time.Before(now) {
			continue
		}
		s.emitted[i] = true
		candles, err := view.Candles(ctx, s.drivingTF, op.Asset, 1)
		if err != nil {
			return nil, err
		}
		var price float64
		if len(candles) > 0 {
			price = candles[len(candles)-1].Close
		}
		out = append(out, risk.ProposedOperation{
			Asset: op.Asset, Side: op.Side, Quantity: op.Quantity, Value: op.Quantity * price,
		})
	}
	return out, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `docker compose exec go go test ./internal/strategy/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add simulation/internal/strategy/strategy.go simulation/internal/strategy/fixed.go simulation/internal/strategy/fixed_test.go
git commit -m "feat(simulation): Strategy/MarketView interfaces and FixedOperationsStrategy replay"
```

---

### Task 14: `internal/strategy` — `MovingAverageCrossStrategy`

**Files:**
- Create: `simulation/internal/strategy/movingaverage.go`
- Test: `simulation/internal/strategy/movingaverage_test.go`

**Interfaces:**
- Consumes: Task 13's `Strategy`/`MarketView`.
- Produces: `type MovingAverageCrossStrategy struct{Asset,Timeframe string; ShortPeriod,LongPeriod int; TradeValue float64}`, `(s *MovingAverageCrossStrategy) Decide(...)`.

- [ ] **Step 1: Write the failing test**

```go
// simulation/internal/strategy/movingaverage_test.go
package strategy

import (
	"context"
	"testing"
	"time"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

type seriesView struct {
	now     time.Time
	candles []storage.Candle // fixed full series, most recent last
}

func (v *seriesView) Now() time.Time { return v.now }
func (v *seriesView) Candles(ctx context.Context, timeframe, asset string, n int) ([]storage.Candle, error) {
	if n > len(v.candles) {
		n = len(v.candles)
	}
	return v.candles[len(v.candles)-n:], nil
}

func closes(vals ...float64) []storage.Candle {
	cs := make([]storage.Candle, len(vals))
	for i, v := range vals {
		cs[i] = storage.Candle{Close: v}
	}
	return cs
}

func TestMovingAverageCrossStrategy_BuysOnGoldenCross(t *testing.T) {
	s := &MovingAverageCrossStrategy{Asset: "BTC", Timeframe: "1h", ShortPeriod: 2, LongPeriod: 4, TradeValue: 1000}

	// Long-period series still flat/declining: short == long, no cross yet.
	view := &seriesView{candles: closes(100, 100, 100, 100)}
	if _, err := s.Decide(context.Background(), view, portfolio.Snapshot{}); err != nil {
		t.Fatalf("Decide (warm-up): %v", err)
	}

	// Now short average (last 2) rises above long average (last 4).
	view.candles = closes(100, 100, 110, 130)
	ops, err := s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide (cross): %v", err)
	}
	if len(ops) != 1 || ops[0].Side != risk.SideBuy || ops[0].Asset != "BTC" {
		t.Fatalf("ops = %+v, want a single BTC buy", ops)
	}
}

func TestMovingAverageCrossStrategy_SellsOnDeathCrossIfHoldingPosition(t *testing.T) {
	s := &MovingAverageCrossStrategy{Asset: "BTC", Timeframe: "1h", ShortPeriod: 2, LongPeriod: 4, TradeValue: 1000}
	view := &seriesView{candles: closes(100, 100, 110, 130)} // short above long
	if _, err := s.Decide(context.Background(), view, portfolio.Snapshot{}); err != nil {
		t.Fatalf("Decide (warm-up): %v", err)
	}

	view.candles = closes(110, 130, 90, 80) // short now below long
	snap := portfolio.Snapshot{Positions: map[string]risk.Position{"BTC": {Asset: "BTC", Quantity: 10}}}
	ops, err := s.Decide(context.Background(), view, snap)
	if err != nil {
		t.Fatalf("Decide (death cross): %v", err)
	}
	if len(ops) != 1 || ops[0].Side != risk.SideSell || ops[0].Quantity != 10 {
		t.Fatalf("ops = %+v, want a single sell of the full 10 BTC position", ops)
	}
}

func TestMovingAverageCrossStrategy_NoSignal_ReturnsNoOps(t *testing.T) {
	s := &MovingAverageCrossStrategy{Asset: "BTC", Timeframe: "1h", ShortPeriod: 2, LongPeriod: 4, TradeValue: 1000}
	view := &seriesView{candles: closes(100, 100)} // fewer than LongPeriod candles
	ops, err := s.Decide(context.Background(), view, portfolio.Snapshot{})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("ops = %+v, want none (insufficient history)", ops)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec go go test ./internal/strategy/... -run TestMovingAverage -v`
Expected: FAIL — `MovingAverageCrossStrategy` undefined.

- [ ] **Step 3: Write `movingaverage.go`**

```go
// simulation/internal/strategy/movingaverage.go
package strategy

import (
	"context"

	"risk-engine/risk"

	"simulation/internal/portfolio"
	"simulation/internal/storage"
)

// MovingAverageCrossStrategy buys Asset when the short-period SMA crosses
// above the long-period SMA while flat, and sells the full position when
// it crosses back below — a minimal example proving the end-to-end loop
// works with a real decision, not just replay.
type MovingAverageCrossStrategy struct {
	Asset       string
	Timeframe   string
	ShortPeriod int
	LongPeriod  int
	TradeValue  float64 // cash value of each buy, in quote currency

	wasAbove bool
	hasPrev  bool
}

func (s *MovingAverageCrossStrategy) Decide(ctx context.Context, view MarketView, snap portfolio.Snapshot) ([]risk.ProposedOperation, error) {
	candles, err := view.Candles(ctx, s.Timeframe, s.Asset, s.LongPeriod)
	if err != nil {
		return nil, err
	}
	if len(candles) < s.LongPeriod {
		return nil, nil
	}

	short := sma(candles[len(candles)-s.ShortPeriod:])
	long := sma(candles)
	above := short > long
	wasAbove, hasPrev := s.wasAbove, s.hasPrev
	s.wasAbove, s.hasPrev = above, true

	if !hasPrev {
		return nil, nil
	}

	price := candles[len(candles)-1].Close
	pos := snap.Positions[s.Asset]

	if above && !wasAbove && pos.Quantity == 0 {
		qty := s.TradeValue / price
		return []risk.ProposedOperation{{Asset: s.Asset, Side: risk.SideBuy, Quantity: qty, Value: s.TradeValue}}, nil
	}
	if !above && wasAbove && pos.Quantity > 0 {
		return []risk.ProposedOperation{{Asset: s.Asset, Side: risk.SideSell, Quantity: pos.Quantity, Value: pos.Quantity * price}}, nil
	}
	return nil, nil
}

func sma(candles []storage.Candle) float64 {
	var sum float64
	for _, c := range candles {
		sum += c.Close
	}
	return sum / float64(len(candles))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker compose exec go go test ./internal/strategy/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add simulation/internal/strategy/movingaverage.go simulation/internal/strategy/movingaverage_test.go
git commit -m "feat(simulation): add MovingAverageCrossStrategy as a real-decision example"
```

---

### Task 15: `internal/marketview`

**Files:**
- Create: `simulation/internal/marketview/marketview.go`
- Test: `simulation/internal/marketview/marketview_test.go`

**Interfaces:**
- Consumes: `simulation/internal/storage.Store.RecentCandles`, `risk.ReferenceExchange`.
- Produces: `type View struct{...}`, `New(store *storage.Store) *View`, `(v *View) Advance(now time.Time)`, `(v *View) Now() time.Time`, `(v *View) Candles(ctx, timeframe, asset string, n int) ([]storage.Candle, error)` (satisfies `strategy.MarketView` structurally).

- [ ] **Step 1: Add test-only fixture helpers to `simulation/internal/storage`**

`storage.Store` exposes no raw insert/delete outside itself; this View test (and Task 17's engine tests) need to seed and clean up `candles` rows directly. Add both to `simulation/internal/storage/candles.go` (production file — small, and reused by every fixture-seeding test in this module):

```go
// InsertCandleForTest inserts or updates one candle row directly — a thin
// seeding seam for this module's own tests (marketview, engine) that need
// fixture data. Not used by any production code path.
func (s *Store) InsertCandleForTest(ctx context.Context, exchange, symbol, timeframe string, ts time.Time, open, high, low, close, volume float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (exchange, symbol, timeframe, ts) DO UPDATE
		SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
		    close = EXCLUDED.close, volume = EXCLUDED.volume
	`, exchange, symbol, timeframe, ts, open, high, low, close, volume)
	return err
}

// DeleteCandlesForTest removes every candle for exchange/symbol/timeframe —
// paired with InsertCandleForTest for fixture cleanup in this module's tests.
func (s *Store) DeleteCandlesForTest(ctx context.Context, exchange, symbol, timeframe string) {
	s.pool.Exec(ctx, `DELETE FROM candles WHERE exchange = $1 AND symbol = $2 AND timeframe = $3`, exchange, symbol, timeframe)
}
```

- [ ] **Step 2: Write the failing test**

```go
// simulation/internal/marketview/marketview_test.go
package marketview

import (
	"context"
	"os"
	"testing"
	"time"

	"risk-engine/risk"

	"simulation/internal/storage"
)

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping marketview tests")
	}
	s, err := storage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestView_Candles_UsesAdvancedNowAsCutoff(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	base := time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed candle: %v", err)
		}
	}
	must(store.InsertCandleForTest(ctx, risk.ReferenceExchange, "MVCOIN", "1h", base, 100, 100, 100, 100, 1))
	must(store.InsertCandleForTest(ctx, risk.ReferenceExchange, "MVCOIN", "1h", base.Add(time.Hour), 101, 101, 101, 101, 1))
	must(store.InsertCandleForTest(ctx, risk.ReferenceExchange, "MVCOIN", "1h", base.Add(2*time.Hour), 999, 999, 999, 999, 1))
	t.Cleanup(func() {
		store.DeleteCandlesForTest(context.Background(), risk.ReferenceExchange, "MVCOIN", "1h")
	})

	view := New(store)
	view.Advance(base.Add(2 * time.Hour)) // the [base+1h, base+2h) candle just closed

	candles, err := view.Candles(ctx, "1h", "MVCOIN", 10)
	if err != nil {
		t.Fatalf("Candles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len(candles) = %d, want 2, got %+v", len(candles), candles)
	}
	if candles[len(candles)-1].Close != 101 {
		t.Errorf("most recent visible close = %v, want 101", candles[len(candles)-1].Close)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `docker compose exec go go test ./internal/marketview/... -v`
Expected: FAIL — `View`/`New` undefined, `InsertCandleForTest`/`DeleteCandlesForTest` undefined.

- [ ] **Step 4: Write `marketview.go`**

```go
// simulation/internal/marketview/marketview.go
package marketview

import (
	"context"
	"time"

	"risk-engine/risk"

	"simulation/internal/storage"
)

// View implements strategy.MarketView: candles closed at or before Now(),
// read from risk.ReferenceExchange — the same reference exchange
// risk-engine's own quality checks use.
type View struct {
	store *storage.Store
	now   time.Time
}

func New(store *storage.Store) *View {
	return &View{store: store}
}

// Advance moves the view's simulated "now" forward — called once per
// engine loop iteration, before Strategy.Decide.
func (v *View) Advance(now time.Time) {
	v.now = now
}

func (v *View) Now() time.Time {
	return v.now
}

func (v *View) Candles(ctx context.Context, timeframe, asset string, n int) ([]storage.Candle, error) {
	return v.store.RecentCandles(ctx, risk.ReferenceExchange, asset, timeframe, n, v.now)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `docker compose exec go go test ./internal/marketview/... ./internal/storage/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add simulation/internal/marketview/marketview.go simulation/internal/marketview/marketview_test.go simulation/internal/storage/candles.go
git commit -m "feat(simulation): MarketView adapter reading candles as-of the simulated instant"
```

---

### Task 16: `internal/engine` — clock and fill

**Files:**
- Create: `simulation/internal/engine/clock.go`
- Create: `simulation/internal/engine/fill.go`
- Test: `simulation/internal/engine/clock_test.go`

**Interfaces:**
- Produces: `type Clock struct{...}`, `NewClock(start, end time.Time, duration time.Duration) *Clock`, `(c *Clock) Next() (openTime, closeTime time.Time, ok bool)`, `type PendingFill struct{Asset string; Side risk.Side; Quantity float64}`, `applyFee(value, feePct float64) float64`.

- [ ] **Step 1: Write the failing test**

```go
// simulation/internal/engine/clock_test.go
package engine

import (
	"testing"
	"time"
)

func TestClock_AdvancesInFixedIncrementsAndStops(t *testing.T) {
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	c := NewClock(start, end, time.Hour)

	var got []time.Time
	for {
		open, close, ok := c.Next()
		if !ok {
			break
		}
		if !close.Sub(open).Equal(time.Hour) {
			t.Fatalf("candle window = %v, want exactly 1h (open=%v close=%v)", close.Sub(open), open, close)
		}
		got = append(got, close)
	}

	want := []time.Time{start.Add(time.Hour), start.Add(2 * time.Hour), start.Add(3 * time.Hour)}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("step %d closeTime = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestApplyFee(t *testing.T) {
	got := applyFee(1000, 0.001)
	if got != 1 {
		t.Errorf("applyFee(1000, 0.001) = %v, want 1", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec go go test ./internal/engine/... -v`
Expected: FAIL — package undefined.

- [ ] **Step 3: Write `clock.go`**

```go
// simulation/internal/engine/clock.go
package engine

import "time"

// Clock advances in fixed increments of duration (the driving timeframe)
// from Start to End. Next reports each step's [openTime, closeTime)
// candle boundary until closeTime would exceed End.
type Clock struct {
	current  time.Time
	end      time.Time
	duration time.Duration
}

func NewClock(start, end time.Time, duration time.Duration) *Clock {
	return &Clock{current: start, end: end, duration: duration}
}

// Next returns the next candle's [openTime, closeTime) and advances the
// clock. ok is false once closeTime would exceed End — the run is done.
func (c *Clock) Next() (openTime, closeTime time.Time, ok bool) {
	closeTime = c.current.Add(c.duration)
	if closeTime.After(c.end) {
		return time.Time{}, time.Time{}, false
	}
	openTime = c.current
	c.current = closeTime
	return openTime, closeTime, true
}
```

- [ ] **Step 4: Write `fill.go`**

```go
// simulation/internal/engine/fill.go
package engine

import "risk-engine/risk"

// PendingFill is an approved operation queued at decision time (a
// candle's close), executed at the NEXT candle's open — never the candle
// that produced the signal, avoiding lookahead bias.
type PendingFill struct {
	Asset    string
	Side     risk.Side
	Quantity float64
}

// applyFee returns the fee amount for a trade of value at feePct (e.g.
// 0.001 for 0.1%).
func applyFee(value, feePct float64) float64 {
	return value * feePct
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `docker compose exec go go test ./internal/engine/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add simulation/internal/engine/clock.go simulation/internal/engine/fill.go simulation/internal/engine/clock_test.go
git commit -m "feat(simulation): driving-timeframe clock and fill primitives"
```

---

### Task 17: `internal/engine` — `Run` orchestration

**Files:**
- Create: `simulation/internal/engine/run.go`
- Test: `simulation/internal/engine/run_test.go`

**Interfaces:**
- Consumes: everything from Tasks 9–16 plus `risk-engine`'s `risk.Evaluate`/`EvalOptions`/`storage.InitRunState`.
- Produces: `type Config struct{StrategyName string; Strategy strategy.Strategy; PeriodStart,PeriodEnd time.Time; Timeframes []string; DrivingTimeframe string; Assets []string; InitialCash,FeePct float64}`, `Run(ctx, riskStore *riskstorage.Store, simStore *simstorage.Store, cfg Config) (runID string, err error)`.

- [ ] **Step 1: Add test-only cleanup helpers**

These integration tests create real `backtest_runs`/`risk_state` rows across both modules; add the cleanup seams now, in the production files, before writing the tests that need them.

Add to `simulation/internal/storage/runs.go`:

```go
// DeleteRunForTest removes a run and everything referencing it (trades,
// equity curve, results) — test-only cleanup, not used by production code.
func (s *Store) DeleteRunForTest(ctx context.Context, runID string) {
	s.pool.Exec(ctx, `DELETE FROM backtest_results WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM backtest_equity_curve WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM backtest_trades WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM backtest_runs WHERE id = $1`, runID)
}

// RunStatus reads back a run's current status — used by tests asserting a
// backtest reached 'completed' or 'failed'.
func (s *Store) RunStatus(ctx context.Context, runID string) (string, error) {
	var status string
	err := s.pool.QueryRow(ctx, `SELECT status FROM backtest_runs WHERE id = $1`, runID).Scan(&status)
	return status, err
}

// TradeCount counts backtest_trades rows for runID — used by tests
// asserting trades were actually recorded, not just that Run returned no
// error.
func (s *Store) TradeCount(ctx context.Context, runID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backtest_trades WHERE run_id = $1`, runID).Scan(&count)
	return count, err
}
```

Add to `risk-engine/storage/state.go` (test-only helper, exported so `simulation`'s cross-module tests can clean up a run's `risk_state`/`risk_decisions` rows without reaching into `risk-engine`'s internals):

```go
// DeleteRunStateForTest removes risk_state/risk_decisions rows for runID —
// test-only cleanup for callers outside this package (e.g. simulation's
// integration tests), not used by production code.
func (s *Store) DeleteRunStateForTest(ctx context.Context, runID string) {
	s.pool.Exec(ctx, `DELETE FROM risk_state WHERE run_id = $1`, runID)
	s.pool.Exec(ctx, `DELETE FROM risk_decisions WHERE run_id = $1`, runID)
}
```

- [ ] **Step 2: Write the failing integration tests**

```go
// simulation/internal/engine/run_test.go
package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	simstorage "simulation/internal/storage"
	"simulation/internal/strategy"
)

func testStores(t *testing.T) (*riskstorage.Store, *simstorage.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping engine integration tests")
	}
	riskStore, err := riskstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("riskstorage.New: %v", err)
	}
	t.Cleanup(func() { riskStore.Close() })
	simStore, err := simstorage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("simstorage.New: %v", err)
	}
	t.Cleanup(func() { simStore.Close() })
	return riskStore, simStore
}

func seedHourlyCandles(t *testing.T, simStore *simstorage.Store, asset string, base time.Time, closes []float64) {
	t.Helper()
	ctx := context.Background()
	for i, c := range closes {
		ts := base.Add(time.Duration(i) * time.Hour)
		if err := simStore.InsertCandleForTest(ctx, risk.ReferenceExchange, asset, "1h", ts, c, c, c, c, 100000); err != nil {
			t.Fatalf("seed candle %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		simStore.DeleteCandlesForTest(context.Background(), risk.ReferenceExchange, asset, "1h")
	})
}

// cleanupRun registers deletion of every row a completed/failed Run call
// may have written, across both modules' stores.
func cleanupRun(t *testing.T, simStore *simstorage.Store, riskStore *riskstorage.Store, runID string) {
	t.Helper()
	if runID == "" {
		return
	}
	t.Cleanup(func() {
		ctx := context.Background()
		simStore.DeleteRunForTest(ctx, runID)
		riskStore.DeleteRunStateForTest(ctx, runID)
	})
}

func TestRun_FullBacktest_ProducesConsistentResults(t *testing.T) {
	riskStore, simStore := testStores(t)
	ctx := context.Background()
	asset := "ENGCOIN1_" + t.Name()
	base := time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC)

	// 6 flat, liquid, low-volatility candles so every risk-engine quality
	// rule passes throughout the run.
	seedHourlyCandles(t, simStore, asset, base, []float64{100, 100, 100, 100, 100, 100})

	strat, err := strategy.NewFixedOperationsStrategy([]strategy.FixedOp{
		{Time: base.Add(90 * time.Minute), Asset: asset, Side: risk.SideBuy, Quantity: 1},
	}, "1h")
	if err != nil {
		t.Fatalf("NewFixedOperationsStrategy: %v", err)
	}

	runID, err := Run(ctx, riskStore, simStore, Config{
		StrategyName: "fixed-replay", Strategy: strat,
		PeriodStart: base, PeriodEnd: base.Add(5 * time.Hour),
		Timeframes: []string{"1h"}, DrivingTimeframe: "1h", Assets: []string{asset},
		InitialCash: 10000, FeePct: 0.001,
	})
	cleanupRun(t, simStore, riskStore, runID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runID == "" {
		t.Fatal("expected a non-empty runID")
	}

	status, err := simStore.RunStatus(ctx, runID)
	if err != nil {
		t.Fatalf("RunStatus: %v", err)
	}
	if status != "completed" {
		t.Fatalf("run status = %q, want %q", status, "completed")
	}

	tradeCount, err := simStore.TradeCount(ctx, runID)
	if err != nil {
		t.Fatalf("TradeCount: %v", err)
	}
	if tradeCount == 0 {
		t.Fatal("expected at least one recorded trade (the scripted buy)")
	}
}

func TestRun_NonLookahead_NeverSeesCandlesPastPeriodEnd(t *testing.T) {
	riskStore, simStore := testStores(t)
	ctx := context.Background()
	asset := "ENGCOIN2_" + t.Name()
	base := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)

	// A moving-average strategy that would clearly signal a buy IF it
	// could see the sharp rise at the end — but that candle is beyond
	// period_end and must never be visible.
	seedHourlyCandles(t, simStore, asset, base, []float64{100, 100, 100, 100, 100, 100, 100, 900})

	strat := &strategy.MovingAverageCrossStrategy{Asset: asset, Timeframe: "1h", ShortPeriod: 2, LongPeriod: 4, TradeValue: 1000}

	runID, err := Run(ctx, riskStore, simStore, Config{
		StrategyName: "moving-average", Strategy: strat,
		PeriodStart: base, PeriodEnd: base.Add(6 * time.Hour), // deliberately excludes the 900-close candle
		Timeframes: []string{"1h"}, DrivingTimeframe: "1h", Assets: []string{asset},
		InitialCash: 10000, FeePct: 0.001,
	})
	cleanupRun(t, simStore, riskStore, runID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	tradeCount, err := simStore.TradeCount(ctx, runID)
	if err != nil {
		t.Fatalf("TradeCount: %v", err)
	}
	if tradeCount != 0 {
		t.Fatalf("trade count = %d, want 0 — a flat series with no visible cross must produce no trades; a non-zero count means the 900-close future candle leaked in", tradeCount)
	}
}

func TestRun_RiskBreach_PausesOnlyThisRun(t *testing.T) {
	riskStore, simStore := testStores(t)
	ctx := context.Background()
	asset := "ENGCOIN3_" + t.Name()
	base := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)

	// Sharp drop drives DailyLoss past the seeded 0.05 limit after the
	// first buy is filled and marked to market.
	seedHourlyCandles(t, simStore, asset, base, []float64{100, 100, 100, 40, 40, 40})

	strat, err := strategy.NewFixedOperationsStrategy([]strategy.FixedOp{
		{Time: base.Add(30 * time.Minute), Asset: asset, Side: risk.SideBuy, Quantity: 90},
	}, "1h")
	if err != nil {
		t.Fatalf("NewFixedOperationsStrategy: %v", err)
	}

	runID, err := Run(ctx, riskStore, simStore, Config{
		StrategyName: "fixed-replay", Strategy: strat,
		PeriodStart: base, PeriodEnd: base.Add(5 * time.Hour),
		Timeframes: []string{"1h"}, DrivingTimeframe: "1h", Assets: []string{asset},
		InitialCash: 10000, FeePct: 0.001,
	})
	cleanupRun(t, simStore, riskStore, runID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	runIDCopy := runID
	state, err := riskStore.GetState(ctx, &runIDCopy)
	if err != nil {
		t.Fatalf("GetState(runID): %v", err)
	}
	if state.Status != riskstorage.StatusPaused {
		t.Fatalf("run's risk_state.status = %q, want %q — the price drop should have breached daily_loss", state.Status, riskstorage.StatusPaused)
	}

	live, err := riskStore.GetState(ctx, nil)
	if err != nil {
		t.Fatalf("GetState(live): %v", err)
	}
	if live.Status != riskstorage.StatusNormal {
		t.Errorf("live Status = %q, want %q — a backtest breach must never pause live operation", live.Status, riskstorage.StatusNormal)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `docker compose exec go go test ./internal/engine/... -run TestRun -v`
Expected: FAIL — `Run`/`Config` undefined.

- [ ] **Step 4: Write `run.go`**

```go
// simulation/internal/engine/run.go
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"simulation/internal/marketview"
	"simulation/internal/metrics"
	"simulation/internal/portfolio"
	simstorage "simulation/internal/storage"
	"simulation/internal/strategy"
)

// Config is everything one backtest run needs, taken directly from CLI
// flags after validation.
type Config struct {
	StrategyName     string
	Strategy         strategy.Strategy
	PeriodStart      time.Time
	PeriodEnd        time.Time
	Timeframes       []string
	DrivingTimeframe string
	Assets           []string
	InitialCash      float64
	FeePct           float64
}

// Run executes one full backtest: creates the run record and an isolated
// risk_state row, advances the simulated clock candle by candle (driving
// timeframe), marks the portfolio to market, applies any pending fill,
// asks the Strategy for new operations, evaluates each through the real
// risk-engine, records every trade attempt, and finally computes and
// persists metrics. A failure mid-run marks the run 'failed' with the
// error message rather than leaving a silent partial result.
func Run(ctx context.Context, riskStore *riskstorage.Store, simStore *simstorage.Store, cfg Config) (runID string, err error) {
	drivingDuration, err := simstorage.TimeframeDuration(cfg.DrivingTimeframe)
	if err != nil {
		return "", fmt.Errorf("engine: driving timeframe: %w", err)
	}

	runID = uuid.NewString()
	if err := simStore.CreateRun(ctx, simstorage.Run{
		ID: runID, StrategyName: cfg.StrategyName,
		PeriodStart: cfg.PeriodStart, PeriodEnd: cfg.PeriodEnd,
		Timeframes: cfg.Timeframes, DrivingTimeframe: cfg.DrivingTimeframe,
		InitialCash: cfg.InitialCash, FeePct: cfg.FeePct,
	}); err != nil {
		return "", fmt.Errorf("engine: create run: %w", err)
	}
	if err := riskStore.InitRunState(ctx, runID); err != nil {
		return runID, finish(ctx, simStore, runID, fmt.Errorf("engine: init run state: %w", err))
	}

	runErr := runLoop(ctx, riskStore, simStore, runID, drivingDuration, cfg)
	return runID, finish(ctx, simStore, runID, runErr)
}

func finish(ctx context.Context, simStore *simstorage.Store, runID string, runErr error) error {
	if err := simStore.FinishRun(ctx, runID, runErr); err != nil {
		if runErr != nil {
			return fmt.Errorf("%w (also failed to record failure: %v)", runErr, err)
		}
		return fmt.Errorf("engine: finish run: %w", err)
	}
	return runErr
}

func runLoop(ctx context.Context, riskStore *riskstorage.Store, simStore *simstorage.Store, runID string, drivingDuration time.Duration, cfg Config) error {
	clock := NewClock(cfg.PeriodStart, cfg.PeriodEnd, drivingDuration)
	view := marketview.New(simStore)
	tracker := portfolio.NewTracker(cfg.InitialCash)

	var pending []PendingFill
	var tradeReturnsPct []float64

	for {
		openTime, closeTime, ok := clock.Next()
		if !ok {
			break
		}
		view.Advance(closeTime)

		candles, err := latestDrivingCandles(ctx, simStore, cfg.Assets, cfg.DrivingTimeframe, closeTime)
		if err != nil {
			return fmt.Errorf("engine: fetch candles at %s: %w", closeTime, err)
		}

		closes := make(map[string]float64, len(candles))
		for asset, c := range candles {
			closes[asset] = c.Close
		}
		cash, positionsValue, totalEquity := tracker.MarkToMarket(closeTime, closes)
		if err := simStore.RecordEquityPoint(ctx, simstorage.EquityPoint{
			RunID: runID, Time: closeTime, Cash: cash, PositionsValue: positionsValue, TotalEquity: totalEquity,
		}); err != nil {
			return fmt.Errorf("engine: record equity point: %w", err)
		}

		for _, fill := range pending {
			c, ok := candles[fill.Asset]
			if !ok {
				continue // no candle for this asset at this instant; the fill is dropped
			}
			openPrice := c.Open
			fee := applyFee(fill.Quantity*openPrice, cfg.FeePct)
			realized := tracker.ApplyFill(portfolio.Fill{
				Time: openTime, Asset: fill.Asset, Side: fill.Side, Quantity: fill.Quantity, Price: openPrice, Fee: fee,
			})
			if fill.Side == risk.SideSell {
				tradeReturnsPct = append(tradeReturnsPct, realized/(fill.Quantity*openPrice)*100)
			}
			if err := simStore.RecordTrade(ctx, simstorage.Trade{
				RunID: runID, Time: openTime, Asset: fill.Asset, Side: string(fill.Side),
				Quantity: fill.Quantity, Price: openPrice, Fee: fee, Allowed: true,
			}); err != nil {
				return fmt.Errorf("engine: record trade: %w", err)
			}
		}
		pending = pending[:0]

		snap := tracker.Snapshot(closeTime)
		proposed, err := cfg.Strategy.Decide(ctx, view, snap)
		if err != nil {
			return fmt.Errorf("engine: strategy decide at %s: %w", closeTime, err)
		}

		asOf := closeTime
		for _, op := range proposed {
			decision, err := risk.Evaluate(ctx, riskStore, risk.PortfolioState(snap), op, risk.EvalOptions{AsOf: &asOf, RunID: &runID})
			if err != nil {
				return fmt.Errorf("engine: risk evaluate at %s: %w", closeTime, err)
			}
			if !decision.Allowed {
				var reason string
				if len(decision.Reasons) > 0 {
					reason = decision.Reasons[0]
				}
				if err := simStore.RecordTrade(ctx, simstorage.Trade{
					RunID: runID, Time: closeTime, Asset: op.Asset, Side: string(op.Side),
					Quantity: op.Quantity, Price: 0, Fee: 0, Allowed: false, RejectReason: &reason,
				}); err != nil {
					return fmt.Errorf("engine: record rejected trade: %w", err)
				}
				continue
			}
			pending = append(pending, PendingFill{Asset: op.Asset, Side: op.Side, Quantity: op.Quantity})
		}
	}

	periodsPerYear := (365 * 24 * time.Hour).Seconds() / drivingDuration.Seconds()
	results := metrics.Compute(tracker.EquityCurve(), tradeReturnsPct, periodsPerYear)
	if err := simStore.SaveResults(ctx, runID, results); err != nil {
		return fmt.Errorf("engine: save results: %w", err)
	}
	return nil
}

// latestDrivingCandles fetches each configured asset's most recent
// driving-timeframe candle closed at or before asOf — used both for
// mark-to-market (via its Close) and for resolving a pending fill's
// execution price (via its Open, this same candle one step later).
func latestDrivingCandles(ctx context.Context, simStore *simstorage.Store, assets []string, drivingTimeframe string, asOf time.Time) (map[string]simstorage.Candle, error) {
	out := map[string]simstorage.Candle{}
	for _, asset := range assets {
		candles, err := simStore.RecentCandles(ctx, risk.ReferenceExchange, asset, drivingTimeframe, 1, asOf)
		if err != nil {
			return nil, err
		}
		if len(candles) == 0 {
			continue
		}
		out[asset] = candles[0]
	}
	return out, nil
}
```

Note: `risk.PortfolioState(snap)` is a direct Go type conversion — valid because `portfolio.Snapshot` and `risk.PortfolioState` have identical underlying types (same field names, types, and order): `Positions map[string]risk.Position; Cash, DailyLoss, WeeklyLoss, Drawdown float64; ConsecutiveLosses int`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `docker compose exec go go test ./internal/engine/... -v`
Expected: PASS.

- [ ] **Step 6: Re-run `risk-engine`'s full suite** (Step 1's `DeleteRunStateForTest` addition touched `risk-engine/storage/state.go`)

Run (from `risk-engine/`): `docker compose exec go go test -p 1 -count=1 ./... -v`
Expected: PASS, unchanged.

- [ ] **Step 7: Commit**

```bash
git add simulation/internal/engine/run.go simulation/internal/engine/run_test.go simulation/internal/storage/runs.go risk-engine/storage/state.go
git commit -m "feat(simulation): orchestrate the full backtest loop against the real risk-engine"
```

---

### Task 18: `cmd/backtest` CLI + final verification

**Files:**
- Create: `simulation/cmd/backtest/main.go`

**Interfaces:**
- Consumes: Task 17's `engine.Run`/`engine.Config`, Task 14's `strategy.MovingAverageCrossStrategy`.

- [ ] **Step 1: Write `main.go`**

```go
// simulation/cmd/backtest/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	riskstorage "risk-engine/storage"

	"simulation/internal/engine"
	simstorage "simulation/internal/storage"
	"simulation/internal/strategy"
)

func main() {
	var (
		periodStartStr = flag.String("period-start", "", "RFC3339 start of the backtest period (required)")
		periodEndStr   = flag.String("period-end", "", "RFC3339 end of the backtest period (required)")
		timeframesStr  = flag.String("timeframes", "", "comma-separated timeframes configured for this run, e.g. 1h,4h (required)")
		drivingTF      = flag.String("driving-timeframe", "", "the finest timeframe, drives the simulated clock (required, must be in -timeframes)")
		assetsStr      = flag.String("assets", "", "comma-separated asset symbols on the reference exchange (required)")
		initialCash    = flag.Float64("initial-cash", 10000, "starting cash")
		feePct         = flag.Float64("fee-pct", 0.001, "fee as a fraction of trade value, e.g. 0.001 for 0.1%")
		shortPeriod    = flag.Int("ma-short-period", 10, "moving-average strategy: short SMA period, in candles")
		longPeriod     = flag.Int("ma-long-period", 30, "moving-average strategy: long SMA period, in candles")
	)
	flag.Parse()

	if err := run(*periodStartStr, *periodEndStr, *timeframesStr, *drivingTF, *assetsStr, *initialCash, *feePct, *shortPeriod, *longPeriod); err != nil {
		log.Fatal(err)
	}
}

func run(periodStartStr, periodEndStr, timeframesStr, drivingTF, assetsStr string, initialCash, feePct float64, shortPeriod, longPeriod int) error {
	periodStart, err := time.Parse(time.RFC3339, periodStartStr)
	if err != nil {
		return fmt.Errorf("invalid -period-start: %w", err)
	}
	periodEnd, err := time.Parse(time.RFC3339, periodEndStr)
	if err != nil {
		return fmt.Errorf("invalid -period-end: %w", err)
	}
	if !periodStart.Before(periodEnd) {
		return fmt.Errorf("-period-start must be before -period-end")
	}
	if feePct < 0 {
		return fmt.Errorf("-fee-pct must be >= 0")
	}
	timeframes := splitNonEmpty(timeframesStr)
	if len(timeframes) == 0 {
		return fmt.Errorf("-timeframes is required")
	}
	if drivingTF == "" {
		return fmt.Errorf("-driving-timeframe is required")
	}
	found := false
	for _, tf := range timeframes {
		if tf == drivingTF {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("-driving-timeframe %q must be one of -timeframes %v", drivingTF, timeframes)
	}
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
	}

	strat := &strategy.MovingAverageCrossStrategy{
		Asset: assets[0], Timeframe: drivingTF,
		ShortPeriod: shortPeriod, LongPeriod: longPeriod, TradeValue: initialCash * 0.1,
	}

	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()

	simStore, err := simstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect simulation storage: %w", err)
	}
	defer simStore.Close()

	runID, err := engine.Run(ctx, riskStore, simStore, engine.Config{
		StrategyName: "moving-average", Strategy: strat,
		PeriodStart: periodStart, PeriodEnd: periodEnd,
		Timeframes: timeframes, DrivingTimeframe: drivingTF, Assets: assets,
		InitialCash: initialCash, FeePct: feePct,
	})
	if err != nil {
		return fmt.Errorf("backtest run %s: %w", runID, err)
	}
	fmt.Printf("backtest run %s completed\n", runID)
	return nil
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

`FixedOperationsStrategy` is intentionally not CLI-selectable — it needs a literal operation list that doesn't map to simple flags, and the spec frames it as the fixture for this module's own integration tests (Task 17), not a CLI-driven strategy. Sub-projects 4/5 replace this CLI's strategy selection entirely once real decision agents exist; no flag-based extensibility mechanism is worth building for the one interim example.

- [ ] **Step 2: Build the binary**

Run (from `simulation/`): `docker compose exec go go build ./...`
Expected: no errors.

- [ ] **Step 3: Manual smoke test against real seeded data**

Run: `docker compose exec go go run ./cmd/backtest -period-start=2023-01-01T00:00:00Z -period-end=2023-01-08T00:00:00Z -timeframes=1h -driving-timeframe=1h -assets=BTCUSDT -initial-cash=10000 -fee-pct=0.001`
Expected: either `backtest run <uuid> completed` (if `market-data` has 1h candles for `BTCUSDT` in that window) or a clean error — not a panic. If no candles exist for that window, this step is informational only (confirms the CLI wires together and fails gracefully); note the outcome, don't block on it.

- [ ] **Step 4: Commit**

```bash
git add simulation/cmd/backtest/main.go
git commit -m "feat(simulation): add backtest CLI (cmd/backtest)"
```

- [ ] **Step 5: Full-suite verification, both modules**

Run (from `risk-engine/`): `docker compose exec go go test -p 1 -count=1 ./... -v && docker compose exec go go vet ./... && docker compose exec go gofmt -l .`
Expected: all tests PASS, `go vet` silent, `gofmt -l .` silent.

Run (from `simulation/`): `docker compose exec go go test -count=1 ./... -v && docker compose exec go go vet ./... && docker compose exec go gofmt -l .`
Expected: all tests PASS, `go vet` silent, `gofmt -l .` silent.

- [ ] **Step 6: Commit (only if verification required fixes)**

```bash
git add risk-engine/ simulation/
git commit -m "fix: address final verification findings"
```
