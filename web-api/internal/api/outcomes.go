package api

import (
	"log"
	"net/http"
)

func handleIntentOutcomes(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		outcomes, err := store.RecentIntentOutcomes(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentIntentOutcomes: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load intent outcomes")
			return
		}
		writeJSON(w, http.StatusOK, outcomes)
	}
}
