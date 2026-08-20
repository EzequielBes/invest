package api

import (
	"errors"
	"log"
	"net/http"

	"web-api/internal/storage"
)

func handleAnalysisRuns(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runs, err := store.RecentAnalysisRuns(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentAnalysisRuns: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load analysis runs")
			return
		}
		writeJSON(w, http.StatusOK, runs)
	}
}

func handleAnalysisRunDetail(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		detail, err := store.AnalysisRunDetail(r.Context(), r.PathValue("id"))
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "analysis run not found")
			return
		}
		if err != nil {
			log.Printf("web-api: AnalysisRunDetail: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load analysis run")
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
