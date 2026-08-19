// strategist/internal/llm/openai_client_test.go
package llm

import (
	"strings"
	"testing"

	"github.com/openai/openai-go"
)

func toolCallCompletion(name, argumentsJSON string) *openai.ChatCompletion {
	return &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					ToolCalls: []openai.ChatCompletionMessageToolCall{
						{Function: openai.ChatCompletionMessageToolCallFunction{Name: name, Arguments: argumentsJSON}},
					},
				},
			},
		},
	}
}

func TestDecisionFromCompletion_ParsesToolCall(t *testing.T) {
	resp := toolCallCompletion(toolName, `{"side":"buy","confidence":0.7,"sizing_pct":0.1,"rationale":"uptrend"}`)

	got, err := decisionFromCompletion(resp)
	if err != nil {
		t.Fatalf("decisionFromCompletion: %v", err)
	}
	if got.Side != "buy" || got.Confidence != 0.7 || got.SizingPct != 0.1 || got.Rationale != "uptrend" {
		t.Fatalf("decision = %+v, want the seeded fields", got)
	}
}

func TestDecisionFromCompletion_RefusalIsFailure(t *testing.T) {
	resp := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Refusal: "can't help with that"}}},
	}

	_, err := decisionFromCompletion(resp)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error = %v, want refusal error", err)
	}
}

func TestDecisionFromCompletion_RejectsMissingOrInvalid(t *testing.T) {
	cases := map[string]*openai.ChatCompletion{
		"nil response":    nil,
		"no choices":      {},
		"wrong tool name": toolCallCompletion("some_other_tool", `{"side":"buy","confidence":0.5,"sizing_pct":0.1,"rationale":"x"}`),
		"invalid side":    toolCallCompletion(toolName, `{"side":"yolo","confidence":0.5,"sizing_pct":0.1,"rationale":"x"}`),
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decisionFromCompletion(resp); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
