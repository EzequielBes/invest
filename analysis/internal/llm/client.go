package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	model     = "claude-sonnet-5"
	maxTokens = 512
)

// Client narrates structured indicators into a short natural-language summary.
type Client interface {
	Summarize(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// AnthropicClient is the production implementation backed by Claude.
type AnthropicClient struct {
	client anthropic.Client
}

// NewAnthropicClient uses the SDK's default ANTHROPIC_API_KEY lookup.
func NewAnthropicClient() *AnthropicClient {
	return &AnthropicClient{client: anthropic.NewClient()}
}

func (c *AnthropicClient) Summarize(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm: summarize: %w", err)
	}
	return narrativeFromResponse(resp)
}

func narrativeFromResponse(resp *anthropic.Message) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("llm: summarize: empty response")
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", fmt.Errorf("llm: summarize: model refused the request")
	}
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			if narrative := strings.TrimSpace(text.Text); narrative != "" {
				return narrative, nil
			}
		}
	}
	return "", fmt.Errorf("llm: summarize: no text block in response")
}
