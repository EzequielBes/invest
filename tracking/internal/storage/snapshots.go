package storage

import (
	"context"
	"time"
)

// Snapshot is one point-in-time measurement of the real account's value.
type Snapshot struct {
	Timestamp      time.Time
	Cash           float64
	PositionsValue float64
	TotalEquity    float64
}

func (s *Store) SaveSnapshot(ctx context.Context, snap Snapshot) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO equity_snapshots (ts, cash, positions_value, total_equity)
		VALUES ($1, $2, $3, $4)
	`, snap.Timestamp, snap.Cash, snap.PositionsValue, snap.TotalEquity)
	return err
}

func (s *Store) RecentSnapshotForTest(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	err := s.pool.QueryRow(ctx, `
		SELECT ts, cash, positions_value, total_equity
		FROM equity_snapshots
		ORDER BY ts DESC LIMIT 1
	`).Scan(&snap.Timestamp, &snap.Cash, &snap.PositionsValue, &snap.TotalEquity)
	return snap, err
}
