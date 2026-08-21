package validation

import (
	"fmt"
	"time"
)

type Finding struct {
	Severity string
	Rule     string
	Message  string
	Evidence map[string]any
}

func ValidateAvailability(decisionAt time.Time, inputTimes []time.Time) []Finding {
	findings := make([]Finding, 0)
	for _, inputAt := range inputTimes {
		if inputAt.After(decisionAt) {
			findings = append(findings, Finding{
				Severity: "invalid",
				Rule:     "future_data",
				Message:  "input was not available at the decision timestamp",
				Evidence: map[string]any{
					"decision_at": decisionAt.UTC().Format(time.RFC3339Nano),
					"input_at":    inputAt.UTC().Format(time.RFC3339Nano),
				},
			})
		}
	}
	return findings
}

func ValidateDecisionTimestamp(decisionAt time.Time) error {
	if decisionAt.IsZero() {
		return fmt.Errorf("decision timestamp is required")
	}
	return nil
}
