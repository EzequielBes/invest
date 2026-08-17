// simulation/internal/engine/clock_test.go
package engine

import (
	"testing"
	"time"
)

func TestClock_AdvancesInFixedIncrementsAndStops(t *testing.T) {
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	c := NewClock(start, end, time.Hour)

	var got []time.Time
	for {
		open, close, ok := c.Next()
		if !ok {
			break
		}
		if close.Sub(open) != time.Hour {
			t.Fatalf("candle window = %v, want exactly 1h (open=%v close=%v)", close.Sub(open), open, close)
		}
		got = append(got, close)
	}

	want := []time.Time{start.Add(time.Hour), start.Add(2 * time.Hour), start.Add(3 * time.Hour)}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("step %d closeTime = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestApplyFee(t *testing.T) {
	got := applyFee(1000, 0.001)
	if got != 1 {
		t.Errorf("applyFee(1000, 0.001) = %v, want 1", got)
	}
}
