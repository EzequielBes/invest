// analysis/internal/llm/openai_client.go
package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
)

const openAIModel = "gpt-5"

// OpenAIClient is the OpenAI-backed implementation of Client — a second
// provider alongside AnthropicClient, selected by availability (see
// select.go).
type OpenAIClient struct {
	client openai.Client
}

// NewOpenAIClient reads its API key from OPENAI_API_KEY, per the SDK's
// default credential resolution.
func NewOpenAIClient() *OpenAIClient {
	return &OpenAIClient{client: openai.NewClient()}
}

func (c *OpenAIClient) Summarize(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               openAIModel,
		MaxCompletionTokens: openai.Int(maxTokens),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm: openai summarize: %w", err)
	}
	return narrativeFromCompletion(resp)
}

func narrativeFromCompletion(resp *openai.ChatCompletion) (string, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("llm: openai summarize: empty response")
	}
	message := resp.Choices[0].Message
	if message.Refusal != "" {
		return "", fmt.Errorf("llm: openai summarize: model refused the request: %s", message.Refusal)
	}
	if narrative := strings.TrimSpace(message.Content); narrative != "" {
		return narrative, nil
	}
	return "", fmt.Errorf("llm: openai summarize: no content in response")
}
