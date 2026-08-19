package llm

import (
	"context"
	"fmt"
	"os"
)

// NewClient returns whichever LLM provider is available, per
// ANTHROPIC_API_KEY/OPENAI_API_KEY: only one set uses that provider
// directly; both set returns a FallbackClient trying the configured
// primary first (LLM_PRIMARY_PROVIDER=anthropic|openai, default
// anthropic) and falling back to the other on a per-call failure;
// neither set is an error, never a silent no-LLM operation.
func NewClient() (Client, error) {
	hasAnthropic := os.Getenv("ANTHROPIC_API_KEY") != ""
	hasOpenAI := os.Getenv("OPENAI_API_KEY") != ""

	switch {
	case hasAnthropic && hasOpenAI:
		primary, secondary := resolvePrimary()
		return &FallbackClient{primary: primary, secondary: secondary}, nil
	case hasAnthropic:
		return NewAnthropicClient(), nil
	case hasOpenAI:
		return NewOpenAIClient(), nil
	default:
		return nil, fmt.Errorf("llm: no provider available (set ANTHROPIC_API_KEY and/or OPENAI_API_KEY)")
	}
}

func resolvePrimary() (primary, secondary Client) {
	anthropicClient := NewAnthropicClient()
	openAIClient := NewOpenAIClient()
	if os.Getenv("LLM_PRIMARY_PROVIDER") == "openai" {
		return openAIClient, anthropicClient
	}
	return anthropicClient, openAIClient
}

// FallbackClient tries primary first; if that call returns an error, it
// retries the same call against secondary and returns secondary's
// result (or error). No state is kept between calls — the next call
// always tries primary first again, even if this one fell back.
type FallbackClient struct {
	primary   Client
	secondary Client
}

func (c *FallbackClient) Decide(ctx context.Context, systemPrompt, userPrompt string) (Decision, error) {
	decision, err := c.primary.Decide(ctx, systemPrompt, userPrompt)
	if err == nil {
		return decision, nil
	}
	return c.secondary.Decide(ctx, systemPrompt, userPrompt)
}
