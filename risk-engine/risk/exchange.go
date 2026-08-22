package risk

import (
	"os"
	"strings"
)

// assetExchange maps each configured asset symbol to the exchange its
// market data lives in. Loaded once from the same env vars market-data's
// collectors use (ASSETS for crypto/binance, ALPACA_ASSETS for stocks),
// so adding an asset to trade means editing one env var in one place,
// not a database migration.
var assetExchange = buildAssetExchangeMap()

func buildAssetExchangeMap() map[string]string {
	m := make(map[string]string)
	for _, asset := range splitEnvList("ASSETS") {
		m[asset] = "binance"
	}
	for _, asset := range splitEnvList("ALPACA_ASSETS") {
		m[asset] = "alpaca"
	}
	return m
}

func splitEnvList(name string) []string {
	raw := os.Getenv(name)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ExchangeFor returns which exchange holds asset's market data. An asset
// absent from both ASSETS and ALPACA_ASSETS falls back to "binance" —
// this system's crypto-only history means an unrecognized symbol behaves
// exactly as it did before per-asset routing existed, never breaking a
// currently-traded asset due to an incomplete env var.
func ExchangeFor(asset string) string {
	if exchange, ok := assetExchange[asset]; ok {
		return exchange
	}
	return "binance"
}

// IsCrypto reports whether asset trades on a crypto exchange, for
// portfolio exposure checks that must count only crypto positions. Stays
// consistent with ExchangeFor's fallback: an asset not routed to alpaca
// is crypto by default.
func IsCrypto(asset string) bool {
	return ExchangeFor(asset) != "alpaca"
}
