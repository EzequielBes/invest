package agents

import (
	"strings"
	"testing"
)

func TestParseCommitteeResponse_ValidatesAssessments(t *testing.T) {
	raw := `{"assessments":[{"asset":"BTC","thesis":"bull","confidence":0.8,"opportunity_score":0.7,"narrative":"Contexto relativo favorável.","evidence":[{"agent_type":"technical","citation":"tendência positiva"}]},{"asset":"ETH","thesis":"neutro","confidence":0.5,"opportunity_score":0.5,"narrative":"Sinais mistos.","evidence":[{"agent_type":"news","citation":"noticiário misto"}]}]}`
	got, err := ParseCommitteeResponse(raw, []string{"BTC", "ETH"})
	if err != nil {
		t.Fatalf("ParseCommitteeResponse: %v", err)
	}
	if len(got) != 2 || got[0].Asset != "BTC" {
		t.Fatalf("assessments = %#v", got)
	}
}

func TestParseCommitteeResponse_RejectsUntrustedOutput(t *testing.T) {
	cases := []string{
		`{"assessments":[{"asset":"BTC","thesis":"bull","confidence":1.1,"opportunity_score":0.7,"narrative":"x","evidence":[{"agent_type":"technical","citation":"x"}]}]}`,
		`{"assessments":[{"asset":"BTC","thesis":"bull","confidence":0.5,"opportunity_score":0.7,"narrative":"x","evidence":[]}]}`,
		`{"assessments":[{"asset":"BTC","thesis":"bull","confidence":0.5,"opportunity_score":0.7,"narrative":"x","evidence":[{"agent_type":"technical","citation":"x"}]},{"asset":"BTC","thesis":"bear","confidence":0.5,"opportunity_score":0.2,"narrative":"x","evidence":[{"agent_type":"news","citation":"x"}]}]}`,
	}
	for _, raw := range cases {
		t.Run("invalid", func(t *testing.T) {
			if _, err := ParseCommitteeResponse(raw, []string{"BTC"}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if _, err := ParseCommitteeResponse("not json", []string{"BTC"}); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error = %v, want decode error", err)
	}
}
