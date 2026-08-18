// strategist/internal/llm/client_test.go
package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func toolUseBlock(t *testing.T, name string, input any) anthropic.ContentBlockUnion {
	t.Helper()
	inputRaw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	raw, err := json.Marshal(map[string]any{
		"type": "tool_use", "id": "toolu_test", "name": name, "input": json.RawMessage(inputRaw),
	})
	if err != nil {
		t.Fatalf("marshal block: %v", err)
	}
	var block anthropic.ContentBlockUnion
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("unmarshal block: %v", err)
	}
	return block
}

func TestDecisionFromResponse_ParsesToolUse(t *testing.T) {
	resp := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			toolUseBlock(t, toolName, map[string]any{
				"side": "buy", "confidence": 0.7, "sizing_pct": 0.1, "rationale": "uptrend",
			}),
		},
	}

	got, err := decisionFromResponse(resp)
	if err != nil {
		t.Fatalf("decisionFromResponse: %v", err)
	}
	if got.Side != "buy" || got.Confidence != 0.7 || got.SizingPct != 0.1 || got.Rationale != "uptrend" {
		t.Fatalf("decision = %+v, want the seeded fields", got)
	}
}

func TestDecisionFromResponse_RefusalIsFailure(t *testing.T) {
	resp := &anthropic.Message{StopReason: anthropic.StopReasonRefusal}

	_, err := decisionFromResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error = %v, want refusal error", err)
	}
}

func TestDecisionFromResponse_RejectsMissingOrInvalid(t *testing.T) {
	cases := map[string]*anthropic.Message{
		"nil response": nil,
		"no blocks":    {},
		"wrong tool name": {
			Content: []anthropic.ContentBlockUnion{toolUseBlock(t, "some_other_tool", map[string]any{
				"side": "buy", "confidence": 0.5, "sizing_pct": 0.1, "rationale": "x",
			})},
		},
		"invalid side": {
			Content: []anthropic.ContentBlockUnion{toolUseBlock(t, toolName, map[string]any{
				"side": "yolo", "confidence": 0.5, "sizing_pct": 0.1, "rationale": "x",
			})},
		},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decisionFromResponse(resp); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
