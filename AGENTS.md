# AGENTS.md — investment-platform

Personal, self-hosted autonomous crypto investment platform. Decomposed
into 10 independent sub-projects (9 complete, 1 in progress as of
2026-08-19) — see `docs/superpowers/specs/` and `docs/superpowers/plans/`
for the design/implementation history of every sub-project, one pair of
`YYYY-MM-DD-<name>.md` files each. Read the relevant spec/plan before
touching a module you haven't worked in yet — they contain the actual
reasoning behind non-obvious decisions, not just what was built.

## Module structure

Each capability is its own Go module at the repo root: `market-data`,
`risk-engine`, `simulation`, `analysis`, `strategist`, `mcp`,
`execution`, `web-api`, `tracking` (as of sub-project 10). `frontend` is
the one non-Go module (React + Vite). Every Go module has:

- Its own `go.mod` (module name matches the directory name, e.g.
  `module strategist`).
- Its own `docker-compose.yml` — a `golang:1.22` (or `1.23` for `mcp`,
  the only module on a newer Go version) dev-shell container bind-mounting
  the module directory, connected to the shared `market-data_default`
  Docker network for Postgres/TimescaleDB access.
- Its own `migrations/NNN_name.sql` files (see Database below) and its
  own `internal/storage` package.
- A `cmd/<module-or-binary-name>/main.go` entrypoint (CLI or long-running
  server, depending on the module).

Sibling-module Go dependencies use `go.mod`'s `replace` directive
(`replace execution => ../execution`) **and** a matching
`docker-compose.yml` volume mount (`../execution:/execution`) — both are
required together, the `replace` alone doesn't make the source available
inside the container.

## Cross-module rules

**A Go module can never import another module's `internal/...` package**,
no matter what `replace` says — Go enforces this on the literal
import-path text, independent of module boundaries (`replace` only
affects version/source resolution for an already-legal import). Two
patterns exist for one module to use another's capability, pick based on
what's actually needed:

1. **Orchestration (calling into another module's business logic):** the
   target module exposes a public `runner` package with a
   `RunWithDSN(ctx, dsn string, ...) (...)` function that connects its own
   storage internally from a plain `dsn string` and returns only
   primitive types (or internal types the caller only ever touches via
   `:=`/`range` field inference, never named explicitly). See
   `analysis/runner`, `strategist/runner`, `simulation/runner`, and how
   `mcp` calls them.
2. **Pure data reads (just need a value from another module's table):**
   read the table directly via raw SQL from your own `internal/storage`
   package — no Go dependency on the owning module at all. See
   `strategist/internal/storage.LatestPrice` (reads `market-data`'s
   `candles` table) or any of `web-api`'s 6+ read endpoints (reads
   `analysis`/`strategist`/`risk-engine`/`execution`/`simulation`/
   `tracking`'s tables, imports none of those modules).
3. **Reusing an already-public API/client (not tied to a specific
   module's internal storage):** a normal Go import is fine — e.g.
   `strategist` and `tracking` both import `execution/executor` (a public
   package with no `internal/` involved) to reuse the authenticated
   Binance client instead of rebuilding it.

**Adding a new dependency to a module other modules import (even
transitively) can break their build even with zero source changes to
them** — Go's module pruning (1.17+) requires every module that
transitively imports a package to declare it in its own `go.mod`/
`go.sum`. After adding a dependency to `analysis`/`strategist`/
`execution` (all imported by `mcp` via their `runner`/`executor`
packages), run `go mod tidy` in `mcp` too and confirm `go build ./...`
there, even if you didn't touch a single `mcp` source line.

## Database

One shared Postgres/TimescaleDB instance (container
`market-data-timescaledb-1`, database `marketdata`, user `marketdata`)
used by every module — each module owns its own tables via its own
`migrations/NNN_name.sql`, no schema-per-module separation. Apply a new
migration with:

```bash
docker exec -i market-data-timescaledb-1 psql -U marketdata -d marketdata < migrations/NNN_name.sql
```

run from the module's own directory on the host (not inside the module's
own `go` dev container — this `docker exec` targets the Postgres
container directly). Verify with
`docker exec market-data-timescaledb-1 psql -U marketdata -d marketdata -c '\d table_name'`.

## Docker Compose workflow

- Bring a module's dev shell up: `cd <module> && docker compose up -d`.
  Run Go commands inside it: `docker compose exec go go build ./...` /
  `go test ./... -v -count=1`.
- **Always pass an explicit `COMPOSE_PROJECT_NAME`** if there's any
  chance of a directory-basename collision (e.g. working in more than one
  checkout/worktree of this repo at once) — `docker compose` derives its
  default project name from the current directory's basename, and two
  checkouts with the same module directory name can silently bind to
  whichever one's `up` ran last.
- **`docker compose exec go pwd` does NOT prove the bind mount is
  correct** — it always prints the in-container path (`/app`), identical
  regardless of which host directory is actually mounted there. The real
  check: `docker inspect <container> --format '{{range .Mounts}}{{.Source}} -> {{.Destination}}{{"\n"}}{{end}}'`
  — compare `.Source` against the host directory you think you're
  testing.
- **This machine has limited resources for many simultaneous per-module
  containers.** Bring up only what you're actively working on; run
  `docker compose down` for a module's container when you're done with
  it, not just at the very end of a session. If containers get killed
  environment-wide (resource pressure), `docker start` alone is often not
  enough to recover — the network attachment can come back stale
  (`lookup timescaledb: server misbehaving`); use
  `docker compose down && docker compose up -d` (full recreate) instead.

## Testing conventions

- **Reduced rigor, deliberately.** Direct/unit tests only for real
  business logic with genuine silent-failure risk (parsing, signing,
  clamping, state machines, error-path branching). Don't chase exhaustive
  coverage on thin wrapper code.
- **Storage-layer tests** read `TEST_DATABASE_URL` from the environment
  (already set in every module's `docker-compose.yml`, same value as
  `DATABASE_URL`) and `t.Skip()` cleanly if it's unset — never fail a
  test suite just because a database isn't reachable in some environment.
- **No automated test calls a real external API** (Binance, Anthropic,
  OpenAI) — those integrations get a fake/interface at the boundary
  (`executor.Client`, `llm.Client`, `binanceOps`, etc.) for unit tests,
  and a real live-API check happens manually, once, as part of finishing
  a sub-project that touches one — never as part of the automated suite.
- HTTP handlers (`web-api`) get tested via `httptest` against a fake
  `dataStore`, never a real database.

## Frontend conventions (`frontend/`)

React + Vite + TypeScript, no router/chart/state-management library —
tab-based navigation via plain `useState`, polling via a small custom
hook (`usePolling`), and a hand-rolled ~30-line SVG line chart
(`EquityCurveChart`) cover everything needed so far. Keep it that way
unless a real requirement can't be met without a new dependency.

**TypeScript project-references gotcha:** `tsconfig.node.json` (the one
covering `vite.config.ts`) needs `composite: true` for `tsc -b` to work,
and a composite/referenced project **cannot** set `noEmit: true` (TS6310)
— this means `npm run build` legitimately leaves `vite.config.js`,
`vite.config.d.ts`, and `*.tsbuildinfo` sitting in `frontend/` afterward.
Don't try to suppress this; `frontend/.gitignore` already excludes all of
it (along with `node_modules/` and `dist/`) — never `git add -A` in
`frontend/` without checking that gitignore is still in place first.

## Known deferred technical debt

These are documented, deliberate, ruled-and-deferred gaps — not
oversights to silently "fix" as a drive-by while working on something
else. If a task specifically calls for addressing one, do it properly
with its own review; otherwise leave it as-is and mention it in your
report if it's relevant to what you touched.

- **No Binance `LOT_SIZE`/step-size quantity rounding** (`execution`
  module) — real orders will likely be rejected by Binance until an
  `exchangeInfo` fetch + floor-to-step is added.
- **Non-deterministic execution idempotency** — the decision ID used as
  Binance's `newClientOrderId` is a fresh UUID per run, not derived from
  `analysis_run_id`+`asset`, so a duplicate `run_strategist` call can
  place a duplicate real order. Fixing this correctly needs an
  upsert-semantics change to `strategist`'s `SaveDecision`, not a quick
  patch.
- **One unpriceable held position fails the entire strategist run**
  (`strategist/runner.buildPortfolio`) — a stray position in a symbol
  `market-data` doesn't track aborts the whole run rather than being
  skipped. Locked in by an existing test
  (`TestRun_MissingHeldPositionPriceFailsTheWholeRun`); changing this
  behavior is a deliberate design call, not a bug fix.

## Git workflow

- Commit locally whenever a logical unit of work is done — small, clear
  commit messages, no need to ask first.
- **Ask before `git push`** unless the user has explicitly said to push
  in the current conversation — this repo's `master` has been pushed in
  bulk before at the user's explicit request, but that's not a standing
  instruction to push automatically every time.
- Never force-push, never rewrite already-pushed history.
- If a task leaves you mid-plan with a module intentionally non-building
  (e.g. a later task in the same plan supplies a file an earlier task's
  code already imports), say so explicitly in your commit message or
  report — don't let a reviewer (human or agent) mistake it for a bug.
