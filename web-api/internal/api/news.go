package api

import (
	"log"
	"net/http"
)

func handleNews(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		news, err := store.RecentNews(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentNews: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load news")
			return
		}
		writeJSON(w, http.StatusOK, news)
	}
}
