package paperexec

import (
	"context"
	"testing"

	"risk-engine/risk"

	"execution/executor"
)

type fakePaperStore struct {
	enabled   bool
	fillCalls int
}

func (f *fakePaperStore) Close() {}

func (f *fakePaperStore) Portfolio(context.Context) (float64, map[string]float64, error) {
	return 0, nil, nil
}

func (f *fakePaperStore) Enabled(context.Context) (bool, error) {
	return f.enabled, nil
}

func (f *fakePaperStore) ApplyFill(context.Context, string, string, string, float64, float64) error {
	f.fillCalls++
	return nil
}

func TestExecute_DisabledDoesNotApplyFill(t *testing.T) {
	store := &fakePaperStore{}
	client := &Client{store: store}

	_, err := client.Execute(context.Background(), "BTC", risk.SideBuy, 1, 100, "decision-1")
	if err == nil {
		t.Fatal("Execute: want disabled error, got nil")
	}
	if store.fillCalls != 0 {
		t.Errorf("ApplyFill calls = %d, want 0", store.fillCalls)
	}
}

var _ executor.Client = (*Client)(nil)
