package exchange

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStringFloat_UnmarshalsQuotedAndRaw(t *testing.T) {
	var quoted StringFloat
	if err := json.Unmarshal([]byte(`"63043.40"`), &quoted); err != nil {
		t.Fatalf("quoted: %v", err)
	}
	if quoted != 63043.40 {
		t.Errorf("quoted = %v, want 63043.40", quoted)
	}

	var raw StringFloat
	if err := json.Unmarshal([]byte(`63043.40`), &raw); err != nil {
		t.Fatalf("raw: %v", err)
	}
	if raw != 63043.40 {
		t.Errorf("raw = %v, want 63043.40", raw)
	}
}

func TestStringInt64_TimeConvertsMillis(t *testing.T) {
	var quoted StringInt64
	if err := json.Unmarshal([]byte(`"1786809600000"`), &quoted); err != nil {
		t.Fatalf("quoted: %v", err)
	}
	want := time.UnixMilli(1786809600000).UTC()
	if got := quoted.Time().UTC(); !got.Equal(want) {
		t.Errorf("Time() = %v, want %v", got, want)
	}

	var raw StringInt64
	if err := json.Unmarshal([]byte(`1786809600000`), &raw); err != nil {
		t.Fatalf("raw: %v", err)
	}
	if raw.Time().UTC() != want {
		t.Errorf("Time() = %v, want %v", raw.Time().UTC(), want)
	}
}
