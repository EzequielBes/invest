// Package paperstore owns the simulated ("paper") trading ledger: a cash
// + positions balance that never touches Binance, the fills that moved
// it, and the on/off switch that gates whether the pipeline is allowed to
// simulate at all. Public (unlike execution/internal/storage) because
// both the MCP server and web-api need to read/write it directly —
// the same "public-package reuse" cross-module pattern risk-engine/storage
// already established.
package paperstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"risk-engine/risk"
)

const startingCash = 10000.0

var (
	ErrInvalidActiveAgent = errors.New("paperstore: active agent must be claude_code or codex")
	ErrTestnetDisabled    = errors.New("paperstore: testnet trading is disabled")
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

// Fill is one persisted paper_fills row — the simulated counterpart of
// execution/internal/storage.Execution, minted every time paperexec's
// Client.Execute "fills" a decision.
type Fill struct {
	ID          string
	Asset       string
	Side        string
	Quantity    float64
	Price       float64
	CostBasis   *float64
	RealizedPnL *float64
	CreatedAt   time.Time
}

// Metrics are the paper ledger values supplied to the risk engine.
type Metrics struct {
	DailyLoss         float64
	WeeklyLoss        float64
	Drawdown          float64
	ConsecutiveLosses int
}

type equitySnapshot struct {
	Equity    float64
	CreatedAt time.Time
}

// AutomationControls are the switches used by an external agent loop. Enabled
// remains the paper-trading switch for compatibility with existing callers.
type AutomationControls struct {
	Enabled        bool
	TestnetEnabled bool
	ActiveAgent    string
}

// AutomationPatch updates only non-nil fields.
type AutomationPatch struct {
	Enabled        *bool
	TestnetEnabled *bool
	ActiveAgent    *string
}

func validateAutomationPatch(patch AutomationPatch) error {
	if patch.ActiveAgent != nil && *patch.ActiveAgent != "claude_code" && *patch.ActiveAgent != "codex" {
		return ErrInvalidActiveAgent
	}
	return nil
}

// Portfolio returns the current simulated cash/positions, seeding a
// fresh singleton row with startingCash the first time it's read.
func (s *Store) Portfolio(ctx context.Context) (float64, map[string]float64, error) {
	var cash float64
	var positionsRaw []byte
	err := s.pool.QueryRow(ctx, `SELECT cash, positions FROM paper_state WHERE id = 1`).Scan(&cash, &positionsRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO paper_state (id, cash, positions, enabled) VALUES (1, $1, '{}', false)
			ON CONFLICT (id) DO NOTHING
		`, startingCash); err != nil {
			return 0, nil, err
		}
		return startingCash, map[string]float64{}, nil
	}
	if err != nil {
		return 0, nil, err
	}
	positions := map[string]float64{}
	if err := json.Unmarshal(positionsRaw, &positions); err != nil {
		return 0, nil, err
	}
	return cash, positions, nil
}

// ApplyFill updates the paper cash/positions ledger for one simulated
// buy or sell and records the fill — a buy spends cash and adds to the
// position, a sell reduces the position and returns cash. decisionID is
// the strategist decision this fill closes the loop on (see
// execution/paperexec.Client.Execute, which is called with
// clientOrderID == decisionID, same convention as the real executor).
func (s *Store) ApplyFill(ctx context.Context, decisionID, asset, side string, quantity, price float64) error {
	// Ensure the singleton exists before locking it in this transaction.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO paper_state (id, cash, positions, enabled) VALUES (1, $1, '{}', false)
		ON CONFLICT (id) DO NOTHING
	`, startingCash); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var cash float64
	var positionsRaw []byte
	var costBasesRaw []byte
	var consecutiveLosses int
	err = tx.QueryRow(ctx, `
		SELECT cash, positions, cost_bases, consecutive_losses FROM paper_state WHERE id = 1 FOR UPDATE
	`).Scan(&cash, &positionsRaw, &costBasesRaw, &consecutiveLosses)
	if err != nil {
		return err
	}
	positions := map[string]float64{}
	if err := json.Unmarshal(positionsRaw, &positions); err != nil {
		return err
	}
	costBases := map[string]float64{}
	if err := json.Unmarshal(costBasesRaw, &costBases); err != nil {
		return err
	}
	value := quantity * price
	var costBasis, realizedPnL *float64
	if side == "buy" {
		oldQuantity := positions[asset]
		basis := (oldQuantity*costBases[asset] + value) / (oldQuantity + quantity)
		cash -= value
		positions[asset] = oldQuantity + quantity
		costBases[asset] = basis
	} else {
		basis := costBases[asset]
		costBasis = &basis
		pnl := (price - basis) * quantity
		realizedPnL = &pnl
		if pnl < 0 {
			consecutiveLosses++
		} else {
			consecutiveLosses = 0
		}
		cash += value
		positions[asset] -= quantity
		if positions[asset] <= 1e-12 {
			delete(positions, asset)
			delete(costBases, asset)
		}
	}
	positionsRaw, err = json.Marshal(positions)
	if err != nil {
		return err
	}
	costBasesRaw, err = json.Marshal(costBases)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE paper_state
		SET cash = $1, positions = $2, cost_bases = $3, consecutive_losses = $4, updated_at = now()
		WHERE id = 1
	`, cash, positionsRaw, costBasesRaw, consecutiveLosses); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO paper_fills (id, asset, side, quantity, price, cost_basis, realized_pnl, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, decisionID, asset, side, quantity, price, costBasis, realizedPnL, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO paper_decision_ids (id, created_at) VALUES ($1, $2)
	`, decisionID, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RecentFills(ctx context.Context, limit int) ([]Fill, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, asset, side, quantity, price, cost_basis, realized_pnl, created_at
		FROM paper_fills ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fills := []Fill{}
	for rows.Next() {
		var f Fill
		if err := rows.Scan(&f.ID, &f.Asset, &f.Side, &f.Quantity, &f.Price, &f.CostBasis, &f.RealizedPnL, &f.CreatedAt); err != nil {
			return nil, err
		}
		fills = append(fills, f)
	}
	return fills, rows.Err()
}

// MarkToMarket values the current paper ledger at the latest 1m price for
// every held asset, persists the equity point, and returns curve-based risk
// metrics for the just-recorded point.
func (s *Store) MarkToMarket(ctx context.Context) (Metrics, error) {
	cash, positions, err := s.Portfolio(ctx)
	if err != nil {
		return Metrics{}, err
	}
	positionsValue := 0.0
	for asset, quantity := range positions {
		var price float64
		err := s.pool.QueryRow(ctx, `
			SELECT close FROM candles
			WHERE exchange = $1 AND symbol = $2 AND timeframe = '1m'
			ORDER BY ts DESC LIMIT 1
		`, risk.ReferenceExchange, asset).Scan(&price)
		if errors.Is(err, pgx.ErrNoRows) {
			return Metrics{}, fmt.Errorf("paperstore: no current price for held position %s", asset)
		}
		if err != nil {
			return Metrics{}, fmt.Errorf("paperstore: read %s price: %w", asset, err)
		}
		positionsValue += quantity * price
	}

	now := time.Now().UTC()
	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO paper_equity_snapshots (cash, positions_value, equity, created_at)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, cash, positionsValue, cash+positionsValue, now).Scan(&id); err != nil {
		return Metrics{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT equity, created_at FROM paper_equity_snapshots WHERE id <= $1 ORDER BY id
	`, id)
	if err != nil {
		return Metrics{}, err
	}
	defer rows.Close()
	points := []equitySnapshot{}
	for rows.Next() {
		var point equitySnapshot
		if err := rows.Scan(&point.Equity, &point.CreatedAt); err != nil {
			return Metrics{}, err
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return Metrics{}, err
	}
	metrics := metricsForEquityCurve(points, now)
	if err := s.pool.QueryRow(ctx, `SELECT consecutive_losses FROM paper_state WHERE id = 1`).Scan(&metrics.ConsecutiveLosses); err != nil {
		return Metrics{}, err
	}
	return metrics, nil
}

func metricsForEquityCurve(points []equitySnapshot, now time.Time) Metrics {
	if len(points) == 0 {
		return Metrics{}
	}
	current := points[len(points)-1].Equity
	peak := current
	for _, point := range points {
		if point.Equity > peak {
			peak = point.Equity
		}
	}
	metrics := Metrics{
		DailyLoss:  periodLoss(points, startOfUTCDay(now)),
		WeeklyLoss: periodLoss(points, startOfUTCWeek(now)),
	}
	if peak > 0 && current < peak {
		metrics.Drawdown = (peak - current) / peak
	}
	return metrics
}

func periodLoss(points []equitySnapshot, start time.Time) float64 {
	current := points[len(points)-1].Equity
	baseline := current
	for _, point := range points {
		if !point.CreatedAt.Before(start) {
			baseline = point.Equity
			break
		}
	}
	if baseline > 0 && current < baseline {
		return (baseline - current) / baseline
	}
	return 0
}

func startOfUTCDay(t time.Time) time.Time {
	y, month, day := t.UTC().Date()
	return time.Date(y, month, day, 0, 0, 0, 0, time.UTC)
}

func startOfUTCWeek(t time.Time) time.Time {
	day := startOfUTCDay(t)
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
}

// Enabled reports whether simulation mode is switched on — checked by
// the MCP server before spending an LLM call on a paper cycle.
func (s *Store) Enabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT enabled FROM paper_state WHERE id = 1`).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return enabled, err
}

func (s *Store) SetEnabled(ctx context.Context, enabled bool) error {
	// Portfolio() seeds row id=1 if missing, so the UPDATE below always
	// has a row to touch.
	if _, _, err := s.Portfolio(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE paper_state SET enabled = $1, updated_at = now() WHERE id = 1`, enabled)
	return err
}

// GetAutomationControls returns the paper and testnet switches plus the agent
// selected to run an external automation loop.
func (s *Store) GetAutomationControls(ctx context.Context) (AutomationControls, error) {
	if _, _, err := s.Portfolio(ctx); err != nil {
		return AutomationControls{}, err
	}
	var controls AutomationControls
	err := s.pool.QueryRow(ctx, `
		SELECT enabled, testnet_enabled, active_agent FROM paper_state WHERE id = 1
	`).Scan(&controls.Enabled, &controls.TestnetEnabled, &controls.ActiveAgent)
	return controls, err
}

// WithTestnetEnabled runs fn while holding the automation-controls row lock.
// A concurrent disable waits until fn has submitted its order, so it cannot
// slip between the enabled check and the outbound request.
func (s *Store) WithTestnetEnabled(ctx context.Context, fn func() error) error {
	if _, _, err := s.Portfolio(ctx); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT testnet_enabled FROM paper_state WHERE id = 1 FOR UPDATE`).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return ErrTestnetDisabled
	}
	if err := fn(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PatchAutomationControls atomically applies the supplied controls and returns
// their resulting values.
func (s *Store) PatchAutomationControls(ctx context.Context, patch AutomationPatch) (AutomationControls, error) {
	if err := validateAutomationPatch(patch); err != nil {
		return AutomationControls{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO paper_state (id, cash, positions, enabled) VALUES (1, $1, '{}', false)
		ON CONFLICT (id) DO NOTHING
	`, startingCash); err != nil {
		return AutomationControls{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AutomationControls{}, err
	}
	defer tx.Rollback(ctx)

	var controls AutomationControls
	if err := tx.QueryRow(ctx, `
		SELECT enabled, testnet_enabled, active_agent FROM paper_state WHERE id = 1 FOR UPDATE
	`).Scan(&controls.Enabled, &controls.TestnetEnabled, &controls.ActiveAgent); err != nil {
		return AutomationControls{}, err
	}
	if patch.Enabled != nil {
		controls.Enabled = *patch.Enabled
	}
	if patch.TestnetEnabled != nil {
		controls.TestnetEnabled = *patch.TestnetEnabled
	}
	if patch.ActiveAgent != nil {
		controls.ActiveAgent = *patch.ActiveAgent
	}
	if _, err := tx.Exec(ctx, `
		UPDATE paper_state
		SET enabled = $1, testnet_enabled = $2, active_agent = $3, updated_at = now()
		WHERE id = 1
	`, controls.Enabled, controls.TestnetEnabled, controls.ActiveAgent); err != nil {
		return AutomationControls{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AutomationControls{}, err
	}
	return controls, nil
}
