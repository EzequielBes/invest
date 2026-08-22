package config

import (
	"fmt"
	"os"
	"strings"
)

// defaultAssets is a starting curated list by liquidity/market cap. Adjust
// via the ASSETS env var without touching code.
var defaultAssets = []string{
	"BTC", "ETH", "SOL", "BNB", "XRP", "DOGE", "ADA", "AVAX", "LINK", "TON",
	"DOT", "MATIC", "LTC", "BCH", "NEAR", "UNI", "ATOM", "ETC", "APT", "ARB",
}

type Config struct {
	DatabaseURL string
	Assets      []string
	StockAssets []string
	FredAPIKey  string
}

// parseAssetList splits a comma-separated env var into a trimmed,
// non-empty asset list.
func parseAssetList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	assets := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			assets = append(assets, p)
		}
	}
	return assets
}

func Load() (Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	assets := append([]string(nil), defaultAssets...)
	if parsed := parseAssetList(os.Getenv("ASSETS")); parsed != nil {
		assets = parsed
	}

	return Config{
		DatabaseURL: dbURL,
		Assets:      assets,
		StockAssets: parseAssetList(os.Getenv("ALPACA_ASSETS")),
		FredAPIKey:  os.Getenv("FRED_API_KEY"),
	}, nil
}
