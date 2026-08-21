package metrics

import "testing"

func TestSlippageBps_NormalizesBuyAndSell(t *testing.T) {
	buy, err := SlippageBps("buy", 100, 101)
	if err != nil {
		t.Fatalf("SlippageBps buy: %v", err)
	}
	if buy != 100 {
		t.Errorf("buy slippage = %v, want 100", buy)
	}

	sell, err := SlippageBps("SELL", 100, 99)
	if err != nil {
		t.Fatalf("SlippageBps sell: %v", err)
	}
	if sell != 100 {
		t.Errorf("sell slippage = %v, want 100", sell)
	}
}

func TestSlippageBps_RejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		side      string
		requested float64
		filled    float64
	}{
		{side: "hold", requested: 100, filled: 100},
		{side: "buy", requested: 0, filled: 100},
		{side: "sell", requested: 100, filled: -1},
	} {
		if _, err := SlippageBps(test.side, test.requested, test.filled); err == nil {
			t.Errorf("SlippageBps(%q, %v, %v) returned nil error", test.side, test.requested, test.filled)
		}
	}
}

func TestTurnoverPct(t *testing.T) {
	turnover, err := TurnoverPct([]Trade{
		{Quantity: 2, Price: 100},
		{Quantity: -1, Price: 50},
	}, 500)
	if err != nil {
		t.Fatalf("TurnoverPct: %v", err)
	}
	if turnover != 50 {
		t.Errorf("TurnoverPct = %v, want 50", turnover)
	}
}

func TestTurnoverPct_RejectsInvalidEquity(t *testing.T) {
	if _, err := TurnoverPct(nil, 0); err == nil {
		t.Fatal("TurnoverPct accepted zero average equity")
	}
}
