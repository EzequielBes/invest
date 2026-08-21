// execution/executor/client_test.go
package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"risk-engine/risk"

	"execution/internal/binanceclient"
	"execution/internal/storage"
	"execution/paperstore"
)

type fakeBinance struct {
	placeOrder  binanceclient.Order
	placeCalls  int
	placeHook   func()
	statusSeq   []binanceclient.Order
	statusIdx   int
	cancelOrder binanceclient.Order
	cancelErr   error
	cancelCalls int
	// postCancelOrder is returned by GetOrderStatus once CancelOrder has
	// been called — simulates the order's true state as discovered by the
	// status re-check after a failed cancel (e.g. it filled in the race
	// window between the last poll and the cancel attempt).
	postCancelOrder binanceclient.Order
	// pollErr, if set, makes GetOrderStatus fail every time it's called
	// before CancelOrder — simulating a poll failure mid-loop. Once
	// cancelCalls > 0, GetOrderStatus falls back to its normal
	// postCancelOrder behavior (the status re-check after cancel).
	pollErr error
}

func (f *fakeBinance) GetAccount(context.Context) (binanceclient.Account, error) {
	return binanceclient.Account{}, nil
}

func (f *fakeBinance) PlaceLimitOrder(context.Context, string, string, float64, float64, string) (binanceclient.Order, error) {
	f.placeCalls++
	if f.placeHook != nil {
		f.placeHook()
	}
	return f.placeOrder, nil
}

func (f *fakeBinance) GetOrderStatus(context.Context, string, string) (binanceclient.Order, error) {
	if f.cancelCalls > 0 {
		return f.postCancelOrder, nil
	}
	if f.pollErr != nil {
		return binanceclient.Order{}, f.pollErr
	}
	o := f.statusSeq[f.statusIdx]
	if f.statusIdx < len(f.statusSeq)-1 {
		f.statusIdx++
	}
	return o, nil
}

func (f *fakeBinance) CancelOrder(context.Context, string, string) (binanceclient.Order, error) {
	f.cancelCalls++
	if f.cancelErr != nil {
		return binanceclient.Order{}, f.cancelErr
	}
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

type fakeControls struct {
	err        error
	calls      int
	inCallback bool
}

func (f *fakeControls) Close() {}

func (f *fakeControls) WithTestnetEnabled(_ context.Context, fn func() error) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.inCallback = true
	defer func() { f.inCallback = false }()
	return fn()
}

func testExecutor(binance *fakeBinance, store *fakeStore) *BinanceExecutor {
	return &BinanceExecutor{
		binance: binance, store: store, controls: &fakeControls{},
		pollInterval: time.Millisecond, fillTimeout: 20 * time.Millisecond,
	}
}

func TestExecute_FilledOnFirstPollNeverCancels(t *testing.T) {
	binance := &fakeBinance{
		placeOrder: binanceclient.Order{Status: "NEW"},
		statusSeq:  []binanceclient.Order{{OrderID: 1, ClientOrderID: "cid", Status: "FILLED", ExecutedQty: 1.0, AvgPrice: 100}},
	}
	store := &fakeStore{}
	e := testExecutor(binance, store)

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
	e := testExecutor(binance, &fakeStore{})
	e.fillTimeout = 3 * time.Millisecond

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
	e := testExecutor(binance, &fakeStore{})
	e.fillTimeout = 3 * time.Millisecond

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "partial" || outcome.FilledQuantity != 0.3 {
		t.Errorf("outcome = %+v, want partial with 0.3 filled", outcome)
	}
}

// TestExecute_CancelFailsButRecheckShowsFillPersistsFill covers the race
// where the order fills on the exchange between the last poll and the
// cancel attempt: CancelOrder fails (Binance rejects cancelling an
// already-filled order), and Execute must fall back to one GetOrderStatus
// re-check rather than discarding the fill as an unpersisted error.
func TestExecute_CancelFailsButRecheckShowsFillPersistsFill(t *testing.T) {
	binance := &fakeBinance{
		placeOrder:      binanceclient.Order{Status: "NEW"},
		statusSeq:       []binanceclient.Order{{Status: "NEW"}},
		cancelErr:       errors.New("binance: order already filled, cannot cancel"),
		postCancelOrder: binanceclient.Order{OrderID: 4, ClientOrderID: "cid", Status: "FILLED", ExecutedQty: 1.0, AvgPrice: 100},
	}
	store := &fakeStore{}
	e := testExecutor(binance, store)
	e.fillTimeout = 3 * time.Millisecond

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Status != "filled" || outcome.FilledQuantity != 1.0 {
		t.Errorf("outcome = %+v, want filled 1.0 (recovered from cancel failure via status re-check)", outcome)
	}
	if len(store.saved) != 1 || store.saved[0].Status != "filled" {
		t.Errorf("saved = %+v, want one filled execution persisted", store.saved)
	}
}

// TestExecute_CancelFailsAndOrderStillOpenReturnsError covers the
// non-race case: CancelOrder fails for an ordinary reason (network blip,
// rate limit) and the order genuinely is still open on the exchange. The
// status re-check must not be trusted to mean "cancelled" here — Execute
// should return a hard error and persist nothing, since the true final
// state is unknown.
func TestExecute_CancelFailsAndOrderStillOpenReturnsError(t *testing.T) {
	binance := &fakeBinance{
		placeOrder:      binanceclient.Order{Status: "NEW"},
		statusSeq:       []binanceclient.Order{{Status: "NEW"}},
		cancelErr:       errors.New("binance: rate limited"),
		postCancelOrder: binanceclient.Order{OrderID: 5, ClientOrderID: "cid", Status: "PARTIALLY_FILLED", ExecutedQty: 0.4, AvgPrice: 100},
	}
	store := &fakeStore{}
	e := testExecutor(binance, store)
	e.fillTimeout = 3 * time.Millisecond

	_, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err == nil {
		t.Fatal("Execute: want error, got nil (order is still open, must not be reported as resolved)")
	}
	if len(store.saved) != 0 {
		t.Errorf("saved = %+v, want nothing persisted for an unresolved order state", store.saved)
	}
}

// TestExecute_PollFailureStillAttemptsCancel covers a GetOrderStatus
// failure DURING polling (not the post-timeout cancel step) — this used
// to return early, abandoning a live order on the exchange with no
// cancel attempt and nothing persisted. Execute must instead fall
// through into the same cancel-and-classify logic a fill-timeout gets.
func TestExecute_PollFailureStillAttemptsCancel(t *testing.T) {
	binance := &fakeBinance{
		placeOrder:  binanceclient.Order{Status: "NEW"},
		pollErr:     errors.New("binance: connection reset"),
		cancelOrder: binanceclient.Order{OrderID: 6, ClientOrderID: "cid", Status: "CANCELED", ExecutedQty: 0},
	}
	store := &fakeStore{}
	e := testExecutor(binance, store)
	e.fillTimeout = 5 * time.Millisecond

	outcome, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1.0, 100, "cid")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if binance.cancelCalls != 1 {
		t.Errorf("cancelCalls = %d, want 1 (a poll failure must still attempt cancel, not abandon the order)", binance.cancelCalls)
	}
	if outcome.Status != "cancelled" || outcome.FilledQuantity != 0 {
		t.Errorf("outcome = %+v, want cancelled with 0 filled", outcome)
	}
	if len(store.saved) != 1 || store.saved[0].Status != "cancelled" {
		t.Errorf("saved = %+v, want one cancelled execution persisted", store.saved)
	}
}

func TestExecute_TestnetDisabledDoesNotPlaceOrder(t *testing.T) {
	binance := &fakeBinance{}
	controls := &fakeControls{err: paperstore.ErrTestnetDisabled}
	e := testExecutor(binance, &fakeStore{})
	e.controls = controls

	_, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1, 100, "cid")
	if !errors.Is(err, paperstore.ErrTestnetDisabled) {
		t.Fatalf("Execute error = %v, want ErrTestnetDisabled", err)
	}
	if controls.calls != 1 {
		t.Errorf("WithTestnetEnabled calls = %d, want 1", controls.calls)
	}
	if binance.placeCalls != 0 {
		t.Errorf("PlaceLimitOrder calls = %d, want 0", binance.placeCalls)
	}
}

func TestExecute_PlacesOrderInsideTestnetAuthorization(t *testing.T) {
	controls := &fakeControls{}
	binance := &fakeBinance{
		placeOrder: binanceclient.Order{OrderID: 1, ClientOrderID: "cid", Status: "FILLED", ExecutedQty: 1, AvgPrice: 100},
		placeHook: func() {
			if !controls.inCallback {
				t.Error("PlaceLimitOrder called outside WithTestnetEnabled")
			}
		},
	}
	e := testExecutor(binance, &fakeStore{})
	e.controls = controls

	if _, err := e.Execute(context.Background(), "BTC", risk.SideBuy, 1, 100, "cid"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if controls.calls != 1 || binance.placeCalls != 1 {
		t.Errorf("authorization/order calls = %d/%d, want 1/1", controls.calls, binance.placeCalls)
	}
}
