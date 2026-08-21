package agents

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type Evidence struct {
	AgentType string `json:"agent_type"`
	Citation  string `json:"citation"`
}

type CommitteeAssessment struct {
	Asset            string     `json:"asset"`
	Thesis           string     `json:"thesis"`
	Confidence       float64    `json:"confidence"`
	OpportunityScore float64    `json:"opportunity_score"`
	Narrative        string     `json:"narrative"`
	Evidence         []Evidence `json:"evidence"`
}

type CommitteeIndicators struct {
	Thesis           string     `json:"thesis"`
	Confidence       float64    `json:"confidence"`
	OpportunityScore float64    `json:"opportunity_score"`
	Evidence         []Evidence `json:"evidence"`
	Quality          any        `json:"quality,omitempty"`
}

type committeeResponse struct {
	Assessments []CommitteeAssessment `json:"assessments"`
}

// ParseCommitteeResponse is the boundary for untrusted structured LLM output.
func ParseCommitteeResponse(raw string, expectedAssets []string) ([]CommitteeAssessment, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	var response committeeResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &response); err != nil {
		return nil, fmt.Errorf("committee: decode response: %w", err)
	}
	if len(response.Assessments) != len(expectedAssets) {
		return nil, fmt.Errorf("committee: got %d assessments, want %d", len(response.Assessments), len(expectedAssets))
	}
	expected := make(map[string]struct{}, len(expectedAssets))
	for _, asset := range expectedAssets {
		expected[asset] = struct{}{}
	}
	seen := make(map[string]struct{}, len(response.Assessments))
	for _, assessment := range response.Assessments {
		if _, ok := expected[assessment.Asset]; !ok {
			return nil, fmt.Errorf("committee: unexpected asset %q", assessment.Asset)
		}
		if _, ok := seen[assessment.Asset]; ok {
			return nil, fmt.Errorf("committee: duplicate asset %q", assessment.Asset)
		}
		seen[assessment.Asset] = struct{}{}
		if assessment.Thesis != "bull" && assessment.Thesis != "bear" && assessment.Thesis != "neutro" {
			return nil, fmt.Errorf("committee: %s has invalid thesis %q", assessment.Asset, assessment.Thesis)
		}
		if !unitInterval(assessment.Confidence) || !unitInterval(assessment.OpportunityScore) {
			return nil, fmt.Errorf("committee: %s has score outside [0,1]", assessment.Asset)
		}
		if strings.TrimSpace(assessment.Narrative) == "" || len(assessment.Evidence) == 0 {
			return nil, fmt.Errorf("committee: %s is missing narrative or evidence", assessment.Asset)
		}
		for _, evidence := range assessment.Evidence {
			if !validEvidenceAgent(evidence.AgentType) || strings.TrimSpace(evidence.Citation) == "" {
				return nil, fmt.Errorf("committee: %s has invalid evidence", assessment.Asset)
			}
		}
	}
	return response.Assessments, nil
}

func EligibleAssets(assets []string, sources []CycleNarrative) []string {
	byAsset := make(map[string]map[string]bool, len(assets))
	for _, source := range sources {
		if source.Asset == "" || source.Narrative == "" {
			continue
		}
		if byAsset[source.Asset] == nil {
			byAsset[source.Asset] = make(map[string]bool)
		}
		byAsset[source.Asset][source.AgentType] = true
	}
	eligible := make([]string, 0, len(assets))
	for _, asset := range assets {
		got := byAsset[asset]
		if got["technical"] && got["derivatives"] && got["news"] {
			eligible = append(eligible, asset)
		}
	}
	return eligible
}

func unitInterval(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }

func validEvidenceAgent(agentType string) bool {
	return agentType == "technical" || agentType == "derivatives" || agentType == "news" || agentType == "macro" || agentType == "risk_context"
}
