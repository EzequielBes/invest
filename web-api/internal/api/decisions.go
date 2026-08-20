package api

import (
	"log"
	"net/http"
)

func handleDecisions(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decisions, err := store.RecentDecisions(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentDecisions: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load decisions")
			return
		}
		writeJSON(w, http.StatusOK, decisions)
	}
}
