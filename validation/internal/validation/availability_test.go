package validation

import (
	"testing"
	"time"
)

func TestValidateAvailability_FutureInputIsInvalidFinding(t *testing.T) {
	decisionAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	findings := ValidateAvailability(decisionAt, []time.Time{decisionAt.Add(-time.Minute), decisionAt.Add(time.Minute)})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].Rule != "future_data" || findings[0].Severity != "invalid" {
		t.Errorf("finding = %+v, want future_data/invalid", findings[0])
	}
}

func TestValidateAvailability_AllowsDecisionTimeInput(t *testing.T) {
	decisionAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if findings := ValidateAvailability(decisionAt, []time.Time{decisionAt}); len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
	}
}
