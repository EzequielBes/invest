package api

import (
	"net/http"
	"os"
)

// configStatus reports only WHETHER each external integration's
// credentials are configured — never the value itself. This deliberately
// never accepts or stores a secret via the API; credentials remain
// environment-variable-only, same security posture as every other
// module in this repo.
type configStatus struct {
	BinanceConfigured   bool `json:"binance_configured"`
	AnthropicConfigured bool `json:"anthropic_configured"`
	OpenAIConfigured    bool `json:"openai_configured"`
}

func handleConfigStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, configStatus{
			BinanceConfigured:   os.Getenv("BINANCE_API_KEY") != "" && os.Getenv("BINANCE_API_SECRET") != "",
			AnthropicConfigured: os.Getenv("ANTHROPIC_API_KEY") != "",
			OpenAIConfigured:    os.Getenv("OPENAI_API_KEY") != "",
		})
	}
}
