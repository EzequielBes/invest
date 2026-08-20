package api

import (
	"encoding/json"
	"log"
	"net/http"

	"execution/paperstore"
)

// simulationStatus is what both GET /api/simulation/status and POST
// /api/simulation/toggle return — the same shape, since a toggle
// response is just the status re-read after the write.
type simulationStatus struct {
	Enabled   bool               `json:"enabled"`
	Cash      float64            `json:"cash"`
	Positions map[string]float64 `json:"positions"`
	Fills     []paperstore.Fill  `json:"fills"`
}

func handleSimulationStatus(paper paperStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := readSimulationStatus(r, paper)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load simulation status")
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func handleSimulationToggle(paper paperStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := paper.SetEnabled(r.Context(), body.Enabled); err != nil {
			log.Printf("web-api: SetEnabled: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update simulation status")
			return
		}
		status, err := readSimulationStatus(r, paper)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load simulation status")
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func readSimulationStatus(r *http.Request, paper paperStore) (simulationStatus, error) {
	enabled, err := paper.Enabled(r.Context())
	if err != nil {
		log.Printf("web-api: Enabled: %v", err)
		return simulationStatus{}, err
	}
	cash, positions, err := paper.Portfolio(r.Context())
	if err != nil {
		log.Printf("web-api: Portfolio: %v", err)
		return simulationStatus{}, err
	}
	fills, err := paper.RecentFills(r.Context(), defaultLimit)
	if err != nil {
		log.Printf("web-api: RecentFills: %v", err)
		return simulationStatus{}, err
	}
	return simulationStatus{Enabled: enabled, Cash: cash, Positions: positions, Fills: fills}, nil
}

func handlePaperDecisions(store dataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decisions, err := store.RecentPaperDecisions(r.Context(), parseLimit(r))
		if err != nil {
			log.Printf("web-api: RecentPaperDecisions: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load paper decisions")
			return
		}
		writeJSON(w, http.StatusOK, decisions)
	}
}
