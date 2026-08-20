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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const startingCash = 10000.0

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
	ID        string
	Asset     string
	Side      string
	Quantity  float64
	Price     float64
	CreatedAt time.Time
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
	cash, positions, err := s.Portfolio(ctx)
	if err != nil {
		return err
	}
	value := quantity * price
	if side == "buy" {
		cash -= value
		positions[asset] += quantity
	} else {
		cash += value
		positions[asset] -= quantity
		if positions[asset] <= 1e-12 {
			delete(positions, asset)
		}
	}
	positionsRaw, err := json.Marshal(positions)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE paper_state SET cash = $1, positions = $2, updated_at = now() WHERE id = 1
	`, cash, positionsRaw); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO paper_fills (id, asset, side, quantity, price, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, decisionID, asset, side, quantity, price, time.Now().UTC()); err != nil {
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
		SELECT id, asset, side, quantity, price, created_at
		FROM paper_fills ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fills := []Fill{}
	for rows.Next() {
		var f Fill
		if err := rows.Scan(&f.ID, &f.Asset, &f.Side, &f.Quantity, &f.Price, &f.CreatedAt); err != nil {
			return nil, err
		}
		fills = append(fills, f)
	}
	return fills, rows.Err()
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
