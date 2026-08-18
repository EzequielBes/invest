package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestNarrativeFromResponse_ReturnsText(t *testing.T) {
	resp := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{textBlock(t, "  market summary  ")},
	}

	got, err := narrativeFromResponse(resp)
	if err != nil {
		t.Fatalf("narrativeFromResponse: %v", err)
	}
	if got != "market summary" {
		t.Fatalf("narrative = %q, want trimmed text", got)
	}
}

func TestNarrativeFromResponse_RefusalIsFailure(t *testing.T) {
	resp := &anthropic.Message{
		StopReason: anthropic.StopReasonRefusal,
		Content:    []anthropic.ContentBlockUnion{textBlock(t, "refusal explanation")},
	}

	_, err := narrativeFromResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error = %v, want refusal error", err)
	}
}

func TestNarrativeFromResponse_RejectsMissingText(t *testing.T) {
	for name, resp := range map[string]*anthropic.Message{
		"nil response": nil,
		"no blocks":    {},
		"blank text":   {Content: []anthropic.ContentBlockUnion{textBlock(t, "  ")}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := narrativeFromResponse(resp); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func textBlock(t *testing.T, text string) anthropic.ContentBlockUnion {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"type": "text", "text": text})
	if err != nil {
		t.Fatalf("marshal text block: %v", err)
	}
	var block anthropic.ContentBlockUnion
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("unmarshal text block: %v", err)
	}
	return block
}
