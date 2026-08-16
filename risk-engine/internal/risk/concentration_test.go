package risk

import "testing"

func TestCheckAssetConcentration_RejectsOverLimit(t *testing.T) {
	portfolio := PortfolioState{
		Cash: 5000,
		Positions: map[string]Position{
			"BTC": {Asset: "BTC", Quantity: 0.1, Value: 4000},
		},
	}
	proposed := ProposedOperation{Asset: "BTC", Side: SideBuy, Quantity: 0.05, Value: 2000}

	result := checkAssetConcentration(portfolio, proposed, 0.5)

	if result.Passed {
		t.Fatalf("expected rejection: BTC would be (4000+2000)/(5000+4000) = %.4f, limit 0.5", result.Measured)
	}
	if result.Rule != "asset_concentration" {
		t.Errorf("Rule = %q", result.Rule)
	}
}

func TestCheckAssetConcentration_AllowsUnderLimit(t *testing.T) {
	portfolio := PortfolioState{
		Cash: 8000,
		Positions: map[string]Position{
			"BTC": {Asset: "BTC", Quantity: 0.05, Value: 2000},
		},
	}
	proposed := ProposedOperation{Asset: "BTC", Side: SideBuy, Quantity: 0.01, Value: 500}

	result := checkAssetConcentration(portfolio, proposed, 0.5)

	if !result.Passed {
		t.Fatalf("expected approval: BTC would be (2000+500)/(8000+2000) = %.4f, limit 0.5", result.Measured)
	}
}

func TestCheckCryptoTotalConcentration_RejectsOverLimit(t *testing.T) {
	portfolio := PortfolioState{
		Cash: 1000,
		Positions: map[string]Position{
			"BTC": {Asset: "BTC", Value: 8000},
			"ETH": {Asset: "ETH", Value: 500},
		},
	}
	proposed := ProposedOperation{Asset: "ETH", Side: SideBuy, Quantity: 1, Value: 800}

	result := checkCryptoTotalConcentration(portfolio, proposed, 0.9)

	if result.Passed {
		t.Fatalf("expected rejection: crypto would be (8000+500+800)/(1000+8000+500) = %.4f, limit 0.9", result.Measured)
	}
}

func TestCheckMaxTradeValue(t *testing.T) {
	tooLarge := ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 1500}
	if r := checkMaxTradeValue(tooLarge, 1000); r.Passed {
		t.Fatalf("expected rejection for value 1500 > limit 1000")
	}

	fine := ProposedOperation{Asset: "BTC", Side: SideBuy, Value: 500}
	if r := checkMaxTradeValue(fine, 1000); !r.Passed {
		t.Fatalf("expected approval for value 500 <= limit 1000")
	}
}
