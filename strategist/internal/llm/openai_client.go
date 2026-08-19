// strategist/internal/llm/openai_client.go
package llm

import (
	"context"
	"encoding/json"
	"fmt"

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

var openAIDecisionTool = openai.ChatCompletionToolParam{
	Function: openai.FunctionDefinitionParam{
		Name:        toolName,
		Description: openai.String("Record the trading decision for this asset."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"side": map[string]any{
					"type":        "string",
					"enum":        []string{"buy", "sell", "hold"},
					"description": "The proposed action for this asset.",
				},
				"confidence": map[string]any{
					"type":        "number",
					"minimum":     0,
					"maximum":     1,
					"description": "How confident the decision is, from 0 (a guess) to 1 (very confident).",
				},
				"sizing_pct": map[string]any{
					"type":        "number",
					"minimum":     0,
					"maximum":     1,
					"description": "Fraction of total portfolio value to allocate to this trade. Ignored when side is hold.",
				},
				"rationale": map[string]any{
					"type":        "string",
					"description": "A short (2-4 sentence) explanation for the decision.",
				},
			},
			"required": []string{"side", "confidence", "sizing_pct", "rationale"},
		},
	},
}

func (c *OpenAIClient) Decide(ctx context.Context, systemPrompt, userPrompt string) (Decision, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               openAIModel,
		MaxCompletionTokens: openai.Int(maxTokens),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
		Tools: []openai.ChatCompletionToolParam{openAIDecisionTool},
		ToolChoice: openai.ChatCompletionToolChoiceOptionParamOfChatCompletionNamedToolChoice(
			openai.ChatCompletionNamedToolChoiceFunctionParam{Name: toolName},
		),
	})
	if err != nil {
		return Decision{}, fmt.Errorf("llm: openai decide: %w", err)
	}
	return decisionFromCompletion(resp)
}

func decisionFromCompletion(resp *openai.ChatCompletion) (Decision, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return Decision{}, fmt.Errorf("llm: openai decide: empty response")
	}
	message := resp.Choices[0].Message
	if message.Refusal != "" {
		return Decision{}, fmt.Errorf("llm: openai decide: model refused the request: %s", message.Refusal)
	}
	for _, call := range message.ToolCalls {
		if call.Function.Name != toolName {
			continue
		}
		var d Decision
		if err := json.Unmarshal([]byte(call.Function.Arguments), &d); err != nil {
			return Decision{}, fmt.Errorf("llm: openai decide: unmarshal tool arguments: %w", err)
		}
		if d.Side != "buy" && d.Side != "sell" && d.Side != "hold" {
			return Decision{}, fmt.Errorf("llm: openai decide: unexpected side %q", d.Side)
		}
		return d, nil
	}
	return Decision{}, fmt.Errorf("llm: openai decide: no %s tool call in response", toolName)
}
