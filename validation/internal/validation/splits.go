package validation

import (
	"fmt"
	"sort"
	"time"
)

const (
	SplitTrain      = "train"
	SplitValidation = "validation"
	SplitHoldout    = "holdout"
)

type Split struct {
	Kind           string    `json:"kind"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	EmbargoMinutes int       `json:"embargo_minutes"`
}

func ValidateSplits(splits []Split) error {
	if len(splits) != 3 {
		return fmt.Errorf("exactly train, validation, and holdout splits are required")
	}

	byKind := make(map[string]Split, len(splits))
	for _, split := range splits {
		if split.Kind != SplitTrain && split.Kind != SplitValidation && split.Kind != SplitHoldout {
			return fmt.Errorf("unknown split kind %q", split.Kind)
		}
		if _, exists := byKind[split.Kind]; exists {
			return fmt.Errorf("duplicate %s split", split.Kind)
		}
		if split.Start.IsZero() || split.End.IsZero() || !split.Start.Before(split.End) {
			return fmt.Errorf("%s split must have a non-empty time range", split.Kind)
		}
		if split.EmbargoMinutes < 0 {
			return fmt.Errorf("%s split has negative embargo", split.Kind)
		}
		byKind[split.Kind] = split
	}

	ordered := []Split{byKind[SplitTrain], byKind[SplitValidation], byKind[SplitHoldout]}
	for i := 1; i < len(ordered); i++ {
		previous := ordered[i-1]
		current := ordered[i]
		minimumStart := previous.End.Add(time.Duration(previous.EmbargoMinutes) * time.Minute)
		if current.Start.Before(minimumStart) {
			return fmt.Errorf("%s must start after %s and its embargo", current.Kind, previous.Kind)
		}
	}
	return nil
}

func SortSplits(splits []Split) []Split {
	sorted := append([]Split(nil), splits...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	return sorted
}
