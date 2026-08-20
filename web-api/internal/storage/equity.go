package storage

import "context"

// RecentEquitySnapshots reads real portfolio-value snapshots persisted by the
// tracking module. It returns the most recent snapshots oldest-first so they
// can be charted directly.
func (s *Store) RecentEquitySnapshots(ctx context.Context, limit int) ([]EquityPoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, cash, positions_value, total_equity FROM (
			SELECT ts, cash, positions_value, total_equity
			FROM equity_snapshots
			ORDER BY ts DESC
			LIMIT $1
		) recent
		ORDER BY ts ASC
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshots := []EquityPoint{}
	for rows.Next() {
		var snapshot EquityPoint
		if err := rows.Scan(&snapshot.Timestamp, &snapshot.Cash, &snapshot.PositionsValue, &snapshot.TotalEquity); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}
