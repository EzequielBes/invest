package validation

import (
	"strings"
	"testing"
	"time"
)

func TestValidateSplits_AcceptsOrderedSplitsWithEmbargo(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	splits := []Split{{Kind: SplitTrain, Start: start, End: start.Add(24 * time.Hour), EmbargoMinutes: 30}, {Kind: SplitValidation, Start: start.Add(24*time.Hour + 30*time.Minute), End: start.Add(48*time.Hour + 30*time.Minute)}, {Kind: SplitHoldout, Start: start.Add(48*time.Hour + 30*time.Minute), End: start.Add(72*time.Hour + 30*time.Minute)}}
	if err := ValidateSplits(splits); err != nil {
		t.Fatalf("ValidateSplits: %v", err)
	}
}

func TestValidateSplits_RejectsOverlapAndMissingHoldout(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	overlapped := []Split{{Kind: SplitTrain, Start: start, End: start.Add(24 * time.Hour)}, {Kind: SplitValidation, Start: start.Add(23 * time.Hour), End: start.Add(48 * time.Hour)}, {Kind: SplitHoldout, Start: start.Add(48 * time.Hour), End: start.Add(72 * time.Hour)}}
	if err := ValidateSplits(overlapped); err == nil || !strings.Contains(err.Error(), "validation") {
		t.Fatalf("overlap error = %v, want validation ordering error", err)
	}
	if err := ValidateSplits(overlapped[:2]); err == nil {
		t.Fatal("missing holdout error = nil")
	}
}
