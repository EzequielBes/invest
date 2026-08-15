package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) InsertNewsItem(ctx context.Context, source, title, body, url string, publishedAt time.Time) (bool, error) {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO news_items (source, published_at, title, body, url)
		VALUES ($1, $2, $3, $4, $5)
	`, source, publishedAt, title, body, url)
	if err == nil {
		return true, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation on url
		return false, nil
	}
	return false, err
}
