// execution/executor/client_test.go
package executor

import (
	"context"
	"testing"
	"time"

	"risk-engine/risk"

	"execution/internal/binanceclient"
	"execution/internal/storage"
)

type fakeBinance struct {
	placeOrder  binanceclient.Order
	statusSeq   []binanceclient.Order
	statusIdx   int
	cancelOrder binanceclient.Order
	cancelCalls int
}

func (f *fakeBinance) GetAccount(context.Context) (binanceclient.Account, error) {
	return binanceclient.Account{}, nil
}

func (f *fakeBinance) PlaceLimitOrder(context.Context, string, string, float64, float64, string) (binanceclient.Order, error) {
	return f.placeOrder, nil
}

func (f *fakeBinance) GetOrderStatus(context.Context, string, string) (binanceclient.Order, error) {
	o := f.statusSeq[f.statusIdx]
	if f.statusIdx < len(f.statusSeq)-1 {
		f.statusIdx++
	}
	return o, nil
}

func (f *fakeBinance) CancelOrder(context.Context, string, string) (binanceclient.Order, error) {
	f.cancelCalls++
	return f.cancelOrder, nil
}

type fakeStore struct {
	saved []storage.Execution
}

func (f *fakeStore) SaveExecution(_ context.Context, e storage.Execution) error {
	f.saved = append(f.saved, e)
	return nil
}

func (f *fakeStore) Close() {}

func TestExecute_FilledOnFirstPollNeverCancels(t *testing.T) {
	binance := &fakeBinance{
		placeOrder: binanceclient.Order{Status: "NEW"},
		statusSeq:  []binanceclient.Order{{OrderID: 1, ClientOrderID: "cid", Status: "FILLED", ExecutedQty: 1.0, AvgPrice: 100}},
	}
	store := &fakeStore{}
	e := &BinanceExecutor{binance: binance, store: store, pollInterval: time.Millisecond, fillTimeout: 20 * time.Millisecond}

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "filled" || outcome.FilledQuantity != 1.0 {
		t.Errorf("outcome = %+v, want filled 1.0", outcome)
	}
	if binance.cancelCalls != 0 {
		t.Errorf("cancelCalls = %d, want 0", binance.cancelCalls)
	}
	if len(store.saved) != 1 || store.saved[0].Status != "filled" {
		t.Errorf("saved = %+v, want one filled execution persisted", store.saved)
	}
}

func TestExecute_TimeoutWithNoFillCancels(t *testing.T) {
	binance := &fakeBinance{
		placeOrder:  binanceclient.Order{Status: "NEW"},
		statusSeq:   []binanceclient.Order{{Status: "NEW"}},
		cancelOrder: binanceclient.Order{OrderID: 2, ClientOrderID: "cid", Status: "CANCELED", ExecutedQty: 0},
	}
	e := &BinanceExecutor{binance: binance, store: &fakeStore{}, pollInterval: time.Millisecond, fillTimeout: 3 * time.Millisecond}

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "cancelled" || outcome.FilledQuantity != 0 {
		t.Errorf("outcome = %+v, want cancelled with 0 filled", outcome)
	}
	if binance.cancelCalls != 1 {
		t.Errorf("cancelCalls = %d, want 1", binance.cancelCalls)
	}
}

func TestExecute_TimeoutWithPartialFillReportsPartial(t *testing.T) {
	binance := &fakeBinance{
		placeOrder:  binanceclient.Order{Status: "NEW"},
		statusSeq:   []binanceclient.Order{{Status: "PARTIALLY_FILLED", ExecutedQty: 0.3}},
		cancelOrder: binanceclient.Order{OrderID: 3, ClientOrderID: "cid", Status: "CANCELED", ExecutedQty: 0.3, AvgPrice: 100},
	}
	e := &BinanceExecutor{binance: binance, store: &fakeStore{}, pollInterval: time.Millisecond, fillTimeout: 3 * time.Millisecond}

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "partial" || outcome.FilledQuantity != 0.3 {
		t.Errorf("outcome = %+v, want partial with 0.3 filled", outcome)
	}
}
