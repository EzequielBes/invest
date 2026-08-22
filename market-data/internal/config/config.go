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
	FredAPIKey  string
}

func Load() (Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	assets := append([]string(nil), defaultAssets...)
	if raw := os.Getenv("ASSETS"); raw != "" {
		parts := strings.Split(raw, ",")
		assets = make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				assets = append(assets, p)
			}
		}
	}

	return Config{DatabaseURL: dbURL, Assets: assets, FredAPIKey: os.Getenv("FRED_API_KEY")}, nil
}
