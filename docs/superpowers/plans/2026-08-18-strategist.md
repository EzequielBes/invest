# Agente Estrategista + Motor de Decisão Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `strategist` Go module: a CLI that reads an existing `analysis_run_id`'s results, asks Claude for a structured buy/sell/hold decision per asset via tool use, sizes and validates any buy/sell through the real `risk-engine`, and persists every decision (including holds and rejections) — never executing a real order.

**Architecture:** Same shape as `analysis`/`simulation`/`risk-engine`: a standalone Go module (`strategist/`) with its own `go.mod`, `docker-compose.yml`, and TimescaleDB migration, depending on `risk-engine` via a local `replace` and reading `analysis_results`/`candles` directly from the shared database (no Go import of the `analysis` module). `internal/strategist/decide.go` holds the one piece of real business logic (prompt assembly, sizing math, calling `risk.Evaluate`); `cmd/strategist/main.go` wires DB reads, the per-asset loop, and persistence around it.

**Tech Stack:** Go 1.22, `anthropic-sdk-go@v1.9.0` (pinned — see Global Constraints), `google/uuid@v1.6.0`, `jackc/pgx/v5`, TimescaleDB (shared `market-data` instance), Docker Compose dev container.

**Spec:** `docs/superpowers/specs/2026-08-18-strategist-design.md`

## Global Constraints

- **Never run `go get github.com/anthropics/anthropic-sdk-go` without `@v1.9.0`.** `@latest` resolves to a version requiring Go 1.24; the dev container is `golang:1.22` with `GOTOOLCHAIN=local` (errors instead of auto-switching toolchains). Same restriction already hit and resolved in the `analysis` module (sub-project 4).
- **Pin `google/uuid@v1.6.0`** explicitly, same version already used by `analysis`/`simulation`, for consistency.
- **Every `docker compose` command for this module's dev container must be prefixed with `COMPOSE_PROJECT_NAME=strategist-dev`** on the host (never `docker compose exec -e COMPOSE_PROJECT_NAME=...` — that sets the variable *inside* the container, not the project name). Verify the container is actually bound to the intended directory with `docker inspect strategist-dev-go-1 --format '{{range .Mounts}}{{.Source}} -> {{.Destination}}{{"\n"}}{{end}}'` if test results ever look stale — `docker compose exec go pwd` always prints `/app` regardless of which host directory is mounted there, so it does **not** prove the bind is correct.
- **IDs are `uuid.NewString()`** generated in Go, stored as `TEXT` in Postgres — same convention as every other module (`analysis`, `simulation`, `risk-engine`).
- **JSONB columns:** `json.Marshal(x)` → pass the resulting `[]byte` directly as the query parameter.
- **Reduced test rigor** (per project-wide preference): test the sizing/orchestration logic and the LLM response-parsing boundary directly (Tasks 5, 6, 7, 8 below); skip dedicated tests for thin storage wrappers (Tasks 2, 3, 4) beyond a build check. A follow-up checklist of additional test scenarios (for a separate TDD pass) gets written after this plan is executed, same as `docs/superpowers/plans/2026-08-18-analysis-agents-TEST-CHECKLIST.md` did for sub-project 4 — not part of this plan's tasks.
- **No real execution.** Nothing in this plan calls an exchange. The end state of every asset is a row in `strategist_decisions`.

---

### Task 1: Scaffold the module

**Files:**
- Create: `strategist/go.mod`
- Create: `strategist/docker-compose.yml`
- Create: `strategist/migrations/001_init.sql`
- Create: `strategist/internal/storage/db.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `storage.Store` (opaque handle), `storage.New(ctx, dsn) (*Store, error)`, `(*Store).Close()` — consumed by every later task that touches the database.

- [ ] **Step 1: Write `go.mod`**

```go
// strategist/go.mod
module strategist

go 1.22

require github.com/jackc/pgx/v5 v5.6.0

replace risk-engine => ../risk-engine
```

- [ ] **Step 2: Write `docker-compose.yml`**

```yaml
# strategist/docker-compose.yml
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
-- strategist/migrations/001_init.sql
CREATE TABLE IF NOT EXISTS strategist_decisions (
    id                TEXT PRIMARY KEY,
    analysis_run_id   TEXT NOT NULL,
    asset             TEXT NOT NULL,
    side              TEXT NOT NULL,
    confidence        DOUBLE PRECISION NOT NULL,
    sizing_pct        DOUBLE PRECISION NOT NULL DEFAULT 0,
    rationale         TEXT NOT NULL DEFAULT '',
    proposed_quantity DOUBLE PRECISION NOT NULL DEFAULT 0,
    proposed_value    DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_allowed      BOOLEAN,
    risk_reasons      JSONB NOT NULL DEFAULT '[]',
    created_at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS strategist_decisions_run_id ON strategist_decisions (analysis_run_id);
```

- [ ] **Step 4: Write `internal/storage/db.go`**

```go
// strategist/internal/storage/db.go
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

- [ ] **Step 5: Bring up the dev container and apply the migration**

```bash
cd strategist
COMPOSE_PROJECT_NAME=strategist-dev docker compose up -d
COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go mod tidy
docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/001_init.sql
```

Expected: `go mod tidy` completes without touching the Go version (stays on
1.22 — if it tries to bump it, something pulled in a dependency that
needs a newer Go; stop and investigate before continuing). The `psql`
command prints `CREATE TABLE`/`CREATE INDEX` with no errors. If it prints
`relation "strategist_decisions" already exists` on a re-run, that's fine
(idempotent `IF NOT EXISTS`).

- [ ] **Step 6: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go build ./...`
Expected: no errors (only `internal/storage/db.go` exists so far — builds trivially).

- [ ] **Step 7: Commit**

```bash
git add strategist/go.mod strategist/go.sum strategist/docker-compose.yml strategist/migrations/001_init.sql strategist/internal/storage/db.go
git commit -m "feat(strategist): scaffold module, docker-compose, and migration"
```

---

### Task 2: Read analysis results

**Files:**
- Create: `strategist/internal/storage/analysisdata.go`

**Interfaces:**
- Consumes: `storage.Store` (Task 1).
- Produces: `storage.AgentResult{AgentType, Asset, Indicators, Narrative}`, `(*Store).ResultsForRun(ctx, runID string) ([]AgentResult, error)` — consumed by Task 7's CLI, which groups these by asset in memory.

- [ ] **Step 1: Write `internal/storage/analysisdata.go`**

```go
// strategist/internal/storage/analysisdata.go
package storage

import (
	"context"
	"encoding/json"
)

// AgentResult is one analysis_results row (owned by the analysis module,
// read here from the same shared database — no Go dependency on that
// module). AgentType is "technical", "derivatives", "news", or
// "risk_context"; Asset is "" for risk_context, which is portfolio-level.
type AgentResult struct {
	AgentType  string
	Asset      string
	Indicators map[string]any
	Narrative  string
}

// ResultsForRun returns every analysis_results row for runID — a run
// typically has only a handful of rows (a few assets times up to four
// agents), so callers group by asset/agent_type in memory rather than
// querying per asset.
func (s *Store) ResultsForRun(ctx context.Context, runID string) ([]AgentResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT agent_type, asset, indicators, narrative
		FROM analysis_results
		WHERE run_id = $1
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AgentResult
	for rows.Next() {
		var r AgentResult
		var raw []byte
		if err := rows.Scan(&r.AgentType, &r.Asset, &raw, &r.Narrative); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &r.Indicators); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
```

- [ ] **Step 2: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go build ./...`. Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add strategist/internal/storage/analysisdata.go
git commit -m "feat(strategist): read analysis_results for a run"
```

---

### Task 3: Read the current price

**Files:**
- Create: `strategist/internal/storage/marketdata.go`

**Interfaces:**
- Consumes: `storage.Store` (Task 1).
- Produces: `(*Store).LatestPrice(ctx, exchange, symbol, timeframe string) (price float64, found bool, err error)` — consumed by Task 7's CLI (sizing and portfolio valuation).

- [ ] **Step 1: Write `internal/storage/marketdata.go`**

```go
// strategist/internal/storage/marketdata.go
package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// LatestPrice returns the most recent closed candle's close price for
// exchange/symbol/timeframe (the market-data module's candles table,
// read here directly — no Go dependency on that module). found is false
// if no candle has been collected yet.
func (s *Store) LatestPrice(ctx context.Context, exchange, symbol, timeframe string) (price float64, found bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT close FROM candles
		WHERE exchange = $1 AND symbol = $2 AND timeframe = $3
		ORDER BY ts DESC LIMIT 1
	`, exchange, symbol, timeframe).Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return price, true, nil
}
```

- [ ] **Step 2: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go build ./...`. Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add strategist/internal/storage/marketdata.go
git commit -m "feat(strategist): read the latest candle close price"
```

---

### Task 4: Persist decisions

**Files:**
- Create: `strategist/internal/storage/decisions.go`

**Interfaces:**
- Consumes: `storage.Store` (Task 1).
- Produces: `storage.Decision{ID, AnalysisRunID, Asset, Side, Confidence, SizingPct, Rationale, ProposedQuantity, ProposedValue, RiskAllowed *bool, RiskReasons []string, CreatedAt}`, `(*Store).SaveDecision(ctx, d Decision) error` — consumed by Task 7's CLI. `(*Store).DecisionsForTest`/`DeleteDecisionsForRunForTest` — consumed by Task 8's integration test.

- [ ] **Step 1: Write `internal/storage/decisions.go`**

```go
// strategist/internal/storage/decisions.go
package storage

import (
	"context"
	"encoding/json"
	"time"
)

// Decision is one persisted strategist_decisions row: the LLM's decision
// (Side/Confidence/SizingPct/Rationale) plus the sized proposal and the
// risk-engine's verdict on it. RiskAllowed is nil when Side is "hold"
// (risk.Evaluate is never called) or when risk.Evaluate itself failed —
// the LLM's decision is persisted either way.
type Decision struct {
	ID               string
	AnalysisRunID    string
	Asset            string
	Side             string
	Confidence       float64
	SizingPct        float64
	Rationale        string
	ProposedQuantity float64
	ProposedValue    float64
	RiskAllowed      *bool
	RiskReasons      []string
	CreatedAt        time.Time
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
			 proposed_quantity, proposed_value, risk_allowed, risk_reasons, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, d.ID, d.AnalysisRunID, d.Asset, d.Side, d.Confidence, d.SizingPct, d.Rationale,
		d.ProposedQuantity, d.ProposedValue, d.RiskAllowed, raw, d.CreatedAt)
	return err
}

// DecisionsForTest reads persisted decisions for a run, in insertion
// order — used by tests.
func (s *Store) DecisionsForTest(ctx context.Context, runID string) ([]Decision, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, analysis_run_id, asset, side, confidence, sizing_pct, rationale,
		       proposed_quantity, proposed_value, risk_allowed, risk_reasons, created_at
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
			&d.Rationale, &d.ProposedQuantity, &d.ProposedValue, &d.RiskAllowed, &reasonsRaw, &d.CreatedAt); err != nil {
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

- [ ] **Step 2: Verify it builds**

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go build ./...`. Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add strategist/internal/storage/decisions.go
git commit -m "feat(strategist): persist strategist_decisions"
```

---

### Task 5: LLM client with tool use

**Files:**
- Create: `strategist/internal/llm/client.go`
- Test: `strategist/internal/llm/client_test.go`

**Interfaces:**
- Consumes: `github.com/anthropics/anthropic-sdk-go` (pinned below).
- Produces: `llm.Decision{Side, Confidence, SizingPct, Rationale}`, `llm.Client` interface with `Decide(ctx, systemPrompt, userPrompt string) (Decision, error)`, `llm.AnthropicClient` (implements `llm.Client`) with constructor `llm.NewAnthropicClient() *AnthropicClient` — consumed by Task 6's orchestration (via the interface) and Task 7's CLI (via the constructor). Task 8's integration test implements `llm.Client` with a fake.

- [ ] **Step 1: Pin the Anthropic SDK**

```bash
COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go get github.com/anthropics/anthropic-sdk-go@v1.9.0
```

Do **not** run this without `@v1.9.0` — see Global Constraints.

- [ ] **Step 2: Write `internal/llm/client.go`**

```go
// strategist/internal/llm/client.go
package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	model     = "claude-sonnet-5"
	maxTokens = 512
	toolName  = "record_decision"
)

// Decision is the strategist's structured output for one asset. Side is
// "buy", "sell", or "hold"; SizingPct is only meaningful when Side isn't
// "hold".
type Decision struct {
	Side       string  `json:"side"`
	Confidence float64 `json:"confidence"`
	SizingPct  float64 `json:"sizing_pct"`
	Rationale  string  `json:"rationale"`
}

// Client asks the LLM to decide what to do about one asset, given a
// prompt describing its current indicators/narratives.
type Client interface {
	Decide(ctx context.Context, systemPrompt, userPrompt string) (Decision, error)
}

// AnthropicClient is the production implementation. It uses tool use
// (forced via ToolChoice) to get a structured response instead of
// parsing free text.
type AnthropicClient struct {
	client anthropic.Client
}

// NewAnthropicClient reads its API key from ANTHROPIC_API_KEY, per the
// SDK's default credential resolution.
func NewAnthropicClient() *AnthropicClient {
	return &AnthropicClient{client: anthropic.NewClient()}
}

var decisionTool = anthropic.ToolUnionParamOfTool(
	anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"side": map[string]any{
				"type":        "string",
				"enum":        []string{"buy", "sell", "hold"},
				"description": "The proposed action for this asset.",
			},
			"confidence": map[string]any{
				"type":        "number",
				"minimum":     0,
				"maximum":     1,
				"description": "How confident the decision is, from 0 (a guess) to 1 (very confident).",
			},
			"sizing_pct": map[string]any{
				"type":        "number",
				"minimum":     0,
				"maximum":     1,
				"description": "Fraction of total portfolio value to allocate to this trade. Ignored when side is hold.",
			},
			"rationale": map[string]any{
				"type":        "string",
				"description": "A short (2-4 sentence) explanation for the decision.",
			},
		},
		Required: []string{"side", "confidence", "sizing_pct", "rationale"},
	},
	toolName,
)

func (c *AnthropicClient) Decide(ctx context.Context, systemPrompt, userPrompt string) (Decision, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools:      []anthropic.ToolUnionParam{decisionTool},
		ToolChoice: anthropic.ToolChoiceParamOfTool(toolName),
	})
	if err != nil {
		return Decision{}, fmt.Errorf("llm: decide: %w", err)
	}
	return decisionFromResponse(resp)
}

func decisionFromResponse(resp *anthropic.Message) (Decision, error) {
	if resp == nil {
		return Decision{}, fmt.Errorf("llm: decide: empty response")
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Decision{}, fmt.Errorf("llm: decide: model refused the request")
	}
	for _, block := range resp.Content {
		toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok || toolUse.Name != toolName {
			continue
		}
		var d Decision
		if err := json.Unmarshal(toolUse.Input, &d); err != nil {
			return Decision{}, fmt.Errorf("llm: decide: unmarshal tool input: %w", err)
		}
		if d.Side != "buy" && d.Side != "sell" && d.Side != "hold" {
			return Decision{}, fmt.Errorf("llm: decide: unexpected side %q", d.Side)
		}
		return d, nil
	}
	return Decision{}, fmt.Errorf("llm: decide: no %s tool call in response", toolName)
}
```

- [ ] **Step 3: Write `internal/llm/client_test.go`**

`decisionFromResponse` is the boundary that parses untrusted LLM output —
it's the one piece of this task worth testing directly (mirrors
`narrativeFromResponse` in `analysis/internal/llm/client_test.go`).

```go
// strategist/internal/llm/client_test.go
package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func toolUseBlock(t *testing.T, name string, input any) anthropic.ContentBlockUnion {
	t.Helper()
	inputRaw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	raw, err := json.Marshal(map[string]any{
		"type": "tool_use", "id": "toolu_test", "name": name, "input": json.RawMessage(inputRaw),
	})
	if err != nil {
		t.Fatalf("marshal block: %v", err)
	}
	var block anthropic.ContentBlockUnion
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("unmarshal block: %v", err)
	}
	return block
}

func TestDecisionFromResponse_ParsesToolUse(t *testing.T) {
	resp := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			toolUseBlock(t, toolName, map[string]any{
				"side": "buy", "confidence": 0.7, "sizing_pct": 0.1, "rationale": "uptrend",
			}),
		},
	}

	got, err := decisionFromResponse(resp)
	if err != nil {
		t.Fatalf("decisionFromResponse: %v", err)
	}
	if got.Side != "buy" || got.Confidence != 0.7 || got.SizingPct != 0.1 || got.Rationale != "uptrend" {
		t.Fatalf("decision = %+v, want the seeded fields", got)
	}
}

func TestDecisionFromResponse_RefusalIsFailure(t *testing.T) {
	resp := &anthropic.Message{StopReason: anthropic.StopReasonRefusal}

	_, err := decisionFromResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error = %v, want refusal error", err)
	}
}

func TestDecisionFromResponse_RejectsMissingOrInvalid(t *testing.T) {
	cases := map[string]*anthropic.Message{
		"nil response": nil,
		"no blocks":    {},
		"wrong tool name": {
			Content: []anthropic.ContentBlockUnion{toolUseBlock(t, "some_other_tool", map[string]any{
				"side": "buy", "confidence": 0.5, "sizing_pct": 0.1, "rationale": "x",
			})},
		},
		"invalid side": {
			Content: []anthropic.ContentBlockUnion{toolUseBlock(t, toolName, map[string]any{
				"side": "yolo", "confidence": 0.5, "sizing_pct": 0.1, "rationale": "x",
			})},
		},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decisionFromResponse(resp); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go test ./internal/llm/... -v`
Expected: PASS for all four tests (the table test reports 4 subtests).

- [ ] **Step 5: Commit**

```bash
git add strategist/internal/llm/client.go strategist/internal/llm/client_test.go strategist/go.mod strategist/go.sum
git commit -m "feat(strategist): Anthropic LLM client with tool-use decisions"
```

---

### Task 6: Decision orchestration

**Files:**
- Create: `strategist/internal/strategist/decide.go`
- Test: `strategist/internal/strategist/decide_test.go`

**Interfaces:**
- Consumes: `storage.AgentResult` (Task 2), `llm.Client`/`llm.Decision` (Task 5), `risk.ProposedOperation`/`risk.Side`/`risk.PortfolioState`/`risk.EvalOptions`/`risk.Evaluate`/`risk.Decision` (from `risk-engine/risk`, already built in sub-project 2).
- Produces: `strategist.Outcome{Decision, Quantity, Value, Risk *risk.Decision, RiskErr error}`, `strategist.Decide(ctx, riskStore *riskstorage.Store, client llm.Client, asset string, perAsset []storage.AgentResult, riskContext storage.AgentResult, portfolio risk.PortfolioState, portfolioValue, price float64) (Outcome, error)` — consumed by Task 7's CLI.

This is the task with real business logic (prompt assembly, sizing math,
the risk.Evaluate call and its error handling) — it gets a direct unit
test per the project's reduced-but-targeted test policy, unlike the thin
storage wrappers in Tasks 2-4.

- [ ] **Step 1: Write `internal/strategist/decide.go`**

```go
// strategist/internal/strategist/decide.go
package strategist

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"strategist/internal/llm"
	"strategist/internal/storage"
)

const systemPrompt = `Você é um estrategista de investimentos em criptomoedas. Você recebe indicadores técnicos, de derivativos, de notícias e de contexto de risco sobre um ativo, e decide se deve comprar, vender ou manter a posição atual. Nunca sugira sizing_pct acima de 0.25 (25% do portfólio) numa única operação. Responda sempre usando a ferramenta record_decision.`

// Outcome is what deciding on one asset produces. Risk is nil when
// Decision.Side is "hold" (risk.Evaluate is never called for a hold) or
// when risk.Evaluate itself failed — RiskErr is set in that second case
// so the caller can log it while still persisting the LLM's decision.
type Outcome struct {
	Decision llm.Decision
	Quantity float64
	Value    float64
	Risk     *risk.Decision
	RiskErr  error
}

// Decide asks the LLM for a decision about asset from its three per-asset
// analysis results (technical, derivatives, news) plus the shared
// risk-context result, then — for buy/sell — sizes the proposed operation
// against portfolioValue/price and validates it via risk.Evaluate.
//
// Returns an error only when there is no decision at all to persist:
// missing analysis data for asset, or an LLM failure. A risk.Evaluate
// failure is reported through Outcome.RiskErr instead of the return
// error, since the LLM's decision is still worth keeping in that case.
func Decide(
	ctx context.Context,
	riskStore *riskstorage.Store,
	client llm.Client,
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

	decision, err := client.Decide(ctx, systemPrompt, userPrompt)
	if err != nil {
		return Outcome{}, fmt.Errorf("strategist: %s: decide: %w", asset, err)
	}

	outcome := Outcome{Decision: decision}
	if decision.Side == "hold" {
		return outcome, nil
	}

	outcome.Quantity = decision.SizingPct * portfolioValue / price
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

- [ ] **Step 2: Write `internal/strategist/decide_test.go`**

```go
// strategist/internal/strategist/decide_test.go
package strategist

import (
	"context"
	"errors"
	"strings"
	"testing"

	"risk-engine/risk"

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

func validPerAsset() []storage.AgentResult {
	return []storage.AgentResult{
		{AgentType: "technical", Asset: "BTC", Narrative: "n1"},
		{AgentType: "derivatives", Asset: "BTC", Narrative: "n2"},
		{AgentType: "news", Asset: "BTC", Narrative: "n3"},
	}
}

func TestDecide_HoldNeverCallsRiskEvaluate(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "hold", Rationale: "wait and see"}}

	// riskStore is nil on purpose: a hold must return before touching it —
	// passing nil here turns any accidental risk.Evaluate call into an
	// immediate nil-pointer panic, which is exactly the assertion we want.
	outcome, err := Decide(context.Background(), nil, client, "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if outcome.Risk != nil || outcome.RiskErr != nil {
		t.Errorf("outcome = %+v, want no risk evaluation for a hold", outcome)
	}
	if outcome.Quantity != 0 || outcome.Value != 0 {
		t.Errorf("outcome = %+v, want zero quantity/value for a hold", outcome)
	}
}

func TestDecide_LLMFailureReturnsError(t *testing.T) {
	client := &fakeLLMClient{err: errors.New("rate limited")}

	_, err := Decide(context.Background(), nil, client, "BTC", validPerAsset(), storage.AgentResult{}, riskPortfolioState(), 10000, 100)
	if err == nil {
		t.Fatal("expected an error when the LLM call fails, got nil")
	}
}

func TestDecide_MissingAnalysisDataReturnsErrorBeforeCallingLLM(t *testing.T) {
	client := &fakeLLMClient{decision: llm.Decision{Side: "hold"}}
	incomplete := []storage.AgentResult{{AgentType: "technical", Asset: "BTC", Narrative: "n1"}}

	if _, err := Decide(context.Background(), nil, client, "BTC", incomplete, storage.AgentResult{}, riskPortfolioState(), 10000, 100); err == nil {
		t.Fatal("expected an error for incomplete analysis data, got nil")
	}
}

func riskPortfolioState() risk.PortfolioState {
	return risk.PortfolioState{}
}
```

- [ ] **Step 3: Resolve the new `risk-engine` import and run the tests**

This is the first task in this module that imports `risk-engine/risk` —
`go.mod`'s `replace` directive already points at `../risk-engine`, but
the `require risk-engine v0.0.0` line still needs to be added:

```bash
COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go mod tidy
COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go test ./internal/strategist/... -v
```

Expected: `go mod tidy` adds `require risk-engine v0.0.0` to `go.mod`
without changing the Go version. Tests: PASS for all 5 tests.
(`TestDecide_HoldNeverCallsRiskEvaluate` and the other `Decide` tests pass
a nil `*riskstorage.Store` — that's safe here specifically because both
scenarios return before `risk.Evaluate` would dereference it; if either
test starts failing with a nil-pointer panic instead of the expected
outcome, that panic **is** the test catching a real bug, not a fixture
problem.)

- [ ] **Step 4: Commit**

```bash
git add strategist/internal/strategist/decide.go strategist/internal/strategist/decide_test.go strategist/go.mod strategist/go.sum
git commit -m "feat(strategist): decision orchestration — prompt, sizing, risk.Evaluate"
```

---

### Task 7: CLI

**Files:**
- Create: `strategist/cmd/strategist/main.go`
- Test: `strategist/cmd/strategist/flags_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2-6 (`storage.Store`, `riskstorage.Store`, `llm.NewAnthropicClient`, `strategist.Decide`, `strategist.Outcome`).
- Produces: the `strategist` binary, and `Run(ctx, store, riskStore, client, runID string, assets []string, timeframe string, cash float64, positions map[string]float64, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) error` — exported so Task 8's integration test can call it directly with a fake `llm.Client`. Also `parsePositions(string) (map[string]float64, error)` — tested directly in this task (pure function, no DB/LLM).

- [ ] **Step 1: Pin `google/uuid`**

```bash
COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go get github.com/google/uuid@v1.6.0
```

- [ ] **Step 2: Write `cmd/strategist/main.go`**

```go
// strategist/cmd/strategist/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"strategist/internal/llm"
	"strategist/internal/storage"
	"strategist/internal/strategist"
)

func main() {
	runID := flag.String("run-id", "", "analysis_run_id to decide from (required)")
	assetsStr := flag.String("assets", "", "comma-separated asset symbols to decide on (required)")
	timeframe := flag.String("timeframe", "1h", "timeframe used to look up the current price")
	cash := flag.Float64("cash", 0, "cash available, in USD (required)")
	positionsStr := flag.String("positions", "", "comma-separated SYMBOL:quantity current positions")
	dailyLoss := flag.Float64("daily-loss", 0, "portfolio daily loss so far, as a fraction (e.g. 0.02 = 2%)")
	weeklyLoss := flag.Float64("weekly-loss", 0, "portfolio weekly loss so far, as a fraction")
	drawdown := flag.Float64("drawdown", 0, "portfolio drawdown from peak, as a fraction")
	consecutiveLosses := flag.Int("consecutive-losses", 0, "number of consecutive losing trades")
	flag.Parse()

	if err := run(context.Background(), *runID, *assetsStr, *timeframe, *cash, *positionsStr, *dailyLoss, *weeklyLoss, *drawdown, *consecutiveLosses); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, runID, assetsStr, timeframe string, cash float64, positionsStr string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) error {
	if runID == "" {
		return fmt.Errorf("-run-id is required")
	}
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
	}
	positions, err := parsePositions(positionsStr)
	if err != nil {
		return err
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

	client := llm.NewAnthropicClient()
	return Run(ctx, store, riskStore, client, runID, assets, timeframe, cash, positions, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
}

// Run decides on every requested asset and persists the outcome. It
// returns an error only for a whole-run failure (can't read analysis
// results, can't price a held position); a single asset failing to
// decide is logged to stderr and skipped, not returned as an error.
func Run(
	ctx context.Context,
	store *storage.Store,
	riskStore *riskstorage.Store,
	client llm.Client,
	runID string,
	assets []string,
	timeframe string,
	cash float64,
	positions map[string]float64,
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

	portfolio, portfolioValue, err := buildPortfolio(ctx, store, positions, cash, timeframe, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
	if err != nil {
		return fmt.Errorf("build portfolio: %w", err)
	}

	for _, asset := range assets {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, asset, timeframe)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: read price: %v\n", asset, err)
			continue
		}
		if !found {
			fmt.Fprintf(os.Stderr, "%s: no price data, skipping\n", asset)
			continue
		}

		outcome, err := strategist.Decide(ctx, riskStore, client, asset, byAsset[asset], riskContext, portfolio, portfolioValue, price)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", asset, err)
			continue
		}
		if outcome.RiskErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", outcome.RiskErr)
		}
		if err := save(ctx, store, runID, asset, outcome); err != nil {
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
// (cash + all position values) for sizing.
func buildPortfolio(ctx context.Context, store *storage.Store, positions map[string]float64, cash float64, timeframe string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) (risk.PortfolioState, float64, error) {
	riskPositions := make(map[string]risk.Position, len(positions))
	total := cash
	for symbol, qty := range positions {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, symbol, timeframe)
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
	return portfolio, total, nil
}

func save(ctx context.Context, store *storage.Store, runID, asset string, outcome strategist.Outcome) error {
	d := storage.Decision{
		ID: uuid.NewString(), AnalysisRunID: runID, Asset: asset,
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
	return store.SaveDecision(ctx, d)
}

func report(asset string, outcome strategist.Outcome) {
	if outcome.Decision.Side == "hold" {
		fmt.Printf("%s: hold — %s\n", asset, outcome.Decision.Rationale)
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
	fmt.Printf("%s: %s %.6f (%s) — %s\n", asset, outcome.Decision.Side, outcome.Quantity, status, outcome.Decision.Rationale)
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

func parsePositions(value string) (map[string]float64, error) {
	positions := make(map[string]float64)
	for _, entry := range splitNonEmpty(value) {
		symbol, qtyStr, found := strings.Cut(entry, ":")
		symbol = strings.TrimSpace(symbol)
		qtyStr = strings.TrimSpace(qtyStr)
		if !found || symbol == "" || qtyStr == "" {
			return nil, fmt.Errorf("invalid -positions entry %q (want SYMBOL:quantity)", entry)
		}
		qty, err := strconv.ParseFloat(qtyStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid -positions entry %q: %w", entry, err)
		}
		positions[symbol] = qty
	}
	return positions, nil
}
```

- [ ] **Step 3: Write `cmd/strategist/flags_test.go`**

```go
// strategist/cmd/strategist/flags_test.go
package main

import "testing"

func TestParsePositions(t *testing.T) {
	positions, err := parsePositions("BTC:0.5, ETH:2")
	if err != nil {
		t.Fatalf("parsePositions: %v", err)
	}
	if positions["BTC"] != 0.5 || positions["ETH"] != 2 {
		t.Fatalf("positions = %#v, want BTC:0.5 ETH:2", positions)
	}
	if _, err := parsePositions("BTC"); err == nil {
		t.Fatal("expected an error for a malformed entry, got nil")
	}
	if _, err := parsePositions("BTC:notanumber"); err == nil {
		t.Fatal("expected an error for a non-numeric quantity, got nil")
	}
}

func TestParsePositions_Empty(t *testing.T) {
	positions, err := parsePositions("")
	if err != nil {
		t.Fatalf("parsePositions: %v", err)
	}
	if len(positions) != 0 {
		t.Fatalf("positions = %#v, want empty", positions)
	}
}
```

- [ ] **Step 4: Verify it builds and the flag tests pass**

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go build ./...`
Expected: no errors.

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go test ./cmd/strategist/... -v -run TestParsePositions`
Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add strategist/cmd/strategist/main.go strategist/cmd/strategist/flags_test.go strategist/go.mod strategist/go.sum
git commit -m "feat(strategist): CLI wiring analysis results, pricing, and decisions"
```

---

### Task 8: End-to-end integration test

**Files:**
- Test: `strategist/cmd/strategist/main_test.go`

**Interfaces:**
- Consumes: `Run` (Task 7, exported for this reason), `storage.New`/`ResultsForRun` (Tasks 1-2, seeded directly by SQL in this test), `storage.DecisionsForTest`/`DeleteDecisionsForRunForTest` (Task 4), `riskstorage.New` (`risk-engine/storage`), `llm.Client` (Task 5, implemented by a local fake).

This is the other piece of complex/orchestration logic worth a direct
test: a real run end to end, against the real database and the real
`risk.Evaluate`, with only the LLM faked out.

- [ ] **Step 1: Write `cmd/strategist/main_test.go`**

```go
// strategist/cmd/strategist/main_test.go
package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"

	riskstorage "risk-engine/storage"

	"strategist/internal/llm"
	"strategist/internal/storage"
)

type fakeLLMClient struct {
	decision llm.Decision
}

func (f *fakeLLMClient) Decide(context.Context, string, string) (llm.Decision, error) {
	return f.decision, nil
}

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

// seedAnalysisRun writes a minimal analysis_runs + analysis_results
// fixture directly via SQL (this module never writes those tables in
// production — they belong to the analysis module — so a test-only
// insert here is the right way to set up a fixture, not a shortcut
// around a missing helper).
func seedAnalysisRun(t *testing.T, store *storage.Store, runID, asset string, includeAllThreeAgents bool) {
	t.Helper()
	execSQL(t, store, `INSERT INTO analysis_runs (id, started_at, timeframe, status) VALUES ($1, now(), '1h', 'completed')`, runID)
	t.Cleanup(func() {
		execSQLIgnoreError(store, `DELETE FROM analysis_results WHERE run_id = $1`, runID)
		execSQLIgnoreError(store, `DELETE FROM analysis_runs WHERE id = $1`, runID)
	})

	agentTypes := []string{"technical", "derivatives", "news"}
	if !includeAllThreeAgents {
		agentTypes = agentTypes[:1]
	}
	for _, agentType := range agentTypes {
		execSQL(t, store, `
			INSERT INTO analysis_results (id, run_id, agent_type, asset, indicators, narrative, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
		`, uuid.NewString(), runID, agentType, asset, jsonObj(t, map[string]any{"seeded": true}), agentType+" narrative for "+asset)
	}
	execSQL(t, store, `
		INSERT INTO analysis_results (id, run_id, agent_type, asset, indicators, narrative, created_at)
		VALUES ($1, $2, 'risk_context', '', $3, 'risk context narrative', now())
	`, uuid.NewString(), runID, jsonObj(t, map[string]any{"risk_status": "normal"}))
}

// seedCandle inserts one candle at ts=now() and registers its own cleanup.
// Always call this with a clearly-fake symbol (see the TESTASSET*
// constants below), never a real one like "BTC" — this candle would
// otherwise become the *latest* row for that symbol (ts=now()) in the
// shared dev TimescaleDB for as long as the test is running, ahead of any
// real collected data, and could confuse a concurrent manual run of
// cmd/analysis or cmd/strategist against that symbol.
func seedCandle(t *testing.T, store *storage.Store, symbol string, price float64) {
	t.Helper()
	execSQL(t, store, `
		INSERT INTO candles (exchange, symbol, timeframe, ts, open, high, low, close, volume)
		VALUES ('binance', $1, '1h', now(), $2, $2, $2, $2, 100)
		ON CONFLICT (exchange, symbol, timeframe, ts) DO NOTHING
	`, symbol, price)
	t.Cleanup(func() {
		execSQLIgnoreError(store, `DELETE FROM candles WHERE exchange = 'binance' AND symbol = $1`, symbol)
	})
}

// Fake asset symbols for these tests — never real ones (see seedCandle).
const (
	testAssetBuy        = "TESTASSETBUY"
	testAssetHold       = "TESTASSETHOLD"
	testAssetIncomplete = "TESTASSETINCOMPLETE"
)

// execSQL is a tiny helper so fixtures above can run arbitrary SQL through
// the *storage.Store without a dedicated exported method — storage.Store
// only exposes the reads/writes production code needs, not a generic
// query escape hatch, so tests reach the pool through this instead.
func execSQL(t *testing.T, store *storage.Store, sql string, args ...any) {
	t.Helper()
	if err := storage.ExecForTest(context.Background(), store, sql, args...); err != nil {
		t.Fatalf("seed SQL %q: %v", sql, err)
	}
}

// execSQLIgnoreError is for best-effort cleanup in t.Cleanup callbacks —
// mirrors analysis/internal/storage/runs.go's DeleteRunForTest, which
// discards its Exec errors the same way, since a cleanup failure
// shouldn't mask the actual test failure it ran after.
func execSQLIgnoreError(store *storage.Store, sql string, args ...any) {
	_ = storage.ExecForTest(context.Background(), store, sql, args...)
}

func jsonObj(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestRun_BuyDecisionIsValidatedAndPersisted(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID := uuid.NewString()
	seedAnalysisRun(t, store, runID, testAssetBuy, true)
	seedCandle(t, store, testAssetBuy, 50000)
	t.Cleanup(func() {
		store.DeleteDecisionsForRunForTest(ctx, runID)
	})

	client := &fakeLLMClient{decision: llm.Decision{Side: "buy", Confidence: 0.8, SizingPct: 0.1, Rationale: "uptrend"}}
	if err := Run(ctx, store, riskStore, client, runID, []string{testAssetBuy}, "1h", 10000, nil, 0, 0, 0, 0); err != nil {
		t.Fatalf("Run: %v", err)
	}

	decisions, err := store.DecisionsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("DecisionsForTest: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	d := decisions[0]
	if d.Side != "buy" || d.Asset != testAssetBuy {
		t.Errorf("decision = %+v, want side=buy asset=%s", d, testAssetBuy)
	}
	if d.RiskAllowed == nil {
		t.Error("RiskAllowed is nil, want risk.Evaluate to have run and recorded a verdict")
	}
	wantQuantity := 0.1 * 10000 / 50000
	if d.ProposedQuantity != wantQuantity {
		t.Errorf("ProposedQuantity = %v, want %v (sizing_pct * portfolio value / price)", d.ProposedQuantity, wantQuantity)
	}
}

func TestRun_HoldSkipsRiskEvaluateButIsPersisted(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID := uuid.NewString()
	seedAnalysisRun(t, store, runID, testAssetHold, true)
	seedCandle(t, store, testAssetHold, 3000)
	t.Cleanup(func() {
		store.DeleteDecisionsForRunForTest(ctx, runID)
	})

	client := &fakeLLMClient{decision: llm.Decision{Side: "hold", Rationale: "no clear signal"}}
	if err := Run(ctx, store, riskStore, client, runID, []string{testAssetHold}, "1h", 10000, nil, 0, 0, 0, 0); err != nil {
		t.Fatalf("Run: %v", err)
	}

	decisions, err := store.DecisionsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("DecisionsForTest: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	if decisions[0].Side != "hold" || decisions[0].RiskAllowed != nil {
		t.Errorf("decision = %+v, want side=hold and RiskAllowed=nil", decisions[0])
	}
}

func TestRun_IncompleteAnalysisSkipsAssetWithoutPersisting(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID := uuid.NewString()
	seedAnalysisRun(t, store, runID, testAssetIncomplete, false) // only "technical" seeded
	seedCandle(t, store, testAssetIncomplete, 150)
	t.Cleanup(func() {
		store.DeleteDecisionsForRunForTest(ctx, runID)
	})

	client := &fakeLLMClient{decision: llm.Decision{Side: "hold"}}
	if err := Run(ctx, store, riskStore, client, runID, []string{testAssetIncomplete}, "1h", 10000, nil, 0, 0, 0, 0); err != nil {
		t.Fatalf("Run: %v", err)
	}

	decisions, err := store.DecisionsForTest(ctx, runID)
	if err != nil {
		t.Fatalf("DecisionsForTest: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("len(decisions) = %d, want 0 (incomplete analysis data must not produce an implicit decision)", decisions)
	}
}

func TestRun_MissingHeldPositionPriceFailsTheWholeRun(t *testing.T) {
	store, riskStore := testStores(t)
	ctx := context.Background()
	runID := uuid.NewString()
	seedAnalysisRun(t, store, runID, testAssetBuy, true)
	seedCandle(t, store, testAssetBuy, 50000)
	t.Cleanup(func() {
		store.DeleteDecisionsForRunForTest(ctx, runID)
	})

	client := &fakeLLMClient{decision: llm.Decision{Side: "hold"}}
	positions := map[string]float64{"TESTASSETNOPRICE": 1}
	err := Run(ctx, store, riskStore, client, runID, []string{testAssetBuy}, "1h", 10000, positions, 0, 0, 0, 0)
	if err == nil {
		t.Fatal("expected an error when a held position has no price data, got nil")
	}
}
```

- [ ] **Step 2: Add `storage.ExecForTest`**

The test fixtures above need to insert directly into `analysis_runs`,
`analysis_results`, and `candles` — tables this module never writes in
production. Add a minimal test-only escape hatch rather than exporting
the whole `*pgxpool.Pool`:

**Modify:** `strategist/internal/storage/db.go` — add at the end of the file:

```go
// ExecForTest runs an arbitrary statement against the store's pool — used
// only by tests to seed fixtures in tables this module never writes in
// production (analysis_runs, analysis_results, candles all belong to
// other modules). Never call this from production code.
func ExecForTest(ctx context.Context, s *Store, sql string, args ...any) error {
	_, err := s.pool.Exec(ctx, sql, args...)
	return err
}
```

- [ ] **Step 3: Run the tests**

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go test ./cmd/strategist/... -v`
Expected: PASS for all 4 tests (`TEST_DATABASE_URL` is already set inside
the container per the docker-compose environment from Task 1).

- [ ] **Step 4: Run the full module test suite**

Run: `COMPOSE_PROJECT_NAME=strategist-dev docker compose exec go go test ./... -v`
Expected: all tests pass (llm, strategist, cmd/strategist).

- [ ] **Step 5: Commit**

```bash
git add strategist/cmd/strategist/main_test.go strategist/internal/storage/db.go
git commit -m "test(strategist): end-to-end integration tests with a fake LLM client"
```

---

## Execution Handoff

After Task 8, before merging: run a final review of the whole branch
against `docs/superpowers/specs/2026-08-18-strategist-design.md` (not just
task-by-task), then write a follow-up test checklist for any remaining
coverage gaps — same format and purpose as
`docs/superpowers/plans/2026-08-18-analysis-agents-TEST-CHECKLIST.md` did
for sub-project 4 — for a separate TDD pass. This is not a task in this
plan; it happens after execution completes, same as it did last time.
