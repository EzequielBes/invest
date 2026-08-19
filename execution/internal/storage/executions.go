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
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, asset, side, requested_quantity, price, order_id, client_order_id,
		       status, filled_quantity, filled_price, created_at
		FROM executions WHERE id = $1
	`, id).Scan(&e.ID, &e.Asset, &e.Side, &e.RequestedQuantity, &e.Price, &e.OrderID, &e.ClientOrderID,
		&e.Status, &e.FilledQuantity, &e.FilledPrice, &createdAt)
	if err == nil {
		// Strip monotonic clock by reconstructing from Unix nano
		e.CreatedAt = time.Unix(0, createdAt.UnixNano()).UTC()
	}
	return e, err
}

// DeleteExecutionForTest removes one executions row — used by tests to
// clean up after themselves.
func (s *Store) DeleteExecutionForTest(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM executions WHERE id = $1`, id)
	return err
}
