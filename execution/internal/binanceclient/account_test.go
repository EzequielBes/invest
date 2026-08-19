// execution/internal/binanceclient/account_test.go
package binanceclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAccount_ParsesBalanceAndFiltersZeroPositions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("signature") == "" || r.URL.Query().Get("timestamp") == "" {
			t.Errorf("request missing signature/timestamp: %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("X-MBX-APIKEY"); got != "test-key" {
			t.Errorf("X-MBX-APIKEY = %q, want test-key", got)
		}
		w.Write([]byte(`{
			"availableBalance": "1000.50",
			"positions": [
				{"symbol": "BTCUSDT", "positionAmt": "0.500"},
				{"symbol": "ETHUSDT", "positionAmt": "0.000"}
			]
		}`))
	}))
	defer server.Close()

	client := New("test-key", "test-secret", server.URL)
	account, err := client.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if account.AvailableBalance != 1000.50 {
		t.Errorf("AvailableBalance = %v, want 1000.50", account.AvailableBalance)
	}
	if len(account.Positions) != 1 {
		t.Fatalf("Positions = %+v, want exactly 1 (zero-quantity position filtered out)", account.Positions)
	}
	if account.Positions[0].Asset != "BTC" || account.Positions[0].Quantity != 0.5 {
		t.Errorf("Positions[0] = %+v, want {BTC 0.5}", account.Positions[0])
	}
}
