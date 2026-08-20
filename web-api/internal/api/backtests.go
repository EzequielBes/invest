package api

import (
	"errors"
	"log"
	"net/http"

	"web-api/internal/storage"
)

func handleBacktests(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runs, err := store.RecentBacktests(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentBacktests: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load backtests")
			return
		}
		writeJSON(w, http.StatusOK, runs)
	}
}

func handleBacktestDetail(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		detail, err := store.BacktestDetail(r.Context(), r.PathValue("id"))
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "backtest not found")
			return
		}
		if err != nil {
			log.Printf("web-api: BacktestDetail: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load backtest")
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
