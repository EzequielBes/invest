package storage

import (
	"context"
	"testing"
	"time"
)

func TestRecentEquitySnapshots_ReturnsChronologicalOrderUpToLimit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	var tableExists bool
	if err := store.pool.QueryRow(ctx, `SELECT to_regclass('equity_snapshots') IS NOT NULL`).Scan(&tableExists); err != nil {
		t.Fatalf("check equity_snapshots table: %v", err)
	}
	if !tableExists {
		t.Skip("equity_snapshots table is not present; apply tracking migration first")
	}
	base := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)

	for i, ts := range []time.Time{base, base.Add(time.Minute), base.Add(2 * time.Minute)} {
		var id int64
		err := store.pool.QueryRow(ctx, `
			INSERT INTO equity_snapshots (ts, cash, positions_value, total_equity)
			VALUES ($1, $2, 0, $2)
			RETURNING id
		`, ts, float64(i+1)*100).Scan(&id)
		if err != nil {
			t.Fatalf("seed equity_snapshots: %v", err)
		}
		t.Cleanup(func() { deleteEquitySnapshotForTest(t, store, id) })
	}

	snapshots, err := store.RecentEquitySnapshots(ctx, 2)
	if err != nil {
		t.Fatalf("RecentEquitySnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("len(snapshots) = %d, want 2 (limit enforced)", len(snapshots))
	}
	if !snapshots[0].Timestamp.Equal(base.Add(time.Minute)) || !snapshots[1].Timestamp.Equal(base.Add(2*time.Minute)) {
		t.Errorf("snapshots = %+v, want the two newest fixtures in chronological order", snapshots)
	}
}

func deleteEquitySnapshotForTest(t *testing.T, store *Store, id int64) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `DELETE FROM equity_snapshots WHERE id = $1`, id); err != nil {
		t.Errorf("cleanup equity snapshot %d: %v", id, err)
	}
}
