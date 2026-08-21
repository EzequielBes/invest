package paperstore

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestAutomationPatch_RejectsInvalidActiveAgent(t *testing.T) {
	agent := "invalid"
	if err := validateAutomationPatch(AutomationPatch{ActiveAgent: &agent}); !errors.Is(err, ErrInvalidActiveAgent) {
		t.Errorf("validateAutomationPatch = %v, want ErrInvalidActiveAgent", err)
	}
}

func TestPatchAutomationControls_UpdatesOnlyProvidedFields(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	original, err := store.GetAutomationControls(ctx)
	if err != nil {
		t.Fatalf("GetAutomationControls: %v", err)
	}
	defer func() {
		if _, err := store.PatchAutomationControls(ctx, AutomationPatch{
			Enabled:        &original.Enabled,
			TestnetEnabled: &original.TestnetEnabled,
			ActiveAgent:    &original.ActiveAgent,
		}); err != nil {
			t.Errorf("restore controls: %v", err)
		}
	}()

	testnetEnabled := !original.TestnetEnabled
	agent := "codex"
	if original.ActiveAgent == agent {
		agent = "claude_code"
	}
	got, err := store.PatchAutomationControls(ctx, AutomationPatch{
		TestnetEnabled: &testnetEnabled,
		ActiveAgent:    &agent,
	})
	if err != nil {
		t.Fatalf("PatchAutomationControls: %v", err)
	}
	if got.Enabled != original.Enabled || got.TestnetEnabled != testnetEnabled || got.ActiveAgent != agent {
		t.Errorf("controls = %+v, want enabled=%v testnet_enabled=%v active_agent=%q", got, original.Enabled, testnetEnabled, agent)
	}
}

func TestApplyFill_BuyThenSellUpdatesCashAndPositions(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	store, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// Reset to a known baseline so this test is repeatable.
	var originalCash float64
	var originalPositions []byte
	var originalCostBases []byte
	var originalConsecutiveLosses int
	if err := store.pool.QueryRow(ctx, `
		SELECT cash, positions, cost_bases, consecutive_losses FROM paper_state WHERE id = 1
	`).Scan(&originalCash, &originalPositions, &originalCostBases, &originalConsecutiveLosses); err != nil {
		t.Fatalf("read original paper state: %v", err)
	}
	defer func() {
		if _, err := store.pool.Exec(ctx, `
			UPDATE paper_state SET cash = $1, positions = $2, cost_bases = $3, consecutive_losses = $4 WHERE id = 1
		`, originalCash, originalPositions, originalCostBases, originalConsecutiveLosses); err != nil {
			t.Errorf("restore paper state: %v", err)
		}
	}()
	if _, err := store.pool.Exec(ctx, `
		UPDATE paper_state SET cash = $1, positions = '{}', cost_bases = '{}', consecutive_losses = 0 WHERE id = 1
	`, startingCash); err != nil {
		t.Fatalf("reset paper state: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM paper_fills WHERE id LIKE 'test-paper-%'`); err != nil {
		t.Fatalf("cleanup fills: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM paper_decision_ids WHERE id LIKE 'test-paper-%'`); err != nil {
		t.Fatalf("cleanup decision IDs: %v", err)
	}
	defer store.pool.Exec(ctx, `DELETE FROM paper_fills WHERE id LIKE 'test-paper-%'`)
	defer store.pool.Exec(ctx, `DELETE FROM paper_decision_ids WHERE id LIKE 'test-paper-%'`)

	cashBefore, _, err := store.Portfolio(ctx)
	if err != nil {
		t.Fatalf("Portfolio: %v", err)
	}

	if err := store.ApplyFill(ctx, "test-paper-buy", "BTC", "buy", 0.1, 50000); err != nil {
		t.Fatalf("ApplyFill buy: %v", err)
	}
	cash, positions, err := store.Portfolio(ctx)
	if err != nil {
		t.Fatalf("Portfolio after buy: %v", err)
	}
	if want := cashBefore - 5000; cash != want {
		t.Errorf("cash after buy = %v, want %v", cash, want)
	}
	if positions["BTC"] != 0.1 {
		t.Errorf("positions[BTC] after buy = %v, want 0.1", positions["BTC"])
	}
	if err := store.ApplyFill(ctx, "test-paper-buy-2", "BTC", "buy", 0.1, 60000); err != nil {
		t.Fatalf("ApplyFill second buy: %v", err)
	}

	if err := store.ApplyFill(ctx, "test-paper-sell", "BTC", "sell", 0.1, 45000); err != nil {
		t.Fatalf("ApplyFill sell: %v", err)
	}
	cash, positions, err = store.Portfolio(ctx)
	if err != nil {
		t.Fatalf("Portfolio after sell: %v", err)
	}
	if want := cashBefore - 5000 - 6000 + 4500; cash != want {
		t.Errorf("cash after sell = %v, want %v", cash, want)
	}
	if positions["BTC"] != 0.1 {
		t.Errorf("positions[BTC] after partial sale = %v, want 0.1", positions["BTC"])
	}
	var costBasis, realizedPnL float64
	if err := store.pool.QueryRow(ctx, `
		SELECT cost_basis, realized_pnl FROM paper_fills WHERE id = 'test-paper-sell'
	`).Scan(&costBasis, &realizedPnL); err != nil {
		t.Fatalf("read audited fill: %v", err)
	}
	if costBasis != 55000 || realizedPnL != -1000 {
		t.Errorf("audited fill = cost basis %v, realized P&L %v, want 55000, -1000", costBasis, realizedPnL)
	}
	var consecutiveLosses int
	if err := store.pool.QueryRow(ctx, `SELECT consecutive_losses FROM paper_state WHERE id = 1`).Scan(&consecutiveLosses); err != nil {
		t.Fatalf("read loss streak: %v", err)
	}
	if consecutiveLosses != 1 {
		t.Errorf("consecutive losses after losing sell = %d, want 1", consecutiveLosses)
	}
	if err := store.ApplyFill(ctx, "test-paper-sell-2", "BTC", "sell", 0.1, 56000); err != nil {
		t.Fatalf("ApplyFill profitable sell: %v", err)
	}
	var costBases []byte
	if err := store.pool.QueryRow(ctx, `SELECT cost_bases, consecutive_losses FROM paper_state WHERE id = 1`).Scan(&costBases, &consecutiveLosses); err != nil {
		t.Fatalf("read paper state after sale: %v", err)
	}
	if string(costBases) != "{}" || consecutiveLosses != 0 {
		t.Errorf("state after profitable close = cost bases %s, losses %d, want {}, 0", costBases, consecutiveLosses)
	}
}

func TestMetricsForEquityCurve_DerivesLossesAndDrawdown(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	points := []equitySnapshot{
		{Equity: 10000, CreatedAt: now.AddDate(0, 0, -8)},
		{Equity: 12000, CreatedAt: now.AddDate(0, 0, -7)},
		{Equity: 11000, CreatedAt: now.Add(-48 * time.Hour)},
		{Equity: 11000, CreatedAt: now.Add(-time.Hour)},
		{Equity: 9900, CreatedAt: now},
	}
	metrics := metricsForEquityCurve(points, now)
	if metrics.DailyLoss != 0.1 {
		t.Errorf("DailyLoss = %v, want 0.1", metrics.DailyLoss)
	}
	if metrics.WeeklyLoss != 0.1 {
		t.Errorf("WeeklyLoss = %v, want 0.1", metrics.WeeklyLoss)
	}
	if metrics.Drawdown != 0.175 {
		t.Errorf("Drawdown = %v, want 0.175", metrics.Drawdown)
	}
	if metrics.ConsecutiveLosses != 0 {
		t.Errorf("ConsecutiveLosses = %d, want 0", metrics.ConsecutiveLosses)
	}
}
