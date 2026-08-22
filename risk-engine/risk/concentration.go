package risk

import "fmt"

// checkAssetConcentration rejects an operation that would push a single
// asset's share of total portfolio value (cash + all positions) above
// maxPct. Buying/selling is assumed to move value 1:1 between cash and the
// position (no slippage/fee modeling in this phase), so total portfolio
// value is unchanged by the operation itself.
func checkAssetConcentration(portfolio PortfolioState, proposed ProposedOperation, maxPct float64) RuleResult {
	total := portfolio.Cash
	for _, p := range portfolio.Positions {
		total += p.Value
	}
	if total <= 0 {
		return RuleResult{Rule: "asset_concentration", Passed: true, Limit: maxPct, Detail: "no portfolio value to evaluate"}
	}

	assetAfter := portfolio.Positions[proposed.Asset].Value
	if proposed.Side == SideBuy {
		assetAfter += proposed.Value
	} else {
		assetAfter -= proposed.Value
		if assetAfter < 0 {
			assetAfter = 0
		}
	}

	pct := assetAfter / total
	return RuleResult{
		Rule: "asset_concentration", Passed: pct <= maxPct,
		Measured: pct, Limit: maxPct,
		Detail: fmt.Sprintf("%s would be %.1f%% of portfolio after this operation", proposed.Asset, pct*100),
	}
}

// checkCryptoTotalConcentration rejects an operation that would push total
// crypto exposure above maxPct of total portfolio value. Non-crypto
// positions (e.g. stocks) count toward total but not toward crypto.
func checkCryptoTotalConcentration(portfolio PortfolioState, proposed ProposedOperation, maxPct float64) RuleResult {
	total := portfolio.Cash
	var crypto float64
	for asset, p := range portfolio.Positions {
		total += p.Value
		if IsCrypto(asset) {
			crypto += p.Value
		}
	}
	if total <= 0 {
		return RuleResult{Rule: "crypto_total_concentration", Passed: true, Limit: maxPct, Detail: "no portfolio value to evaluate"}
	}

	if IsCrypto(proposed.Asset) {
		if proposed.Side == SideBuy {
			crypto += proposed.Value
		} else {
			crypto -= proposed.Value
			if crypto < 0 {
				crypto = 0
			}
		}
	}

	pct := crypto / total
	return RuleResult{
		Rule: "crypto_total_concentration", Passed: pct <= maxPct,
		Measured: pct, Limit: maxPct,
		Detail: fmt.Sprintf("total crypto exposure would be %.1f%% of portfolio after this operation", pct*100),
	}
}

// checkMaxTradeValue rejects a single operation larger than maxValue.
func checkMaxTradeValue(proposed ProposedOperation, maxValue float64) RuleResult {
	return RuleResult{
		Rule: "max_trade_value", Passed: proposed.Value <= maxValue,
		Measured: proposed.Value, Limit: maxValue,
		Detail: fmt.Sprintf("operation value %.2f", proposed.Value),
	}
}
