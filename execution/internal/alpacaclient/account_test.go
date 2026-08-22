package alpacaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAccount_ParsesCashAndPositions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/account", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"cash": "9000.50", "buying_power": "9000.50"}`))
	})
	mux.HandleFunc("/v2/positions", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"symbol": "AAPL", "qty": "5"}, {"symbol": "MSFT", "qty": "2.5"}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New("key", "secret", srv.URL)
	account, err := c.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if account.Cash != 9000.50 {
		t.Errorf("Cash = %v, want 9000.50", account.Cash)
	}
	if len(account.Positions) != 2 {
		t.Fatalf("len(Positions) = %d, want 2", len(account.Positions))
	}
	if account.Positions[0].Asset != "AAPL" || account.Positions[0].Quantity != 5 {
		t.Errorf("Positions[0] = %+v, want AAPL/5", account.Positions[0])
	}
	if account.Positions[1].Asset != "MSFT" || account.Positions[1].Quantity != 2.5 {
		t.Errorf("Positions[1] = %+v, want MSFT/2.5", account.Positions[1])
	}
}
