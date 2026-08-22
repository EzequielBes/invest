package alpacaclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"context"
)

func TestPlaceOrder_SendsCorrectBodyAndParsesResponse(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/orders" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"id": "ord-1", "client_order_id": "client-1", "status": "new", "filled_qty": "0", "filled_avg_price": null}`))
	}))
	defer srv.Close()

	c := New("key", "secret", srv.URL)
	order, err := c.PlaceOrder(context.Background(), "AAPL", "buy", 5, "client-1")
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if order.OrderID != "ord-1" || order.Status != "new" {
		t.Errorf("order = %+v", order)
	}
	if gotBody["symbol"] != "AAPL" || gotBody["side"] != "buy" || gotBody["type"] != "market" {
		t.Errorf("request body = %+v, want market order for AAPL/buy", gotBody)
	}
	if gotBody["client_order_id"] != "client-1" {
		t.Errorf("client_order_id = %v, want client-1", gotBody["client_order_id"])
	}
}

func TestGetOrderStatus_ParsesFilledOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": "ord-1", "client_order_id": "client-1", "status": "filled", "filled_qty": "5", "filled_avg_price": "231.50"}`))
	}))
	defer srv.Close()

	c := New("key", "secret", srv.URL)
	order, err := c.GetOrderStatus(context.Background(), "client-1")
	if err != nil {
		t.Fatalf("GetOrderStatus: %v", err)
	}
	if order.Status != "filled" || order.FilledQty != 5 || order.FilledAvgPrice != 231.50 {
		t.Errorf("order = %+v", order)
	}
}

func TestCancelOrder_SendsDeleteByOrderID(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent) // Alpaca's real cancel response: 204, no body
	}))
	defer srv.Close()

	c := New("key", "secret", srv.URL)
	if err := c.CancelOrder(context.Background(), "ord-1"); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/v2/orders/ord-1" {
		t.Errorf("path = %q, want /v2/orders/ord-1 (cancel is by order_id, not client_order_id)", gotPath)
	}
}
