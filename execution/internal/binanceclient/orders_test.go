// execution/internal/binanceclient/orders_test.go
package binanceclient

import "testing"

func TestParseOrder_ParsesNumericStringFields(t *testing.T) {
	order, err := parseOrder([]byte(`{
		"orderId": 12345,
		"clientOrderId": "abc-123",
		"status": "PARTIALLY_FILLED",
		"executedQty": "0.250",
		"avgPrice": "45000.12"
	}`))
	if err != nil {
		t.Fatalf("parseOrder: %v", err)
	}
	want := Order{OrderID: 12345, ClientOrderID: "abc-123", Status: "PARTIALLY_FILLED", ExecutedQty: 0.25, AvgPrice: 45000.12}
	if order != want {
		t.Errorf("order = %+v, want %+v", order, want)
	}
}

func TestSymbolFor(t *testing.T) {
	if got := symbolFor("BTC"); got != "BTCUSDT" {
		t.Errorf("symbolFor(BTC) = %q, want BTCUSDT", got)
	}
}
