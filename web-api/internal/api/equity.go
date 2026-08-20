package api

import (
	"log"
	"net/http"
)

func handleEquitySnapshots(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshots, err := store.RecentEquitySnapshots(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentEquitySnapshots: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load equity snapshots")
			return
		}
		writeJSON(w, http.StatusOK, snapshots)
	}
}
