package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"execution/paperstore"
)

type automationControlsResponse struct {
	PaperEnabled   bool   `json:"paper_enabled"`
	TestnetEnabled bool   `json:"testnet_enabled"`
	ActiveAgent    string `json:"active_agent"`
}

func handleAutomationControls(paper paperStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controls, err := paper.GetAutomationControls(r.Context())
		if err != nil {
			log.Printf("web-api: GetAutomationControls: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to load automation controls")
			return
		}
		writeJSON(w, http.StatusOK, automationControlsResponse{
			PaperEnabled: controls.Enabled, TestnetEnabled: controls.TestnetEnabled, ActiveAgent: controls.ActiveAgent,
		})
	}
}

func handlePatchAutomationControls(paper paperStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		patch, ok := parseAutomationPatch(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid automation controls")
			return
		}
		controls, err := paper.PatchAutomationControls(r.Context(), patch)
		if err != nil {
			log.Printf("web-api: PatchAutomationControls: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to update automation controls")
			return
		}
		writeJSON(w, http.StatusOK, automationControlsResponse{
			PaperEnabled: controls.Enabled, TestnetEnabled: controls.TestnetEnabled, ActiveAgent: controls.ActiveAgent,
		})
	}
}

func parseAutomationPatch(r *http.Request) (paperstore.AutomationPatch, bool) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&fields); err != nil || len(fields) == 0 || decoder.Decode(&struct{}{}) != io.EOF {
		return paperstore.AutomationPatch{}, false
	}

	var patch paperstore.AutomationPatch
	for name, raw := range fields {
		switch name {
		case "paper_enabled":
			var value bool
			if json.Unmarshal(raw, &value) != nil || string(raw) == "null" {
				return paperstore.AutomationPatch{}, false
			}
			patch.Enabled = &value
		case "testnet_enabled":
			var value bool
			if json.Unmarshal(raw, &value) != nil || string(raw) == "null" {
				return paperstore.AutomationPatch{}, false
			}
			patch.TestnetEnabled = &value
		case "active_agent":
			var value string
			if json.Unmarshal(raw, &value) != nil || (value != "claude_code" && value != "codex") {
				return paperstore.AutomationPatch{}, false
			}
			patch.ActiveAgent = &value
		default:
			return paperstore.AutomationPatch{}, false
		}
	}
	return patch, true
}
