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

// ExecForTest runs an arbitrary statement against the store's pool — used
// only by tests to seed fixtures in tables this module never writes in
// production (analysis_runs, analysis_results, candles all belong to
// other modules). Never call this from production code.
func ExecForTest(ctx context.Context, s *Store, sql string, args ...any) error {
	_, err := s.pool.Exec(ctx, sql, args...)
	return err
}
