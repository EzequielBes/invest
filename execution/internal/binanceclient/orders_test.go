// execution/internal/binanceclient/orders_test.go
package binanceclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

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

func TestLotSizeRoundQuantity_DecimalSteps(t *testing.T) {
	filter, err := parseLotSize("0.0001", "0.0001")
	if err != nil {
		t.Fatal(err)
	}
	got, err := filter.roundQuantity(0.00039999999999999996)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.0003" {
		t.Errorf("rounded quantity = %q, want 0.0003", got)
	}
	if _, err := filter.roundQuantity(0.000099); err == nil {
		t.Error("below-minimum quantity was accepted")
	}
}

func TestPlaceLimitOrder_FetchesAndCachesLotSize(t *testing.T) {
	exchangeInfoCalls := 0
	orderCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fapi/v1/exchangeInfo":
			exchangeInfoCalls++
			if got := r.URL.Query().Get("symbol"); got != "BTCUSDT" {
				t.Errorf("symbol = %q, want BTCUSDT", got)
			}
			w.Write([]byte(`{"symbols":[{"symbol":"BTCUSDT","filters":[{"filterType":"LOT_SIZE","minQty":"0.001","stepSize":"0.001"}]}]}`))
		case "/fapi/v1/order":
			orderCalls++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read order body: %v", err)
				return
			}
			params, _ := url.ParseQuery(string(body))
			if got := params.Get("quantity"); got != "0.123" {
				t.Errorf("quantity = %q, want 0.123", got)
			}
			w.Write([]byte(`{"orderId":1,"clientOrderId":"id","status":"NEW","executedQty":"0","avgPrice":"0"}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New("key", "secret", server.URL)
	for range 2 {
		if _, err := client.PlaceLimitOrder(context.Background(), "BTC", "buy", 0.1239, 100, "id"); err != nil {
			t.Fatalf("PlaceLimitOrder: %v", err)
		}
	}
	if exchangeInfoCalls != 1 || orderCalls != 2 {
		t.Errorf("exchangeInfo calls = %d, order calls = %d; want 1 and 2", exchangeInfoCalls, orderCalls)
	}
}
