package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	riskstorage "risk-engine/storage"

	"execution/paperstore"

	"web-api/internal/storage"
)

const (
	defaultLimit = 50
	maxLimit     = 500
)

type dataStore interface {
	RecentDecisions(context.Context, int) ([]storage.Decision, error)
	RecentPaperDecisions(context.Context, int) ([]storage.Decision, error)
	RecentIntentOutcomes(context.Context, int) ([]storage.IntentOutcome, error)
	LiveRiskState(context.Context) (storage.RiskStateResponse, error)
	RecentAnalysisRuns(context.Context, int) ([]storage.AnalysisRun, error)
	AnalysisRunDetail(context.Context, string) (storage.AnalysisRunDetail, error)
	RecentBacktests(context.Context, int) ([]storage.BacktestRun, error)
	BacktestDetail(context.Context, string) (storage.BacktestDetail, error)
	RecentValidationRuns(context.Context, int) ([]storage.ValidationRun, error)
	ValidationRunDetail(context.Context, string) (storage.ValidationRunDetail, error)
	RecentEquitySnapshots(context.Context, int) ([]storage.EquityPoint, error)
	RecentNews(context.Context, int) ([]storage.NewsItem, error)
	LatestPrice(ctx context.Context, exchange, symbol, timeframe string) (price float64, found bool, err error)
}

// paperStore is the subset of *execution/paperstore.Store the simulation
// endpoints call — narrowed so tests can fake it.
type paperStore interface {
	Enabled(context.Context) (bool, error)
	SetEnabled(context.Context, bool) error
	GetAutomationControls(context.Context) (paperstore.AutomationControls, error)
	PatchAutomationControls(context.Context, paperstore.AutomationPatch) (paperstore.AutomationControls, error)
	Portfolio(context.Context) (cash float64, positions map[string]float64, err error)
	RecentFills(context.Context, int) ([]paperstore.Fill, error)
}

// NewServer serves the API — read-only except for POST /api/backtests
// (triggers a real backtest run) and the simulation endpoints (toggle +
// read the paper/live-validation portfolio) — and, when configured, the
// built frontend files. dsn/riskStore back the backtest-trigger endpoint
// (see simulate.go); every other handler only ever touches store/paper.
func NewServer(store dataStore, dsn string, riskStore *riskstorage.Store, paper paperStore, frontendDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/decisions", handleDecisions(store))
	mux.HandleFunc("GET /api/risk-state", handleRiskState(store))
	mux.HandleFunc("GET /api/analysis-runs", handleAnalysisRuns(store))
	mux.HandleFunc("GET /api/analysis-runs/{id}", handleAnalysisRunDetail(store))
	mux.HandleFunc("GET /api/backtests", handleBacktests(store))
	mux.HandleFunc("GET /api/backtests/{id}", handleBacktestDetail(store))
	mux.HandleFunc("GET /api/validation-runs", handleValidationRuns(store))
	mux.HandleFunc("GET /api/validation-runs/{id}", handleValidationRunDetail(store))
	mux.HandleFunc("POST /api/backtests", handleTriggerBacktest(dsn, riskStore))
	mux.HandleFunc("GET /api/equity-snapshots", handleEquitySnapshots(store))
	mux.HandleFunc("GET /api/news", handleNews(store))
	mux.HandleFunc("GET /api/config-status", handleConfigStatus())
	mux.HandleFunc("GET /api/simulation/status", handleSimulationStatus(paper))
	mux.HandleFunc("POST /api/simulation/toggle", handleSimulationToggle(paper))
	mux.HandleFunc("GET /api/automation-controls", handleAutomationControls(paper))
	mux.HandleFunc("PATCH /api/automation-controls", handlePatchAutomationControls(paper))
	mux.HandleFunc("GET /api/paper-decisions", handlePaperDecisions(store))
	mux.HandleFunc("GET /api/intent-outcomes", handleIntentOutcomes(store))
	if frontendDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(frontendDir)))
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("web-api: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func parseLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}
