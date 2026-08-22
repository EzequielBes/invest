// execution/executor/alpaca_test.go
package executor

import (
	"context"
	"testing"
	"time"

	"risk-engine/risk"

	"execution/internal/alpacaclient"
)

type fakeAlpaca struct {
	placeOrder  alpacaclient.Order
	statusSeq   []alpacaclient.Order
	statusIdx   int
	cancelErr   error
	cancelCalls int
	// postCancelOrder is what GetOrderStatus returns after CancelOrder has
	// been called — Alpaca's cancel has no response body, so Execute must
	// always re-fetch status afterward, unlike Binance's cancel response.
	postCancelOrder alpacaclient.Order
}

func (f *fakeAlpaca) GetAccount(context.Context) (alpacaclient.Account, error) {
	return alpacaclient.Account{}, nil
}

func (f *fakeAlpaca) PlaceOrder(context.Context, string, string, float64, string) (alpacaclient.Order, error) {
	return f.placeOrder, nil
}

func (f *fakeAlpaca) GetOrderStatus(context.Context, string) (alpacaclient.Order, error) {
	if f.cancelCalls > 0 {
		return f.postCancelOrder, nil
	}
	o := f.statusSeq[f.statusIdx]
	if f.statusIdx < len(f.statusSeq)-1 {
		f.statusIdx++
	}
	return o, nil
}

func (f *fakeAlpaca) CancelOrder(context.Context, string) error {
	f.cancelCalls++
	return f.cancelErr
}

type fakeAlpacaControls struct{ calls int }

func (f *fakeAlpacaControls) Close() {}
func (f *fakeAlpacaControls) WithAlpacaPaperEnabled(_ context.Context, fn func() error) error {
	f.calls++
	return fn()
}

func testAlpacaExecutor(alpaca *fakeAlpaca, store *fakeStore) *AlpacaExecutor {
	return &AlpacaExecutor{
		alpaca: alpaca, store: store, controls: &fakeAlpacaControls{},
		pollInterval: time.Millisecond, fillTimeout: 20 * time.Millisecond,
	}
}

func TestAlpacaExecute_FilledOnFirstPollNeverCancels(t *testing.T) {
	alpaca := &fakeAlpaca{
		placeOrder: alpacaclient.Order{Status: "new"},
		statusSeq:  []alpacaclient.Order{{OrderID: "1", ClientOrderID: "cid", Status: "filled", FilledQty: 5, FilledAvgPrice: 230}},
	}
	store := &fakeStore{}
	e := testAlpacaExecutor(alpaca, store)

	outcome, err := e.Execute(context.Background(), "AAPL", risk.SideBuy, 5, 230, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "filled" || outcome.FilledQuantity != 5 {
		t.Errorf("outcome = %+v, want filled 5", outcome)
	}
	if alpaca.cancelCalls != 0 {
		t.Errorf("cancelCalls = %d, want 0", alpaca.cancelCalls)
	}
	if len(store.saved) != 1 || store.saved[0].Status != "filled" {
		t.Errorf("saved = %+v, want one filled execution persisted", store.saved)
	}
}

func TestAlpacaExecute_TimeoutWithNoFillCancels(t *testing.T) {
	alpaca := &fakeAlpaca{
		placeOrder:      alpacaclient.Order{Status: "new"},
		statusSeq:       []alpacaclient.Order{{Status: "new"}},
		postCancelOrder: alpacaclient.Order{OrderID: "2", ClientOrderID: "cid", Status: "canceled", FilledQty: 0},
	}
	e := testAlpacaExecutor(alpaca, &fakeStore{})
	e.fillTimeout = 3 * time.Millisecond

	outcome, err := e.Execute(context.Background(), "AAPL", risk.SideBuy, 5, 230, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "cancelled" || outcome.FilledQuantity != 0 {
		t.Errorf("outcome = %+v, want cancelled with 0 filled", outcome)
	}
	if alpaca.cancelCalls != 1 {
		t.Errorf("cancelCalls = %d, want 1", alpaca.cancelCalls)
	}
}

func TestAlpacaExecute_TimeoutWithPartialFillReportsPartial(t *testing.T) {
	alpaca := &fakeAlpaca{
		placeOrder:      alpacaclient.Order{Status: "new"},
		statusSeq:       []alpacaclient.Order{{Status: "partially_filled", FilledQty: 2}},
		postCancelOrder: alpacaclient.Order{OrderID: "3", ClientOrderID: "cid", Status: "canceled", FilledQty: 2, FilledAvgPrice: 230},
	}
	e := testAlpacaExecutor(alpaca, &fakeStore{})
	e.fillTimeout = 3 * time.Millisecond

	outcome, err := e.Execute(context.Background(), "AAPL", risk.SideBuy, 5, 230, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "partial" || outcome.FilledQuantity != 2 {
		t.Errorf("outcome = %+v, want partial 2", outcome)
	}
}

func TestAlpacaExecute_GatesOnAlpacaPaperEnabled(t *testing.T) {
	alpaca := &fakeAlpaca{
		placeOrder: alpacaclient.Order{Status: "new"},
		statusSeq:  []alpacaclient.Order{{Status: "filled", FilledQty: 5, FilledAvgPrice: 230}},
	}
	controls := &fakeAlpacaControls{}
	e := &AlpacaExecutor{
		alpaca: alpaca, store: &fakeStore{}, controls: controls,
		pollInterval: time.Millisecond, fillTimeout: 20 * time.Millisecond,
	}

	if _, err := e.Execute(context.Background(), "AAPL", risk.SideBuy, 5, 230, "cid"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if controls.calls != 1 {
		t.Errorf("WithAlpacaPaperEnabled calls = %d, want 1 (order placement must be gated)", controls.calls)
	}
}
