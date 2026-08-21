package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type DuplicateClientOrderIDError struct {
	ClientOrderID string
	Count         int
}

func (e *DuplicateClientOrderIDError) Error() string {
	return fmt.Sprintf("client_order_id %q matches %d executions", e.ClientOrderID, e.Count)
}

// Execution is the read-only execution data needed to audit a real fill.
// ClientOrderID is the documented link from a strategist decision to its order.
type Execution struct {
	ID                string
	Asset             string
	Side              string
	RequestedQuantity float64
	Price             float64
	ClientOrderID     string
	Status            string
	FilledQuantity    float64
	FilledPrice       float64
	CreatedAt         time.Time
}

// ExecutionByClientOrderID deliberately uses only the documented order link.
// It does not infer a relationship from executions.id or position history.
func (s *Store) ExecutionByClientOrderID(ctx context.Context, clientOrderID string) (Execution, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, asset, side, requested_quantity, price, client_order_id,
			status, filled_quantity, filled_price, created_at
		FROM executions
		WHERE client_order_id = $1
		ORDER BY id`, clientOrderID)
	if err != nil {
		return Execution{}, err
	}
	defer rows.Close()

	var execution Execution
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&execution.ID, &execution.Asset, &execution.Side, &execution.RequestedQuantity,
			&execution.Price, &execution.ClientOrderID, &execution.Status,
			&execution.FilledQuantity, &execution.FilledPrice, &execution.CreatedAt); err != nil {
			return Execution{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return Execution{}, err
	}
	if count == 0 {
		return Execution{}, pgx.ErrNoRows
	}
	if count > 1 {
		return Execution{}, &DuplicateClientOrderIDError{ClientOrderID: clientOrderID, Count: count}
	}
	return execution, nil
}
