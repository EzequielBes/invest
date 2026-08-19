// analysis/internal/llm/openai_client_test.go
package llm

import (
	"strings"
	"testing"

	"github.com/openai/openai-go"
)

func TestNarrativeFromCompletion_ReturnsText(t *testing.T) {
	resp := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{Message: openai.ChatCompletionMessage{Content: "  market summary  "}},
		},
	}

	got, err := narrativeFromCompletion(resp)
	if err != nil {
		t.Fatalf("narrativeFromCompletion: %v", err)
	}
	if got != "market summary" {
		t.Fatalf("narrative = %q, want trimmed text", got)
	}
}

func TestNarrativeFromCompletion_RefusalIsFailure(t *testing.T) {
	resp := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{Message: openai.ChatCompletionMessage{Refusal: "can't help with that"}},
		},
	}

	_, err := narrativeFromCompletion(resp)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error = %v, want refusal error", err)
	}
}

func TestNarrativeFromCompletion_LengthIsFailure(t *testing.T) {
	resp := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{FinishReason: "length", Message: openai.ChatCompletionMessage{Content: ""}},
		},
	}

	_, err := narrativeFromCompletion(resp)
	if err == nil || !strings.Contains(err.Error(), "max_completion_tokens") {
		t.Fatalf("error = %v, want max_completion_tokens error", err)
	}
}

func TestNarrativeFromCompletion_RejectsMissingText(t *testing.T) {
	cases := map[string]*openai.ChatCompletion{
		"nil response": nil,
		"no choices":   {},
		"blank text":   {Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "  "}}}},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := narrativeFromCompletion(resp); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
