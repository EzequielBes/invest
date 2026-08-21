package api

import (
	"errors"
	"log"
	"net/http"

	"web-api/internal/storage"
)

func handleValidationRuns(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runs, err := store.RecentValidationRuns(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentValidationRuns: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load validation runs")
			return
		}
		writeJSON(w, http.StatusOK, runs)
	}
}

func handleValidationRunDetail(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		detail, err := store.ValidationRunDetail(r.Context(), r.PathValue("id"))
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "validation run not found")
			return
		}
		if err != nil {
			log.Printf("web-api: ValidationRunDetail: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load validation run")
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
