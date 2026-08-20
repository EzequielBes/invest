package api

import (
	"log"
	"net/http"
)

func handleRiskState(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := store.LiveRiskState(r.Context())
		if err != nil {
			log.Printf("web-api: LiveRiskState: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load risk state")
			return
		}
		writeJSON(w, http.StatusOK, state)
	}
}
