package storage

import (
	"context"
	"time"
)

func (s *Store) StartRun(ctx context.Context, collector, symbol string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO collector_runs (collector, symbol, started_at, status)
		VALUES ($1, $2, $3, 'running')
		RETURNING id
	`, collector, symbol, time.Now().UTC()).Scan(&id)
	return id, err
}

func (s *Store) FinishRun(ctx context.Context, runID int64, status string, runErr error) error {
	var errText *string
	if runErr != nil {
		s := runErr.Error()
		errText = &s
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE collector_runs SET finished_at = $2, status = $3, error = $4
		WHERE id = $1
	`, runID, time.Now().UTC(), status, errText)
	return err
}
