package agents

import (
	"context"
	"os"
	"testing"
	"time"

	"analysis/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testMacroStore(t *testing.T) *storage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping macro agent test")
	}
	s, err := storage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// seedMacroObservation/deleteMacroObservation write directly via their own
// pgxpool connection — analysis's storage.Store has no write method for
// macro_indicators (that table is written by market-data's poller), so
// fixture setup connects independently, the same cross-module fixture
// pattern risk-engine's storagetest.Seeder uses.
func seedMacroObservation(ctx context.Context, _ *storage.Store, seriesID string, observedAt time.Time, value float64) error {
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		INSERT INTO macro_indicators (series_id, observed_at, value, fetched_at)
		VALUES ($1, $2, $3, now())
	`, seriesID, observedAt, value)
	return err
}

func deleteMacroObservation(ctx context.Context, _ *storage.Store, seriesID string) {
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		return
	}
	defer pool.Close()
	pool.Exec(ctx, `DELETE FROM macro_indicators WHERE series_id = $1`, seriesID)
}

func TestMacro_ReportsRealFedFundsRate(t *testing.T) {
	store := testMacroStore(t)
	ctx := context.Background()

	// Insert directly via the same table macro.go reads — analysis has no
	// write access to market-data's InsertMacroObservation, so this seeds
	// through raw SQL like other cross-module fixture setups in this repo.
	seedTime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if err := seedMacroObservation(ctx, store, "FEDFUNDS", seedTime, 5.50); err != nil {
		t.Fatalf("seed FEDFUNDS: %v", err)
	}
	t.Cleanup(func() { deleteMacroObservation(context.Background(), store, "FEDFUNDS") })

	output, err := Macro(ctx, store)
	if err != nil {
		t.Fatalf("Macro: %v", err)
	}
	ind, ok := output.Indicators.(MacroIndicators)
	if !ok {
		t.Fatalf("Indicators type = %T, want MacroIndicators", output.Indicators)
	}
	if ind.FedFundsRate != 5.50 {
		t.Errorf("FedFundsRate = %v, want 5.50 (real value, not fabricated)", ind.FedFundsRate)
	}
}

func TestMacro_ReturnsZeroValueWhenNoDataCollectedYet(t *testing.T) {
	store := testMacroStore(t)
	ctx := context.Background()

	output, err := Macro(ctx, store)
	if err != nil {
		t.Fatalf("Macro: %v", err)
	}
	ind := output.Indicators.(MacroIndicators)
	// No FRED_API_KEY / poller run yet in this environment is a normal,
	// honest state — the agent must not fabricate a plausible-looking
	// number, it should surface absence as absence.
	if ind.FedFundsRate != 0 {
		t.Errorf("FedFundsRate = %v, want 0 when no observation exists", ind.FedFundsRate)
	}
}
